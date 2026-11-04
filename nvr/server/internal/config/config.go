// Package config reads nvrd's infrastructure settings from NVR_* environment variables.
// Per-camera and retention settings live in the database instead.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen         string
	DataDir        string
	Go2rtcURL      string
	Go2rtcRTSP     string
	FFmpeg         string
	FFprobe        string
	Location       *time.Location
	AdminUser      string
	AdminPassword  string
	SecureCookie   bool
	LogLevel       slog.Level
	SegmentSeconds int
	SnapshotEvery  time.Duration
	// Extra browser origins (host patterns) allowed to open the live websocket, for
	// access through a reverse proxy that rewrites Host, e.g. "cctv.tarkiman.com".
	AllowedOrigins []string
	// Webhooks for alerts (comma-separated). Discord webhook URLs get Discord embeds;
	// other URLs get generic JSON.
	NotifyWebhooks []string
	// How long a camera must be down before it alerts.
	NotifyOfflineAfter time.Duration
	// Public base URL used for links in notifications, e.g. https://cctv.tarkiman.com.
	PublicURL string
}

func Load() (*Config, error) {
	c := &Config{
		Listen:        env("NVR_LISTEN", ":8090"),
		DataDir:       env("NVR_DATA_DIR", "/data"),
		Go2rtcURL:     strings.TrimRight(env("NVR_GO2RTC_URL", "http://127.0.0.1:1984"), "/"),
		Go2rtcRTSP:    strings.TrimRight(env("NVR_GO2RTC_RTSP", "rtsp://127.0.0.1:8556"), "/"),
		FFmpeg:        env("NVR_FFMPEG", "ffmpeg"),
		FFprobe:       env("NVR_FFPROBE", "ffprobe"),
		AdminUser:     os.Getenv("NVR_ADMIN_USER"),
		AdminPassword: os.Getenv("NVR_ADMIN_PASSWORD"),
	}

	loc, err := time.LoadLocation(env("NVR_TZ", "Asia/Jakarta"))
	if err != nil {
		return nil, fmt.Errorf("NVR_TZ: %w", err)
	}
	c.Location = loc

	if c.SecureCookie, err = strconv.ParseBool(env("NVR_SECURE_COOKIE", "false")); err != nil {
		return nil, fmt.Errorf("NVR_SECURE_COOKIE: %w", err)
	}
	if err := c.LogLevel.UnmarshalText([]byte(env("NVR_LOG_LEVEL", "info"))); err != nil {
		return nil, fmt.Errorf("NVR_LOG_LEVEL: %w", err)
	}
	if c.SegmentSeconds, err = strconv.Atoi(env("NVR_SEGMENT_SECONDS", "600")); err != nil || c.SegmentSeconds < 10 {
		return nil, fmt.Errorf("NVR_SEGMENT_SECONDS must be an integer >= 10")
	}
	sec, err := strconv.Atoi(env("NVR_SNAPSHOT_SECONDS", "60"))
	if err != nil || sec < 10 {
		return nil, fmt.Errorf("NVR_SNAPSHOT_SECONDS must be an integer >= 10")
	}
	c.SnapshotEvery = time.Duration(sec) * time.Second
	for _, o := range strings.Split(os.Getenv("NVR_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	for _, u := range strings.Split(os.Getenv("NVR_NOTIFY_WEBHOOK"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
				return nil, fmt.Errorf("NVR_NOTIFY_WEBHOOK: %q is not an http(s) URL", u)
			}
			c.NotifyWebhooks = append(c.NotifyWebhooks, u)
		}
	}
	mins, err := strconv.Atoi(env("NVR_NOTIFY_OFFLINE_MINUTES", "5"))
	if err != nil || mins < 1 {
		return nil, fmt.Errorf("NVR_NOTIFY_OFFLINE_MINUTES must be an integer >= 1")
	}
	c.NotifyOfflineAfter = time.Duration(mins) * time.Minute
	c.PublicURL = strings.TrimRight(os.Getenv("NVR_PUBLIC_URL"), "/")
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
