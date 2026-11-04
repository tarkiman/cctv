// Package snapshot grabs one keyframe per camera at a fixed interval. Decoding only
// keyframes (-skip_frame nokey) keeps the cost at one image per interval instead of a
// continuous decode.
package snapshot

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

type Snapshotter struct {
	Tools    ffmpeg.Tools
	Health   *health.Tracker
	DataDir  string
	Location *time.Location
	Every    time.Duration
	Log      *slog.Logger
}

// LatestPath is where the newest snapshot of a camera is kept.
func LatestPath(dataDir, slug string) string {
	return filepath.Join(dataDir, "snapshots", slug, "latest.jpg")
}

// FramesDir is where timelapse frames of a camera for one local date are kept.
func FramesDir(dataDir, slug, date string) string {
	return filepath.Join(dataDir, "timelapse", slug, "frames", date)
}

// RunCamera takes snapshots until ctx is cancelled.
func (s *Snapshotter) RunCamera(ctx context.Context, cam store.Camera) {
	// Spread cameras out so their keyframe waits don't overlap.
	jitter := time.Duration(rand.Int64N(int64(10 * time.Second)))
	select {
	case <-ctx.Done():
		return
	case <-time.After(jitter):
	}
	t := time.NewTicker(s.Every)
	defer t.Stop()
	for {
		if err := s.take(ctx, cam); err != nil && ctx.Err() == nil {
			s.Log.Debug("snapshot failed", "camera", cam.Slug, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Snapshotter) take(ctx context.Context, cam store.Camera) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	latest := LatestPath(s.DataDir, cam.Slug)
	if err := os.MkdirAll(filepath.Dir(latest), 0o750); err != nil {
		return err
	}
	tmp := latest + ".tmp.jpg"
	err := s.Tools.Run(ctx,
		"-rtsp_transport", "tcp",
		"-skip_frame", "nokey",
		"-i", s.Tools.StreamURL(cam.MainStream),
		"-frames:v", "1",
		"-vf", "scale=-2:'min(720,ih)'",
		"-q:v", "4",
		"-f", "image2", "-y", tmp,
	)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, latest); err != nil {
		return err
	}
	now := time.Now()
	s.Health.SnapshotTaken(cam.ID, now)

	if cam.TimelapseEnabled {
		local := now.In(s.Location)
		dir := FramesDir(s.DataDir, cam.Slug, local.Format("2006-01-02"))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		if err := copyFile(latest, filepath.Join(dir, local.Format("150405")+".jpg")); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
