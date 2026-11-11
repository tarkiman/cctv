package recorder

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
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
			if err := tools.Run(ctx, remuxArgs(segs[0], mp4, p.Codec, 0)...); err != nil {
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

func TestRepairFPS(t *testing.T) {
	start := time.Date(2026, 11, 10, 5, 50, 0, 0, time.UTC)
	if fps, sec := repairFPS(20<<20, start, start.Add(600*time.Second), 15000, 1260); fps != 25 || sec != 600 {
		t.Fatalf("fps %.2f sec %.0f, want 25 / 600", fps, sec)
	}
	for _, c := range []struct {
		name   string
		size   int64
		end    time.Duration
		frames int64
	}{
		{"tiny file", 10 << 10, 600 * time.Second, 15000},
		{"no frames", 20 << 20, 600 * time.Second, 0},
		{"mtime before start", 20 << 20, -time.Second, 15000},
		{"far too long", 20 << 20, 5 * time.Hour, 15000},
		{"absurd rate", 20 << 20, 10 * time.Second, 15000},
	} {
		if fps, _ := repairFPS(c.size, start, start.Add(c.end), c.frames, 1260); fps != 0 {
			t.Errorf("%s: repaired at %.2f fps", c.name, fps)
		}
	}
}

// TestFinalizeBrokenTimestamps: a segment whose video timestamps collapsed (all zero) is
// kept with timestamps rebuilt from its frame count; a tiny broken one is quarantined.
func TestFinalizeBrokenTimestamps(t *testing.T) {
	ff, err1 := exec.LookPath("ffmpeg")
	fp, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "nvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st := store.New(d)
	camID, err := st.Cameras.CreateDiscovered(ctx, "taman", "taman", sql.NullString{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &Recorder{
		Tools: ffmpeg.Tools{FFmpeg: ff, FFprobe: fp}, Store: st, Health: health.New(sse.NewHub()),
		DataDir: dir, Location: time.UTC, SegmentSeconds: 600, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	incoming := r.incomingDir("taman")
	if err := os.MkdirAll(incoming, 0o750); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 10, 5, 50, 0, 0, time.UTC)
	broken := func(name string, seconds int, size string) string {
		good := filepath.Join(dir, name+".good.ts")
		bad := filepath.Join(incoming, start.Format(nameLayout)+".ts")
		if err := r.Tools.Run(ctx, "-f", "lavfi", "-i", "testsrc=size="+size+":rate=15", "-t", strconv.Itoa(seconds),
			"-c:v", "libx264", "-bf", "0", "-g", "15", "-pix_fmt", "yuv420p", "-f", "mpegts", "-y", good); err != nil {
			t.Skipf("cannot build source (%v)", err)
		}
		if err := r.Tools.Run(ctx, "-i", good, "-c", "copy", "-bsf:v", "setts=ts=0", "-f", "mpegts", "-y", bad); err != nil {
			t.Fatal(err)
		}
		end := start.Add(time.Duration(seconds) * time.Second)
		if err := os.Chtimes(bad, end, end); err != nil {
			t.Fatal(err)
		}
		return bad
	}

	// Big enough to repair: 20 s at 15 fps, written over 20 s.
	ts := broken("big", 20, "1280x720")
	if p, err := r.Tools.ProbeFile(ctx, ts); err != nil || p.Duration >= 1 {
		t.Fatalf("test segment should probe as <1 s: %+v %v", p, err)
	}
	if err := r.finalize(ctx, job{cameraID: camID, slug: "taman", tsPath: ts}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	rec, err := st.Recordings.Latest(ctx, camID)
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.EndAt - rec.StartAt; got != 20 {
		t.Fatalf("indexed length %d s, want 20", got)
	}
	mp4, err := r.Tools.ProbeFile(ctx, filepath.Join(dir, rec.Path))
	if err != nil || mp4.Duration < 19.5 || mp4.Duration > 20.5 {
		t.Fatalf("repaired mp4 duration %+v %v", mp4, err)
	}
	if q, _ := filepath.Glob(filepath.Join(dir, "recordings", "taman", ".quarantine", "*.ts")); len(q) != 1 {
		t.Fatalf("original not kept in quarantine: %v", q)
	}

	// Too small to be worth it: dropped into quarantine, nothing indexed.
	ts = broken("small", 2, "64x64")
	if err := r.finalize(ctx, job{cameraID: camID, slug: "taman", tsPath: ts}); err == nil {
		t.Fatal("tiny broken segment was indexed")
	}
	if _, err := os.Stat(ts); !os.IsNotExist(err) {
		t.Fatal("tiny broken segment left in .incoming")
	}
}
