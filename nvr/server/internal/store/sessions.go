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

// Device is a session as shown to its owner.
type Device struct {
	ID        string `db:"id"`
	CreatedAt int64  `db:"created_at"`
	LastSeen  int64  `db:"last_seen"`
	UserAgent string `db:"user_agent"`
	IP        string `db:"ip"`
}

func (s *SessionStore) ListByUser(ctx context.Context, userID int64) ([]Device, error) {
	var out []Device
	err := s.db.SelectContext(ctx, &out, `
		SELECT id, created_at, last_seen, COALESCE(user_agent, '') AS user_agent, COALESCE(ip, '') AS ip
		FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_seen DESC`, userID, now())
	return out, err
}

// DeleteForUser removes one session, only if it belongs to userID.
func (s *SessionStore) DeleteForUser(ctx context.Context, userID int64, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
