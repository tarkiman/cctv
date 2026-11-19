package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/vision"
)

// visionReport receives what nvr-vision saw on a camera. It authenticates with the
// shared NVR_VISION_TOKEN (Authorization: Bearer …), not a user session.
func (s *Server) visionReport(w http.ResponseWriter, r *http.Request) {
	token := s.Config.VisionToken
	got := r.Header.Get("Authorization")
	if s.Config.VisionURL == "" || token == "" || subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+token)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Token nvr-vision tidak valid.")
		return
	}
	var rep vision.Report
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&rep); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format laporan tidak valid.")
		return
	}
	cam, err := s.Store.Cameras.ByID(r.Context(), rep.CameraID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "camera_not_found", "Kamera tidak ditemukan.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.Vision.Handle(r.Context(), cam, &rep); err != nil {
		s.Log.Warn("vision report", "camera", cam.Slug, "err", err)
		writeError(w, http.StatusBadRequest, "invalid_report", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
