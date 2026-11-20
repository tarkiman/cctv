package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Person is someone named in the face gallery.
type Person struct {
	ID        int64  `db:"id" json:"id"`
	Name      string `db:"name" json:"name"`
	Notify    bool   `db:"notify" json:"notify"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}

var ErrNameTaken = errors.New("name taken")

type PeopleStore struct{ db *sqlx.DB }

func (s *PeopleStore) List(ctx context.Context) ([]Person, error) {
	var out []Person
	err := s.db.SelectContext(ctx, &out, `SELECT id, name, notify, created_at FROM people ORDER BY name COLLATE NOCASE`)
	return out, err
}

func (s *PeopleStore) ByID(ctx context.Context, id int64) (*Person, error) {
	var p Person
	if err := s.db.GetContext(ctx, &p, `SELECT id, name, notify, created_at FROM people WHERE id = ?`, id); err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// Ensure returns the person with this name (case-insensitive), creating it if needed.
func (s *PeopleStore) Ensure(ctx context.Context, name string) (*Person, error) {
	name = strings.TrimSpace(name)
	var p Person
	err := s.db.GetContext(ctx, &p, `SELECT id, name, notify, created_at FROM people WHERE name = ? COLLATE NOCASE`, name)
	if err == nil {
		return &p, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO people (name, created_at) VALUES (?, ?)`, name, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.ByID(ctx, id)
}

func (s *PeopleStore) Update(ctx context.Context, p *Person) error {
	_, err := s.db.ExecContext(ctx, `UPDATE people SET name = ?, notify = ? WHERE id = ?`, strings.TrimSpace(p.Name), p.Notify, p.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrNameTaken
	}
	return err
}

// Delete removes a person; their samples become unlabelled again (and age out).
func (s *PeopleStore) Delete(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE face_samples SET person_id = NULL WHERE person_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE face_samples SET match_id = NULL, match_score = NULL WHERE match_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM people WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
