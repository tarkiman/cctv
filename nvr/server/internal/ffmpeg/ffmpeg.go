// Package ffmpeg runs ffmpeg/ffprobe subprocesses and keeps their recent stderr for
// diagnostics.
package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Tools struct {
	FFmpeg  string
	FFprobe string
	RTSP    string // go2rtc RTSP base, e.g. rtsp://127.0.0.1:8556
}

// StreamURL is the go2rtc restream URL for a stream name.
func (t Tools) StreamURL(stream string) string { return t.RTSP + "/" + stream }

// LogRing keeps the last N lines written to it. Safe for concurrent use.
type LogRing struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func NewLogRing(max int) *LogRing { return &LogRing{max: max} }

func (r *LogRing) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, time.Now().UTC().Format("15:04:05")+" "+line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *LogRing) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// Last returns the most recent line, or "".
func (r *LogRing) Last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) == 0 {
		return ""
	}
	return r.lines[len(r.lines)-1]
}

// Command builds an ffmpeg command that is interrupted (SIGINT, so it finalizes output)
// when ctx is cancelled, and killed if it does not exit within 10 seconds.
func (t Tools) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, t.FFmpeg, append([]string{"-hide_banner", "-nostdin"}, args...)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGINT) }
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// Run runs ffmpeg to completion, returning stderr in the error on failure.
func (t Tools) Run(ctx context.Context, args ...string) error {
	cmd := t.Command(ctx, append([]string{"-loglevel", "error"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(lastLines(stderr.String(), 3)))
	}
	return nil
}

// RunNice runs ffmpeg at the lowest CPU priority, for batch work such as timelapse
// encoding that must not compete with recording.
func (t Tools) RunNice(ctx context.Context, args ...string) error {
	full := append([]string{"-n", "19", t.FFmpeg, "-hide_banner", "-nostdin", "-loglevel", "error"}, args...)
	cmd := exec.CommandContext(ctx, "nice", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(lastLines(stderr.String(), 3)))
	}
	return nil
}

// PipeStderr copies lines from r into ring until EOF.
func PipeStderr(r io.Reader, ring *LogRing) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			ring.Add(line)
		}
	}
}

type Probe struct {
	Codec    string
	Width    int64
	Height   int64
	Duration float64
}

// ProbeFile reads codec, size and duration of the first video stream of a file.
func (t Tools) ProbeFile(ctx context.Context, path string) (*Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, t.FFprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height:format=duration",
		"-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	var res struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			Width     int64  `json:"width"`
			Height    int64  `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	if len(res.Streams) == 0 {
		return nil, errors.New("no video stream")
	}
	p := &Probe{Codec: res.Streams[0].CodecName, Width: res.Streams[0].Width, Height: res.Streams[0].Height}
	p.Duration, _ = strconv.ParseFloat(res.Format.Duration, 64)
	return p, nil
}

// MP4Tag is the sample entry tag browsers expect for a codec in MP4 (Safari requires hvc1
// for HEVC).
func MP4Tag(codec string) string {
	switch codec {
	case "hevc":
		return "hvc1"
	case "h264":
		return "avc1"
	}
	return ""
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
