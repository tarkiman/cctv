package health

import (
	"strings"
	"testing"
)

func TestExplain(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		xiaomi bool
		want   string
	}{
		{"xiaomi 404", []string{"rtsp://127.0.0.1:8556/c301: Server returned 404 Not Found"}, true, "Sesi Xiaomi"},
		{"plain 404", []string{"Server returned 404 Not Found"}, false, "kamera menyala"},
		{"dns", []string{"lookup account.xiaomi.com on 1.1.1.1:53: i/o timeout"}, true, "DNS"},
		{"auth", []string{"401 Unauthorized"}, false, "menolak login"},
		{"unknown", []string{"something else"}, false, ""},
	}
	for _, c := range cases {
		got := Explain(c.lines, c.xiaomi)
		if c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%s: Explain = %q, want containing %q", c.name, got, c.want)
		}
	}
}
