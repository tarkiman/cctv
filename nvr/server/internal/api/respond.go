package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("request failed", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "Terjadi kesalahan di server.")
}

func decodeJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}

// cameraFromPath loads the camera named by the {id} URL parameter or writes a 404.
func (s *Server) cameraFromPath(w http.ResponseWriter, r *http.Request, param string) (*store.Camera, bool) {
	id, ok := idParam(r, param)
	if !ok {
		writeError(w, http.StatusNotFound, "camera_not_found", "Kamera tidak ditemukan.")
		return nil, false
	}
	cam, err := s.Store.Cameras.ByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "camera_not_found", "Kamera tidak ditemukan.")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return cam, true
}

func unixTime(t int64) time.Time { return time.Unix(t, 0).UTC() }

// parseTime accepts RFC 3339 timestamps from query strings.
func parseTime(v string) (time.Time, error) { return time.Parse(time.RFC3339, v) }
