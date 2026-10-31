// Package store opens the SQLite database that serve, tick and run share.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite" // pure-Go driver, registers "sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Store is an open database plus, for serve, the lock that keeps other
// servers out.
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
// journal_mode is left out: it is a property of the file, set once by
// setWAL, and setting it on every connection races a concurrent open into
// SQLITE_BUSY. _txlock=immediate makes every transaction take the write
// lock at BEGIN, so a migration rereads user_version under that lock.
// path must be absolute: the file: URI form is built from a URL, which
// escapes ?, # and % so SQLite opens exactly path.
func dsn(path string) string {
	q := url.Values{
		"_pragma": {
			"busy_timeout(5000)",
			"foreign_keys(ON)",
			"synchronous(NORMAL)",
		},
		"_txlock": {"immediate"},
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
}

const (
	walAttempts = 50
	walPause    = 20 * time.Millisecond
)

// setWAL switches the database file to WAL. A concurrent open can make the
// switch fail with SQLITE_BUSY without SQLite calling the busy handler, so
// busy_timeout does not cover it: SQLITE_BUSY is retried walAttempts times,
// walPause apart, before it is returned.
func setWAL(ctx context.Context, db *sql.DB) error {
	var mode string
	var err error
	for range walAttempts {
		err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode)
		if !isBusy(err) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(walPause):
		}
	}
	if err != nil {
		return fmt.Errorf("set journal_mode: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("set journal_mode: database answered %q, want wal", mode)
	}
	return nil
}

func isBusy(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && e.Code()&0xff == sqlite3.SQLITE_BUSY
}

// Open takes the single-server lock and opens the database at path,
// creating its directory (0700) when missing. A second Open on the same
// path fails with a *LockedError until the first Store is closed.
func Open(ctx context.Context, path string) (*Store, error) {
	return open(ctx, path, true)
}

// OpenShared opens the database at path like Open, without the
// single-server lock: it creates no <path>.lock and runs next to a serve.
func OpenShared(ctx context.Context, path string) (*Store, error) {
	return open(ctx, path, false)
}

func open(ctx context.Context, path string, lock bool) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	var lockFile *os.File
	if lock {
		if lockFile, err = acquireLock(path + ".lock"); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err == nil {
		err = setWAL(ctx, db)
		if err == nil {
			err = migrate(ctx, db, migrationFS)
		}
		if err != nil {
			_ = db.Close()
		}
	}
	if err != nil {
		if lockFile != nil {
			_ = lockFile.Close()
		}
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return &Store{DB: db, lock: lockFile}, nil
}

// Close closes the database, then releases the lock when the Store holds one.
func (s *Store) Close() error {
	err := s.DB.Close()
	if s.lock != nil {
		err = errors.Join(err, s.lock.Close())
	}
	return err
}
