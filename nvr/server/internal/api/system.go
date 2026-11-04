package api

import (
	"context"
	"net/http"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/notify"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", "Database tidak bisa diakses.")
		return
	}
	if _, err := s.Go2rtc.Streams(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "go2rtc_unavailable", "go2rtc tidak bisa dihubungi.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) systemStats(w http.ResponseWriter, r *http.Request) {
	usage, err := s.Store.Recordings.Usage(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	lastDay, err := s.Store.Recordings.BytesSince(r.Context(), time.Now().Add(-24*time.Hour).Unix())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	type camUsage struct {
		store.CameraUsage
		OldestAt    *time.Time `json:"oldest_at"`
		Last24Bytes int64      `json:"last_24h_bytes"`
	}
	cams := make([]camUsage, 0, len(usage))
	for _, u := range usage {
		cu := camUsage{CameraUsage: u, Last24Bytes: lastDay[u.CameraID]}
		if u.Oldest > 0 {
			t := unixTime(u.Oldest)
			cu.OldestAt = &t
		}
		cams = append(cams, cu)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host":           s.Sys.Read(s.Config.DataDir),
		"cameras":        cams,
		"version":        s.Version,
		"go2rtc_version": s.Go2rtc.Version(r.Context()),
		"timezone":       s.Config.Location.String(),
		"notify_targets": len(s.Config.NotifyWebhooks),
	})
}

// systemStatus is the admin view of each camera's workers, including recent ffmpeg logs.
func (s *Server) systemStatus(w http.ResponseWriter, r *http.Request) {
	cams, err := s.Store.Cameras.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	type item struct {
		ID     int64    `json:"id"`
		Slug   string   `json:"slug"`
		Name   string   `json:"name"`
		Status any      `json:"status"`
		Log    []string `json:"log"`
	}
	out := make([]item, 0, len(cams))
	for _, c := range cams {
		out = append(out, item{ID: c.ID, Slug: c.Slug, Name: c.Name, Status: s.Health.Get(c.ID), Log: s.Health.Log(c.ID).Lines()})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.Settings.All(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, all)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]int
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	for k, v := range req {
		lim, ok := store.SettingLimits[k]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown_setting", "Pengaturan tidak dikenal: "+k)
			return
		}
		if v < lim[0] || v > lim[1] {
			writeError(w, http.StatusBadRequest, "invalid_setting", "Nilai di luar batas untuk "+k)
			return
		}
	}
	for k, v := range req {
		if err := s.Store.Settings.Set(r.Context(), k, v); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	s.getSettings(w, r)
}

// stream is the SSE endpoint; it starts with the full camera status so a reconnecting
// client never shows stale state.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	s.Hub.Serve(w, r, func(send func(string, any)) {
		for _, st := range s.Health.All() {
			send("camera.status", st)
		}
	})
}

// notifyTest sends a test message to every configured webhook.
func (s *Server) notifyTest(w http.ResponseWriter, r *http.Request) {
	if !s.Notifier.Enabled() {
		writeError(w, http.StatusConflict, "notify_disabled", "Notifikasi belum diatur. Isi NVR_NOTIFY_WEBHOOK di .env lalu restart nvrd.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := s.Notifier.Send(ctx, notify.Message{
		Kind: "test", Level: notify.Info, Title: "Tes notifikasi NVR Lite",
		Text: "Kalau pesan ini sampai, notifikasi kamera offline dan disk penuh akan dikirim ke sini.",
		URL:  s.Config.PublicURL,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "notify_failed", "Gagal mengirim: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
