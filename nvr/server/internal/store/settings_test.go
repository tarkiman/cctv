package store

import "testing"

func TestOverrideDefault(t *testing.T) {
	old := settingDefaults[SettingSnapshotSeconds]
	defer func() { settingDefaults[SettingSnapshotSeconds] = old }()
	OverrideDefault(SettingSnapshotSeconds, 30)
	if settingDefaults[SettingSnapshotSeconds] != 30 {
		t.Fatal("override not applied")
	}
	OverrideDefault(SettingSnapshotSeconds, 5) // below the limit
	if settingDefaults[SettingSnapshotSeconds] != 10 {
		t.Fatalf("not clamped: %d", settingDefaults[SettingSnapshotSeconds])
	}
	OverrideDefault("nope", 1)
	if _, ok := settingDefaults["nope"]; ok {
		t.Fatal("unknown key added")
	}
}
