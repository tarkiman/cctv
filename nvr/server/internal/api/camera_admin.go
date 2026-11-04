package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/camera"
	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

func (s *Server) cameraSources(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	src, err := s.Cameras.Sources(r.Context(), cam)
	if err != nil {
		writeError(w, http.StatusBadGateway, "go2rtc_error", "Konfigurasi go2rtc tidak bisa dibaca.")
		return
	}
	writeJSON(w, http.StatusOK, src)
}

type sourcesRequest struct {
	Main camera.SourceInput  `json:"main"`
	Sub  *camera.SourceInput `json:"sub"`
}

// resolve turns the request into go2rtc URLs; it writes the error response itself.
func (s *Server) resolveSources(w http.ResponseWriter, r *http.Request, cam *store.Camera, req sourcesRequest) (string, string, bool) {
	main, err := s.Cameras.ResolveSource(r.Context(), cam, "main", req.Main)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_url", "URL stream utama tidak valid. Contoh: rtsp://192.168.1.30:554/stream1")
		return "", "", false
	}
	sub := ""
	if req.Sub != nil && !req.Sub.Empty() {
		if sub, err = s.Cameras.ResolveSource(r.Context(), cam, "sub", *req.Sub); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_url", "URL sub-stream tidak valid.")
			return "", "", false
		}
	}
	return main, sub, true
}

func (s *Server) putCameraSources(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	var req sourcesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	main, sub, ok := s.resolveSources(w, r, cam, req)
	if !ok {
		return
	}
	switch err := s.Cameras.SetSources(r.Context(), cam, main, sub); {
	case errors.Is(err, camera.ErrNotEditable):
		writeError(w, http.StatusConflict, "not_editable", "Sumber kamera ini dikelola lewat go2rtc.")
		return
	case err != nil:
		s.Log.Error("set camera sources", "camera", cam.Slug, "err", err)
		writeError(w, http.StatusBadGateway, "go2rtc_error", "go2rtc menolak perubahan sumber.")
		return
	}
	s.cameraSources(w, r)
}

func (s *Server) createCamera(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
		sourcesRequest
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	main, sub, ok := s.resolveSources(w, r, nil, req.sourcesRequest)
	if !ok {
		return
	}
	cam, err := s.Cameras.Add(r.Context(), req.Slug, req.Name, main, sub)
	switch {
	case errors.Is(err, camera.ErrSlugInvalid):
		writeError(w, http.StatusBadRequest, "invalid_slug", "ID kamera 1–32 karakter: huruf kecil, angka, - atau _, dan tidak boleh berakhiran _sub.")
		return
	case errors.Is(err, camera.ErrSlugTaken):
		writeError(w, http.StatusConflict, "slug_taken", "ID kamera sudah dipakai.")
		return
	case err != nil:
		s.Log.Error("add camera", "slug", req.Slug, "err", err)
		writeError(w, http.StatusBadGateway, "go2rtc_error", "Kamera gagal ditambahkan ke go2rtc.")
		return
	}
	writeJSON(w, http.StatusCreated, s.cameraDTO(r, cam))
}

func (s *Server) deleteCamera(w http.ResponseWriter, r *http.Request) {
	cam, ok := s.cameraFromPath(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Confirm != cam.Slug {
		writeError(w, http.StatusBadRequest, "confirm_required", "Ketik ID kamera untuk konfirmasi.")
		return
	}
	switch err := s.Cameras.Remove(r.Context(), cam); {
	case errors.Is(err, camera.ErrNotEditable):
		writeError(w, http.StatusConflict, "not_editable", "Kamera Xiaomi tidak bisa dihapus dari sini; hapus lewat go2rtc.")
		return
	case err != nil:
		s.Log.Error("remove camera", "camera", cam.Slug, "err", err)
		writeError(w, http.StatusBadGateway, "go2rtc_error", "Kamera gagal dihapus.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// probeCamera tests a source directly (camera → nvrd, not via go2rtc) and reports codec,
// resolution and frame rate, with advice for browser playback.
func (s *Server) probeCamera(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source   camera.SourceInput `json:"source"`
		CameraID int64              `json:"camera_id"`
		Stream   string             `json:"stream"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	var cam *store.Camera
	if req.CameraID > 0 {
		c, err := s.Store.Cameras.ByID(r.Context(), req.CameraID)
		if err == nil {
			cam = c
		}
	}
	src, err := s.Cameras.ResolveSource(r.Context(), cam, req.Stream, req.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_url", "URL tidak valid. Contoh: rtsp://192.168.1.30:554/stream1")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	info, err := s.Tools.ProbeURL(ctx, src)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": explainProbe(err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "info": info, "advice": probeAdvice(info, req.Stream)})
}

func explainProbe(msg string) string {
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "401") || strings.Contains(l, "unauthorized"):
		return "Kamera menolak login: username atau password salah."
	case strings.Contains(l, "404") || strings.Contains(l, "not found"):
		return "Kamera tersambung tetapi path stream salah (404). Cek path di manual kamera, mis. /stream1 atau /V_ENC_000."
	case strings.Contains(l, "connection refused"):
		return "Kamera menolak koneksi di port itu. Cek nomor port (RTSP biasanya 554) dan pastikan RTSP aktif di aplikasi kamera."
	case strings.Contains(l, "timed out") || strings.Contains(l, "timeout") || strings.Contains(l, "no route") || strings.Contains(l, "unreachable"):
		return "Kamera tidak menjawab. Cek IP kamera (mungkin berubah) dan pastikan kamera menyala di jaringan yang sama."
	}
	return "Gagal tersambung: " + msg
}

func probeAdvice(info *ffmpeg.StreamInfo, stream string) []string {
	var out []string
	if info.Codec == "hevc" && stream != "sub" {
		out = append(out, "Video HEVC (H.265): direkam normal, tetapi live HD hanya jalan di browser yang mendukung HEVC. Tambahkan sub-stream H.264 untuk live di semua perangkat.")
	}
	if info.Codec != "h264" && info.Codec != "hevc" {
		out = append(out, "Codec "+info.Codec+" tidak bisa diputar browser; pilih H.264 atau H.265 di pengaturan kamera.")
	}
	if stream == "sub" && info.Width > 1280 {
		out = append(out, "Resolusi sub-stream cukup besar; untuk grid live, 640×360 atau 800×448 sudah cukup dan lebih ringan.")
	}
	return out
}
