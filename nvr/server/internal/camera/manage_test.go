package camera

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/go2rtc"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// fakeGo2rtc keeps streams in memory and serves the endpoints the manager uses.
type fakeGo2rtc struct {
	mu      sync.Mutex
	streams map[string]string
}

func (f *fakeGo2rtc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/config":
		out, _ := yaml.Marshal(map[string]any{"streams": f.streams})
		w.Write(out)
	case r.URL.Path == "/api/streams" && r.Method == http.MethodGet:
		m := map[string]go2rtc.Stream{}
		for k := range f.streams {
			m[k] = go2rtc.Stream{}
		}
		json.NewEncoder(w).Encode(m)
	case r.URL.Path == "/api/streams" && r.Method == http.MethodPut:
		f.streams[r.URL.Query().Get("name")] = r.URL.Query().Get("src")
	case r.URL.Path == "/api/streams" && r.Method == http.MethodDelete:
		delete(f.streams, r.URL.Query().Get("src"))
	default:
		http.NotFound(w, r)
	}
}

func TestAddEditRemoveCamera(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "nvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fake := &fakeGo2rtc{streams: map[string]string{"c301": "xiaomi://u:p@192.168.1.4?did=1"}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	m := &Manager{
		Store: store.New(d), Go2rtc: go2rtc.New(srv.URL), Health: health.New(sse.NewHub()),
		DataDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	cam, err := m.Add(ctx, "Garasi", "Garasi Depan", "rtsp://admin:a%40b@192.168.1.30:554/main", "rtsp://admin:a%40b@192.168.1.30:554/sub")
	if err != nil {
		t.Fatal(err)
	}
	if cam.Slug != "garasi" || cam.Name != "Garasi Depan" || !cam.SubStream.Valid {
		t.Fatalf("camera = %+v", cam)
	}
	if _, err := m.Add(ctx, "garasi", "", "rtsp://1.2.3.4/x", ""); err != ErrSlugTaken {
		t.Fatalf("duplicate slug: %v", err)
	}
	if _, err := m.Add(ctx, "x_sub", "", "rtsp://1.2.3.4/x", ""); err != ErrSlugInvalid {
		t.Fatalf("_sub slug: %v", err)
	}

	// IP changed: keep the password, drop the sub-stream.
	main, err := m.ResolveSource(ctx, cam, "main", SourceInput{URL: "rtsp://192.168.1.31:554/main", Username: "admin"})
	if err != nil || main != "rtsp://admin:a%40b@192.168.1.31:554/main" {
		t.Fatalf("resolve = %q, %v", main, err)
	}
	if err := m.SetSources(ctx, cam, main, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.streams["garasi_sub"]; ok || fake.streams["garasi"] != main {
		t.Fatalf("streams after edit = %v", fake.streams)
	}
	src, _ := m.Sources(ctx, cam)
	if !src.Editable || src.Sub != nil || src.Main.URL != "rtsp://192.168.1.31:554/main" {
		t.Fatalf("sources = %+v", src)
	}

	// Xiaomi cameras are read-only here.
	x, _ := m.Store.Cameras.BySlug(ctx, "c301")
	if xs, _ := m.Sources(ctx, x); xs.Editable || !xs.Xiaomi || xs.Main != nil {
		t.Fatalf("xiaomi sources = %+v", xs)
	}
	if err := m.Remove(ctx, x); err != ErrNotEditable {
		t.Fatalf("remove xiaomi: %v", err)
	}

	os.MkdirAll(filepath.Join(dir, "recordings", "garasi", "2026-11-04"), 0o755)
	cam, _ = m.Store.Cameras.BySlug(ctx, "garasi")
	if err := m.Remove(ctx, cam); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.streams["garasi"]; ok {
		t.Fatal("stream not removed from go2rtc")
	}
	if _, err := os.Stat(filepath.Join(dir, "recordings", "garasi")); !os.IsNotExist(err) {
		t.Fatal("recordings not removed")
	}
	if _, err := m.Store.Cameras.BySlug(ctx, "garasi"); err == nil {
		t.Fatal("camera row not removed")
	}
}
