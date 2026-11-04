package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/export"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// maxClip bounds one export; stream copy makes long clips cheap in CPU but they are
// large downloads (an hour of bardi main is ~150 MB).
const maxClip = 3 * time.Hour

func (s *Server) clipRange(w http.ResponseWriter, r *http.Request) (*store.Camera, []store.Recording, time.Time, time.Time, bool) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return nil, nil, time.Time{}, time.Time{}, false
	}
	from, err1 := parseTime(r.URL.Query().Get("from"))
	to, err2 := parseTime(r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil || !to.After(from) {
		writeError(w, http.StatusBadRequest, "invalid_range", "Rentang waktu tidak valid: akhir harus setelah awal.")
		return nil, nil, from, to, false
	}
	if to.Sub(from) > maxClip {
		writeError(w, http.StatusBadRequest, "range_too_long", "Klip maksimal 3 jam. Pecah menjadi beberapa klip.")
		return nil, nil, from, to, false
	}
	recs, err := s.Store.Recordings.Range(r.Context(), cam.ID, from.Unix(), to.Unix())
	if err != nil {
		s.internalError(w, r, err)
		return nil, nil, from, to, false
	}
	return cam, recs, from, to, true
}

func (s *Server) exportEstimate(w http.ResponseWriter, r *http.Request) {
	_, recs, from, to, ok := s.clipRange(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, export.EstimateRange(recs, from, to))
}

// exportClip builds the clip, then serves it as a download (with Content-Length, so phones
// show progress) and deletes it.
func (s *Server) exportClip(w http.ResponseWriter, r *http.Request) {
	cam, recs, from, to, ok := s.clipRange(w, r)
	if !ok {
		return
	}
	path, err := s.Export.Clip(r.Context(), recs, from, to)
	switch {
	case errors.Is(err, export.ErrNoFootage):
		writeError(w, http.StatusNotFound, "no_footage", "Tidak ada rekaman pada rentang ini.")
		return
	case errors.Is(err, export.ErrBusy):
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusTooManyRequests, "export_busy", "Export lain sedang diproses. Coba lagi sebentar.")
		return
	case err != nil:
		if r.Context().Err() == nil {
			s.Log.Error("export clip", "camera", cam.Slug, "err", err)
			writeError(w, http.StatusInternalServerError, "export_failed", "Gagal membuat klip.")
		}
		return
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	loc := s.Config.Location
	name := fmt.Sprintf("%s_%s_%s-%s.mp4", cam.Slug, from.In(loc).Format("2006-01-02"),
		from.In(loc).Format("150405"), to.In(loc).Format("150405"))
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Cache-Control", "no-store")
	s.Log.Info("clip exported", "camera", cam.Slug, "from", from, "to", to, "bytes", info.Size())
	http.ServeContent(w, r, name, info.ModTime(), f)
}
