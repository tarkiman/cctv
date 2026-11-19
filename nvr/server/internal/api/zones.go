package api

import (
	"net/http"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// cameraZones lists a camera's detection zones (admin).
func (s *Server) cameraZones(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	zones, err := s.Store.Zones.List(r.Context(), cam.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

// putCameraZones replaces a camera's zones (admin). The detector picks them up within
// 30 seconds without restarting.
func (s *Server) putCameraZones(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	var zones []store.Zone
	if err := decodeJSON(r, &zones); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	if len(zones) > store.MaxZones {
		writeError(w, http.StatusBadRequest, "too_many_zones", "Maksimal 8 zona per kamera.")
		return
	}
	for i := range zones {
		if err := zones[i].Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_zone", err.Error())
			return
		}
	}
	if err := s.Store.Zones.Replace(r.Context(), cam.ID, zones); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.cameraZones(w, r)
}
