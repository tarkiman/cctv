package store

import "testing"

func TestZoneContainsAndValidate(t *testing.T) {
	z := Zone{Kind: ZoneInside, Name: " Dalam ", Points: []Point{{0.1, 0.6}, {0.9, 0.6}, {0.9, 1}, {0.1, 1}}}
	if err := z.Validate(); err != nil || z.Name != "Dalam" {
		t.Fatalf("valid zone: %v %q", err, z.Name)
	}
	if !z.Contains(Point{0.5, 0.8}) || z.Contains(Point{0.5, 0.3}) || z.Contains(Point{0.05, 0.8}) {
		t.Fatal("contains")
	}
	for _, bad := range []Zone{
		{Kind: "jalan", Name: "x", Points: z.Points},
		{Kind: ZoneOutside, Name: "", Points: z.Points},
		{Kind: ZoneOutside, Name: "x", Points: z.Points[:2]},
		{Kind: ZoneOutside, Name: "x", Points: []Point{{0, 0}, {1.2, 0}, {1, 1}}},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
