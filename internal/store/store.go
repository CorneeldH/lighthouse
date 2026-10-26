// Package store opens the SQLite database behind lighthouse serve.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

// Store is an open database plus the lock that keeps other servers out.
type Store struct {
	DB   *sql.DB
	lock *os.File
}

// DefaultPath returns ${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/lighthouse.db.
// XDG_STATE_HOME counts only when it is an absolute path.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "lighthouse", "lighthouse.db"), nil
}

// dsn builds the connection string; the pragmas apply to every connection.
// path must be absolute: the file: URI form is built from a URL, which
// escapes ?, # and % so SQLite opens exactly path.
func dsn(path string) string {
	q := url.Values{"_pragma": {
		"journal_mode(WAL)",
		"busy_timeout(5000)",
		"foreign_keys(ON)",
		"synchronous(NORMAL)",
	}}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
}

// Open takes the single-server lock and opens the database at path,
// creating its directory (0700) when missing. A second Open on the same
// path fails with a *LockedError until the first Store is closed.
func Open(ctx context.Context, path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	lock, err := acquireLock(path + ".lock")
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err == nil {
		err = db.PingContext(ctx)
		if err == nil {
			err = migrate(ctx, db, migrationFS)
		}
		if err != nil {
			_ = db.Close()
		}
	}
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return &Store{DB: db, lock: lock}, nil
}

// Close closes the database, then releases the lock.
func (s *Store) Close() error {
	return errors.Join(s.DB.Close(), s.lock.Close())
}
