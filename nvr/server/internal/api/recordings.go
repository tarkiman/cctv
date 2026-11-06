package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/timelapse"
)

// Segments closer than this are drawn as one continuous range on the timeline.
const mergeGap = 5

type recordingDTO struct {
	ID        int64     `json:"id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Codec     string    `json:"codec,omitempty"`
	Width     int64     `json:"width,omitempty"`
	Height    int64     `json:"height,omitempty"`
	SizeBytes int64     `json:"size_bytes"`
	URL       string    `json:"url"`
}

func toRecordingDTO(r *store.Recording) recordingDTO {
	return recordingDTO{
		ID: r.ID, Start: unixTime(r.StartAt), End: unixTime(r.EndAt),
		Codec: r.Codec.String, Width: r.Width.Int64, Height: r.Height.Int64,
		SizeBytes: r.SizeBytes, URL: fmt.Sprintf("/api/v1/recordings/%d/file", r.ID),
	}
}

type timeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// timeline returns merged recording ranges for one local (WIB) day.
func (s *Server) timeline(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	loc := s.Config.Location
	date := r.URL.Query().Get("date")
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_date", "Parameter date harus berformat YYYY-MM-DD.")
		return
	}
	from, to := day, day.AddDate(0, 0, 1)
	recs, err := s.Store.Recordings.Range(r.Context(), cam.ID, from.Unix(), to.Unix())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	ranges := []timeRange{}
	var cur *timeRange
	for _, rec := range recs {
		start := max(rec.StartAt, from.Unix())
		end := min(rec.EndAt, to.Unix())
		if cur != nil && start-cur.End.Unix() <= mergeGap {
			if end > cur.End.Unix() {
				cur.End = unixTime(end)
			}
			continue
		}
		ranges = append(ranges, timeRange{unixTime(start), unixTime(end)})
		cur = &ranges[len(ranges)-1]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":     date,
		"timezone": loc.String(),
		"start":    from.UTC(),
		"end":      to.UTC(),
		"ranges":   ranges,
		"events":   []any{},
	})
}

func (s *Server) listRecordings(w http.ResponseWriter, r *http.Request) {
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
	recs, err := s.Store.Recordings.Range(r.Context(), cam.ID, from.Unix(), to.Unix())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]recordingDTO, 0, len(recs))
	for i := range recs {
		out = append(out, toRecordingDTO(&recs[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// recordingAt finds the segment covering t (or the next one after a gap) and the offset
// into it where playback should start.
func (s *Server) recordingAt(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	t, err := parseTime(r.URL.Query().Get("t"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time", "Parameter t harus RFC 3339.")
		return
	}
	rec, err := s.Store.Recordings.At(r.Context(), cam.ID, t.Unix())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_recording", "Tidak ada rekaman pada atau setelah waktu ini.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	offset := max(0, t.Unix()-rec.StartAt)
	writeJSON(w, http.StatusOK, map[string]any{"recording": toRecordingDTO(rec), "offset": offset})
}

func (s *Server) recordingNext(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "no_recording", "Rekaman tidak ditemukan.")
		return
	}
	cur, err := s.Store.Recordings.ByID(r.Context(), id)
	if err == nil && !s.canSeeID(r, cur.CameraID) {
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_recording", "Rekaman tidak ditemukan.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	next, err := s.Store.Recordings.Next(r.Context(), cur.CameraID, cur.StartAt)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_recording", "Belum ada segmen berikutnya.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRecordingDTO(next))
}

func (s *Server) recordingFile(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "no_recording", "Rekaman tidak ditemukan.")
		return
	}
	rec, err := s.Store.Recordings.ByID(r.Context(), id)
	if err == nil && !s.canSeeID(r, rec.CameraID) {
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_recording", "Rekaman tidak ditemukan.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.serveMedia(w, r, filepath.Join(s.Config.DataDir, rec.Path), "video/mp4", r.URL.Query().Has("download"))
}

func (s *Server) listTimelapse(w http.ResponseWriter, r *http.Request) {
	cams, err := s.visibleCameras(r)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	type item struct {
		CameraID    int64    `json:"camera_id"`
		Dates       []string `json:"dates"`
		TodayFrames int      `json:"today_frames"`
		Enabled     bool     `json:"enabled"`
	}
	out := make([]item, 0, len(cams))
	for _, c := range cams {
		out = append(out, item{
			CameraID:    c.ID,
			Dates:       timelapse.Dates(s.Config.DataDir, c.Slug),
			TodayFrames: timelapse.TodayFrames(s.Config.DataDir, c.Slug, s.Config.Location),
			Enabled:     c.TimelapseEnabled,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) timelapseFile(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "cameraID")
	if !ok {
		return
	}
	date := chi.URLParam(r, "date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeError(w, http.StatusNotFound, "no_timelapse", "Timelapse tidak ditemukan.")
		return
	}
	s.serveMedia(w, r, timelapse.VideoPath(s.Config.DataDir, cam.Slug, date), "video/mp4", r.URL.Query().Has("download"))
}

// serveMedia serves a file with Range support (needed for video seeking).
func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, path, contentType string, download bool) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "file_missing", "File tidak ditemukan di disk.")
		return
	}
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
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	}
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}
