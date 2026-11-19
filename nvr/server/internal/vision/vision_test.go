package vision

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

func newTracker(t *testing.T) (*Tracker, *store.Camera) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "nvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	st := store.New(d)
	id, err := st.Cameras.CreateDiscovered(ctx, "bardi", "bardi", sql.NullString{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	cam, _ := st.Cameras.ByID(ctx, id)
	return &Tracker{Store: st, DataDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Quiet: 10 * time.Second, FaceEvery: time.Second}, cam
}

var jpeg = base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0xFF, 0xD9})

func emb() []float32 {
	v := make([]float32, 128)
	v[0] = 1
	return v
}

// TestTrackerEventsAndFaces: one event per zone while reports continue, closed after
// the quiet period; face samples throttled to one per second.
func TestTrackerEventsAndFaces(t *testing.T) {
	tr, cam := newTracker(t)
	ctx := context.Background()
	t0 := time.Now().Truncate(time.Second)
	for i := 0; i < 6; i++ { // 6 reports, 0.5 s apart
		at := t0.Add(time.Duration(i) * 500 * time.Millisecond)
		r := &Report{CameraID: cam.ID, At: float64(at.UnixMilli()) / 1000,
			Persons: []Person{{Zone: "inside", Score: 0.6 + float64(i)/100}, {Zone: "outside", Score: 0.5}, {Zone: "nonsense"}},
			Faces:   []FaceHit{{Zone: "inside", Score: 0.9, Size: 90, Embedding: emb(), JPEG: jpeg}}}
		if err := tr.Handle(ctx, cam, r); err != nil {
			t.Fatal(err)
		}
	}
	evs, _ := tr.Store.Events.Range(ctx, cam.ID, t0.Unix()-10, t0.Unix()+10)
	if len(evs) != 2 {
		t.Fatalf("events = %d, want one per zone", len(evs))
	}
	if n, _ := tr.Store.Faces.Count(ctx, cam.ID); n != 3 {
		t.Fatalf("face samples = %d, want 3 (one per second over 2.5 s)", n)
	}
	files, _ := filepath.Glob(filepath.Join(tr.DataDir, "faces", "bardi", "*", "*.jpg"))
	if len(files) != 3 {
		t.Fatalf("face files = %d", len(files))
	}

	tr.closeAll(t0.Add(5 * time.Second)) // not quiet long enough yet
	if len(tr.open) != 2 {
		t.Fatal("closed too early")
	}
	tr.closeAll(t0.Add(time.Minute))
	evs, _ = tr.Store.Events.Range(ctx, cam.ID, t0.Unix()-10, t0.Unix()+10)
	for _, e := range evs {
		if !e.EndAt.Valid || !e.Zone.Valid || e.Label != "person" {
			t.Fatalf("event not closed or missing zone: %+v", e)
		}
		if e.Zone.String == "inside" && e.Score.Float64 < 0.64 {
			t.Fatalf("peak score not kept: %v", e.Score.Float64)
		}
	}

	// Not a JPEG: refused, nothing stored.
	bad := &Report{CameraID: cam.ID, At: float64(t0.Add(30 * time.Second).Unix()), Faces: []FaceHit{{Zone: "inside", Score: 1, Size: 99, Embedding: emb(), JPEG: "aGVsbG8="}}}
	if err := tr.Handle(ctx, cam, bad); err == nil {
		t.Fatal("non-JPEG face accepted")
	}
}

func TestClientWatch(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/watch" || r.Header.Get("Authorization") != "Bearer secret-token-123456789012345" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "secret-token-123456789012345")
	zones := []store.Zone{{Kind: "inside", Points: []store.Point{{0, 0.5}, {1, 0.5}, {1, 1}}}}
	if err := c.Watch(context.Background(), 1, "bardi_sub", "bardi", zones, 20); err != nil {
		t.Fatal(err)
	}
	if got["sub"] != "bardi_sub" || got["main"] != "bardi" || len(got["zones"].([]any)) != 1 {
		t.Fatalf("watch body = %v", got)
	}
	if err := NewClient(srv.URL, "wrong").Watch(context.Background(), 1, "a", "b", nil, 20); err == nil {
		t.Fatal("wrong token accepted")
	}
}
