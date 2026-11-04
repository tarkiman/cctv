package notify

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/supervisor"
)

// TestPreview posts one sample of each message to a real webhook, to check the layout:
//
//	NVR_PREVIEW_WEBHOOK=<url> NVR_PREVIEW_IMAGE=latest.jpg go test -run TestPreview ./internal/notify
func TestPreview(t *testing.T) {
	u := os.Getenv("NVR_PREVIEW_WEBHOOK")
	if u == "" {
		t.Skip("NVR_PREVIEW_WEBHOOK not set")
	}
	loc, _ := time.LoadLocation("Asia/Jakarta")
	w := &Watcher{
		Notifier: New([]string{u}, loc, "NVR Lite · umma (contoh)", slog.Default()),
		Host:     "umma", PublicURL: "https://cctv.tarkiman.com", OfflineAfter: 5 * time.Minute,
	}
	now := time.Now()
	since := now.Add(-7 * time.Minute)
	last := now.Add(-9 * time.Minute)
	cam := store.Camera{ID: 3, Name: "c301", Slug: "c301"}
	off := w.offlineMessage(cam, health.CameraStatus{
		State: supervisor.Offline, Restarts: 4, LastSegmentAt: &last,
		Message: "Kamera tidak bisa dihubungi lewat P2P Xiaomi. Sesi Xiaomi kemungkinan kedaluwarsa (login ulang di dashboard go2rtc, region sg) atau IP kamera berubah.",
	}, since, now)
	off.Image, off.ImageLabel = os.Getenv("NVR_PREVIEW_IMAGE"), "Snapshot terakhir sebelum kamera mati"
	disk := Message{Kind: "disk_low", Level: Alert, Title: "💾 Disk rekaman hampir penuh",
		Text: "Retensi sudah menghapus rekaman lama tetapi ruang tetap menipis, jadi ada file lain yang memenuhi disk (atau rekaman yang dilindungi). Cek isi `/DATA` di server.",
		Fields: []Field{
			{Name: "Sisa", Value: "19.2 GB (4.3%)", Inline: true},
			{Name: "Kapasitas", Value: "447.1 GB", Inline: true},
			{Name: "Batas retensi", Value: "10% bebas", Inline: true},
		}, URL: "https://cctv.tarkiman.com/system"}
	for _, m := range []Message{off, w.recoveredMessage(cam, since, now.Add(5*time.Minute)), disk} {
		m.Title = "[CONTOH] " + m.Title
		if err := w.Notifier.Send(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
}
