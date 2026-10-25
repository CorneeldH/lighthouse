package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	cases := []struct {
		name, xdg, home, want string
		wantErr               bool
	}{
		{"xdg wins", "/x", "/h", "/x/lighthouse", false},
		{"home fallback", "", "/h", "/h/.cache/lighthouse", false},
		{"relative xdg kept", "rel", "/h", "rel/lighthouse", false},
		{"both empty", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", c.xdg)
			t.Setenv("HOME", c.home)
			p, err := DefaultPaths()
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if p.Dir != c.want {
				t.Errorf("Dir = %q, want %q", p.Dir, c.want)
			}
		})
	}
}

func TestPathsFiles(t *testing.T) {
	p := Paths{Dir: "/d"}
	for got, want := range map[string]string{
		p.Cache():   "/d/usage-window.json",
		p.Alarm():   "/d/usage-window.alarm",
		p.Lock():    "/d/usage-window.lock",
		p.Workdir(): "/d/usage-refresh",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestNewState(t *testing.T) {
	now := utc(t, "2026-09-05T12:00:00Z")
	w, err := Parse(fixture(t, "usage_full.json"), now, amsterdam(t))
	if err != nil {
		t.Fatal(err)
	}
	got := NewState(w, now, amsterdam(t))
	want := State{
		Schema:      1,
		GeneratedAt: "2026-09-05T12:00:00Z",
		Session:     WindowState{99, "2026-09-06T12:30:00Z", "14:30"},
		Week:        WindowState{48, "2026-09-10T14:59:00Z", "do 16:59"},
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	got = NewState(Windows{Week: w.Week}, now, amsterdam(t))
	if got.Session != (WindowState{}) {
		t.Errorf("session without reset = %+v, want zero", got.Session)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "usage-window.json")
	want := State{Schema: 1, GeneratedAt: "2026-09-05T12:00:00Z",
		Session: WindowState{99, "2026-09-06T12:30:00Z", "14:30"},
		Week:    WindowState{48, "2026-09-10T14:59:00Z", "do 16:59"}}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	got, at, ok := ReadState(path)
	if !ok {
		t.Fatal("ReadState not ok")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !at.Equal(utc(t, "2026-09-05T12:00:00Z")) {
		t.Errorf("generated_at = %v", at)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want only the cache file", len(entries))
	}
}

func TestReadStateRejects(t *testing.T) {
	cases := map[string]string{
		"absent":       "",
		"bad json":     "{",
		"other schema": `{"schema":2,"generated_at":"2026-09-05T12:00:00Z"}`,
		"bad time":     `{"schema":1,"generated_at":"gisteren"}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "c.json")
			if name != "absent" {
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, ok := ReadState(path); ok {
				t.Error("ok = true, want false")
			}
		})
	}
}

func TestWriteAtomicRemovesTmpOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("new")); err == nil {
		t.Fatal("want error when target is a non-empty directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want only the target", len(entries))
	}
}

func TestWriteAtomicKeepsOldOnFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := writeAtomic(path, []byte("new")); err == nil {
		t.Fatal("want error on read-only dir")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "old" {
		t.Errorf("file = %q, %v; want old", b, err)
	}
}
