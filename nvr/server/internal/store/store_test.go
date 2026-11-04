package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "nvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return store.New(d)
}

func TestRecordingsRangeAtNextAndExpiry(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	camID, err := s.Cameras.CreateDiscovered(ctx, "bardi", "bardi", sql.NullString{String: "bardi_sub", Valid: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, start := range []int64{1000, 1600, 3000} {
		r := &store.Recording{CameraID: camID, StartAt: start, EndAt: start + 600, Path: filepath.Join("r", string(rune('a'+i))), SizeBytes: 10}
		if err := s.Recordings.Insert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Recordings.Range(ctx, camID, 1500, 2300)
	if err != nil || len(got) != 2 {
		t.Fatalf("Range = %d recs, err %v; want 2", len(got), err)
	}

	at, err := s.Recordings.At(ctx, camID, 2500) // inside a gap -> next segment
	if err != nil || at.StartAt != 3000 {
		t.Fatalf("At(gap) = %+v, %v; want segment starting 3000", at, err)
	}
	next, err := s.Recordings.Next(ctx, camID, 1000)
	if err != nil || next.StartAt != 1600 {
		t.Fatalf("Next = %+v, %v", next, err)
	}
	if _, err := s.Recordings.Next(ctx, camID, 3000); err != store.ErrNotFound {
		t.Fatalf("Next after last = %v; want ErrNotFound", err)
	}

	exp, err := s.Recordings.Expired(ctx, camID, 2300, false, 10)
	if err != nil || len(exp) != 2 {
		t.Fatalf("Expired = %d, %v; want 2", len(exp), err)
	}

	cam, err := s.Cameras.ByID(ctx, camID)
	if err != nil {
		t.Fatal(err)
	}
	if cam.StreamFor("sub") != "bardi_sub" || cam.StreamFor("main") != "bardi" || cam.RetentionDays != 14 {
		t.Fatalf("camera defaults/streams wrong: %+v", cam)
	}
}

func TestSettingsDefaultsAndOverride(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if v := s.Settings.Int(ctx, store.SettingMinFreePercent); v != 10 {
		t.Fatalf("default min_free_percent = %d", v)
	}
	if err := s.Settings.Set(ctx, store.SettingMinFreePercent, 15); err != nil {
		t.Fatal(err)
	}
	if v := s.Settings.Int(ctx, store.SettingMinFreePercent); v != 15 {
		t.Fatalf("min_free_percent after set = %d", v)
	}
}
