package timelapse

import "testing"

func TestPruneBefore(t *testing.T) {
	cases := []struct {
		date, today string
		keep        int
		want        bool
	}{
		{"2026-11-01", "2026-11-04", 3, false},
		{"2026-10-31", "2026-11-04", 3, true},
		{"2026-11-03", "2026-11-04", 0, true},
		{"bad", "2026-11-04", 0, false},
	}
	for _, c := range cases {
		if got := pruneBefore(c.date, c.today, c.keep); got != c.want {
			t.Errorf("pruneBefore(%s, %s, %d) = %v, want %v", c.date, c.today, c.keep, got, c.want)
		}
	}
}
