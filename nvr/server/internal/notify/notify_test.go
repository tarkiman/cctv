package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/supervisor"
)

type sink struct {
	mu   sync.Mutex
	msgs []Message
}

func (s *sink) handler(w http.ResponseWriter, r *http.Request) {
	var m Message
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.msgs = append(s.msgs, m)
	s.mu.Unlock()
}

func (s *sink) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.msgs {
		out = append(out, m.Kind)
	}
	return out
}

func TestWatcherAlertsOncePerOutage(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "nvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st := store.New(d)
	id, err := st.Cameras.CreateDiscovered(ctx, "c301", "c301", sql.NullString{}, 0)
	if err != nil {
		t.Fatal(err)
	}

	var got sink
	srv := httptest.NewServer(http.HandlerFunc(got.handler))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := health.New(sse.NewHub())
	tr.SetXiaomi(id, true)
	var diskFree uint64 = 50
	clock := time.Unix(1_800_000_000, 0)
	w := &Watcher{
		Notifier: New([]string{srv.URL}, nil, "test", log), Store: st, Health: tr,
		Disk:         func() (uint64, uint64) { return 100, diskFree },
		OfflineAfter: 5 * time.Minute, Log: log,
		now: func() time.Time { return clock },
	}
	step := func(d time.Duration) { clock = clock.Add(d); w.Check(ctx) }

	tr.SetState(id, supervisor.Running, "")
	step(0)
	tr.SetState(id, supervisor.Offline, "Sesi Xiaomi kemungkinan kedaluwarsa")
	step(time.Minute)     // outage starts
	step(4 * time.Minute) // 4 min: still quiet
	if k := got.kinds(); len(k) != 0 {
		t.Fatalf("alerted too early: %v", k)
	}
	step(time.Minute) // 5 min: alert
	step(time.Minute) // no repeat
	tr.SetState(id, supervisor.Running, "")
	step(time.Minute) // recovered

	diskFree = 4 // below half of the 10% floor
	step(time.Minute)
	step(time.Minute)
	diskFree = 20
	step(time.Minute)

	want := []string{"camera_offline", "camera_recovered", "disk_low", "disk_ok"}
	k := got.kinds()
	if len(k) != len(want) {
		t.Fatalf("kinds = %v, want %v", k, want)
	}
	for i := range want {
		if k[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", k, want)
		}
	}
	if got.msgs[0].Title != "🔑 c301: sesi Xiaomi perlu login ulang" {
		t.Errorf("title = %q", got.msgs[0].Title)
	}
}

func TestDiscordPayloadAndRedact(t *testing.T) {
	u := "https://discord.com/api/webhooks/123/secret-token"
	n := New([]string{u}, nil, "f", nil)
	b, ct, err := n.discord(Message{Title: "x", Level: Alert, At: time.Unix(0, 0)})
	if err != nil || ct != "application/json" {
		t.Fatal(err, ct)
	}
	var p struct {
		Embeds []struct{ Title string } `json:"embeds"`
	}
	if json.Unmarshal(b, &p); len(p.Embeds) != 1 || p.Embeds[0].Title != "x" {
		t.Fatalf("discord payload = %s", b)
	}
	if r := redact(u); r != "https://discord.com/…" {
		t.Errorf("redact = %q", r)
	}
	if isDiscord("https://example.com/api/webhooks/1") {
		t.Error("non-discord host treated as discord")
	}
}

func TestGowaRequest(t *testing.T) {
	var gotAuth, gotQuery string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		gotAuth, gotQuery = u+":"+p, r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	u := strings.Replace(srv.URL, "http://", "http://wa:s3cret@", 1) + "/send/message?phone=6281200000000"
	n := New([]string{u}, nil, "f", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := n.Send(context.Background(), Message{Title: "Judul", Text: "Isi", URL: "https://x"}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "wa:s3cret" || gotQuery != "" {
		t.Errorf("auth=%q query=%q", gotAuth, gotQuery)
	}
	if body["phone"] != "6281200000000" || body["message"] != "*Judul*\nIsi\nhttps://x" {
		t.Errorf("body = %v", body)
	}
	if strings.Contains(redact(u), "s3cret") {
		t.Error("redact leaks password")
	}
}
