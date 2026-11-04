package export

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

func TestEstimateRange(t *testing.T) {
	recs := []store.Recording{
		{StartAt: 0, EndAt: 600, SizeBytes: 600},
		{StartAt: 900, EndAt: 1500, SizeBytes: 600}, // 300 s gap before
	}
	e := EstimateRange(recs, time.Unix(300, 0), time.Unix(1200, 0))
	if e.Segments != 2 || e.Seconds != 600 || e.Bytes != 600 || e.GapSecs != 300 {
		t.Fatalf("estimate = %+v", e)
	}
}

func TestClipTrimsAcrossSegments(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	tools := ffmpeg.Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}
	ctx := context.Background()
	var recs []store.Recording
	for i, start := range []int64{1000, 1010} {
		rel := filepath.Join("rec", string(rune('a'+i))+".mp4")
		os.MkdirAll(filepath.Join(dir, "rec"), 0o755)
		err := tools.Run(ctx, "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=10",
			"-c:v", "libx264", "-g", "10", "-pix_fmt", "yuv420p", "-y", filepath.Join(dir, rel))
		if err != nil {
			t.Skip("cannot encode test clip:", err)
		}
		recs = append(recs, store.Recording{StartAt: start, EndAt: start + 10, Path: rel, Codec: sql.NullString{String: "h264", Valid: true}})
	}
	x := New(tools, dir)
	out, err := x.Clip(ctx, recs, time.Unix(1005, 0), time.Unix(1015, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(out)
	p, err := tools.ProbeFile(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	if p.Duration < 9.5 || p.Duration > 11.5 {
		t.Fatalf("clip duration = %.2fs, want ~10s", p.Duration)
	}
	if _, err := x.Clip(ctx, recs, time.Unix(2000, 0), time.Unix(2010, 0)); err != ErrNoFootage {
		t.Fatalf("err = %v, want ErrNoFootage", err)
	}
}
