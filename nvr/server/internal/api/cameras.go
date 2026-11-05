package api

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/snapshot"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

type cameraDTO struct {
	ID                 int64               `json:"id"`
	Slug               string              `json:"slug"`
	Name               string              `json:"name"`
	HasSubStream       bool                `json:"has_sub_stream"`
	Codec              string              `json:"codec,omitempty"`
	Width              int64               `json:"width,omitempty"`
	Height             int64               `json:"height,omitempty"`
	RecordEnabled      bool                `json:"record_enabled"`
	RecordStream       string              `json:"record_stream"`
	RetentionDays      int                 `json:"retention_days"`
	EventRetentionDays int                 `json:"event_retention_days"`
	SnapshotEnabled    bool                `json:"snapshot_enabled"`
	TimelapseEnabled   bool                `json:"timelapse_enabled"`
	DetectEnabled      bool                `json:"detect_enabled"`
	SortOrder          int                 `json:"sort_order"`
	Status             health.CameraStatus `json:"status"`
}

func (s *Server) cameraDTO(r *http.Request, c *store.Camera) cameraDTO {
	d := cameraDTO{
		ID: c.ID, Slug: c.Slug, Name: c.Name, HasSubStream: c.SubStream.Valid,
		RecordEnabled: c.RecordEnabled, RecordStream: c.RecordStream,
		RetentionDays: c.RetentionDays, EventRetentionDays: c.EventRetentionDays,
		SnapshotEnabled: c.SnapshotEnabled, TimelapseEnabled: c.TimelapseEnabled,
		DetectEnabled: c.DetectEnabled, SortOrder: c.SortOrder,
		Status: s.Health.Get(c.ID),
	}
	if last, err := s.Store.Recordings.Latest(r.Context(), c.ID); err == nil {
		d.Codec, d.Width, d.Height = last.Codec.String, last.Width.Int64, last.Height.Int64
	}
	return d
}

func (s *Server) listCameras(w http.ResponseWriter, r *http.Request) {
	cams, err := s.Store.Cameras.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]cameraDTO, 0, len(cams))
	for i := range cams {
		out = append(out, s.cameraDTO(r, &cams[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getCamera(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.cameraDTO(r, cam))
}

func (s *Server) patchCamera(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	before := *cam
	var req struct {
		Name               *string `json:"name"`
		RecordEnabled      *bool   `json:"record_enabled"`
		RecordStream       *string `json:"record_stream"`
		RetentionDays      *int    `json:"retention_days"`
		EventRetentionDays *int    `json:"event_retention_days"`
		SnapshotEnabled    *bool   `json:"snapshot_enabled"`
		TimelapseEnabled   *bool   `json:"timelapse_enabled"`
		DetectEnabled      *bool   `json:"detect_enabled"`
		SortOrder          *int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 60 {
			writeError(w, http.StatusBadRequest, "invalid_name", "Nama kamera wajib diisi, maksimal 60 karakter.")
			return
		}
		cam.Name = name
	}
	if req.RecordStream != nil {
		if *req.RecordStream != "main" && *req.RecordStream != "sub" {
			writeError(w, http.StatusBadRequest, "invalid_record_stream", "Stream rekaman harus 'main' atau 'sub'.")
			return
		}
		if *req.RecordStream == "sub" && !cam.SubStream.Valid {
			writeError(w, http.StatusBadRequest, "no_sub_stream", "Kamera ini tidak punya sub-stream.")
			return
		}
		cam.RecordStream = *req.RecordStream
	}
	for _, v := range []struct {
		in  *int
		out *int
	}{{req.RetentionDays, &cam.RetentionDays}, {req.EventRetentionDays, &cam.EventRetentionDays}} {
		if v.in == nil {
			continue
		}
		if *v.in < 1 || *v.in > 365 {
			writeError(w, http.StatusBadRequest, "invalid_retention", "Retensi harus antara 1 dan 365 hari.")
			return
		}
		*v.out = *v.in
	}
	if req.RecordEnabled != nil {
		cam.RecordEnabled = *req.RecordEnabled
	}
	if req.SnapshotEnabled != nil {
		cam.SnapshotEnabled = *req.SnapshotEnabled
	}
	if req.TimelapseEnabled != nil {
		cam.TimelapseEnabled = *req.TimelapseEnabled
	}
	if req.DetectEnabled != nil {
		cam.DetectEnabled = *req.DetectEnabled
	}
	if req.SortOrder != nil {
		cam.SortOrder = *req.SortOrder
	}
	if err := s.Store.Cameras.Update(r.Context(), cam); err != nil {
		s.internalError(w, r, err)
		return
	}
	// Restart only what changed: a detection toggle never interrupts recording, and a
	// rename or sort change restarts nothing. Restarts can take a few seconds (the
	// recorder closes its segment first), so they happen in the background.
	recorderChanged := before.RecordEnabled != cam.RecordEnabled || before.RecordStream != cam.RecordStream ||
		before.SnapshotEnabled != cam.SnapshotEnabled
	detectChanged := before.DetectEnabled != cam.DetectEnabled
	go func(id int64) {
		var err error
		switch {
		case recorderChanged:
			err = s.Cameras.Reload(s.Cameras.Context(), id)
		case detectChanged:
			err = s.Cameras.ReloadDetect(s.Cameras.Context(), id)
		}
		if err != nil {
			s.Log.Error("reload camera", "id", id, "err", err)
		}
	}(cam.ID)
	writeJSON(w, http.StatusOK, s.cameraDTO(r, cam))
}

func (s *Server) syncCameras(w http.ResponseWriter, r *http.Request) {
	if err := s.Cameras.Sync(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "go2rtc_unavailable", "go2rtc tidak bisa dihubungi.")
		return
	}
	s.listCameras(w, r)
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	path := snapshot.LatestPath(s.Config.DataDir, cam.Slug)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_snapshot", "Belum ada snapshot untuk kamera ini.")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "latest.jpg", info.ModTime().Truncate(time.Second), f)
}
