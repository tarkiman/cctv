package store

import (
	"context"

	"github.com/jmoiron/sqlx"
)

type Session struct {
	ID        string `db:"id"`
	UserID    int64  `db:"user_id"`
	CreatedAt int64  `db:"created_at"`
	ExpiresAt int64  `db:"expires_at"`
	LastSeen  int64  `db:"last_seen"`
}

type SessionStore struct{ db *sqlx.DB }

func (s *SessionStore) Create(ctx context.Context, id string, userID, expiresAt int64, userAgent, ip string) error {
	t := now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, userID, t, expiresAt, t, userAgent, ip)
	return err
}

// Valid returns the session if it exists and has not expired.
func (s *SessionStore) Valid(ctx context.Context, id string) (*Session, error) {
	var ss Session
	err := s.db.GetContext(ctx, &ss, `
		SELECT id, user_id, created_at, expires_at, last_seen
		FROM sessions WHERE id = ? AND expires_at > ?`, id, now())
	if err != nil {
		return nil, notFound(err)
	}
	return &ss, nil
}

func (s *SessionStore) Touch(ctx context.Context, id string, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen = ?, expires_at = ? WHERE id = ?`,
		now(), expiresAt, id)
	return err
}

func (s *SessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteOthers removes every session of the user except keepID (used after a password change).
func (s *SessionStore) DeleteOthers(ctx context.Context, userID int64, keepID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepID)
	return err
}

func (s *SessionStore) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now())
	return err
}
