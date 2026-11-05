package detect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// ErrSettingsChanged makes the supervisor restart the worker with new settings.
var ErrSettingsChanged = errors.New("detection settings changed")

// Worker runs motion detection for cameras that have detection enabled. It decodes the
// camera's sub-stream (or main stream when there is none) at a low frame rate into
// small gray frames; nothing else is decoded.
type Worker struct {
	Tools  ffmpeg.Tools
	Store  *store.Store
	Health *health.Tracker
	Log    *slog.Logger
}

type params struct {
	fps       int
	threshold int
	minArea   float64
}

func (w *Worker) params(ctx context.Context) params {
	all, _ := w.Store.Settings.All(ctx)
	return params{
		fps:       all[store.SettingDetectFPS],
		threshold: all[store.SettingMotionThreshold],
		minArea:   float64(all[store.SettingMotionMinArea]) / 1000,
	}
}

// RunCamera detects motion on one camera until ctx is cancelled or the stream stops.
func (w *Worker) RunCamera(ctx context.Context, cam store.Camera) error {
	if err := w.Store.Events.CloseDangling(ctx, cam.ID); err != nil {
		return err
	}
	p := w.params(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ring := ffmpeg.NewLogRing(20)
	cmd := w.Tools.Command(ctx,
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", w.Tools.StreamURL(cam.StreamFor("sub")),
		"-an",
		"-vf", fmt.Sprintf("fps=%d,scale=%d:%d,format=gray", p.fps, FrameW, FrameH),
		"-f", "rawvideo", "pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	w.Health.SetDetect(cam.ID, "starting", "")
	if err := cmd.Start(); err != nil {
		return err
	}
	go ffmpeg.PipeStderr(stderr, ring)

	// No frame for this long means the stream is dead; restart.
	const stallAfter = 30 * time.Second
	stall := time.AfterFunc(stallAfter, func() {
		ring.Add("nvrd: no frame for " + stallAfter.String())
		cancel()
	})
	defer stall.Stop()

	gate := NewGate(p.threshold, p.minArea)
	tr := &Tracker{Confirm: 2, Quiet: 10 * p.fps} // ends after 10 s without motion
	var (
		eventID    int64
		lastMotion time.Time
		recheck    = time.Now().Add(30 * time.Second)
		buf        = make([]byte, FrameW*FrameH)
		frames     int
		runErr     error
	)
	closeEvent := func(peak float64) {
		if eventID == 0 {
			return
		}
		// Use a fresh context: the event must be closed even while shutting down.
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccancel()
		if err := w.Store.Events.End(cctx, eventID, lastMotion.Unix(), peak); err != nil {
			w.Log.Warn("close motion event", "camera", cam.Slug, "err", err)
		}
		w.Health.SetMotion(cam.ID, false, lastMotion)
		eventID = 0
	}

	for {
		if _, err := io.ReadFull(stdout, buf); err != nil {
			break
		}
		stall.Reset(stallAfter)
		if frames++; frames == 1 {
			w.Health.SetDetect(cam.ID, "running", "")
		}
		now := time.Now()
		res := gate.Feed(buf)
		if res.Motion {
			lastMotion = now
		}
		switch t, peak := tr.Step(res); t {
		case Started:
			id, err := w.Store.Events.Start(ctx, cam.ID, "motion", now.Unix())
			if err != nil {
				w.Log.Warn("start motion event", "camera", cam.Slug, "err", err)
				continue
			}
			eventID = id
			w.Health.SetMotion(cam.ID, true, now)
		case Ended:
			closeEvent(peak)
		}
		// Thresholds apply live; a new frame rate needs a new ffmpeg.
		if now.After(recheck) {
			recheck = now.Add(30 * time.Second)
			np := w.params(ctx)
			gate.Threshold, gate.MinArea = np.threshold, np.minArea
			if np.fps != p.fps {
				runErr = ErrSettingsChanged
				cancel()
			}
		}
	}
	if tr.Active() {
		_, peak := tr.Step(Result{})
		closeEvent(peak)
	}
	waitErr := cmd.Wait()
	switch {
	case runErr != nil:
		return runErr
	case ctx.Err() != nil && frames > 0:
		return fmt.Errorf("stream stalled or stopped: %s", ring.Last())
	case waitErr != nil:
		return fmt.Errorf("%w: %s", waitErr, ring.Last())
	}
	return errors.New("stream ended")
}
