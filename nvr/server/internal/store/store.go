// Package store holds hand-written SQL for each table, mapped with sqlx.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sqlx.DB

	Users      *UserStore
	Sessions   *SessionStore
	Cameras    *CameraStore
	Recordings *RecordingStore
	Settings   *SettingStore
}

func New(db *sqlx.DB) *Store {
	return &Store{
		db:         db,
		Users:      &UserStore{db: db},
		Sessions:   &SessionStore{db: db},
		Cameras:    &CameraStore{db: db},
		Recordings: &RecordingStore{db: db},
		Settings:   &SettingStore{db: db},
	}
}

func (s *Store) DB() *sqlx.DB { return s.db }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func now() int64 { return time.Now().Unix() }

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
