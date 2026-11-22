package vision

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/notify"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// unit returns a normalised embedding pointing mostly along axis i (with a little of
// axis j, so different faces of one person are alike but not identical).
func unit(i, j int, w float64) []float32 {
	v := make([]float32, 128)
	v[i] = float32(1)
	v[j] = float32(w)
	n := math.Sqrt(1 + w*w)
	for k := range v {
		v[k] /= float32(n)
	}
	return v
}

type hook struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (h *hook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var m notify.Message
	json.NewDecoder(r.Body).Decode(&m)
	h.mu.Lock()
	h.msgs = append(h.msgs, m)
	h.mu.Unlock()
}

func (h *hook) kinds() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, m := range h.msgs {
		out = append(out, m.Kind)
	}
	return out
}

func waitKinds(t *testing.T, h *hook, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.kinds()) < n && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // and nothing more
	return h.kinds()
}

func TestRecognitionArrivalOncePerVisit(t *testing.T) {
	tr, cam := newTracker(t)
	ctx := context.Background()
	h := &hook{}
	srv := httptest.NewServer(h)
	defer srv.Close()
	tr.Notifier = notify.New([]string{srv.URL}, time.UTC, "", slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Arrival notifications are off by default (spike S12); this installation has them.
	tr.Store.Settings.Set(ctx, store.SettingFaceNotify, 1)
	tr.Store.Settings.Set(ctx, store.SettingFaceMatch, 40)
	// Label one sample of "Ayah".
	p, _ := tr.Store.People.Ensure(ctx, "Ayah")
	id, _ := tr.Store.Faces.Insert(ctx, &store.FaceSample{CameraID: cam.ID, At: 1, Zone: "inside", Path: "x.jpg", Score: 0.9, Size: 100, Embedding: unit(0, 1, 0.3)})
	tr.Store.Faces.Label(ctx, []int64{id}, p.ID)
	tr.InvalidateGallery()

	t0 := time.Now()
	var clock time.Time
	tr.Now = func() time.Time { return clock }
	face := func(at time.Time, emb []float32) *Report {
		clock = at
		return &Report{CameraID: cam.ID, At: float64(at.UnixMilli()) / 1000,
			Faces: []FaceHit{{Zone: "inside", Score: 0.8, Size: 90, Embedding: emb, JPEG: jpeg}}}
	}
	// A stranger: never announced.
	tr.Handle(ctx, cam, face(t0, unit(5, 6, 0.3)))
	tr.Handle(ctx, cam, face(t0.Add(2*time.Second), unit(5, 6, 0.3)))
	// Ayah, a borderline match twice within 20 s: one arrival. More frames, still one.
	for i := 0; i < 4; i++ {
		tr.Handle(ctx, cam, face(t0.Add(time.Duration(10+i*2)*time.Second), unit(0, 2, 0.6)))
	}
	if got := waitKinds(t, h, 1); len(got) != 1 || got[0] != "person_arrived" {
		t.Fatalf("notifications = %v, want one arrival", got)
	}
	// Same visit 10 minutes later: nothing. After the 30 min gap: a new arrival.
	tr.Handle(ctx, cam, face(t0.Add(10*time.Minute), unit(0, 1, 0.3)))
	if got := waitKinds(t, h, 2); len(got) != 1 {
		t.Fatalf("re-notified within a visit: %v", got)
	}
	later := t0.Add(50 * time.Minute)
	tr.Handle(ctx, cam, face(later, unit(0, 1, 0.3)))
	tr.Handle(ctx, cam, face(later.Add(time.Second), unit(0, 1, 0.3)))
	if got := waitKinds(t, h, 2); len(got) != 2 {
		t.Fatalf("no arrival for a new visit: %v", got)
	}
	h.mu.Lock()
	title := h.msgs[0].Title
	h.mu.Unlock()
	if title != "👤 Ayah masuk rumah" {
		t.Fatalf("title %q", title)
	}

	// Person with notify off: recognised, not announced.
	pp, _ := tr.Store.People.ByID(ctx, p.ID)
	pp.Notify = false
	tr.Store.People.Update(ctx, pp)
	tr.InvalidateGallery()
	much := t0.Add(3 * time.Hour)
	tr.Handle(ctx, cam, face(much, unit(0, 1, 0.3)))
	tr.Handle(ctx, cam, face(much.Add(time.Second), unit(0, 1, 0.3)))
	if got := waitKinds(t, h, 3); len(got) != 2 {
		t.Fatalf("announced a person with notify off: %v", got)
	}
}

func TestLoitering(t *testing.T) {
	tr, cam := newTracker(t)
	ctx := context.Background()
	h := &hook{}
	srv := httptest.NewServer(h)
	defer srv.Close()
	tr.Notifier = notify.New([]string{srv.URL}, time.UTC, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	out := func(at time.Time) {
		tr.Handle(ctx, cam, &Report{CameraID: cam.ID, At: float64(at.UnixMilli()) / 1000, Persons: []Person{{Zone: "outside", Score: 0.5}}})
	}
	// Drive outside() directly with synthetic times; Handle is covered at the end.
	settings, _ := tr.Store.Settings.All(ctx)
	base := time.Now()
	// Passers-by: 10 s now and then; never 60 s within 3 minutes.
	for i := 0; i < 10; i++ {
		s := base.Add(time.Duration(i) * 5 * time.Minute)
		for k := 0; k <= 10; k += 2 {
			tr.outside(cam, s.Add(time.Duration(k)*time.Second), settings)
		}
	}
	if got := waitKinds(t, h, 1); len(got) != 0 {
		t.Fatalf("passers-by alerted: %v", got)
	}
	// Someone pacing: seen every 3 s (gaps up to 6 s merge) for 70 s.
	s := base.Add(2 * time.Hour)
	for k := 0; k <= 70; k += 3 {
		tr.outside(cam, s.Add(time.Duration(k)*time.Second), settings)
	}
	if got := waitKinds(t, h, 1); len(got) != 1 || got[0] != "loitering" {
		t.Fatalf("loitering not alerted once: %v", got)
	}
	// Still there: no second alert within the cooldown.
	for k := 72; k <= 200; k += 3 {
		tr.outside(cam, s.Add(time.Duration(k)*time.Second), settings)
	}
	if got := waitKinds(t, h, 2); len(got) != 1 {
		t.Fatalf("alert repeated within cooldown: %v", got)
	}
	// A known person seen recently suppresses it (family chatting at the gate).
	s2 := base.Add(5 * time.Hour)
	tr.mu.Lock()
	tr.lastKnown = s2
	tr.mu.Unlock()
	for k := 0; k <= 90; k += 3 {
		tr.outside(cam, s2.Add(time.Duration(k)*time.Second), settings)
	}
	if got := waitKinds(t, h, 2); len(got) != 1 {
		t.Fatalf("alerted while a known person was around: %v", got)
	}
	out(time.Now()) // through Handle: no panic, opens an outside event
}

func TestCluster(t *testing.T) {
	var samples []store.FaceSample
	for i := 0; i < 5; i++ {
		samples = append(samples, store.FaceSample{ID: int64(i), Score: 0.8, Size: 90, Embedding: unit(0, 1+i, 0.2)})
	}
	for i := 0; i < 3; i++ {
		samples = append(samples, store.FaceSample{ID: int64(10 + i), Score: 0.8, Size: 90, Embedding: unit(50, 51+i, 0.2)})
	}
	cs := Cluster(samples, 0.45)
	if len(cs) != 2 || len(cs[0]) != 5 || len(cs[1]) != 3 {
		t.Fatalf("clusters = %d (%d, ...)", len(cs), len(cs[0]))
	}
	if s := Similarity(Centroid(cs[0]), unit(0, 1, 0.2)); s < 0.9 {
		t.Fatalf("centroid similarity %.2f", s)
	}
}

// TestAssistantEvents: a known arrival is greeted once; a stranger standing at the gate
// is asked once (not a passer-by, not while family was just seen).
func TestAssistantEvents(t *testing.T) {
	tr, cam := newTracker(t)
	ctx := context.Background()
	var mu sync.Mutex
	var got []AssistantEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e AssistantEvent
		json.NewDecoder(r.Body).Decode(&e)
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}))
	defer srv.Close()
	tr.Assistant = NewAssistant(srv.URL+"/cctv?token=x", slog.New(slog.NewTextHandler(io.Discard, nil)))
	events := func(n int) []AssistantEvent {
		deadline := time.Now().Add(3 * time.Second)
		for {
			mu.Lock()
			out := append([]AssistantEvent(nil), got...)
			mu.Unlock()
			if len(out) >= n || time.Now().After(deadline) {
				time.Sleep(50 * time.Millisecond)
				mu.Lock()
				out = append([]AssistantEvent(nil), got...)
				mu.Unlock()
				return out
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	settings, _ := tr.Store.Settings.All(ctx)
	base := time.Now()

	// A passer-by (6 s) is not asked.
	for k := 0; k <= 6; k += 2 {
		tr.outside(cam, base.Add(time.Duration(k)*time.Second), settings)
	}
	if e := events(1); len(e) != 0 {
		t.Fatalf("passer-by asked: %+v", e)
	}
	// Standing at the gate 16 s: asked once, not again within the cooldown.
	s := base.Add(10 * time.Minute)
	for k := 0; k <= 40; k += 2 {
		tr.outside(cam, s.Add(time.Duration(k)*time.Second), settings)
	}
	e := events(1)
	if len(e) != 1 || e[0].Kind != "visitor" || e[0].Seconds < 15 || e[0].Camera != cam.Name {
		t.Fatalf("visitor events = %+v", e)
	}

	// A known arrival is greeted by name (with the Discord arrival notice off).
	p, _ := tr.Store.People.Ensure(ctx, "Abi")
	id, _ := tr.Store.Faces.Insert(ctx, &store.FaceSample{CameraID: cam.ID, At: 1, Zone: "inside", Path: "x.jpg", Score: 0.9, Size: 100, Embedding: unit(3, 4, 0.2)})
	tr.Store.Faces.Label(ctx, []int64{id}, p.ID)
	tr.InvalidateGallery()
	settings, _ = tr.Store.Settings.All(ctx)
	at := base.Add(time.Hour)
	f := &FaceHit{Zone: "inside", Score: 0.9, Size: 100, Embedding: unit(3, 4, 0.2), JPEG: jpeg}
	tr.recognize(ctx, cam, f, at, settings)
	tr.recognize(ctx, cam, f, at.Add(time.Second), settings)
	e = events(2)
	if len(e) != 2 || e[1].Kind != "person_arrived" || e[1].Name != "Abi" {
		t.Fatalf("arrival events = %+v", e)
	}
	// Family just seen: a stranger at the gate is not asked.
	for k := 0; k <= 40; k += 2 {
		tr.outside(cam, at.Add(time.Duration(30+k)*time.Second), settings)
	}
	if e := events(3); len(e) != 2 {
		t.Fatalf("asked while family was around: %+v", e)
	}
}
