package vision

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/notify"
	"github.com/tarkiman/cctv/nvr/server/internal/snapshot"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// Recognition (Tahap E step 3): faces are matched against the samples the admin
// labelled in the face gallery. A known person's arrival is announced once per visit;
// an unknown person staying in front of the fence raises a loitering alert.

// Similarity is the cosine similarity of two L2-normalised embeddings.
func Similarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

type gallery struct {
	people  map[int64]store.Person
	samples map[int64][][]float32 // person -> embeddings
}

// InvalidateGallery makes the next match reload the labelled samples (after the admin
// labels, unlabels or deletes samples or people).
func (t *Tracker) InvalidateGallery() {
	t.mu.Lock()
	t.gallery = nil
	t.mu.Unlock()
}

func (t *Tracker) loadGallery(ctx context.Context) (*gallery, error) {
	t.mu.Lock()
	g := t.gallery
	t.mu.Unlock()
	if g != nil {
		return g, nil
	}
	people, err := t.Store.People.List(ctx)
	if err != nil {
		return nil, err
	}
	samples, err := t.Store.Faces.Labeled(ctx)
	if err != nil {
		return nil, err
	}
	g = &gallery{people: map[int64]store.Person{}, samples: map[int64][][]float32{}}
	for _, p := range people {
		g.people[p.ID] = p
	}
	for _, s := range samples {
		if _, ok := g.people[s.PersonID.Int64]; ok && len(s.Embedding) == 128 {
			g.samples[s.PersonID.Int64] = append(g.samples[s.PersonID.Int64], s.Embedding)
		}
	}
	t.mu.Lock()
	t.gallery = g
	t.mu.Unlock()
	return g, nil
}

// Match returns the best matching person and its similarity (the person's closest
// sample), or 0 when nobody reaches the threshold.
func (g *gallery) Match(emb []float32, threshold float64) (int64, float64) {
	var best int64
	bestScore := 0.0
	for id, embs := range g.samples {
		for _, e := range embs {
			if s := Similarity(emb, e); s > bestScore {
				best, bestScore = id, s
			}
		}
	}
	if bestScore < threshold {
		return 0, bestScore
	}
	return best, bestScore
}

// Suggest is Match for the face gallery UI (no threshold on the score returned).
func (t *Tracker) Suggest(ctx context.Context, emb []float32) (store.Person, float64, bool) {
	g, err := t.loadGallery(ctx)
	if err != nil {
		return store.Person{}, 0, false
	}
	id, score := g.Match(emb, 0)
	p, ok := g.people[id]
	return p, score, ok
}

type visit struct {
	lastSeen time.Time
	hits     []time.Time
	notified bool
}

type loiter struct {
	spans     [][2]time.Time // merged periods with someone outside
	lastAlert time.Time
}

// recognize matches the report's best face and handles arrivals. It returns the match
// to store with the sample.
func (t *Tracker) recognize(ctx context.Context, cam *store.Camera, f *FaceHit, at time.Time, settings map[string]int) (int64, float64) {
	g, err := t.loadGallery(ctx)
	if err != nil || len(g.samples) == 0 {
		return 0, 0
	}
	threshold := float64(settings[store.SettingFaceMatch]) / 100
	id, score := g.Match(f.Embedding, threshold)
	if id == 0 {
		return 0, 0
	}
	person := g.people[id]
	gap := time.Duration(settings[store.SettingFaceVisitMin]) * time.Minute

	t.mu.Lock()
	if t.visits == nil {
		t.visits = map[int64]*visit{}
	}
	v := t.visits[id]
	if v == nil || at.Sub(v.lastSeen) > gap {
		v = &visit{} // a new visit
		t.visits[id] = v
	}
	v.lastSeen = at
	t.lastKnown = at
	// Two matches within 20 s, or one clear match, make an arrival: a single borderline
	// frame (a look-alike, a blurred face) does not.
	recent := v.hits[:0]
	for _, h := range v.hits {
		if at.Sub(h) <= 20*time.Second {
			recent = append(recent, h)
		}
	}
	v.hits = append(recent, at)
	arrive := !v.notified && (len(v.hits) >= 2 || score >= threshold+0.1)
	if arrive {
		v.notified = true
	}
	t.mu.Unlock()

	if arrive {
		t.Log.Info("known person arrived", "person", person.Name, "camera", cam.Slug, "score", math.Round(score*100)/100)
		if person.Notify && settings[store.SettingFaceNotify] == 1 {
			t.notifyArrival(cam, person, score, f, at)
		}
	}
	return id, score
}

func (t *Tracker) notifyArrival(cam *store.Camera, p store.Person, score float64, f *FaceHit, at time.Time) {
	if !t.Notifier.Enabled() {
		return
	}
	m := notify.Message{
		Kind: "person_arrived", Level: notify.Info, Camera: cam.Name, At: at.UTC(),
		Title: "👤 " + p.Name + " masuk rumah",
		Text:  fmt.Sprintf("Dikenali di kamera %s (kemiripan wajah %d%%).", cam.Name, int(math.Round(score*100))),
		URL:   t.link("/camera/" + cam.Slug + "?mode=rekaman"),
		Fields: []notify.Field{
			{Name: "Nama", Value: p.Name, Inline: true},
			{Name: "Kamera", Value: cam.Name, Inline: true},
			{Name: "Waktu", At: &at, Inline: true},
		},
	}
	// The face crop is in the report; give it to the notifier as a file.
	if img, err := base64.StdEncoding.DecodeString(f.JPEG); err == nil {
		tmp := filepath.Join(t.DataDir, "faces", ".notify", fmt.Sprintf("%d.jpg", at.UnixMilli()))
		if os.MkdirAll(filepath.Dir(tmp), 0o750) == nil && os.WriteFile(tmp, img, 0o640) == nil {
			m.Image, m.ImageLabel = tmp, "Wajah yang dikenali"
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := t.Notifier.Send(ctx, m); err == nil {
			t.Log.Info("notification sent", "kind", m.Kind, "person", p.Name)
		}
		if m.Image != "" {
			os.Remove(m.Image)
		}
	}()
}

// outside records someone in front of the fence and raises a loitering alert when an
// unknown person stays there long enough.
func (t *Tracker) outside(cam *store.Camera, at time.Time, settings map[string]int) {
	window := time.Duration(settings[store.SettingLoiterWindow]) * time.Second
	need := time.Duration(settings[store.SettingLoiterSeconds]) * time.Second
	cooldown := time.Duration(settings[store.SettingLoiterCooldownMin]) * time.Minute

	t.mu.Lock()
	if t.loiters == nil {
		t.loiters = map[int64]*loiter{}
	}
	l := t.loiters[cam.ID]
	if l == nil {
		l = &loiter{}
		t.loiters[cam.ID] = l
	}
	// Detections flicker (at night a person behind the gate is found a few times per
	// 10 s), so reports less than 6 s apart count as one continuous presence.
	if n := len(l.spans); n > 0 && at.Sub(l.spans[n-1][1]) <= 6*time.Second {
		l.spans[n-1][1] = at
	} else {
		l.spans = append(l.spans, [2]time.Time{at, at})
	}
	keep := l.spans[:0]
	var dwell time.Duration
	for _, s := range l.spans {
		if at.Sub(s[1]) > window {
			continue
		}
		if s[0].Before(at.Add(-window)) {
			s[0] = at.Add(-window)
		}
		keep = append(keep, s)
		dwell += max(s[1].Sub(s[0]), time.Second)
	}
	l.spans = keep
	known := !t.lastKnown.IsZero() && at.Sub(t.lastKnown) <= window
	alert := dwell >= need && at.Sub(l.lastAlert) >= cooldown && !known
	if alert {
		l.lastAlert = at
		l.spans = nil
	}
	t.mu.Unlock()

	if !alert {
		return
	}
	t.Log.Info("loitering", "camera", cam.Slug, "seconds", int(dwell.Seconds()))
	if settings[store.SettingLoiterNotify] != 1 || !t.Notifier.Enabled() {
		return
	}
	m := notify.Message{
		Kind: "loitering", Level: notify.Warning, Camera: cam.Name, At: at.UTC(),
		Title: "⚠️ Ada orang di depan pagar (" + cam.Name + ")",
		Text: fmt.Sprintf("Seseorang yang tidak dikenali berada di depan pagar sekitar %d detik dalam %d menit terakhir.",
			int(dwell.Seconds()), int(window.Minutes())),
		URL: t.link("/camera/" + cam.Slug),
		Fields: []notify.Field{
			{Name: "Kamera", Value: cam.Name, Inline: true},
			{Name: "Lama", Value: fmt.Sprintf("±%d detik", int(dwell.Seconds())), Inline: true},
			{Name: "Waktu", At: &at, Inline: true},
		},
	}
	if cam.SnapshotEnabled {
		m.Image, m.ImageLabel = snapshot.LatestPath(t.DataDir, cam.Slug), "Snapshot terakhir"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := t.Notifier.Send(ctx, m); err == nil {
			t.Log.Info("notification sent", "kind", m.Kind, "camera", cam.Name)
		}
	}()
}

func (t *Tracker) link(path string) string {
	if t.PublicURL == "" {
		return ""
	}
	return t.PublicURL + path
}

// Cluster groups samples whose embeddings are alike (greedy, against each cluster's
// mean), largest clusters first; for the gallery of unlabelled faces.
func Cluster(samples []store.FaceSample, threshold float64) [][]store.FaceSample {
	type cluster struct {
		sum     []float64
		members []store.FaceSample
	}
	// Clear, large faces first, so they shape the clusters.
	sorted := append([]store.FaceSample(nil), samples...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Score*float64(sorted[i].Size) > sorted[j].Score*float64(sorted[j].Size)
	})
	var cs []*cluster
	for _, s := range sorted {
		if len(s.Embedding) != 128 {
			continue
		}
		var best *cluster
		bestSim := threshold
		for _, c := range cs {
			if sim := cosine(c.sum, s.Embedding); sim >= bestSim {
				best, bestSim = c, sim
			}
		}
		if best == nil {
			best = &cluster{sum: make([]float64, 128)}
			cs = append(cs, best)
		}
		for i, v := range s.Embedding {
			best.sum[i] += float64(v)
		}
		best.members = append(best.members, s)
	}
	sort.SliceStable(cs, func(i, j int) bool { return len(cs[i].members) > len(cs[j].members) })
	out := make([][]store.FaceSample, len(cs))
	for i, c := range cs {
		out[i] = c.members
	}
	return out
}

// Centroid is the normalised mean embedding of samples.
func Centroid(samples []store.FaceSample) []float32 {
	sum := make([]float64, 128)
	for _, s := range samples {
		for i, v := range s.Embedding {
			sum[i] += float64(v)
		}
	}
	var n float64
	for _, v := range sum {
		n += v * v
	}
	n = math.Sqrt(n) + 1e-9
	out := make([]float32, 128)
	for i, v := range sum {
		out[i] = float32(v / n)
	}
	return out
}

func cosine(sum []float64, e []float32) float64 {
	var dot, n float64
	for i, v := range sum {
		dot += v * float64(e[i])
		n += v * v
	}
	if n == 0 {
		return 0
	}
	return dot / math.Sqrt(n)
}
