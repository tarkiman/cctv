package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

type locationDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Cameras int    `json:"cameras"` // cameras visible to the caller
}

// listLocations returns locations that hold at least one camera the caller can see
// (admins get every location, including empty ones).
func (s *Server) listLocations(w http.ResponseWriter, r *http.Request) {
	locs, err := s.Store.Locations.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cams, err := s.visibleCameras(r)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	count := map[int64]int{}
	for _, c := range cams {
		count[c.LocationID.Int64]++
	}
	admin := isAdmin(r)
	out := make([]locationDTO, 0, len(locs))
	for _, l := range locs {
		if admin || count[l.ID] > 0 {
			out = append(out, locationDTO{ID: l.ID, Name: l.Name, Cameras: count[l.ID]})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func validLocationName(w http.ResponseWriter, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 40 {
		writeError(w, http.StatusBadRequest, "invalid_name", "Nama lokasi wajib diisi, maksimal 40 karakter.")
		return "", false
	}
	return name, true
}

func (s *Server) createLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	name, ok := validLocationName(w, req.Name)
	if !ok {
		return
	}
	if _, err := s.Store.Locations.Create(r.Context(), name); err != nil {
		writeError(w, http.StatusConflict, "location_exists", "Nama lokasi sudah dipakai.")
		return
	}
	s.listLocations(w, r)
}

func (s *Server) renameLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "location_not_found", "Lokasi tidak ditemukan.")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	name, ok := validLocationName(w, req.Name)
	if !ok {
		return
	}
	switch err := s.Store.Locations.Rename(r.Context(), id, name); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "location_not_found", "Lokasi tidak ditemukan.")
		return
	case err != nil:
		writeError(w, http.StatusConflict, "location_exists", "Nama lokasi sudah dipakai.")
		return
	}
	s.listLocations(w, r)
}

func (s *Server) deleteLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "location_not_found", "Lokasi tidak ditemukan.")
		return
	}
	switch err := s.Store.Locations.Delete(r.Context(), id); {
	case errors.Is(err, store.ErrLocationInUse):
		writeError(w, http.StatusConflict, "location_in_use", "Pindahkan dulu kamera di lokasi ini ke lokasi lain.")
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "location_not_found", "Lokasi tidak ditemukan.")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.listLocations(w, r)
}

// --- per-user camera access (admin) ---

func (s *Server) getUserAccess(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFromPath(w, r)
	if !ok {
		return
	}
	g, err := s.Store.Access.Get(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) putUserAccess(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFromPath(w, r)
	if !ok {
		return
	}
	var g store.Grants
	if err := decodeJSON(r, &g); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	if u.Role == "admin" && !g.All {
		writeError(w, http.StatusBadRequest, "admin_all", "Admin selalu melihat semua kamera.")
		return
	}
	for _, id := range g.Locations {
		if !s.Store.Locations.Exists(r.Context(), id) {
			writeError(w, http.StatusBadRequest, "invalid_location", "Lokasi tidak ditemukan.")
			return
		}
	}
	for _, id := range append(append([]int64{}, g.Cameras...), g.DenyCameras...) {
		if _, err := s.Store.Cameras.ByID(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_camera", "Kamera tidak ditemukan.")
			return
		}
	}
	if err := s.Store.Access.Set(r.Context(), u.ID, g); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.getUserAccess(w, r)
}
