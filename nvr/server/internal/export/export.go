// Package export cuts a clip out of recorded segments without re-encoding: the
// segments overlapping the requested range are concatenated with ffmpeg's concat
// demuxer (inpoint/outpoint trim the first and last one) and stream-copied into a
// single faststart MP4. Cuts snap to the keyframe at or before the requested start.
package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

var (
	ErrNoFootage = errors.New("no recordings in range")
	ErrBusy      = errors.New("another export is running")
)

type Exporter struct {
	Tools   ffmpeg.Tools
	DataDir string
	sem     chan struct{}
}

func New(tools ffmpeg.Tools, dataDir string) *Exporter {
	return &Exporter{Tools: tools, DataDir: dataDir, sem: make(chan struct{}, 1)}
}

// Estimate describes what a clip over [from, to) would contain.
type Estimate struct {
	Seconds  float64 `json:"seconds"` // footage actually recorded within the range
	Bytes    int64   `json:"bytes"`   // approximate clip size
	Segments int     `json:"segments"`
	GapSecs  float64 `json:"gap_seconds"` // parts of the range without footage (skipped)
}

func EstimateRange(recs []store.Recording, from, to time.Time) Estimate {
	var e Estimate
	f, t := from.Unix(), to.Unix()
	for _, r := range recs {
		s, en := max(r.StartAt, f), min(r.EndAt, t)
		if en <= s || r.EndAt <= r.StartAt {
			continue
		}
		e.Segments++
		e.Seconds += float64(en - s)
		e.Bytes += r.SizeBytes * (en - s) / (r.EndAt - r.StartAt)
	}
	e.GapSecs = float64(t-f) - e.Seconds
	if e.GapSecs < 0 {
		e.GapSecs = 0
	}
	return e
}

// Clip writes the clip to a temporary file and returns its path; the caller removes it.
func (x *Exporter) Clip(ctx context.Context, recs []store.Recording, from, to time.Time) (string, error) {
	select {
	case x.sem <- struct{}{}:
		defer func() { <-x.sem }()
	default:
		return "", ErrBusy
	}
	dir := filepath.Join(x.DataDir, "exports")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	x.cleanup(dir)

	var list strings.Builder
	codec := ""
	n := 0
	for _, r := range recs {
		if r.EndAt <= from.Unix() || r.StartAt >= to.Unix() {
			continue
		}
		path := filepath.Join(x.DataDir, r.Path)
		if _, err := os.Stat(path); err != nil {
			continue // deleted by retention meanwhile
		}
		n++
		if codec == "" && r.Codec.Valid {
			codec = r.Codec.String
		}
		fmt.Fprintf(&list, "file '%s'\n", strings.ReplaceAll(path, "'", `'\''`))
		start := time.Unix(r.StartAt, 0)
		if from.After(start) {
			fmt.Fprintf(&list, "inpoint %.3f\n", from.Sub(start).Seconds())
		}
		if to.Before(time.Unix(r.EndAt, 0)) {
			fmt.Fprintf(&list, "outpoint %.3f\n", to.Sub(start).Seconds())
		}
	}
	if n == 0 {
		return "", ErrNoFootage
	}
	f, err := os.CreateTemp(dir, "clip-*.txt")
	if err != nil {
		return "", err
	}
	listPath := f.Name()
	defer os.Remove(listPath)
	if _, err := f.WriteString(list.String()); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	out := strings.TrimSuffix(listPath, ".txt") + ".mp4"
	args := []string{"-f", "concat", "-safe", "0", "-i", listPath,
		"-map", "0:v:0", "-map", "0:a?", "-c", "copy", "-avoid_negative_ts", "make_zero"}
	if tag := ffmpeg.MP4Tag(codec); tag != "" {
		args = append(args, "-tag:v", tag)
	}
	args = append(args, "-movflags", "+faststart", "-f", "mp4", "-y", out)
	if err := x.Tools.RunNice(ctx, args...); err != nil {
		os.Remove(out)
		return "", err
	}
	return out, nil
}

// cleanup removes clips left behind by interrupted downloads or crashes.
func (x *Exporter) cleanup(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
