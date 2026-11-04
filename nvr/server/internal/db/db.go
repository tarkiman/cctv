// Package db opens the SQLite database and applies embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the database at path and migrates it to the latest
// version. Before migrating an existing database to a newer version, it keeps a copy in
// backup/.
func Open(ctx context.Context, path string) (*sqlx.DB, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite has a single writer; one connection avoids SQLITE_BUSY between our own goroutines.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(ctx, db.DB, path); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB, path string) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	current, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return err
	}
	all, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	latest, err := all.Last()
	if err != nil {
		return err
	}
	if current > 0 && current < latest.Version {
		if err := backup(ctx, db, path, current); err != nil {
			return fmt.Errorf("backup before migration: %w", err)
		}
	}
	return goose.UpContext(ctx, db, "migrations")
}

func backup(ctx context.Context, db *sql.DB, path string, version int64) error {
	dir := filepath.Join(filepath.Dir(path), "backup")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	dst := filepath.Join(dir, fmt.Sprintf("nvr-v%d-%s.db", version, time.Now().UTC().Format("20060102T150405")))
	_, err := db.ExecContext(ctx, "VACUUM INTO ?", dst)
	return err
}

// Backup writes a consistent copy of the database to dst.
func Backup(ctx context.Context, db *sqlx.DB, dst string) error {
	_, err := db.ExecContext(ctx, "VACUUM INTO ?", dst)
	return err
}
