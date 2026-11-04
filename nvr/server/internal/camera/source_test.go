package camera

import "testing"

func TestBuildURL(t *testing.T) {
	old := "rtsp://admin:p%40ss@192.168.1.27:554/V_ENC_000"
	cases := []struct {
		name string
		in   SourceInput
		old  string
		want string
	}{
		{"separate creds encoded", SourceInput{URL: "rtsp://192.168.1.30:554/live", Username: "admin", Password: "a@b:c"}, "", "rtsp://admin:a%40b%3Ac@192.168.1.30:554/live"},
		{"pasted raw @ in password", SourceInput{URL: "rtsp://admin:p@ss@192.168.1.27:554/x"}, "", "rtsp://admin:p%40ss@192.168.1.27:554/x"},
		{"keep old password on IP change", SourceInput{URL: "rtsp://192.168.1.99:554/V_ENC_000", Username: "admin"}, old, "rtsp://admin:p%40ss@192.168.1.99:554/V_ENC_000"},
		{"new user drops old password", SourceInput{URL: "rtsp://192.168.1.99/x", Username: "other"}, old, "rtsp://other@192.168.1.99/x"},
		{"no auth", SourceInput{URL: "rtsp://192.168.1.14:8554/unicast"}, "", "rtsp://192.168.1.14:8554/unicast"},
	}
	for _, c := range cases {
		got, err := BuildURL(c.in, c.old)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	for _, bad := range []string{"xiaomi://x@1.2.3.4", "file:///etc/passwd", "rtsp://", "not a url"} {
		if _, err := BuildURL(SourceInput{URL: bad}, ""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	d := Describe(old)
	if d.URL != "rtsp://192.168.1.27:554/V_ENC_000" || d.Username != "admin" || !d.HasPassword {
		t.Errorf("Describe = %+v", d)
	}
	if ok, _ := Editable([]string{"xiaomi://u@1.2.3.4?did=1"}); ok {
		t.Error("xiaomi editable")
	}
}
