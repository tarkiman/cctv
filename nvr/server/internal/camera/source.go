package camera

import (
	"errors"
	"net/url"
	"strings"
)

// Schemes a camera source may use when managed from the UI. Anything else (xiaomi://,
// ffmpeg:, exec:, multiple sources) stays managed in go2rtc.yaml.
var editableSchemes = map[string]bool{"rtsp": true, "rtsps": true, "rtmp": true, "http": true, "https": true}

var ErrInvalidURL = errors.New("invalid source URL")

// SourceInfo describes a source without exposing its password.
type SourceInfo struct {
	URL         string `json:"url"` // credentials removed
	Username    string `json:"username"`
	HasPassword bool   `json:"has_password"`
}

// SourceInput is a source as entered in the UI: the URL (may contain credentials) plus
// optional separate credentials. An empty password keeps the existing one.
type SourceInput struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (in SourceInput) Empty() bool { return strings.TrimSpace(in.URL) == "" }

// Describe splits a source URL into a credential-free URL and the username.
func Describe(src string) *SourceInfo {
	u, err := url.Parse(src)
	if err != nil {
		return &SourceInfo{URL: "(tidak bisa dibaca)"}
	}
	info := &SourceInfo{}
	if u.User != nil {
		info.Username = u.User.Username()
		_, info.HasPassword = u.User.Password()
	}
	u.User = nil
	info.URL = u.String()
	return info
}

// Editable reports whether the sources of a stream can be managed from the UI.
func Editable(srcs []string) (bool, string) {
	switch {
	case len(srcs) == 0:
		return false, "Sumber stream tidak ditemukan di go2rtc.yaml."
	case len(srcs) > 1:
		return false, "Stream ini punya beberapa sumber; ubah lewat go2rtc.yaml."
	case strings.HasPrefix(srcs[0], "xiaomi://"):
		return false, "Kamera Xiaomi tersambung lewat sesi cloud Xiaomi; kelola lewat dashboard go2rtc."
	}
	u, err := url.Parse(srcs[0])
	if err != nil || !editableSchemes[strings.ToLower(u.Scheme)] {
		return false, "Jenis sumber ini dikelola lewat go2rtc.yaml."
	}
	return true, ""
}

// BuildURL turns UI input into the source URL for go2rtc. Credentials are encoded with
// net/url so characters such as "@" in passwords survive. When no password is given,
// the password of old (the current source) is kept if the username is unchanged.
func BuildURL(in SourceInput, old string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || !editableSchemes[strings.ToLower(u.Scheme)] || u.Host == "" {
		return "", ErrInvalidURL
	}
	user, pass, hasPass := "", "", false
	if u.User != nil {
		user = u.User.Username()
		pass, hasPass = u.User.Password()
	}
	if in.Username != "" {
		user = in.Username
	}
	if in.Password != "" {
		pass, hasPass = in.Password, true
	}
	if !hasPass && user != "" && old != "" {
		if o, err := url.Parse(old); err == nil && o.User != nil && o.User.Username() == user {
			pass, hasPass = o.User.Password()
		}
	}
	switch {
	case user == "":
		u.User = nil
	case hasPass:
		u.User = url.UserPassword(user, pass)
	default:
		u.User = url.User(user)
	}
	return u.String(), nil
}
