package api

import (
	"net/http"
	"time"
)

type eventDTO struct {
	ID       int64      `json:"id"`
	CameraID int64      `json:"camera_id"`
	Label    string     `json:"label"`
	Score    *float64   `json:"score"`
	Start    time.Time  `json:"start"`
	End      *time.Time `json:"end"`
	Reviewed bool       `json:"reviewed"`
}

// listEvents returns a camera's events overlapping [from, to) (max 7 days).
func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	from, err1 := parseTime(r.URL.Query().Get("from"))
	to, err2 := parseTime(r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil || !to.After(from) || to.Sub(from) > 7*24*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid_range", "from/to harus RFC 3339, from < to, maksimal 7 hari.")
		return
	}
	evs, err := s.Store.Events.Range(r.Context(), cam.ID, from.Unix(), to.Unix())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]eventDTO, 0, len(evs))
	for _, e := range evs {
		d := eventDTO{ID: e.ID, CameraID: e.CameraID, Label: e.Label, Start: unixTime(e.StartAt), Reviewed: e.Reviewed}
		if e.Score.Valid {
			v := e.Score.Float64
			d.Score = &v
		}
		if e.EndAt.Valid {
			t := unixTime(e.EndAt.Int64)
			d.End = &t
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}
