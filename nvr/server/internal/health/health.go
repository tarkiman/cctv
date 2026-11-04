// Package health tracks the live state of each camera's workers and pushes changes to
// browsers over SSE.
package health

import (
	"strings"
	"sync"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/supervisor"
)

type CameraStatus struct {
	CameraID       int64            `json:"camera_id"`
	State          supervisor.State `json:"state"`
	Message        string           `json:"message,omitempty"`
	LastSegmentAt  *time.Time       `json:"last_segment_at"`
	LastSnapshotAt *time.Time       `json:"last_snapshot_at"`
	Restarts       int              `json:"restarts"`
	Since          time.Time        `json:"since"`
}

type camera struct {
	status CameraStatus
	xiaomi bool
	log    *ffmpeg.LogRing
}

type Tracker struct {
	mu   sync.Mutex
	cams map[int64]*camera
	hub  *sse.Hub
}

func New(hub *sse.Hub) *Tracker {
	return &Tracker{cams: map[int64]*camera{}, hub: hub}
}

func (t *Tracker) get(id int64) *camera {
	c, ok := t.cams[id]
	if !ok {
		c = &camera{
			status: CameraStatus{CameraID: id, State: supervisor.Starting, Since: time.Now().UTC()},
			log:    ffmpeg.NewLogRing(50),
		}
		t.cams[id] = c
	}
	return c
}

// Log returns the camera's ffmpeg log ring (created on first use).
func (t *Tracker) Log(id int64) *ffmpeg.LogRing {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.get(id).log
}

func (t *Tracker) SetXiaomi(id int64, xiaomi bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.get(id).xiaomi = xiaomi
}

// SetState records a worker state; when it changes, browsers are notified.
func (t *Tracker) SetState(id int64, state supervisor.State, message string) {
	t.mu.Lock()
	c := t.get(id)
	if state == supervisor.Running && c.status.State != supervisor.Running {
		c.status.Since = time.Now().UTC()
	}
	changed := c.status.State != state || c.status.Message != message
	c.status.State = state
	c.status.Message = message
	snap := c.status
	t.mu.Unlock()
	if changed {
		t.hub.Publish("camera.status", snap)
	}
}

// Failure records a recorder failure and derives an actionable message from its log.
func (t *Tracker) Failure(id int64, failures int, err error) {
	t.mu.Lock()
	c := t.get(id)
	c.status.Restarts++
	msg := Explain(c.log.Lines(), c.xiaomi)
	if msg == "" && err != nil {
		msg = "Perekam berhenti: " + err.Error()
	}
	t.mu.Unlock()
	state := supervisor.Degraded
	if failures >= 3 {
		state = supervisor.Offline
	}
	t.SetState(id, state, msg)
}

func (t *Tracker) SegmentWritten(id int64, at time.Time) {
	t.mu.Lock()
	c := t.get(id)
	at = at.UTC()
	c.status.LastSegmentAt = &at
	snap := c.status
	t.mu.Unlock()
	t.hub.Publish("camera.status", snap)
}

func (t *Tracker) SnapshotTaken(id int64, at time.Time) {
	t.mu.Lock()
	at = at.UTC()
	t.get(id).status.LastSnapshotAt = &at
	t.mu.Unlock()
	t.hub.Publish("snapshot.updated", map[string]any{"camera_id": id, "at": at})
}

// Seed sets the last known segment time at startup (from the database).
func (t *Tracker) Seed(id int64, lastSegment *time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.get(id).status.LastSegmentAt = lastSegment
}

func (t *Tracker) Get(id int64) CameraStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.get(id).status
}

func (t *Tracker) All() []CameraStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]CameraStatus, 0, len(t.cams))
	for _, c := range t.cams {
		out = append(out, c.status)
	}
	return out
}

// Explain maps known ffmpeg/go2rtc error patterns to a message the user can act on.
func Explain(lines []string, xiaomi bool) string {
	log := strings.ToLower(strings.Join(lines, "\n"))
	switch {
	case strings.Contains(log, "account.xiaomi.com") && strings.Contains(log, "timeout"):
		return "Masalah DNS: go2rtc tidak bisa menghubungi server Xiaomi. Cek DNS go2rtc (harus ke router)."
	case strings.Contains(log, "404 not found") && xiaomi:
		return "Kamera tidak bisa dihubungi lewat P2P Xiaomi. Sesi Xiaomi kemungkinan kedaluwarsa (login ulang di dashboard go2rtc, region sg) atau IP kamera berubah."
	case strings.Contains(log, "404 not found"):
		return "go2rtc tidak bisa terhubung ke kamera. Pastikan kamera menyala dan IP-nya belum berubah."
	case strings.Contains(log, "401 unauthorized"):
		return "Kamera menolak login. Cek username/password sumber stream di go2rtc."
	case strings.Contains(log, "connection refused"):
		return "go2rtc tidak berjalan atau port RTSP-nya salah."
	case strings.Contains(log, "no space left"):
		return "Disk penuh. Kurangi retensi atau kosongkan /DATA."
	}
	return ""
}

// Remove forgets a deleted camera.
func (t *Tracker) Remove(id int64) {
	t.mu.Lock()
	delete(t.cams, id)
	t.mu.Unlock()
	t.hub.Publish("camera.removed", map[string]int64{"camera_id": id})
}
