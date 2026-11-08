// Package recorder records each camera with `ffmpeg -c:v copy` into MPEG-TS segments,
// then remuxes finished segments into faststart MP4 files and indexes them.
//
// MPEG-TS is used while writing because a partially written TS file stays readable if
// the process dies; MP4 is used for storage because browsers seek it far better.
//
// Sound is recorded as AAC (16 kHz mono, 32 kbps): camera audio comes as G.711, Opus or
// MP2, which MP4 players handle poorly or not at all, and transcoding audio is cheap.
// A camera whose audio cannot be decoded is recorded without sound instead of not at
// all (see docs/10-keputusan.md D14).
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

	audioMu      sync.Mutex
	audioBlocked map[int64]time.Time // camera -> audio retry time; zero = until Reload/restart
}

type job struct {
	cameraID int64
	slug     string
	tsPath   string
	audio    bool // recorded with audio on (orphans: false, their state is unknown)
}

const (
	// audioStartTimeout: with audio on, ffmpeg writes its first segment file only once
	// both streams are set up. A connected ffmpeg that still has no file after this long
	// is waiting for audio that never decodes.
	audioStartTimeout = 60 * time.Second
	// audioErrGrace: with audio errors in the log and still no segment file after this
	// long, the audio will not decode (ffmpeg keeps probing it), so fall back without
	// waiting the full audioStartTimeout.
	audioErrGrace = 15 * time.Second
	// audioTimeoutRetry: a run that only timed out (no audio error in the log) may have
	// met a slow source, e.g. a Xiaomi P2P connect, so its audio is tried again later.
	audioTimeoutRetry = time.Hour
)

// audioErrRe matches ffmpeg log lines about the audio input, decoder or encoder: ffmpeg 6
// tags audio input streams "aist#", and decoders log as "[<codec> @ 0x…]" (e.g. cam1's
// broken MP2: "[mp2 @ 0x…] Header missing").
var audioErrRe = regexp.MustCompile(`aist#|Decode error rate|Audio encoding failed|output stream 0:1|` +
	`^\[(mp2|mp3\w*|aac|opus|pcm_\w+|adpcm_\w+|g72\d\w*) @`)

// recordArgs builds the ffmpeg command line for one recording run. wallclock replaces
// the source timestamps with arrival time; it is used for Xiaomi sources, whose audio
// timestamps from go2rtc advance far slower than real time.
func recordArgs(input string, audio, wallclock bool, incoming string, segmentSeconds int) []string {
	args := []string{"-loglevel", "warning"}
	if wallclock {
		args = append(args, "-use_wallclock_as_timestamps", "1")
	}
	if strings.HasPrefix(input, "rtsp") {
		args = append(args, "-rtsp_transport", "tcp")
	}
	args = append(args, "-fflags", "+genpts", "-i", input, "-map", "0:v", "-c:v", "copy")
	if audio {
		args = append(args, "-map", "0:a:0?", "-af", "aresample=async=1",
			"-c:a", "aac", "-ac", "1", "-ar", "16000", "-b:a", "32k")
	} else {
		args = append(args, "-an")
	}
	return append(args,
		"-f", "segment",
		"-segment_time", strconv.Itoa(segmentSeconds),
		"-segment_atclocktime", "1",
		"-reset_timestamps", "1",
		"-strftime", "1",
		"-segment_list", "pipe:1",
		"-segment_list_type", "csv",
		filepath.Join(incoming, "%Y%m%dT%H%M%S.ts"),
	)
}

// remuxArgs turns a finished TS segment into a faststart MP4 without re-encoding.
func remuxArgs(src, dst, videoCodec string) []string {
	args := []string{"-i", src, "-c", "copy"}
	if tag := ffmpeg.MP4Tag(videoCodec); tag != "" {
		args = append(args, "-tag:v", tag)
	}
	return append(args, "-movflags", "+faststart", "-f", "mp4", "-y", dst)
}

// audioAllowed reports whether audio may be recorded for a camera, i.e. it was not
// found unreadable. A failed attempt costs up to a minute of recording (ffmpeg probes
// a broken stream that long), so audio that failed with decode errors is not retried
// on a timer: only after the camera's settings change (ResetAudio) or nvrd restarts.
func (r *Recorder) audioAllowed(id int64) bool {
	r.audioMu.Lock()
	defer r.audioMu.Unlock()
	until, ok := r.audioBlocked[id]
	switch {
	case !ok:
		return true
	case until.IsZero() || time.Now().Before(until):
		return false
	}
	delete(r.audioBlocked, id)
	return true
}

// blockAudio records a camera as video-only for retryAfter, or until Reload/restart
// when retryAfter is 0.
func (r *Recorder) blockAudio(id int64, retryAfter time.Duration) {
	r.audioMu.Lock()
	defer r.audioMu.Unlock()
	if r.audioBlocked == nil {
		r.audioBlocked = map[int64]time.Time{}
	}
	var until time.Time
	if retryAfter > 0 {
		until = time.Now().Add(retryAfter)
	}
	r.audioBlocked[id] = until
}

// ResetAudio forgets that a camera's audio was unreadable (its settings changed).
func (r *Recorder) ResetAudio(id int64) {
	r.audioMu.Lock()
	defer r.audioMu.Unlock()
	delete(r.audioBlocked, id)
}

// wroteSince reports whether ffmpeg created a segment file in dir at or after t.
func wroteSince(dir string, t time.Time) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.ts"))
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && !info.ModTime().Before(t.Add(-time.Second)) {
			return true
		}
	}
	return false
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

	audio := cam.RecordAudio && r.audioAllowed(cam.ID)
	switch {
	case !cam.RecordAudio:
		r.Health.SetAudio(cam.ID, "off")
	case !audio:
		r.Health.SetAudio(cam.ID, "unavailable")
	}

	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := r.Tools.Command(ctx, recordArgs(r.Tools.StreamURL(cam.RecordStreamName()),
		audio, audio && r.Health.Xiaomi(cam.ID), incoming, r.SegmentSeconds)...)
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
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return err
	}
	var audioErr, audioStuck atomic.Bool
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if audio && audioErrRe.MatchString(line) {
				audioErr.Store(true)
			}
			logRing.Add(line)
		}
	}()

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
	if audio {
		stuck := func(needErr bool) func() {
			return func() {
				if (!needErr || audioErr.Load()) && !wroteSince(incoming, started) {
					audioStuck.Store(true)
					cancel()
				}
			}
		}
		early := time.AfterFunc(audioErrGrace, stuck(true))
		defer early.Stop()
		late := time.AfterFunc(audioStartTimeout, stuck(false))
		defer late.Stop()
	}

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		name, _, _ := strings.Cut(sc.Text(), ",")
		if !strings.HasSuffix(name, ".ts") {
			continue
		}
		stall.Reset(stallAfter)
		r.enqueue(ctx, job{cameraID: cam.ID, slug: cam.Slug, tsPath: filepath.Join(incoming, filepath.Base(name)), audio: audio})
	}

	err = cmd.Wait()
	// Audio that never decodes keeps ffmpeg from writing anything (or makes it quit with
	// a decode error), which would leave the camera unrecorded. Record video only.
	if audio && parent.Err() == nil && !wroteSince(incoming, started) && (audioStuck.Load() || audioErr.Load()) {
		if audioErr.Load() {
			r.blockAudio(cam.ID, 0)
			logRing.Add("nvrd: suara kamera tidak bisa dibaca; merekam tanpa suara sampai pengaturan kamera diubah")
		} else {
			r.blockAudio(cam.ID, audioTimeoutRetry)
			logRing.Add("nvrd: rekaman dengan suara tidak kunjung mulai; merekam tanpa suara, dicoba lagi dalam " + audioTimeoutRetry.String())
		}
		r.Log.Warn("camera audio unreadable, recording video only", "camera", cam.Slug, "decode_errors", audioErr.Load())
		return r.RunCamera(parent, cam)
	}
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
	if err := r.Tools.Run(ctx, remuxArgs(j.tsPath, tmp, probe.Codec)...); err != nil {
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
	if j.audio {
		if probe.Audio != "" {
			r.Health.SetAudio(j.cameraID, "on")
		} else {
			r.Health.SetAudio(j.cameraID, "none")
		}
	}
	r.Log.Debug("segment stored", "camera", j.slug, "path", rel, "seconds", int(probe.Duration))
	return nil
}
