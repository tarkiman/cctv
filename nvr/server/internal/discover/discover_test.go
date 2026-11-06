package discover

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
)

func TestHosts(t *testing.T) {
	h := hosts("192.168.1.20/24")
	if len(h) != 253 || h[0] != "192.168.1.1" || h[len(h)-1] != "192.168.1.254" || contains(h, "192.168.1.20") {
		t.Fatalf("hosts: %d first=%s last=%s", len(h), h[0], h[len(h)-1])
	}
	if hostOf("http://192.168.1.27:10000/onvif/device_service http://[fe80::1]/x") != "192.168.1.27" {
		t.Fatal("hostOf")
	}
	if scope("onvif://www.onvif.org/name/IP-Camera onvif://www.onvif.org/hardware/HI3518C", "hardware") != "HI3518C" {
		t.Fatal("scope")
	}
}

// fakeCamera answers DESCRIBE like a Digest-protected camera that only serves
// /stream1 (H.264) and /stream2.
func fakeCamera(t *testing.T) (string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				line, _ := r.ReadString('\n')
				authed := false
				for {
					h, err := r.ReadString('\n')
					if err != nil || h == "\r\n" {
						break
					}
					if strings.HasPrefix(h, "Authorization: Digest") && strings.Contains(h, `username="admin"`) {
						authed = true
					}
				}
				f := strings.Fields(line)
				path := strings.SplitN(strings.TrimPrefix(f[1], "rtsp://"), "/", 2)[1]
				switch {
				case f[0] == "OPTIONS":
					fmt.Fprint(c, "RTSP/1.0 200 OK\r\nCSeq: 1\r\nServer: FakeCam/1.0\r\n\r\n")
				case path != "stream1" && path != "stream2":
					fmt.Fprint(c, "RTSP/1.0 404 Not Found\r\nCSeq: 1\r\n\r\n")
				case !authed:
					fmt.Fprint(c, "RTSP/1.0 401 Unauthorized\r\nCSeq: 1\r\nWWW-Authenticate: Digest realm=\"cam\", nonce=\"abc\"\r\n\r\n")
				default:
					sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n"
					fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: 1\r\nContent-Length: %d\r\n\r\n%s", len(sdp), sdp)
				}
			}(c)
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func TestGuessPaths(t *testing.T) {
	ip, port := fakeCamera(t)
	ctx := context.Background()
	if r := GuessPaths(ctx, ip, port, "", ""); r.Guess != nil || !r.NeedsAuth {
		t.Fatalf("without credentials: %+v", r)
	}
	r := GuessPaths(ctx, ip, port, "admin", "secret")
	if r.Guess == nil || !strings.HasSuffix(r.Guess.Main, "/stream1") || !strings.HasSuffix(r.Guess.Sub, "/stream2") || r.Guess.Codec != "h264" {
		t.Fatalf("with credentials: %+v %+v", r, r.Guess)
	}
	if srv := rtspServer(ctx, ip, port); srv != "FakeCam/1.0" {
		t.Fatalf("server = %q", srv)
	}
}

// TestLiveScan scans the real LAN: NVR_DISCOVER_LIVE=1 go test -run TestLiveScan -v ./internal/discover
func TestLiveScan(t *testing.T) {
	if os.Getenv("NVR_DISCOVER_LIVE") == "" {
		t.Skip("set NVR_DISCOVER_LIVE=1")
	}
	subnets := LocalSubnets()
	t.Logf("subnets: %+v", subnets)
	devs, err := Scan(context.Background(), subnets)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		t.Logf("%+v onvif=%+v", d, d.ONVIF)
	}
	if xaddr, user, pass := os.Getenv("ONVIF_XADDR"), os.Getenv("ONVIF_USER"), os.Getenv("ONVIF_PASS"); xaddr != "" {
		streams, err := ONVIFStreams(context.Background(), xaddr, user, pass)
		t.Logf("onvif streams: %+v err=%v", streams, err)
	}
}
