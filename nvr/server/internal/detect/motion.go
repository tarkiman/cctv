// Package detect finds activity in a camera's (sub-)stream: a cheap pure-Go motion gate
// on small grayscale frames, which later decides when the person model runs.
package detect

// Frame geometry fed by ffmpeg (-vf fps=N,scale=320:180,format=gray).
const (
	FrameW = 320
	FrameH = 180
	cell   = 4 // frames are averaged into 4×4 cells to suppress sensor/compression noise
)

// Gate compares each frame with the previous one at cell resolution.
type Gate struct {
	// Threshold is the mean-brightness change (0..255) for a cell to count as changed.
	Threshold int
	// MinArea is the fraction of changed cells (0..1) that counts as motion.
	MinArea float64
	// GlobalArea is the fraction above which the whole picture changed: a PTZ camera
	// turning, or day/night (IR) switching.
	GlobalArea float64

	prev   []int
	cur    []int
	primed bool
}

// Result describes one frame.
type Result struct {
	Area   float64 // fraction of changed cells
	Global bool    // most of the picture changed
	Motion bool    // Area ≥ MinArea (includes Global)
}

func NewGate(threshold int, minArea float64) *Gate {
	return &Gate{Threshold: threshold, MinArea: minArea, GlobalArea: 0.6}
}

// Feed takes one gray frame of FrameW×FrameH bytes.
func (g *Gate) Feed(frame []byte) Result {
	cw, ch := FrameW/cell, FrameH/cell
	if g.cur == nil {
		g.cur, g.prev = make([]int, cw*ch), make([]int, cw*ch)
	}
	for cy := 0; cy < ch; cy++ {
		for cx := 0; cx < cw; cx++ {
			sum := 0
			for y := 0; y < cell; y++ {
				row := (cy*cell+y)*FrameW + cx*cell
				for x := 0; x < cell; x++ {
					sum += int(frame[row+x])
				}
			}
			g.cur[cy*cw+cx] = sum / (cell * cell)
		}
	}
	changed := 0
	if g.primed {
		for i, v := range g.cur {
			d := v - g.prev[i]
			if d < 0 {
				d = -d
			}
			if d > g.Threshold {
				changed++
			}
		}
	}
	g.prev, g.cur = g.cur, g.prev
	g.primed = true
	r := Result{Area: float64(changed) / float64(cw*ch)}
	r.Global = r.Area >= g.GlobalArea
	r.Motion = r.Area >= g.MinArea
	return r
}

// Tracker turns per-frame results into motion events: an event starts after Confirm
// consecutive motion frames and ends after Quiet frames without motion.
type Tracker struct {
	Confirm int
	Quiet   int

	streak, quiet int
	active        bool
	peak          float64
}

// Transition reported by Step.
type Transition int

const (
	None Transition = iota
	Started
	Ended
)

// Step advances the tracker; peak is the largest area seen during the event (valid on Ended).
func (t *Tracker) Step(r Result) (tr Transition, peak float64) {
	if r.Motion {
		t.streak++
		t.quiet = 0
		if r.Area > t.peak {
			t.peak = r.Area
		}
		if !t.active && t.streak >= t.Confirm {
			t.active = true
			return Started, t.peak
		}
		return None, t.peak
	}
	t.streak = 0
	if !t.active {
		t.peak = 0
		return None, 0
	}
	t.quiet++
	if t.quiet >= t.Quiet {
		peak = t.peak
		t.active, t.quiet, t.peak = false, 0, 0
		return Ended, peak
	}
	return None, t.peak
}

func (t *Tracker) Active() bool { return t.active }
