// Package camera discovers cameras from go2rtc and runs each camera's workers
// (recorder, snapshot), restarting them when the camera's settings change.
package camera

import (
	"context"
	"database/sql"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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
	Log         *slog.Logger

	mu      sync.Mutex
	ctx     context.Context
	workers map[int64]*workers
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
			m.Health.SetXiaomi(c.ID, streams[name].IsXiaomi())
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
		m.Health.SetXiaomi(id, streams[name].IsXiaomi())
		m.Log.Info("camera discovered", "slug", slug, "sub_stream", sub.String)
	}
	return nil
}

// Reload restarts a camera's workers with its current settings.
func (m *Manager) Reload(ctx context.Context, id int64) error {
	cam, err := m.Store.Cameras.ByID(ctx, id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	old := m.workers[id]
	delete(m.workers, id)
	m.mu.Unlock()
	if old != nil {
		// Wait for the old recorder to finish its last segment before a new one starts
		// writing into the same .incoming folder.
		old.cancel()
		old.done.Wait()
	}
	m.start(*cam)
	return nil
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
	run := func(fn func()) {
		m.wg.Add(1)
		w.done.Add(1)
		go func() {
			defer m.wg.Done()
			defer w.done.Done()
			fn()
		}()
	}

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
}
