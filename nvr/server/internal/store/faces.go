package store

import (
	"context"
	"encoding/binary"
	"math"

	"github.com/jmoiron/sqlx"
)

// FaceSample is one face seen by nvr-vision.
type FaceSample struct {
	ID        int64     `db:"id" json:"id"`
	CameraID  int64     `db:"camera_id" json:"camera_id"`
	At        int64     `db:"at" json:"at"` // unix ms
	Zone      string    `db:"zone" json:"zone"`
	Path      string    `db:"path" json:"-"`
	Score     float64   `db:"score" json:"score"`
	Size      int       `db:"size" json:"size"`
	Embedding []float32 `db:"-" json:"-"`
}

type FaceStore struct{ db *sqlx.DB }

// EncodeEmbedding packs an embedding as little-endian float32.
func EncodeEmbedding(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

// DecodeEmbedding is the inverse of EncodeEmbedding.
func DecodeEmbedding(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func (s *FaceStore) Insert(ctx context.Context, f *FaceSample) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO face_samples (camera_id, at, zone, path, score, size, embedding) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.CameraID, f.At, f.Zone, f.Path, f.Score, f.Size, EncodeEmbedding(f.Embedding))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteUnlabeledBefore removes unlabelled samples older than cutoff (unix ms) and
// returns their file paths so the caller can delete the images.
func (s *FaceStore) DeleteUnlabeledBefore(ctx context.Context, cutoff int64) ([]string, error) {
	var paths []string
	if err := s.db.SelectContext(ctx, &paths,
		`SELECT path FROM face_samples WHERE person_id IS NULL AND at < ?`, cutoff); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM face_samples WHERE person_id IS NULL AND at < ?`, cutoff)
	return paths, err
}

// Count returns how many samples a camera has (for status pages and tests).
func (s *FaceStore) Count(ctx context.Context, cameraID int64) (int, error) {
	var n int
	err := s.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM face_samples WHERE camera_id = ?`, cameraID)
	return n, err
}
