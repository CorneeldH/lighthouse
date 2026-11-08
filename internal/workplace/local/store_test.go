//go:build linux

package local

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (store, string) {
	t.Helper()
	state := t.TempDir()
	s, err := (&Adapter{Repo: "o/r", StateDir: state}).store()
	if err != nil {
		t.Fatal(err)
	}
	return s, state
}

func TestStoreRefusesBadConfig(t *testing.T) {
	abs := t.TempDir()
	cases := map[string]Adapter{
		"NoSlash":     {Repo: "or", StateDir: abs},
		"ThreeParts":  {Repo: "o/r/x", StateDir: abs},
		"DotDotOwner": {Repo: "../r", StateDir: abs},
		"DotName":     {Repo: "o/.", StateDir: abs},
		"EmptyName":   {Repo: "o/", StateDir: abs},
		"Space":       {Repo: "o/r x", StateDir: abs},
		"Empty":       {Repo: "", StateDir: abs},
		"RelativeDir": {Repo: "o/r", StateDir: "state"},
		"EmptyDir":    {Repo: "o/r", StateDir: ""},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := a.store(); err == nil {
				t.Errorf("store() with Repo %q, StateDir %q: err = nil, want an error", a.Repo, a.StateDir)
			}
		})
	}
	s, err := (&Adapter{Repo: "Own-er.1/na_me", StateDir: abs}).store()
	if err != nil {
		t.Fatalf("store() of a valid repo: %v", err)
	}
	if want := filepath.Join(abs, "local", "Own-er.1", "na_me"); s.dir != want {
		t.Errorf("dir = %q, want %q", s.dir, want)
	}
}

func TestStoreFileModes(t *testing.T) {
	s, state := newStore(t)
	rec := wsRecord{ID: "local-a-123456", Path: "/p", Branch: "a", CreatedAt: time.Date(2026, 11, 7, 1, 2, 3, 0, time.UTC)}
	if err := s.putWorkspace(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.putSession(rec.ID, sessRecord{Key: "sid.0a1b2c3d", SessionID: "sid", Mode: "start"}); err != nil {
		t.Fatal(err)
	}
	files := 0
	err := filepath.WalkDir(filepath.Join(state, "local"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(0o600)
		if d.IsDir() {
			want = 0o700
		} else {
			files++
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", path, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}
	got, ok, err := s.workspace(rec.ID)
	if err != nil || !ok || got != rec {
		t.Errorf("workspace() = %+v, %v, %v; want %+v, true, nil", got, ok, err, rec)
	}
	ss, err := s.sessions(rec.ID)
	if err != nil || len(ss) != 1 || ss[0].Key != "sid.0a1b2c3d" || ss[0].SessionID != "sid" {
		t.Errorf("sessions() = %+v, %v; want the one session", ss, err)
	}
}

func TestStoreMissingIsEmpty(t *testing.T) {
	s, _ := newStore(t)
	if _, ok, err := s.workspace("local-x-000000"); ok || err != nil {
		t.Errorf("workspace() = ok %v, err %v; want false, nil", ok, err)
	}
	if rs, err := s.workspaces(); len(rs) != 0 || err != nil {
		t.Errorf("workspaces() = %v, %v; want none", rs, err)
	}
	if ss, err := s.sessions("local-x-000000"); len(ss) != 0 || err != nil {
		t.Errorf("sessions() = %v, %v; want none", ss, err)
	}
	if err := s.dropSession("local-x-000000", "k"); err != nil {
		t.Errorf("dropSession of nothing: %v", err)
	}
	if err := s.dropWorkspace("local-x-000000"); err != nil {
		t.Errorf("dropWorkspace of nothing: %v", err)
	}
}

func TestStoreDropWorkspaceRemovesAll(t *testing.T) {
	s, state := newStore(t)
	id, other := "local-a-111111", "local-b-222222"
	for _, ws := range []string{id, other} {
		if err := s.putWorkspace(wsRecord{ID: ws, Path: "/p/" + ws, Branch: "a"}); err != nil {
			t.Fatal(err)
		}
		key := ws + "-sid.0a1b2c3d"
		if err := s.putSession(ws, sessRecord{Key: key, SessionID: ws + "-sid"}); err != nil {
			t.Fatal(err)
		}
		writeFile(t, s.pidPath(ws, key), "12 34\n")
		writeFile(t, s.logPath(key), "log\n")
	}
	// A pid file whose record never landed still goes, its log too.
	writeFile(t, s.pidPath(id, "orphan.0a1b2c3d"), "13 35\n")
	writeFile(t, s.logPath("orphan.0a1b2c3d"), "log\n")

	if err := s.dropWorkspace(id); err != nil {
		t.Fatal(err)
	}
	left := []string{}
	err := filepath.WalkDir(filepath.Join(state, "local"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, "local-a-") || strings.Contains(path, "orphan") {
			left = append(left, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("left after dropWorkspace: %v", left)
	}
	for _, p := range []string{
		filepath.Join(s.dir, "workspaces", other+".json"),
		s.pidPath(other, other+"-sid.0a1b2c3d"),
		s.logPath(other + "-sid.0a1b2c3d"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the other workspace lost %s: %v", p, err)
		}
	}
}

func TestStoreDropSession(t *testing.T) {
	s, _ := newStore(t)
	id := "local-a-111111"
	for _, key := range []string{"a.00000000", "b.00000000"} {
		if err := s.putSession(id, sessRecord{Key: key, SessionID: key[:1]}); err != nil {
			t.Fatal(err)
		}
		writeFile(t, s.pidPath(id, key), "12 34\n")
		writeFile(t, s.logPath(key), "log\n")
	}
	if err := s.dropSession(id, "a.00000000"); err != nil {
		t.Fatal(err)
	}
	ss, err := s.sessions(id)
	if err != nil || len(ss) != 1 || ss[0].Key != "b.00000000" {
		t.Errorf("sessions() = %+v, %v; want only b.00000000", ss, err)
	}
	for _, p := range []string{s.pidPath(id, "a.00000000"), s.logPath("a.00000000")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s: stat err = %v, want not exist", p, err)
		}
	}
	if _, _, ok, _ := s.pid(id, "b.00000000"); !ok {
		t.Error("pid of b.00000000 is gone, want kept")
	}
}

func TestStorePid(t *testing.T) {
	s, _ := newStore(t)
	id := "local-a-111111"
	if _, _, ok, err := s.pid(id, "missing"); ok || err != nil {
		t.Errorf("pid of a missing file = ok %v, err %v; want false, nil", ok, err)
	}
	for _, bad := range []string{"abc", "", "12\n", "12 34 56\n", "-1 34\n", "0 34\n", "12 x\n"} {
		writeFile(t, s.pidPath(id, "k"), bad)
		if _, _, _, err := s.pid(id, "k"); err == nil {
			t.Errorf("pid of %q: err = nil, want an error", bad)
		}
	}
	writeFile(t, s.pidPath(id, "k"), "12 34\n")
	pid, start, ok, err := s.pid(id, "k")
	if pid != 12 || start != 34 || !ok || err != nil {
		t.Errorf("pid = %d, %d, %v, %v; want 12, 34, true, nil", pid, start, ok, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
