// Package retention deletes recordings by age per camera and, when the disk runs low, the
// oldest unprotected recordings across all cameras.
package retention

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/sysinfo"
)

const (
	interval = 15 * time.Minute
	batch    = 200
)

type Janitor struct {
	Store   *store.Store
	Hub     *sse.Hub
	DataDir string
	Log     *slog.Logger
}

func (j *Janitor) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := j.RunOnce(ctx); err != nil && ctx.Err() == nil {
			j.Log.Error("retention", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (j *Janitor) RunOnce(ctx context.Context) error {
	cams, err := j.Store.Cameras.List(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	deleted := 0
	for _, cam := range cams {
		if cam.EventRetentionDays > 0 {
			cutoff := now.Add(-time.Duration(cam.EventRetentionDays) * 24 * time.Hour).Unix()
			if _, err := j.Store.Events.DeleteBefore(ctx, cam.ID, cutoff); err != nil {
				return err
			}
		}
		for _, rule := range []struct {
			protected bool
			days      int
		}{{false, cam.RetentionDays}, {true, cam.EventRetentionDays}} {
			if rule.days <= 0 {
				continue
			}
			cutoff := now.Add(-time.Duration(rule.days) * 24 * time.Hour).Unix()
			for ctx.Err() == nil {
				recs, err := j.Store.Recordings.Expired(ctx, cam.ID, cutoff, rule.protected, batch)
				if err != nil {
					return err
				}
				if len(recs) == 0 {
					break
				}
				n, err := j.remove(ctx, recs)
				deleted += n
				if err != nil {
					return err
				}
			}
		}
	}

	j.pruneQuarantine(now)

	n, err := j.enforceFreeSpace(ctx)
	deleted += n
	if err != nil {
		return err
	}
	if deleted > 0 {
		j.Log.Info("retention removed segments", "count", deleted)
		j.pruneEmptyDirs()
	}
	return nil
}

// enforceFreeSpace deletes the oldest unprotected segments until the free-space floor is met.
func (j *Janitor) enforceFreeSpace(ctx context.Context) (int, error) {
	minFree := j.Store.Settings.Int(ctx, store.SettingMinFreePercent)
	deleted := 0
	warned := false
	for ctx.Err() == nil {
		total, free := sysinfo.Disk(j.DataDir)
		if total == 0 || float64(free)/float64(total)*100 >= float64(minFree) {
			return deleted, nil
		}
		if !warned {
			warned = true
			j.Log.Warn("disk below free-space floor, deleting oldest recordings", "min_free_percent", minFree)
			j.Hub.Publish("system.alert", map[string]string{
				"level":   "warn",
				"message": "Sisa disk di bawah batas; rekaman tertua dihapus otomatis.",
			})
		}
		recs, err := j.Store.Recordings.Oldest(ctx, 20)
		if err != nil || len(recs) == 0 {
			return deleted, err
		}
		n, err := j.remove(ctx, recs)
		deleted += n
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// remove deletes the files first, then the rows: a row without a file is visible and
// harmless, while a file without a row would silently eat disk.
func (j *Janitor) remove(ctx context.Context, recs []store.Recording) (int, error) {
	n := 0
	for _, r := range recs {
		err := os.Remove(filepath.Join(j.DataDir, r.Path))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			j.Log.Warn("retention: remove file", "path", r.Path, "err", err)
			continue
		}
		if err := j.Store.Recordings.Delete(ctx, r.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// QuarantineDays is how long broken segments set aside by the recorder are kept.
const QuarantineDays = 3

// pruneQuarantine deletes broken segments in recordings/<camera>/.quarantine older than
// QuarantineDays.
func (j *Janitor) pruneQuarantine(now time.Time) {
	files, _ := filepath.Glob(filepath.Join(j.DataDir, "recordings", "*", ".quarantine", "*"))
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && now.Sub(info.ModTime()) > QuarantineDays*24*time.Hour {
			os.Remove(f)
		}
	}
}

// pruneEmptyDirs removes empty date folders under recordings/<camera>/.
func (j *Janitor) pruneEmptyDirs() {
	days, _ := filepath.Glob(filepath.Join(j.DataDir, "recordings", "*", "????-??-??"))
	for _, d := range days {
		if entries, err := os.ReadDir(d); err == nil && len(entries) == 0 {
			os.Remove(d)
		}
	}
}
