// Package state provides the SQLite-backed durable state store for codero.
//
// SQLite runs in WAL (Write-Ahead Log) mode so reads do not block writes and
// writes do not block reads. A single open connection (MaxOpenConns=1) is used
// for all writes; the WAL reader model is safe for concurrent reads on the same
// connection pool.
//
// All schema changes are managed as numbered, embedded SQL migrations
// (internal/state/migrations/). Migrations run automatically on Open and the
// daemon must exit if any migration fails — a partial schema is not safe.
package state

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // registers "sqlite3" driver for database/sql (CGo)
)

// ErrMigration is the sentinel error returned when startup migrations fail.
// Callers (daemon startup) must treat this as a fatal, non-retryable error.
var ErrMigration = errors.New("state: migration failed")

// DB wraps the SQLite database connection.
// Obtain one via Open; close with Close when the process exits.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the SQLite database at path, enables WAL mode, and
// runs all pending numbered migrations embedded in the binary.
//
// Parent directories are created automatically. Returns ErrMigration (wrapped)
// if any migration fails — the caller must treat this as a fatal startup error.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("state: create data directory: %w", err)
	}

	sqldb, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("state: open database: %w", err)
	}

	// Single writer; WAL allows concurrent readers on separate connections.
	sqldb.SetMaxOpenConns(1)

	if err := applyPragmas(sqldb); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("state: apply pragmas: %w", err)
	}

	if err := runMigrations(sqldb, path); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("%w: %w", ErrMigration, err)
	}

	return &DB{sql: sqldb}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error { return d.sql.Close() }

// Unwrap returns the underlying *sql.DB.
// Other packages in the state domain may use this for typed queries.
func (d *DB) Unwrap() *sql.DB { return d.sql }

// applyPragmas sets mandatory SQLite runtime options on the connection.
// WAL mode must be set before any migrations run.
func applyPragmas(db *sql.DB) error {
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("exec %q: %w", pragma, err)
		}
	}
	return nil
}
