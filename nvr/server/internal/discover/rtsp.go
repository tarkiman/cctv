package discover

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// pathPairs are common main/sub RTSP paths per camera family, tried in order.
var pathPairs = []struct{ Brand, Main, Sub string }{
	{"Dafang / Xiaomi hack", "/unicast", ""},
	{"Bardi / Tuya / HiSilicon", "/V_ENC_000", "/V_ENC_001"},
	{"TP-Link Tapo", "/stream1", "/stream2"},
	{"Hikvision / EZVIZ", "/Streaming/Channels/101", "/Streaming/Channels/102"},
	{"Dahua / Imou", "/cam/realmonitor?channel=1&subtype=0", "/cam/realmonitor?channel=1&subtype=1"},
	{"Reolink", "/h264Preview_01_main", "/h264Preview_01_sub"},
	{"V380 / Yoosee", "/live/ch00_0", "/live/ch00_1"},
	{"Umum", "/live/main", "/live/sub"},
	{"Umum", "/11", "/12"},
	{"Umum", "/onvif1", "/onvif2"},
	{"Umum", "/live", ""},
	{"Umum", "/h264", ""},
	{"Umum", "/", ""},
}

// Guess is a working RTSP path pair found by DESCRIBE.
type Guess struct {
	Brand    string `json:"brand"`
	Main     string `json:"main"`          // URL without credentials
	Sub      string `json:"sub,omitempty"` // empty when the camera has no matching sub-stream
	Codec    string `json:"codec,omitempty"`
	SubCodec string `json:"sub_codec,omitempty"`
}

// GuessResult reports what the path probe found.
type GuessResult struct {
	Guess     *Guess `json:"guess,omitempty"`
	NeedsAuth bool   `json:"needs_auth"` // the camera wants (other) credentials
	Tried     int    `json:"tried"`
}

// GuessPaths tries the known paths with DESCRIBE until one answers 200.
func GuessPaths(ctx context.Context, ip string, port int, username, password string) GuessResult {
	var res GuessResult
	base := fmt.Sprintf("rtsp://%s:%d", ip, port)
	for _, pp := range pathPairs {
		if ctx.Err() != nil {
			break
		}
		res.Tried++
		code, codec := describe(ctx, base+pp.Main, username, password)
		if code == 401 {
			res.NeedsAuth = true
			continue
		}
		if code != 200 {
			continue
		}
		g := &Guess{Brand: pp.Brand, Main: base + pp.Main, Codec: codec}
		if pp.Sub != "" {
			if c, sc := describe(ctx, base+pp.Sub, username, password); c == 200 {
				g.Sub, g.SubCodec = base+pp.Sub, sc
			}
		}
		res.Guess, res.NeedsAuth = g, false
		return res
	}
	return res
}

// rtspServer identifies the RTSP server software from an OPTIONS reply, or "".
func rtspServer(ctx context.Context, ip string, port int) string {
	resp, err := rtspRequest(ctx, "OPTIONS", fmt.Sprintf("rtsp://%s:%d/", ip, port), "")
	if err != nil {
		return ""
	}
	if srv := resp.header["server"]; srv != "" {
		return srv
	}
	// No Server header: a login realm often names the software (go2rtc uses "go2rtc").
	if m := regexp.MustCompile(`realm="([^"]+)"`).FindStringSubmatch(resp.header["www-authenticate"]); m != nil {
		return m[1]
	}
	return ""
}

// describe returns the DESCRIBE status code (retrying with credentials on 401) and the
// first video codec in the SDP.
func describe(ctx context.Context, rawURL, username, password string) (int, string) {
	resp, err := rtspRequest(ctx, "DESCRIBE", rawURL, "")
	if err != nil {
		return 0, ""
	}
	if resp.code == 401 && username != "" {
		auth := authorization(resp.header["www-authenticate"], "DESCRIBE", rawURL, username, password)
		if auth == "" {
			return 401, ""
		}
		if resp, err = rtspRequest(ctx, "DESCRIBE", rawURL, auth); err != nil {
			return 0, ""
		}
	}
	return resp.code, sdpCodec(resp.body)
}

type rtspResponse struct {
	code   int
	header map[string]string
	body   string
}

func rtspRequest(ctx context.Context, method, rawURL, auth string) (*rtspResponse, error) {
	host := strings.TrimPrefix(rawURL, "rtsp://")
	host, _, _ = strings.Cut(host, "/")
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	req := method + " " + rawURL + " RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: nvr-lite\r\n"
	if method == "DESCRIBE" {
		req += "Accept: application/sdp\r\n"
	}
	if auth != "" {
		req += "Authorization: " + auth + "\r\n"
	}
	if _, err := io.WriteString(c, req+"\r\n"); err != nil {
		return nil, err
	}
	r := bufio.NewReader(c)
	status, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(status)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "RTSP/") {
		return nil, fmt.Errorf("not RTSP")
	}
	resp := &rtspResponse{header: map[string]string{}}
	resp.code, _ = strconv.Atoi(parts[1])
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return resp, nil
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		k, v, _ := strings.Cut(line, ":")
		k = strings.ToLower(strings.TrimSpace(k))
		if _, seen := resp.header[k]; !seen { // keep the first (Digest is usually first)
			resp.header[k] = strings.TrimSpace(v)
		}
	}
	if n, _ := strconv.Atoi(resp.header["content-length"]); n > 0 && n < 64<<10 {
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err == nil {
			resp.body = string(b)
		}
	}
	return resp, nil
}

var authParam = regexp.MustCompile(`(\w+)="?([^",]*)"?`)

// authorization builds a Digest or Basic Authorization header for a challenge.
func authorization(challenge, method, uri, username, password string) string {
	scheme, rest, _ := strings.Cut(challenge, " ")
	switch strings.ToLower(scheme) {
	case "basic":
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	case "digest":
		p := map[string]string{}
		for _, m := range authParam.FindAllStringSubmatch(rest, -1) {
			p[strings.ToLower(m[1])] = m[2]
		}
		h := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
		ha1 := h(username + ":" + p["realm"] + ":" + password)
		ha2 := h(method + ":" + uri)
		resp := h(ha1 + ":" + p["nonce"] + ":" + ha2)
		return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`, username, p["realm"], p["nonce"], uri, resp)
	}
	return ""
}

var rtpmapRe = regexp.MustCompile(`(?m)^a=rtpmap:\d+ ([A-Za-z0-9-]+)/90000`)

func sdpCodec(sdp string) string {
	if m := rtpmapRe.FindStringSubmatch(sdp); m != nil {
		switch strings.ToUpper(m[1]) {
		case "H264":
			return "h264"
		case "H265", "HEVC":
			return "hevc"
		default:
			return strings.ToLower(m[1])
		}
	}
	return ""
}
