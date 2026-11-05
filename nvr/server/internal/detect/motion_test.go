package detect

import "testing"

func frame(fill byte, rect func(x, y int) bool, v byte) []byte {
	f := make([]byte, FrameW*FrameH)
	for y := 0; y < FrameH; y++ {
		for x := 0; x < FrameW; x++ {
			f[y*FrameW+x] = fill
			if rect != nil && rect(x, y) {
				f[y*FrameW+x] = v
			}
		}
	}
	return f
}

func TestGate(t *testing.T) {
	g := NewGate(25, 0.015)
	bg := frame(80, nil, 0)
	if r := g.Feed(bg); r.Motion {
		t.Fatal("first frame reported motion")
	}
	// Sensor noise: ±10 everywhere stays quiet.
	if r := g.Feed(frame(90, nil, 0)); r.Motion {
		t.Fatalf("noise reported as motion: %+v", r)
	}
	// A person-sized bright blob (40×80 px ≈ 5.5% of the frame) is motion, not global.
	person := frame(90, func(x, y int) bool { return x >= 100 && x < 140 && y >= 60 && y < 140 }, 200)
	r := g.Feed(person)
	if !r.Motion || r.Global || r.Area < 0.04 || r.Area > 0.07 {
		t.Fatalf("blob: %+v", r)
	}
	// A PTZ turn changes nearly everything.
	if r := g.Feed(frame(200, nil, 0)); !r.Global || !r.Motion {
		t.Fatalf("pan: %+v", r)
	}
}

func TestTracker(t *testing.T) {
	tr := &Tracker{Confirm: 2, Quiet: 3}
	m, q := Result{Motion: true, Area: 0.05}, Result{}
	steps := []struct {
		in   Result
		want Transition
	}{
		{m, None}, {q, None}, // a single frame is not enough
		{m, None}, {Result{Motion: true, Area: 0.2}, Started},
		{q, None}, {m, None}, {q, None}, {q, None}, {q, Ended},
	}
	var peak float64
	for i, s := range steps {
		got, p := tr.Step(s.in)
		if got != s.want {
			t.Fatalf("step %d: got %v want %v", i, got, s.want)
		}
		if got == Ended {
			peak = p
		}
	}
	if peak != 0.2 {
		t.Fatalf("peak = %v", peak)
	}
}

func BenchmarkGate(b *testing.B) {
	g := NewGate(25, 0.015)
	f1, f2 := frame(80, nil, 0), frame(120, nil, 0)
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			g.Feed(f1)
		} else {
			g.Feed(f2)
		}
	}
}
