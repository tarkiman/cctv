package camera

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tarkiman/cctv/nvr/server/internal/go2rtc"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

var (
	ErrSlugInvalid = errors.New("slug invalid")
	ErrSlugTaken   = errors.New("slug taken")
	ErrNotEditable = errors.New("sources not editable")
)

// Sources describes a camera's main and sub sources for the settings UI.
type Sources struct {
	Editable bool        `json:"editable"`
	Reason   string      `json:"reason,omitempty"`
	Xiaomi   bool        `json:"xiaomi"`
	Main     *SourceInfo `json:"main"`
	Sub      *SourceInfo `json:"sub"`
}

func (m *Manager) Sources(ctx context.Context, cam *store.Camera) (*Sources, error) {
	all, err := m.Go2rtc.Sources(ctx)
	if err != nil {
		return nil, err
	}
	main := all[cam.MainStream]
	out := &Sources{Xiaomi: go2rtc.IsXiaomiSource(main)}
	out.Editable, out.Reason = Editable(main)
	if len(main) > 0 && !out.Xiaomi {
		out.Main = Describe(main[0])
	}
	if sub := all[cam.MainStream+SubSuffix]; len(sub) == 1 {
		if ok, _ := Editable(sub); ok {
			out.Sub = Describe(sub[0])
		} else if out.Editable {
			out.Editable, out.Reason = false, "Sub-stream memakai sumber khusus; ubah lewat go2rtc.yaml."
		}
	}
	return out, nil
}

// ResolveSource builds a source URL from UI input, filling a kept password from the
// camera's current source of the same stream ("main"/"sub"). cam may be nil.
func (m *Manager) ResolveSource(ctx context.Context, cam *store.Camera, stream string, in SourceInput) (string, error) {
	old := ""
	if cam != nil {
		if all, err := m.Go2rtc.Sources(ctx); err == nil {
			name := cam.MainStream
			if stream == "sub" {
				name += SubSuffix
			}
			if srcs := all[name]; len(srcs) == 1 {
				old = srcs[0]
			}
		}
	}
	return BuildURL(in, old)
}

// Add creates a camera from UI input: the streams go into go2rtc (persisted to
// go2rtc.yaml), then the usual discovery picks them up and workers start.
func (m *Manager) Add(ctx context.Context, slug, name string, main string, sub string) (*store.Camera, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugRe.MatchString(slug) || strings.HasSuffix(slug, SubSuffix) {
		return nil, ErrSlugInvalid
	}
	if _, err := m.Store.Cameras.BySlug(ctx, slug); err == nil {
		return nil, ErrSlugTaken
	}
	streams, err := m.Go2rtc.Streams(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := streams[slug]; ok {
		return nil, ErrSlugTaken
	}
	if err := m.Go2rtc.PutStream(ctx, slug, main); err != nil {
		return nil, err
	}
	if sub != "" {
		if err := m.Go2rtc.PutStream(ctx, slug+SubSuffix, sub); err != nil {
			_ = m.Go2rtc.DeleteStream(ctx, slug)
			return nil, err
		}
	}
	if err := m.Sync(ctx); err != nil {
		return nil, err
	}
	cam, err := m.Store.Cameras.BySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("camera not discovered after adding stream: %w", err)
	}
	if name = strings.TrimSpace(name); name != "" && name != cam.Name {
		cam.Name = name
		if err := m.Store.Cameras.Update(ctx, cam); err != nil {
			return nil, err
		}
	}
	m.start(*cam)
	m.Log.Info("camera added", "slug", slug, "sub_stream", sub != "")
	return cam, nil
}

// SetSources replaces a camera's sources (e.g. after its IP changed) and restarts it.
// An empty sub removes the sub-stream.
func (m *Manager) SetSources(ctx context.Context, cam *store.Camera, main, sub string) error {
	all, err := m.Go2rtc.Sources(ctx)
	if err != nil {
		return err
	}
	if ok, _ := Editable(all[cam.MainStream]); !ok {
		return ErrNotEditable
	}
	subName := cam.MainStream + SubSuffix
	if err := m.Go2rtc.PutStream(ctx, cam.MainStream, main); err != nil {
		return err
	}
	switch _, had := all[subName]; {
	case sub != "":
		if err := m.Go2rtc.PutStream(ctx, subName, sub); err != nil {
			return err
		}
	case had:
		if err := m.Go2rtc.DeleteStream(ctx, subName); err != nil {
			return err
		}
	}
	if err := m.Sync(ctx); err != nil {
		return err
	}
	m.Log.Info("camera sources changed", "slug", cam.Slug)
	return m.Reload(ctx, cam.ID)
}

// Remove stops a camera, removes its streams from go2rtc and deletes it together with
// its recordings, snapshots and timelapses. Xiaomi cameras are refused: their stream
// carries a cloud session that cannot be recreated from here.
func (m *Manager) Remove(ctx context.Context, cam *store.Camera) error {
	all, err := m.Go2rtc.Sources(ctx)
	if err != nil {
		return err
	}
	if go2rtc.IsXiaomiSource(all[cam.MainStream]) {
		return ErrNotEditable
	}
	m.stop(m.workers, cam.ID)
	m.stop(m.detects, cam.ID)
	for _, name := range []string{cam.MainStream, cam.MainStream + SubSuffix} {
		if _, ok := all[name]; ok {
			if err := m.Go2rtc.DeleteStream(ctx, name); err != nil {
				return err
			}
		}
	}
	if err := m.Store.Cameras.Delete(ctx, cam.ID); err != nil {
		return err
	}
	m.Health.Remove(cam.ID)
	for _, dir := range []string{"recordings", "snapshots", "timelapse"} {
		p := filepath.Join(m.DataDir, dir, cam.Slug)
		if err := os.RemoveAll(p); err != nil {
			m.Log.Warn("remove camera data", "path", p, "err", err)
		}
	}
	m.Log.Info("camera removed", "slug", cam.Slug)
	return nil
}
