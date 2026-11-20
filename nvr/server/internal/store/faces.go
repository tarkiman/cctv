package store

import (
	"context"
	"database/sql"
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
	// PersonID is the admin's label; MatchID/MatchScore what recognition said.
	PersonID   sql.NullInt64   `db:"person_id" json:"person_id"`
	MatchID    sql.NullInt64   `db:"match_id" json:"match_id"`
	MatchScore sql.NullFloat64 `db:"match_score" json:"match_score"`
}

// sampleRow adds the raw embedding for scanning.
type sampleRow struct {
	FaceSample
	Raw []byte `db:"embedding"`
}

const sampleCols = `id, camera_id, at, zone, path, score, size, person_id, match_id, match_score, embedding`

func scanSamples(rows []sampleRow) []FaceSample {
	out := make([]FaceSample, len(rows))
	for i, r := range rows {
		out[i] = r.FaceSample
		out[i].Embedding = DecodeEmbedding(r.Raw)
	}
	return out
}

// Labeled returns every sample with a person label (the recognition gallery).
func (s *FaceStore) Labeled(ctx context.Context) ([]FaceSample, error) {
	var rows []sampleRow
	err := s.db.SelectContext(ctx, &rows, `SELECT `+sampleCols+` FROM face_samples WHERE person_id IS NOT NULL ORDER BY at DESC`)
	return scanSamples(rows), err
}

// Unlabeled returns the newest unlabelled samples, at most limit.
func (s *FaceStore) Unlabeled(ctx context.Context, limit int) ([]FaceSample, error) {
	var rows []sampleRow
	err := s.db.SelectContext(ctx, &rows, `SELECT `+sampleCols+` FROM face_samples WHERE person_id IS NULL ORDER BY at DESC LIMIT ?`, limit)
	return scanSamples(rows), err
}

func (s *FaceStore) ByID(ctx context.Context, id int64) (*FaceSample, error) {
	var r sampleRow
	if err := s.db.GetContext(ctx, &r, `SELECT `+sampleCols+` FROM face_samples WHERE id = ?`, id); err != nil {
		return nil, notFound(err)
	}
	f := scanSamples([]sampleRow{r})[0]
	return &f, nil
}

// Label sets (or with personID 0 clears) the person label of samples.
func (s *FaceStore) Label(ctx context.Context, ids []int64, personID int64) error {
	if len(ids) == 0 {
		return nil
	}
	q, args, err := sqlx.In(`UPDATE face_samples SET person_id = NULLIF(?, 0) WHERE id IN (?)`, personID, ids)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.db.Rebind(q), args...)
	return err
}

// Delete removes samples and returns their image paths.
func (s *FaceStore) Delete(ctx context.Context, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q, args, err := sqlx.In(`SELECT path FROM face_samples WHERE id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var paths []string
	if err := s.db.SelectContext(ctx, &paths, s.db.Rebind(q), args...); err != nil {
		return nil, err
	}
	q, args, _ = sqlx.In(`DELETE FROM face_samples WHERE id IN (?)`, ids)
	_, err = s.db.ExecContext(ctx, s.db.Rebind(q), args...)
	return paths, err
}

// LastMatch returns when each person was last recognised (unix ms).
func (s *FaceStore) LastMatch(ctx context.Context) (map[int64]int64, error) {
	var rows []struct {
		ID int64 `db:"match_id"`
		At int64 `db:"at"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT match_id, MAX(at) AS at FROM face_samples WHERE match_id IS NOT NULL GROUP BY match_id`); err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.ID] = r.At
	}
	return out, nil
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
		INSERT INTO face_samples (camera_id, at, zone, path, score, size, embedding, match_id, match_score) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.CameraID, f.At, f.Zone, f.Path, f.Score, f.Size, EncodeEmbedding(f.Embedding), f.MatchID, f.MatchScore)
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
