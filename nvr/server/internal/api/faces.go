package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/vision"
)

// Face gallery (Tahap E step 3, admin): unlabelled faces grouped by likeness, people
// and their labelled samples. Labels are what recognition matches against.

const (
	clusterSimilarity = 0.45 // stricter than matching, so a group is one person
	maxGallerySamples = 3000
	previewSamples    = 8
)

type faceClusterDTO struct {
	IDs        []int64 `json:"ids"`
	Preview    []int64 `json:"preview"`
	Count      int     `json:"count"`
	FirstAt    int64   `json:"first_at"`
	LastAt     int64   `json:"last_at"`
	Suggestion *struct {
		PersonID int64   `json:"person_id"`
		Name     string  `json:"name"`
		Score    float64 `json:"score"`
	} `json:"suggestion,omitempty"`
}

func (s *Server) faceClusters(w http.ResponseWriter, r *http.Request) {
	samples, err := s.Store.Faces.Unlabeled(r.Context(), maxGallerySamples)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	threshold := float64(s.Store.Settings.Int(r.Context(), store.SettingFaceMatch)) / 100
	out := []faceClusterDTO{}
	for _, c := range vision.Cluster(samples, clusterSimilarity) {
		d := faceClusterDTO{Count: len(c), FirstAt: c[0].At, LastAt: c[0].At}
		for i, m := range c {
			d.IDs = append(d.IDs, m.ID)
			if i < previewSamples {
				d.Preview = append(d.Preview, m.ID)
			}
			d.FirstAt, d.LastAt = min(d.FirstAt, m.At), max(d.LastAt, m.At)
		}
		if s.Vision != nil {
			if p, score, ok := s.Vision.Suggest(r.Context(), vision.Centroid(c)); ok && score >= threshold {
				d.Suggestion = &struct {
					PersonID int64   `json:"person_id"`
					Name     string  `json:"name"`
					Score    float64 `json:"score"`
				}{p.ID, p.Name, score}
			}
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) faceImage(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Wajah tidak ditemukan.")
		return
	}
	f, err := s.Store.Faces.ByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Wajah tidak ditemukan.")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.Config.DataDir, f.Path))
}

type personDTO struct {
	store.Person
	Samples  int     `json:"samples"`
	Preview  []int64 `json:"preview"`
	LastSeen int64   `json:"last_seen,omitempty"` // unix ms of the last recognition
}

func (s *Server) listPeople(w http.ResponseWriter, r *http.Request) {
	people, err := s.Store.People.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	labeled, err := s.Store.Faces.Labeled(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	last, _ := s.Store.Faces.LastMatch(r.Context())
	byPerson := map[int64][]store.FaceSample{}
	for _, f := range labeled {
		byPerson[f.PersonID.Int64] = append(byPerson[f.PersonID.Int64], f)
	}
	out := make([]personDTO, 0, len(people))
	for _, p := range people {
		d := personDTO{Person: p, Samples: len(byPerson[p.ID]), LastSeen: last[p.ID], Preview: []int64{}}
		fs := byPerson[p.ID]
		sort.SliceStable(fs, func(i, j int) bool { return fs[i].At > fs[j].At })
		for i := 0; i < len(fs) && i < previewSamples; i++ {
			d.Preview = append(d.Preview, fs[i].ID)
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) assignFaces(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs      []int64 `json:"ids"`
		PersonID int64   `json:"person_id"`
		Name     string  `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > maxGallerySamples {
		writeError(w, http.StatusBadRequest, "invalid_body", "Pilih wajah yang akan diberi nama.")
		return
	}
	personID := req.PersonID
	if personID == 0 {
		name := strings.TrimSpace(req.Name)
		if name == "" || len(name) > 40 {
			writeError(w, http.StatusBadRequest, "invalid_name", "Nama wajib diisi, maksimal 40 karakter.")
			return
		}
		p, err := s.Store.People.Ensure(r.Context(), name)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		personID = p.ID
	} else if _, err := s.Store.People.ByID(r.Context(), personID); err != nil {
		writeError(w, http.StatusNotFound, "person_not_found", "Orang tidak ditemukan.")
		return
	}
	if err := s.Store.Faces.Label(r.Context(), req.IDs, personID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.galleryChanged()
	s.listPeople(w, r)
}

func (s *Server) unassignFaces(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_body", "Pilih wajah.")
		return
	}
	if err := s.Store.Faces.Label(r.Context(), req.IDs, 0); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.galleryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteFaces(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > maxGallerySamples {
		writeError(w, http.StatusBadRequest, "invalid_body", "Pilih wajah.")
		return
	}
	paths, err := s.Store.Faces.Delete(r.Context(), req.IDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, p := range paths {
		os.Remove(filepath.Join(s.Config.DataDir, p))
	}
	s.galleryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updatePerson(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	p, err := s.Store.People.ByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "person_not_found", "Orang tidak ditemukan.")
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Notify *bool   `json:"notify"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 40 {
			writeError(w, http.StatusBadRequest, "invalid_name", "Nama wajib diisi, maksimal 40 karakter.")
			return
		}
		p.Name = name
	}
	if req.Notify != nil {
		p.Notify = *req.Notify
	}
	if err := s.Store.People.Update(r.Context(), p); errors.Is(err, store.ErrNameTaken) {
		writeError(w, http.StatusConflict, "name_taken", "Nama itu sudah dipakai.")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.galleryChanged()
	s.listPeople(w, r)
}

func (s *Server) deletePerson(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	if err := s.Store.People.Delete(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.galleryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) galleryChanged() {
	if s.Vision != nil {
		s.Vision.InvalidateGallery()
	}
}
