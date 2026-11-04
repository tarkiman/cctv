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
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
