// Package forgecache is the persistent store behind the forge's ETag cache: one SQLite file
// next to the state database, shared by every lighthouse process. It is a cache, not state: a
// file that cannot be opened or has the wrong schema means no cache, never a failed command.
package forgecache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/store"
	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

// MaxAge is how long an entry may go unused before Open removes it.
const MaxAge = 7 * 24 * time.Hour

const createTable = `CREATE TABLE IF NOT EXISTS entries(
	key TEXT PRIMARY KEY,
	etag TEXT NOT NULL,
	link TEXT NOT NULL,
	body BLOB NOT NULL,
	used_at INTEGER NOT NULL)`

// wantColumns is the schema of entries, in table order.
var wantColumns = []string{"key", "etag", "link", "body", "used_at"}

// Store is the SQLite-backed forge.CacheStore.
type Store struct{ db *sql.DB }

var _ forge.CacheStore = (*Store)(nil)

// DefaultPath is <state>/lighthouse/forge-cache.db, with <state> the XDG_STATE_HOME when that
// is absolute, else $HOME/.local/state when HOME is absolute. It is "" when neither is.
func DefaultPath(getenv func(string) string) string {
	state := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "lighthouse", "forge-cache.db")
}

func dsn(path string) string {
	return store.FileURI(path, url.Values{"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)"}})
}

// Open opens the cache file at path (an absolute path), creating its directory (0o700) and its
// table. A table with another schema is dropped and recreated. Entries unused for MaxAge go.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create forge cache directory: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open forge cache %s: %w", path, err)
	}
	if err := prepare(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open forge cache %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func prepare(ctx context.Context, db *sql.DB) error {
	cols, err := columns(ctx, db)
	if err != nil {
		return err
	}
	if len(cols) > 0 && strings.Join(cols, ",") != strings.Join(wantColumns, ",") {
		if _, err := db.ExecContext(ctx, `DROP TABLE entries`); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM entries WHERE used_at < ?`, time.Now().Add(-MaxAge).Unix())
	return err
}

// columns lists the column names of entries; none when the table does not exist.
func columns(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('entries') ORDER BY cid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Get returns the entry of key and marks it used now.
func (s *Store) Get(ctx context.Context, key string) (forge.CacheEntry, bool, error) {
	var e forge.CacheEntry
	err := s.db.QueryRowContext(ctx, `SELECT etag, link, body FROM entries WHERE key = ?`, key).Scan(&e.ETag, &e.Link, &e.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return forge.CacheEntry{}, false, nil
	}
	if err != nil {
		return forge.CacheEntry{}, false, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE entries SET used_at = ? WHERE key = ?`, time.Now().Unix(), key); err != nil {
		return forge.CacheEntry{}, false, err
	}
	return e, true, nil
}

// Put stores e under key, replacing an earlier entry.
func (s *Store) Put(ctx context.Context, key string, e forge.CacheEntry) error {
	body := e.Body
	if body == nil {
		body = []byte{}
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO entries(key, etag, link, body, used_at) VALUES(?, ?, ?, ?, ?)`,
		key, e.ETag, e.Link, body, time.Now().Unix())
	return err
}

// Delete removes the entry of key; a missing key is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE key = ?`, key)
	return err
}

// OpenDefault opens the cache at DefaultPath. It returns nil, never an error, when there is no
// path or the open fails; a failure is one line on log.
func OpenDefault(getenv func(string) string, log io.Writer) forge.CacheStore {
	path := DefaultPath(getenv)
	if path == "" {
		return nil
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		_, _ = fmt.Fprintf(log, "lighthouse: forge cache off: %v\n", err)
		return nil
	}
	return s
}

// Lazy opens the default cache on the first Store call, once per process, so that building an
// opener creates no file.
type Lazy struct {
	getenv func(string) string
	log    io.Writer
	once   sync.Once
	store  forge.CacheStore
}

// NewLazy returns a Lazy that reads the state directory through getenv and logs to log.
func NewLazy(getenv func(string) string, log io.Writer) *Lazy {
	return &Lazy{getenv: getenv, log: log}
}

// Store is the default cache, opened on the first call; nil when there is none.
func (l *Lazy) Store() forge.CacheStore {
	l.once.Do(func() { l.store = OpenDefault(l.getenv, l.log) })
	return l.store
}
