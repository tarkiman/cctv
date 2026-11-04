package notify

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/health"
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

func (w *Watcher) Run(ctx context.Context) {
	if !w.Notifier.Enabled() {
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
			w.send(ctx, Message{
				Kind: "camera_recovered", Level: Resolved, Camera: cam.Name,
				Title: "✅ " + cam.Name + " kembali merekam",
				Text:  "Mati selama " + humanDuration(now.Sub(cw.badSince)) + ".",
				URL:   w.link(fmt.Sprintf("/cameras/%d", cam.ID)),
			})
			*cw = camWatch{}
		case healthy:
			*cw = camWatch{}
		case cw.badSince.IsZero():
			cw.badSince = now
		case !cw.alerted && now.Sub(cw.badSince) >= w.OfflineAfter:
			cw.alerted = true
			w.send(ctx, offlineMessage(cam, st, now.Sub(cw.badSince), w.link(fmt.Sprintf("/cameras/%d", cam.ID))))
		}
	}
	w.checkDisk(ctx)
}

func offlineMessage(cam store.Camera, st health.CameraStatus, down time.Duration, link string) Message {
	title := "🔴 " + cam.Name + " tidak merekam"
	if strings.Contains(strings.ToLower(st.Message), "sesi xiaomi") {
		title = "🔑 " + cam.Name + ": sesi Xiaomi perlu login ulang"
	}
	text := st.Message
	if text == "" {
		text = "Perekam tidak berjalan."
	}
	text += "\nSudah " + humanDuration(down) + "."
	if st.LastSegmentAt != nil {
		text += " Rekaman terakhir " + humanDuration(time.Since(*st.LastSegmentAt)) + " lalu."
	}
	return Message{Kind: "camera_offline", Level: Alert, Camera: cam.Name, Title: title, Text: text, URL: link}
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
	switch {
	case !w.diskLow && pct < floor/2:
		w.diskLow = true
		w.send(ctx, Message{
			Kind: "disk_low", Level: Alert, Title: "💾 Disk rekaman hampir penuh",
			Text: fmt.Sprintf("Sisa %.1f%% (batas retensi %.0f%%). Retensi tidak berhasil mengosongkan ruang: cek file lain di /DATA.", pct, floor),
			URL:  w.link("/system"),
		})
	case w.diskLow && pct >= floor:
		w.diskLow = false
		w.send(ctx, Message{
			Kind: "disk_ok", Level: Resolved, Title: "💾 Disk rekaman kembali normal",
			Text: fmt.Sprintf("Sisa %.1f%%.", pct), URL: w.link("/system"),
		})
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
