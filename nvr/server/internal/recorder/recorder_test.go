package recorder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
)

func TestRecordArgs(t *testing.T) {
	a := recordArgs("rtsp://127.0.0.1:8556/bardi", false, false, "/x", 600)
	if !slices.Contains(a, "-an") || slices.Contains(a, "-c:a") {
		t.Fatalf("audio off must drop audio: %v", a)
	}
	a = recordArgs("rtsp://127.0.0.1:8556/bardi", true, false, "/x", 600)
	if slices.Contains(a, "-an") || !slices.Contains(a, "aac") || !slices.Contains(a, "0:a:0?") {
		t.Fatalf("audio on must map and encode audio: %v", a)
	}
	if slices.Contains(a, "-use_wallclock_as_timestamps") {
		t.Fatalf("wallclock only for Xiaomi: %v", a)
	}
	a = recordArgs("rtsp://127.0.0.1:8556/c301", true, true, "/x", 600)
	if i := slices.Index(a, "-use_wallclock_as_timestamps"); i < 0 || i > slices.Index(a, "-i") {
		t.Fatalf("wallclock must be an input option: %v", a)
	}
}

func TestWroteSince(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	if wroteSince(dir, start) {
		t.Fatal("empty dir")
	}
	writeFile(t, filepath.Join(dir, "20260101T000000.ts"))
	if !wroteSince(dir, start) {
		t.Fatal("new file not seen")
	}
	if wroteSince(dir, start.Add(time.Hour)) {
		t.Fatal("old file counted as new")
	}
}

func TestAudioBlock(t *testing.T) {
	var r Recorder
	if !r.audioAllowed(1) {
		t.Fatal("allowed by default")
	}
	r.blockAudio(1, 0)
	if r.audioAllowed(1) || !r.audioAllowed(2) {
		t.Fatal("block is per camera")
	}
	r.ResetAudio(1)
	if !r.audioAllowed(1) {
		t.Fatal("reset clears the block")
	}
	r.blockAudio(2, time.Hour)
	if r.audioAllowed(2) {
		t.Fatal("timed block holds")
	}
	r.blockAudio(2, time.Nanosecond)
	time.Sleep(time.Millisecond)
	if !r.audioAllowed(2) {
		t.Fatal("timed block expires")
	}
}

// TestRecordAndRemux runs the real recording and remux command lines on synthetic
// sources shaped like the cameras: G.711 (bardi), Opus (c301) and video only.
func TestRecordAndRemux(t *testing.T) {
	ff, err1 := exec.LookPath("ffmpeg")
	fp, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg not installed")
	}
	tools := ffmpeg.Tools{FFmpeg: ff, FFprobe: fp}
	ctx := context.Background()
	cases := []struct {
		name      string
		audioIn   []string // extra ffmpeg args to make the source's audio
		wallclock bool
		wantAudio string
	}{
		{"alaw", []string{"-f", "lavfi", "-i", "sine=f=440:r=8000", "-c:a", "pcm_alaw"}, false, "aac"},
		{"opus", []string{"-f", "lavfi", "-i", "sine=f=440:r=48000", "-c:a", "libopus", "-ac", "2"}, true, "aac"},
		{"none", nil, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.mkv")
			args := []string{"-f", "lavfi", "-i", "testsrc=size=320x240:rate=15"}
			args = append(args, tc.audioIn...)
			args = append(args, "-t", "4", "-c:v", "libx264", "-g", "15", "-pix_fmt", "yuv420p", "-y", src)
			if err := tools.Run(ctx, args...); err != nil {
				t.Skipf("cannot build source (%v)", err)
			}
			incoming := filepath.Join(dir, "in")
			if err := os.Mkdir(incoming, 0o750); err != nil {
				t.Fatal(err)
			}
			rec := recordArgs(src, true, tc.wallclock, incoming, 600)
			if err := tools.Run(ctx, rec[2:]...); err != nil { // drop "-loglevel warning"
				t.Fatalf("record: %v", err)
			}
			segs, _ := filepath.Glob(filepath.Join(incoming, "*.ts"))
			if len(segs) != 1 {
				t.Fatalf("segments: %v", segs)
			}
			p, err := tools.ProbeFile(ctx, segs[0])
			if err != nil {
				t.Fatal(err)
			}
			mp4 := filepath.Join(dir, "out.mp4")
			if err := tools.Run(ctx, remuxArgs(segs[0], mp4, p.Codec)...); err != nil {
				t.Fatalf("remux: %v", err)
			}
			out, err := tools.ProbeFile(ctx, mp4)
			if err != nil {
				t.Fatal(err)
			}
			if out.Codec != "h264" || out.Audio != tc.wantAudio {
				t.Fatalf("got video %q audio %q, want h264 / %q", out.Codec, out.Audio, tc.wantAudio)
			}
			// Wallclock timestamps follow arrival time, and a file is read far faster than
			// a live camera sends, so only the camera-timestamp cases have a real length.
			if !tc.wallclock && (out.Duration < 3.5 || out.Duration > 4.6) {
				t.Fatalf("duration %.2f", out.Duration)
			}
		})
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAudioErrRe(t *testing.T) {
	for _, l := range []string{
		"[mp2 @ 0x7fff5de23880] Header missing",
		"[aist#0:1/mp2 @ 0x7fadc69800] Decode error rate 1 exceeds maximum 0.666667",
		"[pcm_alaw @ 0x1] invalid packet",
	} {
		if !audioErrRe.MatchString(l) {
			t.Errorf("not matched: %q", l)
		}
	}
	for _, l := range []string{
		"[hevc @ 0x7f] Could not find ref with POC 12",
		"[rtsp @ 0x7f] method DESCRIBE failed: 404 Not Found",
		"[segment @ 0x7f] Opening '/x/20261109T000000.ts' for writing",
	} {
		if audioErrRe.MatchString(l) {
			t.Errorf("video/connection line matched: %q", l)
		}
	}
}
