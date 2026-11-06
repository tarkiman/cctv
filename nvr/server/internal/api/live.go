package api

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
)

// liveWS proxies the browser's websocket to go2rtc's MSE stream for one camera. Only
// nvrd talks to go2rtc; the browser never names a go2rtc stream directly.
func (s *Server) liveWS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("camera"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_camera", "Parameter camera tidak valid.")
		return
	}
	cam, err := s.Store.Cameras.ByID(r.Context(), id)
	if err != nil || !canSee(r, cam) {
		writeError(w, http.StatusNotFound, "camera_not_found", "Kamera tidak ditemukan.")
		return
	}
	stream := cam.StreamFor(r.URL.Query().Get("quality"))

	// Accept verifies that Origin matches Host (or an allowed proxy origin), which blocks
	// cross-site websocket hijacking.
	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.Config.AllowedOrigins})
	if err != nil {
		return
	}
	defer client.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	upstream, _, err := websocket.Dial(ctx, s.Go2rtc.WSURL(stream), nil)
	if err != nil {
		client.Close(websocket.StatusTryAgainLater, "go2rtc tidak bisa dihubungi")
		return
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(32 << 20) // MSE init segments and keyframes can be large
	client.SetReadLimit(1 << 20)

	errc := make(chan error, 2)
	go func() { errc <- pump(ctx, client, upstream) }()
	go func() { errc <- pump(ctx, upstream, client) }()
	<-errc
	cancel()
	client.Close(websocket.StatusNormalClosure, "")
	upstream.Close(websocket.StatusNormalClosure, "")
}

func pump(ctx context.Context, from, to *websocket.Conn) error {
	for {
		typ, rd, err := from.Reader(ctx)
		if err != nil {
			return err
		}
		wr, err := to.Writer(ctx, typ)
		if err != nil {
			return err
		}
		if _, err := io.Copy(wr, rd); err != nil {
			return err
		}
		if err := wr.Close(); err != nil {
			return err
		}
	}
}

// liveWebRTC exchanges an SDP offer with go2rtc for one camera.
func (s *Server) liveWebRTC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("camera"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_camera", "Parameter camera tidak valid.")
		return
	}
	cam, err := s.Store.Cameras.ByID(r.Context(), id)
	if err != nil || !canSee(r, cam) {
		writeError(w, http.StatusNotFound, "camera_not_found", "Kamera tidak ditemukan.")
		return
	}
	answer, err := s.Go2rtc.WebRTC(r.Context(), cam.StreamFor(r.URL.Query().Get("quality")), io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadGateway, "go2rtc_error", "go2rtc menolak sesi WebRTC.")
		return
	}
	w.Header().Set("Content-Type", "application/sdp")
	_, _ = w.Write(answer)
}
