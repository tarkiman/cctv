package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Recording struct {
	ID        int64          `db:"id"`
	CameraID  int64          `db:"camera_id"`
	StartAt   int64          `db:"start_at"`
	EndAt     int64          `db:"end_at"`
	Path      string         `db:"path"`
	SizeBytes int64          `db:"size_bytes"`
	Codec     sql.NullString `db:"codec"`
	Width     sql.NullInt64  `db:"width"`
	Height    sql.NullInt64  `db:"height"`
	Protected bool           `db:"protected"`
}

type RecordingStore struct{ db *sqlx.DB }

const recordingCols = `id, camera_id, start_at, end_at, path, size_bytes, codec, width, height, protected`

func (s *RecordingStore) Insert(ctx context.Context, r *Recording) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO recordings (camera_id, start_at, end_at, path, size_bytes, codec, width, height, protected)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO NOTHING`,
		r.CameraID, r.StartAt, r.EndAt, r.Path, r.SizeBytes, r.Codec, r.Width, r.Height, r.Protected)
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

func (s *RecordingStore) ByID(ctx context.Context, id int64) (*Recording, error) {
	var r Recording
	if err := s.db.GetContext(ctx, &r, `SELECT `+recordingCols+` FROM recordings WHERE id = ?`, id); err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// Range returns segments of a camera that overlap [from, to), oldest first.
func (s *RecordingStore) Range(ctx context.Context, cameraID, from, to int64) ([]Recording, error) {
	var out []Recording
	err := s.db.SelectContext(ctx, &out, `
		SELECT `+recordingCols+` FROM recordings
		WHERE camera_id = ? AND end_at > ? AND start_at < ?
		ORDER BY start_at`, cameraID, from, to)
	return out, err
}

// At returns the segment containing t, or the first segment starting after t when t falls
// into a gap (so playback can continue from the next available footage).
func (s *RecordingStore) At(ctx context.Context, cameraID, t int64) (*Recording, error) {
	var r Recording
	err := s.db.GetContext(ctx, &r, `
		SELECT `+recordingCols+` FROM recordings
		WHERE camera_id = ? AND end_at > ?
		ORDER BY start_at LIMIT 1`, cameraID, t)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// Next returns the segment that follows the one starting at startAt.
func (s *RecordingStore) Next(ctx context.Context, cameraID, startAt int64) (*Recording, error) {
	var r Recording
	err := s.db.GetContext(ctx, &r, `
		SELECT `+recordingCols+` FROM recordings
		WHERE camera_id = ? AND start_at > ?
		ORDER BY start_at LIMIT 1`, cameraID, startAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

func (s *RecordingStore) Latest(ctx context.Context, cameraID int64) (*Recording, error) {
	var r Recording
	err := s.db.GetContext(ctx, &r, `
		SELECT `+recordingCols+` FROM recordings
		WHERE camera_id = ? ORDER BY start_at DESC LIMIT 1`, cameraID)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// Expired returns up to limit segments of a camera that ended before cutoff.
func (s *RecordingStore) Expired(ctx context.Context, cameraID, cutoff int64, protected bool, limit int) ([]Recording, error) {
	var out []Recording
	err := s.db.SelectContext(ctx, &out, `
		SELECT `+recordingCols+` FROM recordings
		WHERE camera_id = ? AND protected = ? AND end_at < ?
		ORDER BY start_at LIMIT ?`, cameraID, protected, cutoff, limit)
	return out, err
}

// Oldest returns the oldest unprotected segments across all cameras.
func (s *RecordingStore) Oldest(ctx context.Context, limit int) ([]Recording, error) {
	var out []Recording
	err := s.db.SelectContext(ctx, &out, `
		SELECT `+recordingCols+` FROM recordings
		WHERE protected = 0 ORDER BY start_at LIMIT ?`, limit)
	return out, err
}

func (s *RecordingStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM recordings WHERE id = ?`, id)
	return err
}

type CameraUsage struct {
	CameraID int64 `db:"camera_id" json:"camera_id"`
	Bytes    int64 `db:"bytes" json:"bytes"`
	Segments int64 `db:"segments" json:"segments"`
	Oldest   int64 `db:"oldest" json:"-"`
}

func (s *RecordingStore) Usage(ctx context.Context) ([]CameraUsage, error) {
	var out []CameraUsage
	err := s.db.SelectContext(ctx, &out, `
		SELECT camera_id, COALESCE(SUM(size_bytes), 0) AS bytes, COUNT(*) AS segments,
			COALESCE(MIN(start_at), 0) AS oldest
		FROM recordings GROUP BY camera_id`)
	return out, err
}

// BytesSince sums segment sizes per camera since t (used to estimate daily disk use).
func (s *RecordingStore) BytesSince(ctx context.Context, t int64) (map[int64]int64, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT camera_id, COALESCE(SUM(size_bytes), 0) FROM recordings
		WHERE start_at >= ? GROUP BY camera_id`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, b int64
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		out[id] = b
	}
	return out, rows.Err()
}
