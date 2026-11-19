package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Zone kinds.
const (
	ZoneInside  = "inside"  // within the fence: people entering (face recognition)
	ZoneOutside = "outside" // right in front of the fence: loitering
)

// Point is a position relative to the picture: 0..1 from the left / top.
type Point [2]float64

// Zone is a polygon on a camera's picture.
type Zone struct {
	ID     int64   `json:"id"`
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

const (
	MaxZones      = 8
	MaxZonePoints = 32
)

// Validate checks a zone as sent by the UI.
func (z *Zone) Validate() error {
	z.Name = strings.TrimSpace(z.Name)
	switch {
	case z.Kind != ZoneInside && z.Kind != ZoneOutside:
		return errors.New("jenis zona harus 'inside' atau 'outside'")
	case z.Name == "" || len(z.Name) > 40:
		return errors.New("nama zona wajib diisi, maksimal 40 karakter")
	case len(z.Points) < 3 || len(z.Points) > MaxZonePoints:
		return fmt.Errorf("zona perlu 3–%d titik", MaxZonePoints)
	}
	for _, p := range z.Points {
		for _, v := range p {
			if math.IsNaN(v) || v < 0 || v > 1 {
				return errors.New("titik zona harus di dalam gambar")
			}
		}
	}
	return nil
}

// Contains reports whether p lies inside the polygon (even-odd rule).
func (z *Zone) Contains(p Point) bool {
	in := false
	n := len(z.Points)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		a, b := z.Points[i], z.Points[j]
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}

type ZoneStore struct{ db *sqlx.DB }

type zoneRow struct {
	ID     int64  `db:"id"`
	Kind   string `db:"kind"`
	Name   string `db:"name"`
	Points string `db:"points"`
}

// List returns a camera's zones in drawing order.
func (s *ZoneStore) List(ctx context.Context, cameraID int64) ([]Zone, error) {
	var rows []zoneRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT id, kind, name, points FROM camera_zones WHERE camera_id = ? ORDER BY sort_order, id`, cameraID); err != nil {
		return nil, err
	}
	out := make([]Zone, 0, len(rows))
	for _, r := range rows {
		z := Zone{ID: r.ID, Kind: r.Kind, Name: r.Name}
		if err := json.Unmarshal([]byte(r.Points), &z.Points); err != nil {
			return nil, fmt.Errorf("zone %d points: %w", r.ID, err)
		}
		out = append(out, z)
	}
	return out, nil
}

// Replace stores zones as the camera's complete set (validated by the caller).
func (s *ZoneStore) Replace(ctx context.Context, cameraID int64, zones []Zone) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM camera_zones WHERE camera_id = ?`, cameraID); err != nil {
		return err
	}
	t := now()
	for i, z := range zones {
		pts, err := json.Marshal(z.Points)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO camera_zones (camera_id, kind, name, points, sort_order, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			cameraID, z.Kind, z.Name, string(pts), i, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}
