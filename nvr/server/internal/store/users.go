package store

import (
	"context"

	"github.com/jmoiron/sqlx"
)

type User struct {
	ID           int64  `db:"id" json:"id"`
	Username     string `db:"username" json:"username"`
	PasswordHash string `db:"password_hash" json:"-"`
	Role         string `db:"role" json:"role"`
	CreatedAt    int64  `db:"created_at" json:"-"`
	UpdatedAt    int64  `db:"updated_at" json:"-"`
}

type UserStore struct{ db *sqlx.DB }

const userCols = `id, username, password_hash, role, created_at, updated_at`

func (s *UserStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM users`)
	return n, err
}

func (s *UserStore) Create(ctx context.Context, username, passwordHash, role string) (int64, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, role, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, username, passwordHash, role, t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *UserStore) ByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u, `SELECT `+userCols+` FROM users WHERE username = ?`, username)
	if err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

func (s *UserStore) ByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u, `SELECT `+userCols+` FROM users WHERE id = ?`, id)
	if err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

func (s *UserStore) SetPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, now(), id)
	return err
}

// UserSummary is a user with activity figures, for the admin user list.
type UserSummary struct {
	User
	Sessions int   `db:"sessions"`
	LastSeen int64 `db:"last_seen"`
}

func (s *UserStore) List(ctx context.Context) ([]UserSummary, error) {
	var out []UserSummary
	err := s.db.SelectContext(ctx, &out, `
		SELECT u.id, u.username, u.password_hash, u.role, u.created_at, u.updated_at,
		       COUNT(s.id) AS sessions, COALESCE(MAX(s.last_seen), 0) AS last_seen
		FROM users u LEFT JOIN sessions s ON s.user_id = u.id AND s.expires_at > ?
		GROUP BY u.id ORDER BY u.role, u.username`, now())
	return out, err
}

func (s *UserStore) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM users WHERE role = 'admin'`)
	return n, err
}

func (s *UserStore) SetRole(ctx context.Context, id int64, role string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, updated_at = ? WHERE id = ?`, role, now(), id)
	return err
}

// Delete removes a user; their sessions go with them (ON DELETE CASCADE).
func (s *UserStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
