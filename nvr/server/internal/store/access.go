package store

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// Grants are a viewer's camera permissions. Admins and users with All see everything.
type Grants struct {
	All         bool    `json:"access_all"`
	Locations   []int64 `json:"locations"`
	Cameras     []int64 `json:"cameras"`      // extra cameras outside granted locations
	DenyCameras []int64 `json:"deny_cameras"` // cameras hidden inside granted locations
}

type AccessStore struct{ db *sqlx.DB }

func (s *AccessStore) Get(ctx context.Context, userID int64) (*Grants, error) {
	g := &Grants{Locations: []int64{}, Cameras: []int64{}, DenyCameras: []int64{}}
	if err := s.db.GetContext(ctx, &g.All, `SELECT access_all FROM users WHERE id = ?`, userID); err != nil {
		return nil, notFound(err)
	}
	rows, err := s.db.QueryxContext(ctx, `SELECT kind, target_id FROM user_access WHERE user_id = ? ORDER BY target_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var id int64
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, err
		}
		switch kind {
		case "location":
			g.Locations = append(g.Locations, id)
		case "camera":
			g.Cameras = append(g.Cameras, id)
		case "deny_camera":
			g.DenyCameras = append(g.DenyCameras, id)
		}
	}
	return g, rows.Err()
}

// Set replaces a user's grants atomically.
func (s *AccessStore) Set(ctx context.Context, userID int64, g Grants) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET access_all = ?, updated_at = ? WHERE id = ?`, g.All, now(), userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_access WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for kind, ids := range map[string][]int64{"location": g.Locations, "camera": g.Cameras, "deny_camera": g.DenyCameras} {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_access (user_id, kind, target_id) VALUES (?, ?, ?)`, userID, kind, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Allows reports whether the grants cover a camera in a location.
func (g *Grants) Allows(cameraID, locationID int64) bool {
	if g.All {
		return true
	}
	has := func(list []int64, id int64) bool {
		for _, v := range list {
			if v == id {
				return true
			}
		}
		return false
	}
	if has(g.Cameras, cameraID) {
		return true
	}
	return has(g.Locations, locationID) && !has(g.DenyCameras, cameraID)
}
