// Package vision connects nvrd to nvr-vision (person + face analysis on another
// machine, Tahap E). nvrd asks it to watch a camera while there is motion in the
// camera's zones (Client.Watch); nvr-vision reports people and faces back to
// POST /api/v1/vision/report, which the Tracker turns into person events per zone and
// face samples.
package vision

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/notify"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// Client calls nvr-vision.
type Client struct {
	URL   string // e.g. http://192.168.1.13:8093
	Token string
	http  *http.Client
}

func NewClient(url, token string) *Client {
	return &Client{URL: url, Token: token, http: &http.Client{Timeout: 5 * time.Second}}
}

type zoneJSON struct {
	Kind   string        `json:"kind"`
	Points []store.Point `json:"points"`
}

// Watch starts or extends nvr-vision's session for a camera.
func (c *Client) Watch(ctx context.Context, cameraID int64, sub, main string, zones []store.Zone, seconds int) error {
	zs := make([]zoneJSON, 0, len(zones))
	for _, z := range zones {
		zs = append(zs, zoneJSON{Kind: z.Kind, Points: z.Points})
	}
	body, _ := json.Marshal(map[string]any{"camera_id": cameraID, "sub": sub, "main": main, "zones": zs, "seconds": seconds})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/watch", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nvr-vision: %s", resp.Status)
	}
	return nil
}

// Report is what nvr-vision posts for one analysed frame.
type Report struct {
	CameraID int64     `json:"camera_id"`
	At       float64   `json:"at"` // unix seconds
	Persons  []Person  `json:"persons"`
	Faces    []FaceHit `json:"faces"`
}

type Person struct {
	Zone  string     `json:"zone"`
	Box   [4]float64 `json:"box"`
	Score float64    `json:"score"`
}

type FaceHit struct {
	Zone      string     `json:"zone"`
	Box       [4]float64 `json:"box"`
	Score     float64    `json:"score"`
	Size      int        `json:"size"`
	Embedding []float32  `json:"embedding"`
	JPEG      string     `json:"jpeg"` // base64
}

// Tracker keeps one open person event per camera and zone while reports keep coming,
// and stores face samples (at most one per camera per FaceEvery).
type Tracker struct {
	Store   *store.Store
	DataDir string
	Log     *slog.Logger
	// A zone's person event ends after this long without a report for it.
	Quiet time.Duration
	// Minimum time between stored face samples of one camera.
	FaceEvery time.Duration
	// Arrival and loitering notifications (nil-safe); PublicURL for their links.
	Notifier  *notify.Notifier
	PublicURL string
	// Assistant gets door events (greetings, visitors at the gate); nil = off.
	Assistant *Assistant
	// Now is the clock (tests replace it); nil = time.Now.
	Now func() time.Time

	mu        sync.Mutex
	open      map[key]*presence
	lastFace  map[int64]time.Time
	gallery   *gallery
	visits    map[int64]*visit // person -> current visit
	loiters   map[int64]*loiter
	lastKnown time.Time // last time any known person was recognised
}

type key struct {
	camera int64
	zone   string
}

type presence struct {
	id       int64
	lastSeen time.Time
	peak     float64
}

// Run closes events whose zone went quiet, until ctx is cancelled.
func (t *Tracker) Run(ctx context.Context) {
	if err := t.Store.Events.CloseDanglingLabel(ctx, "person"); err != nil {
		t.Log.Warn("close dangling person events", "err", err)
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			t.closeAll(time.Now().Add(time.Hour))
			return
		case now := <-tick.C:
			t.closeAll(now)
		}
	}
}

func (t *Tracker) closeAll(now time.Time) {
	t.mu.Lock()
	var done []*presence
	for k, p := range t.open {
		if now.Sub(p.lastSeen) >= t.Quiet {
			done = append(done, p)
			delete(t.open, k)
		}
	}
	t.mu.Unlock()
	for _, p := range done {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := t.Store.Events.End(ctx, p.id, p.lastSeen.Unix(), p.peak); err != nil {
			t.Log.Warn("close person event", "id", p.id, "err", err)
		}
		cancel()
	}
}

// Handle records one report for a known camera.
func (t *Tracker) Handle(ctx context.Context, cam *store.Camera, r *Report) error {
	now := time.Now()
	if t.Now != nil {
		now = t.Now()
	}
	at := time.UnixMilli(int64(r.At * 1000))
	if d := now.Sub(at); d > time.Minute || d < -time.Minute {
		at = now // clocks of the two machines disagree: trust ours
	}
	best := map[string]float64{}
	for _, p := range r.Persons {
		if p.Zone != store.ZoneInside && p.Zone != store.ZoneOutside {
			continue
		}
		best[p.Zone] = max(best[p.Zone], p.Score)
	}
	// A face is seen only inside the fence, so it also means someone is there (person
	// detection flickers at night).
	for _, f := range r.Faces {
		if f.Zone == store.ZoneInside {
			best[f.Zone] = max(best[f.Zone], f.Score)
		}
	}
	for zone, score := range best {
		if err := t.seen(ctx, cam.ID, zone, score, at); err != nil {
			return err
		}
	}
	settings, err := t.Store.Settings.All(ctx)
	if err != nil {
		return err
	}
	if _, ok := best[store.ZoneOutside]; ok {
		t.outside(cam, at, settings)
	}
	f := bestFace(r.Faces)
	if f == nil {
		return nil
	}
	matchID, matchScore := t.recognize(ctx, cam, f, at, settings)
	return t.saveFace(ctx, cam, f, at, matchID, matchScore)
}

// bestFace picks the clearest face of a report (score × size).
func bestFace(faces []FaceHit) *FaceHit {
	var f *FaceHit
	for i := range faces {
		if len(faces[i].Embedding) == 128 && (f == nil || faces[i].Score*float64(faces[i].Size) > f.Score*float64(f.Size)) {
			f = &faces[i]
		}
	}
	return f
}

func (t *Tracker) seen(ctx context.Context, cameraID int64, zone string, score float64, at time.Time) error {
	k := key{cameraID, zone}
	t.mu.Lock()
	if t.open == nil {
		t.open = map[key]*presence{}
	}
	if p, ok := t.open[k]; ok {
		p.lastSeen, p.peak = at, max(p.peak, score)
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()
	id, err := t.Store.Events.StartInZone(ctx, cameraID, "person", zone, at.Unix())
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.open[k] = &presence{id: id, lastSeen: at, peak: score}
	t.mu.Unlock()
	t.Log.Info("person in zone", "camera_id", cameraID, "zone", zone, "score", score)
	return nil
}

func (t *Tracker) saveFace(ctx context.Context, cam *store.Camera, f *FaceHit, at time.Time, matchID int64, matchScore float64) error {
	t.mu.Lock()
	if t.lastFace == nil {
		t.lastFace = map[int64]time.Time{}
	}
	if at.Sub(t.lastFace[cam.ID]) < t.FaceEvery {
		t.mu.Unlock()
		return nil
	}
	t.lastFace[cam.ID] = at
	t.mu.Unlock()

	img, err := base64.StdEncoding.DecodeString(f.JPEG)
	if err != nil || len(img) < 4 || img[0] != 0xFF || img[1] != 0xD8 {
		return fmt.Errorf("face image is not a JPEG")
	}
	rel := filepath.Join("faces", cam.Slug, at.Format("2006-01-02"), fmt.Sprintf("%d.jpg", at.UnixMilli()))
	dst := filepath.Join(t.DataDir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(dst, img, 0o640); err != nil {
		return err
	}
	_, err = t.Store.Faces.Insert(ctx, &store.FaceSample{
		CameraID: cam.ID, At: at.UnixMilli(), Zone: f.Zone, Path: rel, Score: f.Score, Size: f.Size, Embedding: f.Embedding,
		MatchID:    sql.NullInt64{Int64: matchID, Valid: matchID != 0},
		MatchScore: sql.NullFloat64{Float64: matchScore, Valid: matchID != 0},
	})
	return err
}
