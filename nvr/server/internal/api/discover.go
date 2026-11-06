package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/discover"
)

var macRe = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

type foundDevice struct {
	discover.Device
	// new | registered | ip_changed | gateway | ignored
	Status     string `json:"status"`
	CameraID   int64  `json:"camera_id,omitempty"`
	CameraName string `json:"camera_name,omitempty"`
	OldHost    string `json:"old_host,omitempty"` // ip_changed: the address the camera's source still uses
}

// sourceHost extracts the host of a go2rtc source (rtsp://…, xiaomi://…@host?…).
func sourceHost(src string) string {
	u, err := url.Parse(src)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// discoverCameras scans the server's LAN and classifies what it finds against the
// registered cameras. MACs of matched cameras are learned so a later IP change is
// recognised.
func (s *Server) discoverCameras(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	subnets := discover.LocalSubnets()
	devices, err := discover.Scan(ctx, subnets)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, "scan_failed", "Pemindaian jaringan gagal atau terlalu lama.")
		return
	}
	cams, err := s.Store.Cameras.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sources, _ := s.Go2rtc.Sources(r.Context())
	ignored, err := s.Store.Cameras.IgnoredMACs(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	hostCam := map[string]int{} // host → index in cams
	for i, c := range cams {
		for _, name := range []string{c.MainStream, c.MainStream + "_sub"} {
			for _, src := range sources[name] {
				if h := sourceHost(src); h != "" {
					hostCam[h] = i
				}
			}
		}
	}
	out := make([]foundDevice, 0, len(devices))
	for _, d := range devices {
		f := foundDevice{Device: d, Status: "new"}
		byIP := -1
		for _, ip := range d.IPs {
			if i, ok := hostCam[ip]; ok {
				byIP = i
			}
		}
		byMAC := -1
		for i, c := range cams {
			if d.MAC != "" && c.MAC.Valid && c.MAC.String == d.MAC {
				byMAC = i
			}
		}
		switch {
		case d.Gateway:
			f.Status = "gateway"
		case byIP >= 0:
			f.Status, f.CameraID, f.CameraName = "registered", cams[byIP].ID, cams[byIP].Name
			if d.MAC != "" && cams[byIP].MAC.String != d.MAC {
				_ = s.Store.Cameras.SetMAC(r.Context(), cams[byIP].ID, d.MAC)
			}
		case byMAC >= 0:
			c := cams[byMAC]
			f.CameraID, f.CameraName = c.ID, c.Name
			f.Status = "registered" // e.g. a camera routed through a gateway
			if h := sourceHost(firstSource(sources[c.MainStream])); h != "" && net.ParseIP(h) != nil && isLAN(h, subnets) {
				f.Status, f.OldHost = "ip_changed", h
			}
		case d.MAC != "" && ignored[d.MAC]:
			f.Status = "ignored"
		}
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"subnets": subnets, "devices": out})
}

func firstSource(srcs []string) string {
	if len(srcs) == 0 {
		return ""
	}
	return srcs[0]
}

func isLAN(ip string, subnets []discover.Subnet) bool {
	p := net.ParseIP(ip)
	for _, sn := range subnets {
		if _, n, err := net.ParseCIDR(sn.CIDR); err == nil && n.Contains(p) {
			return true
		}
	}
	return false
}

// discoverStreams works out stream URLs for a found device: via ONVIF when it has an
// ONVIF endpoint, otherwise by trying common RTSP paths.
func (s *Server) discoverStreams(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		XAddr    string `json:"onvif_xaddr"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || net.ParseIP(req.IP) == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	if !isLAN(req.IP, discover.LocalSubnets()) {
		writeError(w, http.StatusBadRequest, "not_lan", "Hanya perangkat di jaringan lokal server yang bisa diperiksa.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if req.XAddr != "" {
		u, err := url.Parse(req.XAddr)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !isLAN(u.Hostname(), discover.LocalSubnets()) {
			writeError(w, http.StatusBadRequest, "invalid_xaddr", "Alamat ONVIF tidak valid.")
			return
		}
		streams, err := discover.ONVIFStreams(ctx, req.XAddr, req.Username, req.Password)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, map[string]any{"method": "onvif", "streams": streams})
			return
		case errors.Is(err, discover.ErrAuth):
			writeJSON(w, http.StatusOK, map[string]any{"method": "onvif", "needs_auth": true,
				"error": "Kamera menolak login ONVIF. Cek username/password (biasanya sama dengan aplikasi kamera)."})
			return
		}
		// ONVIF answered oddly: fall back to guessing RTSP paths.
	}
	port := req.Port
	if port == 0 {
		port = 554
	}
	res := discover.GuessPaths(ctx, req.IP, port, req.Username, req.Password)
	body := map[string]any{"method": "rtsp", "guess": res.Guess, "needs_auth": res.NeedsAuth, "tried": res.Tried}
	if res.Guess == nil {
		msg := "Path stream tidak ditemukan di daftar merek umum. Isi URL manual dari manual/aplikasi kamera."
		if res.NeedsAuth {
			msg = "Kamera minta login. Isi username/password kamera lalu coba lagi."
		}
		body["error"] = msg
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) linkDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MAC      string `json:"mac"`
		CameraID int64  `json:"camera_id"`
	}
	if err := decodeJSON(r, &req); err != nil || !macRe.MatchString(strings.ToLower(req.MAC)) {
		writeError(w, http.StatusBadRequest, "invalid_mac", "Alamat MAC tidak valid.")
		return
	}
	if _, err := s.Store.Cameras.ByID(r.Context(), req.CameraID); err != nil {
		writeError(w, http.StatusNotFound, "camera_not_found", "Kamera tidak ditemukan.")
		return
	}
	if err := s.Store.Cameras.SetMAC(r.Context(), req.CameraID, strings.ToLower(req.MAC)); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ignoreDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MAC     string `json:"mac"`
		Ignored bool   `json:"ignored"`
	}
	if err := decodeJSON(r, &req); err != nil || !macRe.MatchString(strings.ToLower(req.MAC)) {
		writeError(w, http.StatusBadRequest, "invalid_mac", "Alamat MAC tidak valid.")
		return
	}
	if err := s.Store.Cameras.SetIgnored(r.Context(), strings.ToLower(req.MAC), req.Ignored); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
