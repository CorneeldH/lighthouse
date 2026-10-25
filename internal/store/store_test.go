package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 700", info.Mode().Perm())
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
