package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
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

func TestLocationsAndGrants(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	home, _ := s.Locations.Default(ctx)
	ibu, err := s.Locations.Create(ctx, "Rumah Ibu")
	if err != nil {
		t.Fatal(err)
	}
	teras, _ := s.Cameras.CreateDiscovered(ctx, "teras", "teras", sql.NullString{}, 0)
	tamu, _ := s.Cameras.CreateDiscovered(ctx, "tamu", "tamu", sql.NullString{}, 1)
	garasi, _ := s.Cameras.CreateDiscovered(ctx, "garasi", "garasi", sql.NullString{}, 2)
	cam, _ := s.Cameras.ByID(ctx, garasi)
	if !cam.LocationID.Valid || cam.LocationID.Int64 != home {
		t.Fatalf("new camera location = %+v, want default %d", cam.LocationID, home)
	}
	cam.LocationID.Int64 = ibu
	if err := s.Cameras.Update(ctx, cam); err != nil {
		t.Fatal(err)
	}
	if err := s.Locations.Delete(ctx, ibu); err != store.ErrLocationInUse {
		t.Fatalf("delete non-empty location: %v", err)
	}

	uid, _ := s.Users.Create(ctx, "ibu", "x", "viewer")
	g, _ := s.Access.Get(ctx, uid)
	if !g.All {
		t.Fatal("new users should default to all cameras")
	}
	// Rumah Ibu (all) + Teras from Rumah, but not Ruang Tamu.
	if err := s.Access.Set(ctx, uid, store.Grants{Locations: []int64{ibu}, Cameras: []int64{teras}}); err != nil {
		t.Fatal(err)
	}
	g, _ = s.Access.Get(ctx, uid)
	if g.All || !g.Allows(garasi, ibu) || !g.Allows(teras, home) || g.Allows(tamu, home) {
		t.Fatalf("grants = %+v", g)
	}
	// Whole Rumah except Ruang Tamu.
	s.Access.Set(ctx, uid, store.Grants{Locations: []int64{home}, DenyCameras: []int64{tamu}})
	g, _ = s.Access.Get(ctx, uid)
	if !g.Allows(teras, home) || g.Allows(tamu, home) || g.Allows(garasi, ibu) {
		t.Fatalf("deny grants = %+v", g)
	}
}
