package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	tests := []struct {
		name, xdg, home, want string
	}{
		{"absolute xdg", "/x/state", "/h", "/x/state/lighthouse/lighthouse.db"},
		{"relative xdg ignored", "rel/state", "/h", "/h/.local/state/lighthouse/lighthouse.db"},
		{"empty xdg", "", "/h", "/h/.local/state/lighthouse/lighthouse.db"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tc.xdg)
			t.Setenv("HOME", tc.home)
			got, err := DefaultPath()
			if err != nil {
				t.Fatalf("DefaultPath: %v", err)
			}
			if got != tc.want {
				t.Errorf("DefaultPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenSetsPragmas(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "My State")
	s, err := Open(ctx, filepath.Join(dir, "l.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	checkPragmas(t, s)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 700", info.Mode().Perm())
	}
}

func TestOpenSharedSetsPragmas(t *testing.T) {
	s, err := OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	defer func() { _ = s.Close() }()
	checkPragmas(t, s)
}

// checkPragmas reads the pragmas on two distinct connections: each pragma
// from the DSN must hold on every connection, not only the first.
func checkPragmas(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	c1, err := s.DB.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn 1: %v", err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := s.DB.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn 2: %v", err)
	}
	defer func() { _ = c2.Close() }()

	want := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"busy_timeout", "5000"},
		{"foreign_keys", "1"},
		{"synchronous", "1"},
	}
	for n, conn := range []*sql.Conn{c1, c2} {
		for _, w := range want {
			var got string
			if err := conn.QueryRowContext(ctx, "PRAGMA "+w.pragma).Scan(&got); err != nil {
				t.Fatalf("conn %d PRAGMA %s: %v", n+1, w.pragma, err)
			}
			if got != w.want {
				t.Errorf("conn %d PRAGMA %s = %q, want %q", n+1, w.pragma, got, w.want)
			}
		}
	}
}

func TestDSNTxLock(t *testing.T) {
	u, err := url.Parse(dsn("/x/l.db"))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := u.Query()
	if got := q.Get("_txlock"); got != "immediate" {
		t.Errorf("_txlock = %q, want %q", got, "immediate")
	}
	for _, p := range q["_pragma"] {
		if strings.HasPrefix(p, "journal_mode") {
			t.Errorf("dsn sets %q; journal_mode belongs to the one-time step in open", p)
		}
	}
}

func TestOpenSharedTakesNoLock(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "l.db")
	served, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = served.Close() }()
	for i := 1; i <= 2; i++ {
		s, err := OpenShared(ctx, path)
		if err != nil {
			t.Fatalf("OpenShared %d next to a serve lock: %v", i, err)
		}
		defer func() { _ = s.Close() }()
	}

	fresh := filepath.Join(t.TempDir(), "sub", "l.db")
	s, err := OpenShared(ctx, fresh)
	if err != nil {
		t.Fatalf("OpenShared on a fresh path: %v", err)
	}
	if _, err := os.Stat(fresh + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat %s.lock: err = %v, want not exist", fresh, err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close shared store: %v", err)
	}
}

func TestOpenTwiceIsLocked(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "l.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_, err = Open(ctx, path)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open err = %v, want ErrLocked", err)
	}
	if want := "another lighthouse serve holds " + path + ".lock"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	third, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("third Open: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Errorf("Close third: %v", err)
	}
}

func TestOpenPutsFilesAtThePath(t *testing.T) {
	for _, name := range []string{"plain", "with space", "with?query", "with#hash", "with%41pct"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			path := filepath.Join(dir, "l.db")
			s, err := Open(context.Background(), path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() { _ = s.Close() }()
			for _, f := range []string{path, path + ".lock"} {
				if _, err := os.Stat(f); err != nil {
					t.Errorf("expected file: %v", err)
				}
			}
			entries, err := os.ReadDir(filepath.Dir(dir))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("parent holds %d entries, want only %q", len(entries), name)
			}
		})
	}
}

func TestOpenRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Open(context.Background(), filepath.Join("sub", "l.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	for _, f := range []string{"l.db", "l.db.lock"} {
		if _, err := os.Stat(filepath.Join(dir, "sub", f)); err != nil {
			t.Errorf("expected file: %v", err)
		}
	}
}
