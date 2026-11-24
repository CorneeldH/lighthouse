package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSidecarMissing(t *testing.T) {
	obs, err := ReadSidecar(t.TempDir(), "s1")
	if err != nil || obs != nil {
		t.Fatalf("obs %v, err %v", obs, err)
	}
}

func TestAppendSidecarKeepsOrder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	a := Observation{At: at(1), LighthouseVersion: "1.0", PackVersion: "p1"}
	b := Observation{At: at(2), LighthouseVersion: "1.1", PackVersion: "p2"}
	for _, o := range []Observation{a, b} {
		if err := AppendSidecar(dir, "s1", o); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadSidecar(dir, "s1")
	if err != nil || len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if other, _ := ReadSidecar(dir, "s2"); other != nil {
		t.Errorf("other session: %+v", other)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

func TestReadSidecarCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s1.observations.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSidecar(dir, "s1"); err == nil {
		t.Fatal("want error")
	}
}

func TestSidecarRejectsPathSession(t *testing.T) {
	if err := AppendSidecar(t.TempDir(), "../x", Observation{}); err == nil {
		t.Fatal("want error")
	}
	if _, err := ReadSidecar(t.TempDir(), ""); err == nil {
		t.Fatal("want error")
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestStateDirFallback(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
		err  bool
	}{
		{"override", map[string]string{"LIGHTHOUSE_STATE_DIR": "/o", "XDG_STATE_HOME": "/x"}, "/o", false},
		{"xdg absolute", map[string]string{"XDG_STATE_HOME": "/x"}, "/x/lighthouse/dispatch", false},
		{"xdg relative falls to home", map[string]string{"XDG_STATE_HOME": "x", "HOME": "/h"}, "/h/.local/state/lighthouse/dispatch", false},
		{"home only", map[string]string{"HOME": "/h"}, "/h/.local/state/lighthouse/dispatch", false},
		{"nothing", map[string]string{}, "", true},
	}
	for _, c := range cases {
		got, err := StateDir(env(c.env))
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%s: %q, %v; want %q err=%v", c.name, got, err, c.want, c.err)
		}
	}
}
