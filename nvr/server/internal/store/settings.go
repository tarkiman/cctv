package store

import (
	"context"
	"strconv"

	"github.com/jmoiron/sqlx"
)

// Global settings and their defaults. Values are stored as text.
const (
	SettingMinFreePercent    = "min_free_percent"
	SettingTimelapseKeepDays = "timelapse_keep_days"
	SettingTimelapseFPS      = "timelapse_fps"
	// Seconds between snapshots; each one is also a timelapse frame.
	SettingSnapshotSeconds = "snapshot_seconds"
	// Detection (applies to cameras with detection on). Frames analysed per second.
	SettingDetectFPS = "detect_fps"
	// Brightness change (0..255) for a picture cell to count as changed.
	SettingMotionThreshold = "motion_threshold"
	// Changed area that counts as motion, in tenths of a percent (15 = 1.5%).
	SettingMotionMinArea = "motion_min_area"

	// Tahap E. Face similarity (×100, SFace cosine) to count as the same person.
	SettingFaceMatch = "face_match"
	// Notify when a known person arrives (1/0); a visit ends after this many minutes
	// without seeing them.
	SettingFaceNotify   = "face_notify"
	SettingFaceVisitMin = "face_visit_min"
	// Loitering: an unknown person in front of the fence for loiter_seconds within
	// loiter_window seconds alerts (1/0), at most once per loiter_cooldown_min.
	SettingLoiterNotify      = "loiter_notify"
	SettingLoiterSeconds     = "loiter_seconds"
	SettingLoiterWindow      = "loiter_window"
	SettingLoiterCooldownMin = "loiter_cooldown_min"
	// The home assistant (NVR_ASSISTANT_URL): greet known arrivals (1/0); ask an unknown
	// person who stands at the gate visitor_seconds within a minute, at most once per
	// visitor_cooldown_min.
	SettingAssistantGreet     = "assistant_greet"
	SettingVisitorSeconds     = "visitor_seconds"
	SettingVisitorCooldownMin = "visitor_cooldown_min"
)

var settingDefaults = map[string]int{
	SettingMinFreePercent:    10,
	SettingTimelapseKeepDays: 3,
	SettingTimelapseFPS:      24,
	SettingSnapshotSeconds:   60, // replaced by NVR_SNAPSHOT_SECONDS at startup (OverrideDefault)
	SettingDetectFPS:         2,
	SettingMotionThreshold:   25,
	SettingMotionMinArea:     15,
	// Spike S12 (2026-11-20): on bardi's high, downward view the same person's faces
	// matched at only ~0.2-0.3 and different people up to ~0.6, so arrival
	// notifications start off and need a clear match; enable them per installation once
	// a camera sees faces at eye level.
	SettingFaceMatch:          55,
	SettingFaceNotify:         0,
	SettingFaceVisitMin:       30,
	SettingLoiterNotify:       1,
	SettingLoiterSeconds:      60,
	SettingLoiterWindow:       180,
	SettingLoiterCooldownMin:  15,
	SettingAssistantGreet:     1,
	SettingVisitorSeconds:     15,
	SettingVisitorCooldownMin: 10,
}

// SettingLimits bounds each setting so the UI cannot store nonsense.
var SettingLimits = map[string][2]int{
	SettingMinFreePercent:     {1, 50},
	SettingTimelapseKeepDays:  {0, 30},
	SettingTimelapseFPS:       {1, 60},
	SettingSnapshotSeconds:    {10, 600},
	SettingDetectFPS:          {1, 5},
	SettingMotionThreshold:    {8, 80},
	SettingMotionMinArea:      {2, 300},
	SettingFaceMatch:          {25, 80},
	SettingFaceNotify:         {0, 1},
	SettingFaceVisitMin:       {5, 240},
	SettingLoiterNotify:       {0, 1},
	SettingLoiterSeconds:      {15, 600},
	SettingLoiterWindow:       {60, 900},
	SettingLoiterCooldownMin:  {1, 240},
	SettingAssistantGreet:     {0, 1},
	SettingVisitorSeconds:     {5, 120},
	SettingVisitorCooldownMin: {1, 120},
}

// OverrideDefault replaces a setting's default (clamped to its limits), e.g. with the
// value from the environment, so the environment decides until the admin changes the
// setting in the UI. Call it at startup, before the settings are read.
func OverrideDefault(key string, value int) {
	if _, ok := settingDefaults[key]; !ok {
		return
	}
	if lim, ok := SettingLimits[key]; ok {
		value = max(lim[0], min(lim[1], value))
	}
	settingDefaults[key] = value
}

type SettingStore struct{ db *sqlx.DB }

// All returns every known setting with stored values overriding defaults.
func (s *SettingStore) All(ctx context.Context) (map[string]int, error) {
	out := make(map[string]int, len(settingDefaults))
	for k, v := range settingDefaults {
		out[k] = v
	}
	rows, err := s.db.QueryxContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if _, known := settingDefaults[k]; !known {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			out[k] = n
		}
	}
	return out, rows.Err()
}

func (s *SettingStore) Int(ctx context.Context, key string) int {
	all, err := s.All(ctx)
	if err != nil {
		return settingDefaults[key]
	}
	return all[key]
}

func (s *SettingStore) Set(ctx context.Context, key string, value int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, strconv.Itoa(value), now())
	return err
}
