package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/auth"
	"github.com/tarkiman/cctv/nvr/server/internal/camera"
	"github.com/tarkiman/cctv/nvr/server/internal/config"
	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/go2rtc"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/sysinfo"
)

type env struct {
	srv    *httptest.Server
	client *http.Client
	store  *store.Store
	camID  int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "nvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	st := store.New(d)
	a := auth.New(st, false)
	if _, err := a.Bootstrap(ctx, "admin", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	camID, err := st.Cameras.CreateDiscovered(ctx, "bardi", "bardi", sql.NullString{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Jakarta")
	hub := sse.NewHub()
	tracker := health.New(hub)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		Config: &config.Config{DataDir: dir, Location: loc},
		Store:  st, Auth: a, Go2rtc: go2rtc.New("http://127.0.0.1:1"), Health: tracker,
		// Not started: reloads after a settings change are no-ops.
		Cameras: &camera.Manager{Store: st, Health: tracker, Log: logger},
		Hub:     hub, Sys: &sysinfo.Sampler{}, Web: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}},
		Log: logger,
	}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &env{srv: ts, client: &http.Client{Jar: jar}, store: st, camID: camID}
}

func (e *env) do(t *testing.T, method, path, body string, csrf bool) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if csrf {
		req.Header.Set(auth.CSRFHeader, auth.CSRFValue)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *env) login(t *testing.T) {
	t.Helper()
	if r := e.do(t, "POST", "/api/v1/auth/login", `{"username":"admin","password":"correct-horse"}`, true); r.StatusCode != 200 {
		t.Fatalf("login status %d", r.StatusCode)
	}
}

func TestAuthFlow(t *testing.T) {
	e := newEnv(t)
	if r := e.do(t, "GET", "/api/v1/cameras", "", false); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated cameras = %d, want 401", r.StatusCode)
	}
	if r := e.do(t, "POST", "/api/v1/auth/login", `{"username":"admin","password":"correct-horse"}`, false); r.StatusCode != http.StatusForbidden {
		t.Fatalf("login without CSRF header = %d, want 403", r.StatusCode)
	}
	for i := 0; i < 5; i++ {
		if r := e.do(t, "POST", "/api/v1/auth/login", `{"username":"admin","password":"wrong"}`, true); r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("bad password attempt %d = %d", i, r.StatusCode)
		}
	}
	if r := e.do(t, "POST", "/api/v1/auth/login", `{"username":"admin","password":"correct-horse"}`, true); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures = %d, want 429", r.StatusCode)
	}
}

func TestCamerasAndTimeline(t *testing.T) {
	e := newEnv(t)
	e.login(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Jakarta")
	day := time.Date(2026, 11, 4, 0, 0, 0, 0, loc)
	// Two contiguous segments, a gap, then one more; plus one segment crossing midnight.
	for i, off := range []time.Duration{-5 * time.Minute, 10 * time.Hour, 10*time.Hour + 10*time.Minute + 2*time.Second, 12 * time.Hour} {
		start := day.Add(off)
		r := &store.Recording{CameraID: e.camID, StartAt: start.Unix(), EndAt: start.Add(10 * time.Minute).Unix(),
			Path: filepath.Join("rec", string(rune('a'+i))), SizeBytes: 1}
		if err := e.store.Recordings.Insert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	r := e.do(t, "GET", "/api/v1/cameras/1/timeline?date=2026-11-04", "", false)
	if r.StatusCode != 200 {
		t.Fatalf("timeline status %d", r.StatusCode)
	}
	var tl struct {
		Ranges []struct{ Start, End time.Time } `json:"ranges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&tl); err != nil {
		t.Fatal(err)
	}
	if len(tl.Ranges) != 3 {
		t.Fatalf("ranges = %d, want 3 (clipped midnight, merged pair, single): %+v", len(tl.Ranges), tl.Ranges)
	}
	if !tl.Ranges[0].Start.Equal(day) {
		t.Fatalf("first range not clipped to local midnight: %v", tl.Ranges[0].Start)
	}
	if got := tl.Ranges[1].End.Sub(tl.Ranges[1].Start); got != 20*time.Minute+2*time.Second {
		t.Fatalf("merged range length = %v", got)
	}

	if r := e.do(t, "PATCH", "/api/v1/cameras/1", `{"retention_days":0}`, true); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid retention accepted: %d", r.StatusCode)
	}
	if r := e.do(t, "PATCH", "/api/v1/cameras/1", `{"record_stream":"sub"}`, true); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("sub record stream without sub-stream accepted: %d", r.StatusCode)
	}

	// Audio is recorded by default and can be switched off per camera.
	for _, want := range []bool{true, false} {
		var cam struct {
			RecordAudio bool `json:"record_audio"`
		}
		r := e.do(t, "GET", "/api/v1/cameras/1", "", false)
		if err := json.NewDecoder(r.Body).Decode(&cam); err != nil || cam.RecordAudio != want {
			t.Fatalf("record_audio = %v (%v), want %v", cam.RecordAudio, err, want)
		}
		if want {
			if r := e.do(t, "PATCH", "/api/v1/cameras/1", `{"record_audio":false}`, true); r.StatusCode != 200 {
				t.Fatalf("switch audio off: %d", r.StatusCode)
			}
		}
	}
}

func TestSPAFallback(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, "GET", "/camera/bardi", "", false)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(body), "app") {
		t.Fatalf("SPA fallback = %d %q", r.StatusCode, body)
	}
	if r.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP header")
	}
}
