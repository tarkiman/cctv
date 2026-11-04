// Package recorder records each camera with `ffmpeg -c copy` into MPEG-TS segments, then
// remuxes finished segments into faststart MP4 files and indexes them.
//
// MPEG-TS is used while writing because a partially written TS file stays readable if
// the process dies; MP4 is used for storage because browsers seek it far better.
package recorder

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/supervisor"
)

// Segment file names are UTC timestamps written by ffmpeg's -strftime.
const nameLayout = "20060102T150405"

type Recorder struct {
	Tools          ffmpeg.Tools
	Store          *store.Store
	Health         *health.Tracker
	DataDir        string
	Location       *time.Location
	SegmentSeconds int
	Log            *slog.Logger

	jobs chan job
}

type job struct {
	cameraID int64
	slug     string
	tsPath   string
}

// Start launches the single remux worker. Remuxing is serialized on purpose to keep
// disk I/O bursts small on the target hardware.
func (r *Recorder) Start(ctx context.Context) {
	r.jobs = make(chan job, 256)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-r.jobs:
				if err := r.finalize(ctx, j); err != nil && ctx.Err() == nil {
					r.Log.Error("finalize segment", "camera", j.slug, "file", filepath.Base(j.tsPath), "err", err)
				}
			}
		}
	}()
}

func (r *Recorder) incomingDir(slug string) string {
	return filepath.Join(r.DataDir, "recordings", slug, ".incoming")
}

// RunCamera records one camera until ctx is cancelled or ffmpeg stops.
func (r *Recorder) RunCamera(ctx context.Context, cam store.Camera) error {
	incoming := r.incomingDir(cam.Slug)
	if err := os.MkdirAll(incoming, 0o750); err != nil {
		return err
	}
	// Anything left in .incoming before ffmpeg starts is complete (leftover from a crash
	// or a shutdown before the remux queue drained).
	r.recoverOrphans(ctx, cam, incoming)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := r.Tools.Command(ctx,
		"-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-fflags", "+genpts",
		"-i", r.Tools.StreamURL(cam.RecordStreamName()),
		"-map", "0:v", "-c:v", "copy", "-an",
		"-f", "segment",
		"-segment_time", strconv.Itoa(r.SegmentSeconds),
		"-segment_atclocktime", "1",
		"-reset_timestamps", "1",
		"-strftime", "1",
		"-segment_list", "pipe:1",
		"-segment_list_type", "csv",
		filepath.Join(incoming, "%Y%m%dT%H%M%S.ts"),
	)
	// -strftime uses the process time zone; file names must be UTC.
	cmd.Env = append(os.Environ(), "TZ=UTC")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	logRing := r.Health.Log(cam.ID)
	if err := cmd.Start(); err != nil {
		return err
	}
	go ffmpeg.PipeStderr(stderr, logRing)

	// ffmpeg that stays up this long is connected; report it as running without waiting
	// for the first segment (which may take NVR_SEGMENT_SECONDS).
	running := time.AfterFunc(20*time.Second, func() {
		r.Health.SetState(cam.ID, supervisor.Running, "")
	})
	defer running.Stop()

	stallAfter := time.Duration(2*r.SegmentSeconds)*time.Second + 30*time.Second
	var stalled atomic.Bool
	stall := time.AfterFunc(stallAfter, func() {
		stalled.Store(true)
		logRing.Add("nvrd: no segment finished within " + stallAfter.String() + ", restarting")
		cancel()
	})
	defer stall.Stop()

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		name, _, _ := strings.Cut(sc.Text(), ",")
		if !strings.HasSuffix(name, ".ts") {
			continue
		}
		stall.Reset(stallAfter)
		r.enqueue(ctx, job{cameraID: cam.ID, slug: cam.Slug, tsPath: filepath.Join(incoming, filepath.Base(name))})
	}

	err = cmd.Wait()
	if stalled.Load() {
		return errors.New("recording stalled")
	}
	if err == nil {
		err = errors.New("ffmpeg exited")
	}
	return err
}

func (r *Recorder) enqueue(ctx context.Context, j job) {
	select {
	case r.jobs <- j:
	case <-ctx.Done():
	}
}

func (r *Recorder) recoverOrphans(ctx context.Context, cam store.Camera, dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.ts"))
	sort.Strings(files)
	for _, f := range files {
		r.enqueue(ctx, job{cameraID: cam.ID, slug: cam.Slug, tsPath: f})
	}
	if len(files) > 0 {
		r.Log.Info("recovering orphan segments", "camera", cam.Slug, "count", len(files))
	}
}

// finalize probes a finished TS segment, remuxes it to MP4 and indexes it.
func (r *Recorder) finalize(ctx context.Context, j job) error {
	if _, err := os.Stat(j.tsPath); errors.Is(err, os.ErrNotExist) {
		return nil // already handled (e.g. queued twice by orphan recovery)
	}
	start, err := time.Parse(nameLayout, strings.TrimSuffix(filepath.Base(j.tsPath), ".ts"))
	if err != nil {
		return fmt.Errorf("unexpected segment name: %w", err)
	}
	probe, err := r.Tools.ProbeFile(ctx, j.tsPath)
	if err != nil || probe.Duration < 1 {
		// Empty or corrupt segment (e.g. the stream dropped right after it began).
		os.Remove(j.tsPath)
		return fmt.Errorf("dropping unreadable segment: %v", err)
	}

	local := start.In(r.Location)
	rel := filepath.Join("recordings", j.slug, local.Format("2006-01-02"), local.Format("150405")+".mp4")
	dst := filepath.Join(r.DataDir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := dst + ".tmp.mp4"
	args := []string{"-i", j.tsPath, "-c", "copy"}
	if tag := ffmpeg.MP4Tag(probe.Codec); tag != "" {
		args = append(args, "-tag:v", tag)
	}
	args = append(args, "-movflags", "+faststart", "-f", "mp4", "-y", tmp)
	if err := r.Tools.Run(ctx, args...); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("remux: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return err
	}

	end := start.Add(time.Duration(probe.Duration * float64(time.Second)))
	rec := &store.Recording{
		CameraID:  j.cameraID,
		StartAt:   start.Unix(),
		EndAt:     end.Unix(),
		Path:      rel,
		SizeBytes: info.Size(),
		Codec:     sql.NullString{String: probe.Codec, Valid: probe.Codec != ""},
		Width:     sql.NullInt64{Int64: probe.Width, Valid: probe.Width > 0},
		Height:    sql.NullInt64{Int64: probe.Height, Valid: probe.Height > 0},
	}
	if err := r.Store.Recordings.Insert(ctx, rec); err != nil {
		return fmt.Errorf("index: %w", err)
	}
	os.Remove(j.tsPath)
	r.Health.SegmentWritten(j.cameraID, end)
	r.Log.Debug("segment stored", "camera", j.slug, "path", rel, "seconds", int(probe.Duration))
	return nil
}
