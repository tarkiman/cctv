// Package timelapse compiles each camera's daily snapshot frames into an MP4 shortly
// after local midnight, and prunes old frames.
package timelapse

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/snapshot"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

type Compiler struct {
	Tools    ffmpeg.Tools
	Store    *store.Store
	DataDir  string
	Location *time.Location
	Log      *slog.Logger
}

// VideoPath is the compiled timelapse of a camera for one local date.
func VideoPath(dataDir, slug, date string) string {
	return filepath.Join(dataDir, "timelapse", slug, date+".mp4")
}

// Run compiles any missed days now, then every day at 00:15 local time.
func (c *Compiler) Run(ctx context.Context) {
	for {
		c.runOnce(ctx)
		now := time.Now().In(c.Location)
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 15, 0, 0, c.Location)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
	}
}

func (c *Compiler) runOnce(ctx context.Context) {
	cams, err := c.Store.Cameras.List(ctx)
	if err != nil {
		c.Log.Error("timelapse: list cameras", "err", err)
		return
	}
	settings, err := c.Store.Settings.All(ctx)
	if err != nil {
		c.Log.Error("timelapse: settings", "err", err)
		return
	}
	today := time.Now().In(c.Location).Format("2006-01-02")
	for _, cam := range cams {
		dates, _ := filepath.Glob(filepath.Join(c.DataDir, "timelapse", cam.Slug, "frames", "*"))
		sort.Strings(dates)
		for _, dir := range dates {
			date := filepath.Base(dir)
			if date >= today || ctx.Err() != nil {
				continue
			}
			out := VideoPath(c.DataDir, cam.Slug, date)
			if _, err := os.Stat(out); err != nil {
				if err := c.compile(ctx, dir, out, settings[store.SettingTimelapseFPS]); err != nil {
					c.Log.Error("timelapse compile", "camera", cam.Slug, "date", date, "err", err)
					continue
				}
				c.Log.Info("timelapse compiled", "camera", cam.Slug, "date", date)
			}
			if pruneBefore(date, today, settings[store.SettingTimelapseKeepDays]) {
				os.RemoveAll(dir)
			}
		}
	}
}

// pruneBefore reports whether frames of date are older than keepDays before today.
func pruneBefore(date, today string, keepDays int) bool {
	d, err1 := time.Parse("2006-01-02", date)
	t, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return false
	}
	return t.Sub(d) > time.Duration(keepDays)*24*time.Hour
}

func (c *Compiler) compile(ctx context.Context, dir, out string, fps int) error {
	frames, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if len(frames) < 2 {
		return nil
	}
	sort.Strings(frames)
	var list strings.Builder
	frameDur := 1.0 / float64(fps)
	for _, f := range frames {
		fmt.Fprintf(&list, "file '%s'\nduration %.6f\n", strings.ReplaceAll(f, "'", `'\''`), frameDur)
	}
	listPath := filepath.Join(dir, "frames.txt")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o640); err != nil {
		return err
	}
	defer os.Remove(listPath)
	tmp := out + ".tmp.mp4"
	err := c.Tools.RunNice(ctx,
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-vf", fmt.Sprintf("fps=%d,scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p", fps),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "28",
		"-movflags", "+faststart", "-f", "mp4", "-y", tmp,
	)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}

// Dates lists the dates (newest first) that have a compiled timelapse for a camera.
func Dates(dataDir, slug string) []string {
	files, _ := filepath.Glob(filepath.Join(dataDir, "timelapse", slug, "*.mp4"))
	out := make([]string, 0, len(files))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".mp4")
		if _, err := time.Parse("2006-01-02", name); err == nil {
			out = append(out, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// TodayFrames counts today's frames so the UI can show progress before compilation.
func TodayFrames(dataDir, slug string, loc *time.Location) int {
	files, _ := filepath.Glob(filepath.Join(snapshot.FramesDir(dataDir, slug, time.Now().In(loc).Format("2006-01-02")), "*.jpg"))
	return len(files)
}
