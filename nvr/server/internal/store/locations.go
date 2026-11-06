package store

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"
)

var ErrLocationInUse = errors.New("location still has cameras")

type Location struct {
	ID        int64  `db:"id"`
	Name      string `db:"name"`
	SortOrder int    `db:"sort_order"`
	CreatedAt int64  `db:"created_at"`
	Cameras   int    `db:"cameras"`
}

type LocationStore struct{ db *sqlx.DB }

func (s *LocationStore) List(ctx context.Context) ([]Location, error) {
	var out []Location
	err := s.db.SelectContext(ctx, &out, `
		SELECT l.id, l.name, l.sort_order, l.created_at, COUNT(c.id) AS cameras
		FROM locations l LEFT JOIN cameras c ON c.location_id = l.id
		GROUP BY l.id ORDER BY l.sort_order, l.id`)
	return out, err
}

// Default is the location new cameras land in (the first one).
func (s *LocationStore) Default(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.GetContext(ctx, &id, `SELECT id FROM locations ORDER BY sort_order, id LIMIT 1`)
	return id, err
}

func (s *LocationStore) Exists(ctx context.Context, id int64) bool {
	var n int
	_ = s.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM locations WHERE id = ?`, id)
	return n == 1
}

func (s *LocationStore) Create(ctx context.Context, name string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO locations (name, sort_order, created_at)
		VALUES (?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM locations), ?)`, name, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *LocationStore) Rename(ctx context.Context, id int64, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE locations SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes an empty location and the access grants that point at it.
func (s *LocationStore) Delete(ctx context.Context, id int64) error {
	var n int
	if err := s.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM cameras WHERE location_id = ?`, id); err != nil {
		return err
	}
	if n > 0 {
		return ErrLocationInUse
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_access WHERE kind = 'location' AND target_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
