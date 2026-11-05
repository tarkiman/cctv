package store_test

import (
	"context"
	"database/sql"
	"testing"
)

func TestEventsAndDetectFlag(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	id, _ := s.Cameras.CreateDiscovered(ctx, "bardi", "bardi", sql.NullString{}, 0)
	cam, _ := s.Cameras.ByID(ctx, id)
	cam.DetectEnabled = true
	if err := s.Cameras.Update(ctx, cam); err != nil {
		t.Fatal(err)
	}
	if cam, _ = s.Cameras.ByID(ctx, id); !cam.DetectEnabled {
		t.Fatal("detect_enabled not saved")
	}

	e1, _ := s.Events.Start(ctx, id, "motion", 1000)
	if err := s.Events.End(ctx, e1, 1030, 0.12); err != nil {
		t.Fatal(err)
	}
	s.Events.Start(ctx, id, "motion", 2000) // left open (crash)
	if err := s.Events.CloseDangling(ctx, id); err != nil {
		t.Fatal(err)
	}
	evs, err := s.Events.Range(ctx, id, 0, 5000)
	if err != nil || len(evs) != 2 || evs[0].Score.Float64 != 0.12 || !evs[1].EndAt.Valid {
		t.Fatalf("events = %+v, %v", evs, err)
	}
	if evs, _ := s.Events.Range(ctx, id, 1020, 1500); len(evs) != 1 {
		t.Fatalf("overlap query = %d", len(evs))
	}
	if n, _ := s.Events.DeleteBefore(ctx, id, 1500); n != 1 {
		t.Fatalf("deleted %d", n)
	}
}
