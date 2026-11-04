package notify

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/snapshot"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/supervisor"
)

// Watcher turns camera health and disk space into notifications. A camera must stay
// unhealthy for OfflineAfter before it alerts, so short reconnects stay quiet; it sends
// exactly one alert per outage and one message when the camera recovers.
type Watcher struct {
	Notifier     *Notifier
	Store        *store.Store
	Health       *health.Tracker
	Disk         func() (total, free uint64)
	DataDir      string
	Host         string // shown in repair hints, e.g. "umma"
	PublicURL    string
	OfflineAfter time.Duration
	Interval     time.Duration
	Log          *slog.Logger

	now     func() time.Time
	cams    map[int64]*camWatch
	diskLow bool
}

type camWatch struct {
	badSince time.Time
	alerted  bool
}

func (w *Watcher) Enabled() bool { return w != nil && w.Notifier.Enabled() }

func (w *Watcher) Run(ctx context.Context) {
	if !w.Enabled() {
		w.Log.Info("notifications disabled (NVR_NOTIFY_WEBHOOK not set)")
		return
	}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check evaluates every camera and the disk once.
func (w *Watcher) Check(ctx context.Context) {
	if w.now == nil {
		w.now = time.Now
	}
	if w.cams == nil {
		w.cams = map[int64]*camWatch{}
	}
	now := w.now()
	cams, err := w.Store.Cameras.List(ctx)
	if err != nil {
		w.Log.Warn("notify: list cameras", "err", err)
		return
	}
	for _, cam := range cams {
		if !cam.RecordEnabled {
			delete(w.cams, cam.ID)
			continue
		}
		st := w.Health.Get(cam.ID)
		cw := w.cams[cam.ID]
		if cw == nil {
			cw = &camWatch{}
			w.cams[cam.ID] = cw
		}
		healthy := st.State == supervisor.Running || st.State == supervisor.Disabled
		switch {
		case healthy && cw.alerted:
			w.send(ctx, w.recoveredMessage(cam, cw.badSince, now))
			*cw = camWatch{}
		case healthy:
			*cw = camWatch{}
		case cw.badSince.IsZero():
			cw.badSince = now
		case !cw.alerted && now.Sub(cw.badSince) >= w.OfflineAfter:
			cw.alerted = true
			w.send(ctx, w.offlineMessage(cam, st, cw.badSince, now))
		}
	}
	w.checkDisk(ctx)
}

var stateLabel = map[supervisor.State]string{
	supervisor.Starting: "🟡 Menghubungkan",
	supervisor.Running:  "🟢 Merekam",
	supervisor.Degraded: "🟠 Bermasalah, mencoba ulang",
	supervisor.Offline:  "🔴 Offline",
	supervisor.Disabled: "⚪ Dinonaktifkan",
}

func (w *Watcher) offlineMessage(cam store.Camera, st health.CameraStatus, since, now time.Time) Message {
	xiaomi := strings.Contains(strings.ToLower(st.Message), "sesi xiaomi")
	m := Message{
		Kind: "camera_offline", Level: Alert, Camera: cam.Name, At: now,
		Title: "🔴 " + cam.Name + " berhenti merekam",
		URL:   w.link(fmt.Sprintf("/cameras/%d", cam.ID)),
	}
	if xiaomi {
		m.Title = "🔑 " + cam.Name + ": sesi Xiaomi perlu login ulang"
	}
	m.Text = st.Message
	if m.Text == "" {
		m.Text = "Perekam tidak berjalan dan belum diketahui penyebabnya. Cek log kamera di halaman Status."
	}
	m.Fields = []Field{
		{Name: "Status", Value: stateLabel[st.State], Inline: true},
		{Name: "Sudah mati", Value: humanDuration(now.Sub(since)), Inline: true},
		{Name: "Percobaan ulang", Value: fmt.Sprintf("%d×", st.Restarts), Inline: true},
		{Name: "Mulai bermasalah", At: &since, Inline: true},
	}
	if st.LastSegmentAt != nil {
		m.Fields = append(m.Fields, Field{Name: "Rekaman terakhir", At: st.LastSegmentAt, Inline: true})
	}
	if xiaomi {
		host := w.Host
		if host == "" {
			host = "<server-nvr>"
		}
		m.Fields = append(m.Fields, Field{Name: "Cara memperbaiki", Value: "1. `ssh -L 1984:127.0.0.1:1984 " + host + "`\n" +
			"2. Buka http://localhost:1984/add.html → **Xiaomi** → login (region **sg**)\n" +
			"3. Perekam tersambung lagi otomatis dalam 1–2 menit"})
	}
	w.attachSnapshot(&m, cam, "Snapshot terakhir sebelum kamera mati")
	return m
}

func (w *Watcher) recoveredMessage(cam store.Camera, since, now time.Time) Message {
	m := Message{
		Kind: "camera_recovered", Level: Resolved, Camera: cam.Name, At: now,
		Title: "✅ " + cam.Name + " kembali merekam",
		Text:  "Perekam tersambung lagi; rekaman berlanjut normal.",
		URL:   w.link(fmt.Sprintf("/cameras/%d", cam.ID)),
		Fields: []Field{
			{Name: "Lama mati", Value: humanDuration(now.Sub(since)), Inline: true},
			{Name: "Mati sejak", At: &since, Inline: true},
			{Name: "Pulih", At: &now, Inline: true},
		},
	}
	return m
}

// checkDisk alerts when free space drops below half the retention floor: retention keeps
// the disk at the floor by deleting recordings, so falling well below it means something
// other than recordings is filling the disk (or recordings are protected).
func (w *Watcher) checkDisk(ctx context.Context) {
	total, free := w.Disk()
	if total == 0 {
		return
	}
	floor := float64(w.Store.Settings.Int(ctx, store.SettingMinFreePercent))
	pct := float64(free) / float64(total) * 100
	fields := []Field{
		{Name: "Sisa", Value: fmt.Sprintf("%s (%.1f%%)", fmtBytes(free), pct), Inline: true},
		{Name: "Kapasitas", Value: fmtBytes(total), Inline: true},
		{Name: "Batas retensi", Value: fmt.Sprintf("%.0f%% bebas", floor), Inline: true},
	}
	switch {
	case !w.diskLow && pct < floor/2:
		w.diskLow = true
		w.send(ctx, Message{
			Kind: "disk_low", Level: Alert, Title: "💾 Disk rekaman hampir penuh",
			Text: "Retensi sudah menghapus rekaman lama tetapi ruang tetap menipis, jadi ada file lain " +
				"yang memenuhi disk (atau rekaman yang dilindungi). Cek isi `/DATA` di server.",
			Fields: fields, URL: w.link("/system"),
		})
	case w.diskLow && pct >= floor:
		w.diskLow = false
		w.send(ctx, Message{
			Kind: "disk_ok", Level: Resolved, Title: "💾 Disk rekaman kembali lega",
			Fields: fields, URL: w.link("/system"),
		})
	}
}

// Startup announces that nvrd (re)started, with a quick summary of what it found.
func (w *Watcher) Startup(ctx context.Context, version string) {
	if !w.Enabled() {
		return
	}
	m := w.summary(ctx, "startup", "🚀 NVR Lite aktif", "Server baru menyala (setelah deploy atau reboot). "+
		"Notifikasi berikutnya hanya dikirim bila ada masalah.")
	m.Fields = append([]Field{{Name: "Versi", Value: "`" + version + "`", Inline: true}}, m.Fields...)
	w.send(ctx, m)
}

// Test sends a test message and returns the delivery error, for the Status page button.
func (w *Watcher) Test(ctx context.Context) error {
	m := w.summary(ctx, "test", "🔔 Tes notifikasi", "Notifikasi NVR Lite tersambung ke sini. Yang akan dikirim:\n"+
		fmt.Sprintf("• Kamera berhenti merekam ≥ %s (termasuk sesi Xiaomi kedaluwarsa), dan saat pulih\n", humanDuration(w.OfflineAfter))+
		"• Disk rekaman hampir penuh\n"+
		"• Server menyala lagi setelah deploy atau reboot")
	return w.Notifier.Send(ctx, m)
}

func (w *Watcher) summary(ctx context.Context, kind, title, text string) Message {
	m := Message{Kind: kind, Level: Info, Title: title, Text: text, URL: w.PublicURL}
	if cams, err := w.Store.Cameras.List(ctx); err == nil {
		var names []string
		for _, c := range cams {
			if c.RecordEnabled {
				names = append(names, c.Name)
			}
		}
		m.Fields = append(m.Fields, Field{Name: "Kamera direkam", Value: fmt.Sprintf("%d — %s", len(names), strings.Join(names, ", ")), Inline: true})
	}
	if total, free := w.Disk(); total > 0 {
		m.Fields = append(m.Fields, Field{Name: "Disk bebas", Value: fmt.Sprintf("%s dari %s", fmtBytes(free), fmtBytes(total)), Inline: true})
	}
	return m
}

func (w *Watcher) attachSnapshot(m *Message, cam store.Camera, label string) {
	if w.DataDir != "" && cam.SnapshotEnabled {
		m.Image, m.ImageLabel = snapshot.LatestPath(w.DataDir, cam.Slug), label
	}
}

func (w *Watcher) send(ctx context.Context, m Message) {
	sctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := w.Notifier.Send(sctx, m); err == nil {
		w.Log.Info("notification sent", "kind", m.Kind, "camera", m.Camera)
	}
}

func (w *Watcher) link(path string) string {
	if w.PublicURL == "" {
		return ""
	}
	return w.PublicURL + path
}

func humanDuration(d time.Duration) string {
	m := int(math.Round(d.Minutes()))
	switch {
	case m < 1:
		return "kurang dari 1 menit"
	case m < 60:
		return fmt.Sprintf("%d menit", m)
	case m < 48*60:
		if m%60 == 0 {
			return fmt.Sprintf("%d jam", m/60)
		}
		return fmt.Sprintf("%d jam %d menit", m/60, m%60)
	default:
		return fmt.Sprintf("%d hari", m/(24*60))
	}
}

func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
