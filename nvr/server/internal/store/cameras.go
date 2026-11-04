package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Camera struct {
	ID                 int64          `db:"id"`
	Slug               string         `db:"slug"`
	Name               string         `db:"name"`
	MainStream         string         `db:"main_stream"`
	SubStream          sql.NullString `db:"sub_stream"`
	SourceManaged      bool           `db:"source_managed"`
	RecordEnabled      bool           `db:"record_enabled"`
	RecordStream       string         `db:"record_stream"`
	RetentionDays      int            `db:"retention_days"`
	EventRetentionDays int            `db:"event_retention_days"`
	SnapshotEnabled    bool           `db:"snapshot_enabled"`
	TimelapseEnabled   bool           `db:"timelapse_enabled"`
	DetectEnabled      bool           `db:"detect_enabled"`
	SortOrder          int            `db:"sort_order"`
	CreatedAt          int64          `db:"created_at"`
	UpdatedAt          int64          `db:"updated_at"`
}

// RecordStreamName is the go2rtc stream the recorder should read.
func (c *Camera) RecordStreamName() string {
	if c.RecordStream == "sub" && c.SubStream.Valid {
		return c.SubStream.String
	}
	return c.MainStream
}

// StreamFor maps a requested quality ("main"/"sub") to a go2rtc stream name, falling back
// to the main stream when the camera has no sub-stream.
func (c *Camera) StreamFor(quality string) string {
	if quality == "sub" && c.SubStream.Valid {
		return c.SubStream.String
	}
	return c.MainStream
}

type CameraStore struct{ db *sqlx.DB }

const cameraCols = `id, slug, name, main_stream, sub_stream, source_managed, record_enabled,
	record_stream, retention_days, event_retention_days, snapshot_enabled, timelapse_enabled,
	detect_enabled, sort_order, created_at, updated_at`

func (s *CameraStore) List(ctx context.Context) ([]Camera, error) {
	var out []Camera
	err := s.db.SelectContext(ctx, &out, `SELECT `+cameraCols+` FROM cameras ORDER BY sort_order, id`)
	return out, err
}

func (s *CameraStore) ByID(ctx context.Context, id int64) (*Camera, error) {
	var c Camera
	if err := s.db.GetContext(ctx, &c, `SELECT `+cameraCols+` FROM cameras WHERE id = ?`, id); err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

func (s *CameraStore) BySlug(ctx context.Context, slug string) (*Camera, error) {
	var c Camera
	if err := s.db.GetContext(ctx, &c, `SELECT `+cameraCols+` FROM cameras WHERE slug = ?`, slug); err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

// CreateDiscovered inserts a camera found in go2rtc; its source stays managed in go2rtc.yaml.
func (s *CameraStore) CreateDiscovered(ctx context.Context, slug, mainStream string, subStream sql.NullString, sortOrder int) (int64, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO cameras (slug, name, main_stream, sub_stream, source_managed, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?)`, slug, slug, mainStream, subStream, sortOrder, t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *CameraStore) SetSubStream(ctx context.Context, id int64, sub sql.NullString) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cameras SET sub_stream = ?, updated_at = ? WHERE id = ?`, sub, now(), id)
	return err
}

// Update writes the user-editable fields of c.
func (s *CameraStore) Update(ctx context.Context, c *Camera) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cameras SET name = ?, record_enabled = ?, record_stream = ?, retention_days = ?,
			event_retention_days = ?, snapshot_enabled = ?, timelapse_enabled = ?, sort_order = ?,
			updated_at = ?
		WHERE id = ?`,
		c.Name, c.RecordEnabled, c.RecordStream, c.RetentionDays, c.EventRetentionDays,
		c.SnapshotEnabled, c.TimelapseEnabled, c.SortOrder, now(), c.ID)
	return err
}

// Delete removes a camera with its source rows and recording index (ON DELETE CASCADE).
func (s *CameraStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cameras WHERE id = ?`, id)
	return err
}
