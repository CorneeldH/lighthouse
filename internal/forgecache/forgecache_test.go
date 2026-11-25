package forgecache

import (
	"context"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/testpath"
)

func openTemp(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, filepath.Join(t.TempDir(), "sub", "c.db"))
	want := testEntry()
	if err := s.Put(ctx, "k", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "k")
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Get = %+v, %v, %v; want %+v, true, nil", got, ok, err, want)
	}
	if err := s.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, "k"); ok {
		t.Fatal("Get after Delete: ok = true")
	}
}

func TestStoreSharedBetweenOpens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put(ctx, "k", testEntry()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := openTemp(t, path)
	if _, ok, err := second.Get(ctx, "k"); err != nil || !ok {
		t.Fatalf("second Open Get = %v, %v; want found", ok, err)
	}
}

func TestStorePrunesOldEntries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	s := openTemp(t, path)
	for _, k := range []string{"old", "fresh"} {
		if err := s.Put(ctx, k, testEntry()); err != nil {
			t.Fatal(err)
		}
	}
	age := func(key string, d time.Duration) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, `UPDATE entries SET used_at = ? WHERE key = ?`, time.Now().Add(-d).Unix(), key); err != nil {
			t.Fatal(err)
		}
	}
	age("old", 8*24*time.Hour)
	age("fresh", 6*24*time.Hour)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again := openTemp(t, path)
	if _, ok, _ := again.Get(ctx, "old"); ok {
		t.Error("8 day old entry survived Open")
	}
	if _, ok, _ := again.Get(ctx, "fresh"); !ok {
		t.Error("6 day old entry was pruned")
	}
}

func TestStoreGetTouches(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, filepath.Join(t.TempDir(), "c.db"))
	if err := s.Put(ctx, "k", testEntry()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE entries SET used_at = 1`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get(ctx, "k"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	var used int64
	if err := s.db.QueryRowContext(ctx, `SELECT used_at FROM entries WHERE key = 'k'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(time.Unix(used, 0)); d > time.Minute || d < -time.Minute {
		t.Fatalf("used_at = %d, not now", used)
	}
}

func TestStoreWrongSchemaRecreates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	s := openTemp(t, path)
	if _, err := s.db.ExecContext(ctx, `DROP TABLE entries; CREATE TABLE entries(x)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := openTemp(t, path)
	if err := fresh.Put(ctx, "k", testEntry()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := fresh.Get(ctx, "k"); err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
}

func TestDefaultPath(t *testing.T) {
	x, h := testpath.Abs("x", "state"), testpath.Abs("h")
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"absolute xdg", map[string]string{"XDG_STATE_HOME": x, "HOME": h}, filepath.Join(x, "lighthouse", "forge-cache.db")},
		{"relative xdg", map[string]string{"XDG_STATE_HOME": "rel", "HOME": h}, filepath.Join(h, ".local", "state", "lighthouse", "forge-cache.db")},
		{"relative home", map[string]string{"HOME": "h"}, ""},
		{"none", map[string]string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultPath(func(k string) string { return tt.env[k] }); got != tt.want {
				t.Errorf("DefaultPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenDefault(t *testing.T) {
	var log strings.Builder
	if s := OpenDefault(func(string) string { return "" }, &log); s != nil {
		t.Errorf("no path: store = %v, want nil", s)
	}
	dir := t.TempDir()
	// A regular file where the directory should go: Open fails, one log line, nil store.
	if err := writeFile(filepath.Join(dir, "lighthouse")); err != nil {
		t.Fatal(err)
	}
	s := OpenDefault(func(k string) string {
		if k == "XDG_STATE_HOME" {
			return dir
		}
		return ""
	}, &log)
	if s != nil {
		t.Errorf("failing open: store = %v, want nil", s)
	}
	if n := strings.Count(log.String(), "\n"); n != 1 || !strings.Contains(log.String(), "forge cache") {
		t.Errorf("log = %q, want one forge cache line", log.String())
	}
	good := OpenDefault(func(k string) string {
		if k == "XDG_STATE_HOME" {
			return t.TempDir()
		}
		return ""
	}, io.Discard)
	if good == nil {
		t.Fatal("good path: store = nil")
	}
	_ = good.(*Store).Close()
}

func TestLazyOpensOnceAndOnDemand(t *testing.T) {
	state := t.TempDir()
	getenv := func(k string) string {
		if k == "XDG_STATE_HOME" {
			return state
		}
		return ""
	}
	l := NewLazy(getenv, io.Discard)
	if names, _ := readDirNames(state); len(names) != 0 {
		t.Fatalf("NewLazy created %v", names)
	}
	a, b := l.Store(), l.Store()
	if a == nil || a != b {
		t.Fatalf("Store twice = %v, %v; want the same non-nil store", a, b)
	}
	_ = a.(*Store).Close()
	if NewLazy(func(string) string { return "" }, io.Discard).Store() != nil {
		t.Error("Lazy without a path: want nil")
	}
}
