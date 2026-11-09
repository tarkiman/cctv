// Package camera discovers cameras from go2rtc and runs each camera's workers
// (recorder, snapshot), restarting them when the camera's settings change.
package camera

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/detect"
	"github.com/tarkiman/cctv/nvr/server/internal/go2rtc"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/recorder"
	"github.com/tarkiman/cctv/nvr/server/internal/snapshot"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/supervisor"
)

// SubSuffix marks a go2rtc stream as the low-resolution sub-stream of another stream
// (e.g. "bardi_sub" belongs to "bardi").
const SubSuffix = "_sub"

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type Manager struct {
	Store       *store.Store
	Go2rtc      *go2rtc.Client
	Health      *health.Tracker
	Recorder    *recorder.Recorder
	Snapshotter *snapshot.Snapshotter
	Detector    *detect.Worker
	DataDir     string
	Log         *slog.Logger

	mu      sync.Mutex
	ctx     context.Context
	workers map[int64]*workers // recorder + snapshot
	detects map[int64]*workers // detector, separate so toggling it never interrupts recording
	wg      sync.WaitGroup
}

// workers are the running goroutines of one camera.
type workers struct {
	cancel context.CancelFunc
	done   sync.WaitGroup
}

// Start syncs cameras from go2rtc (retrying until go2rtc answers) and starts workers.
// It returns once workers are running; they stop when ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.workers = map[int64]*workers{}
	m.detects = map[int64]*workers{}
	m.mu.Unlock()

	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			if err := m.Sync(ctx); err == nil {
				break
			} else {
				m.Log.Warn("go2rtc not reachable, retrying", "err", err, "retry_in", backoff)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
		}
		cams, err := m.Store.Cameras.List(ctx)
		if err != nil {
			m.Log.Error("list cameras", "err", err)
			return
		}
		for _, cam := range cams {
			m.start(cam)
		}
	}()
}

// Context is the lifetime context workers run under (for background reloads).
func (m *Manager) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

// Wait blocks until all workers have stopped (after the Start context is cancelled).
func (m *Manager) Wait() { m.wg.Wait() }

// Sync creates cameras for go2rtc streams that are not known yet and links sub-streams.
func (m *Manager) Sync(ctx context.Context) error {
	streams, err := m.Go2rtc.Streams(ctx)
	if err != nil {
		return err
	}
	cams, err := m.Store.Cameras.List(ctx)
	if err != nil {
		return err
	}
	sources, _ := m.Go2rtc.Sources(ctx) // nil on error: fall back to producers
	xiaomi := func(name string) bool {
		if srcs, ok := sources[name]; ok {
			return go2rtc.IsXiaomiSource(srcs)
		}
		return streams[name].IsXiaomi()
	}
	byMain := map[string]store.Camera{}
	for _, c := range cams {
		byMain[c.MainStream] = c
	}

	names := make([]string, 0, len(streams))
	for name := range streams {
		names = append(names, name)
	}
	sort.Strings(names)
	order := len(cams)
	for _, name := range names {
		if strings.HasSuffix(name, SubSuffix) {
			continue
		}
		sub := sql.NullString{}
		if _, ok := streams[name+SubSuffix]; ok {
			sub = sql.NullString{String: name + SubSuffix, Valid: true}
		}
		if c, ok := byMain[name]; ok {
			if c.SubStream != sub {
				if err := m.Store.Cameras.SetSubStream(ctx, c.ID, sub); err != nil {
					return err
				}
			}
			m.Health.SetXiaomi(c.ID, xiaomi(name))
			continue
		}
		slug := strings.ToLower(name)
		if !slugRe.MatchString(slug) {
			m.Log.Warn("skipping go2rtc stream with unsupported name", "stream", name)
			continue
		}
		id, err := m.Store.Cameras.CreateDiscovered(ctx, slug, name, sub, order)
		if err != nil {
			return err
		}
		order++
		m.Health.SetXiaomi(id, xiaomi(name))
		m.Log.Info("camera discovered", "slug", slug, "sub_stream", sub.String)
	}
	return nil
}

// Reload restarts all of a camera's workers with its current settings.
func (m *Manager) Reload(ctx context.Context, id int64) error {
	cam, err := m.Store.Cameras.ByID(ctx, id)
	if err != nil {
		return err
	}
	// Wait for the old recorder to finish its last segment before a new one starts
	// writing into the same .incoming folder.
	m.stop(m.workers, id)
	m.stop(m.detects, id)
	if m.Recorder != nil {
		m.Recorder.ResetAudio(id) // settings changed: try the camera's audio again
	}
	m.start(*cam)
	return nil
}

// ReloadDetect restarts only the detector (after detection was switched on or off), so
// recording continues without a gap.
func (m *Manager) ReloadDetect(ctx context.Context, id int64) error {
	cam, err := m.Store.Cameras.ByID(ctx, id)
	if err != nil {
		return err
	}
	m.stop(m.detects, id)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startDetectLocked(*cam)
	return nil
}

// stop cancels one worker group of a camera and waits for it to exit.
func (m *Manager) stop(group map[int64]*workers, id int64) {
	m.mu.Lock()
	w := group[id]
	delete(group, id)
	m.mu.Unlock()
	if w != nil {
		w.cancel()
		w.done.Wait()
	}
}

// spawn runs fn as part of worker group w.
func (m *Manager) spawn(w *workers, fn func()) {
	m.wg.Add(1)
	w.done.Add(1)
	go func() {
		defer m.wg.Done()
		defer w.done.Done()
		fn()
	}()
}

func (m *Manager) start(cam store.Camera) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	w := &workers{cancel: cancel}
	m.workers[cam.ID] = w
	run := func(fn func()) { m.spawn(w, fn) }

	if last, err := m.Store.Recordings.Latest(ctx, cam.ID); err == nil {
		t := time.Unix(last.EndAt, 0).UTC()
		m.Health.Seed(cam.ID, &t)
	}

	log := m.Log.With("camera", cam.Slug)
	if cam.RecordEnabled {
		run(func() {
			supervisor.Run(ctx, log, func(ctx context.Context) error {
				return m.Recorder.RunCamera(ctx, cam)
			}, supervisor.Hooks{
				OnStart: func() {
					if st := m.Health.Get(cam.ID).State; st != supervisor.Degraded && st != supervisor.Offline {
						m.Health.SetState(cam.ID, supervisor.Starting, "")
					}
				},
				OnFailure: func(err error, failures int, _ time.Duration) {
					m.Health.Failure(cam.ID, failures, err)
				},
			})
		})
	} else {
		m.Health.SetState(cam.ID, supervisor.Disabled, "Perekaman dimatikan untuk kamera ini.")
	}

	if cam.SnapshotEnabled {
		run(func() { m.Snapshotter.RunCamera(ctx, cam) })
	}

	m.startDetectLocked(cam)
}

// startDetectLocked starts the detector group of a camera; m.mu must be held.
func (m *Manager) startDetectLocked(cam store.Camera) {
	if m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	if cam.DetectEnabled && m.Detector != nil {
		ctx, cancel := context.WithCancel(m.ctx)
		w := &workers{cancel: cancel}
		m.detects[cam.ID] = w
		dlog := m.Log.With("camera", cam.Slug, "worker", "detect")
		m.spawn(w, func() {
			supervisor.Run(ctx, dlog, func(ctx context.Context) error {
				return m.Detector.RunCamera(ctx, cam)
			}, supervisor.Hooks{
				OnFailure: func(err error, _ int, _ time.Duration) {
					if !errors.Is(err, detect.ErrSettingsChanged) {
						m.Health.SetDetect(cam.ID, "error", err.Error())
					}
				},
			})
			m.Health.SetDetect(cam.ID, "off", "")
		})
	} else {
		m.Health.SetDetect(cam.ID, "off", "")
	}
}
