package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Event struct {
	ID           int64           `db:"id"`
	CameraID     int64           `db:"camera_id"`
	Label        string          `db:"label"`
	Score        sql.NullFloat64 `db:"score"`
	StartAt      int64           `db:"start_at"`
	EndAt        sql.NullInt64   `db:"end_at"`
	SnapshotPath sql.NullString  `db:"snapshot_path"`
	Box          sql.NullString  `db:"box"`
	Reviewed     bool            `db:"reviewed"`
}

type EventStore struct{ db *sqlx.DB }

const eventCols = `id, camera_id, label, score, start_at, end_at, snapshot_path, box, reviewed`

// Start records the beginning of an event and returns its ID.
func (s *EventStore) Start(ctx context.Context, cameraID int64, label string, at int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO events (camera_id, label, start_at) VALUES (?, ?, ?)`, cameraID, label, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// End closes an event with its final score.
func (s *EventStore) End(ctx context.Context, id, at int64, score float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE events SET end_at = ?, score = ? WHERE id = ?`, at, score, id)
	return err
}

// CloseDangling ends events left open by a crash or restart, at their start time.
func (s *EventStore) CloseDangling(ctx context.Context, cameraID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE events SET end_at = start_at WHERE camera_id = ? AND end_at IS NULL`, cameraID)
	return err
}

// Range returns a camera's events overlapping [from, to), oldest first.
func (s *EventStore) Range(ctx context.Context, cameraID, from, to int64) ([]Event, error) {
	var out []Event
	err := s.db.SelectContext(ctx, &out, `
		SELECT `+eventCols+` FROM events
		WHERE camera_id = ? AND COALESCE(end_at, start_at) >= ? AND start_at < ?
		ORDER BY start_at`, cameraID, from, to)
	return out, err
}

// DeleteBefore removes a camera's events that started before cutoff.
func (s *EventStore) DeleteBefore(ctx context.Context, cameraID, cutoff int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE camera_id = ? AND start_at < ?`, cameraID, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
