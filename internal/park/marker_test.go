package park

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/testpath"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func markerJSON(id, reset, created string) string {
	return `{"session_id":"` + id + `","cwd":"/w","window":"session","reset_at":"` + reset +
		`","created_at":"` + created + `","source":"usage-guard"}`
}

const (
	resetOK   = "2026-11-01T21:00:00+01:00"
	createdOK = "2026-11-01T13:00:00Z"
)

func TestReadMarkers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.json", `{"session_id":"b","cwd":"/w","window":"week","reset_at":"`+resetOK+
		`","created_at":"`+createdOK+`","source":"usage-guard","controller":true}`)
	writeFile(t, dir, "a.json", markerJSON("a", resetOK, createdOK))
	writeFile(t, dir, "a-b.json", markerJSON("a-b", resetOK, createdOK))
	writeFile(t, dir, "a.handoff.md", "# handoff")

	got, err := ReadMarkers(dir)
	if err != nil {
		t.Fatal(err)
	}
	// "a-b.json" sorts before "a.json" by file name, so only a sort on SessionID gives a, a-b, b.
	if len(got) != 3 || got[0].SessionID != "a" || got[1].SessionID != "a-b" || got[2].SessionID != "b" {
		t.Fatalf("markers = %+v, want a, a-b, b", got)
	}
	if got[0].Controller || !got[2].Controller {
		t.Errorf("Controller = %v, %v; want false, true", got[0].Controller, got[2].Controller)
	}
	if got[2].Window != "week" || got[2].Cwd != "/w" {
		t.Errorf("fields not read: %+v", got[2])
	}
}

func TestReadMarkersSkipsBadFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.json", "{")
	writeFile(t, dir, "a.json", markerJSON("b", resetOK, createdOK))
	writeFile(t, dir, "x.y.json", markerJSON("x.y", resetOK, createdOK))
	writeFile(t, dir, "r.json", markerJSON("r", "morgen", createdOK))
	writeFile(t, dir, "c.json", markerJSON("c", resetOK, "gisteren"))
	writeFile(t, dir, "ok.json", markerJSON("ok", resetOK, createdOK))

	got, err := ReadMarkers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "ok" {
		t.Fatalf("markers = %+v, want only ok", got)
	}
	if _, ok := got[0].Reset(); !ok {
		t.Error("Reset() of a returned marker must read")
	}
	if _, ok := got[0].Created(); !ok {
		t.Error("Created() of a returned marker must read")
	}
}

func TestReadMarkersMissingDir(t *testing.T) {
	got, err := ReadMarkers(filepath.Join(t.TempDir(), "nope"))
	if got != nil || err != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestMarkerKey(t *testing.T) {
	m := Marker{SessionID: "s1", CreatedAt: createdOK, ResetAt: resetOK}
	want := "usage:s1@2026-11-01T13:00:00Z:2026-11-01T21:00:00+01:00"
	if got := m.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestRemoveMarkerOnlySameKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "s1.json", markerJSON("s1", resetOK, createdOK))
	keyA := "usage:s1@" + createdOK + ":" + resetOK
	keyB := "usage:s1@2026-11-01T14:00:00Z:" + resetOK
	path := filepath.Join(dir, "s1.json")

	if err := RemoveMarker(dir, "s1", keyB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("marker with another key must stay: %v", err)
	}
	if err := RemoveMarker(dir, "s1", keyA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("marker with the same key must go, stat err = %v", err)
	}
	if err := RemoveMarker(dir, "s1", keyA); err != nil {
		t.Errorf("second remove = %v, want nil", err)
	}
}

func TestMarkerDir(t *testing.T) {
	x, h := testpath.Abs("x"), testpath.Abs("h")
	homeResume := filepath.Join(h, ".local", "share", "lighthouse", "resume")
	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{"xdg", map[string]string{"XDG_DATA_HOME": x, "HOME": h}, filepath.Join(x, "lighthouse", "resume"), false},
		{"home", map[string]string{"HOME": h}, homeResume, false},
		{"relative xdg falls to home", map[string]string{"XDG_DATA_HOME": "x", "HOME": h}, homeResume, false},
		{"both relative", map[string]string{"XDG_DATA_HOME": "x", "HOME": "h"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarkerDir(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("MarkerDir = %q, %v; want %q, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestMarkerDirControllerRunUsesStateDir(t *testing.T) {
	run := "11111111-2222-4333-8444-555555555555"
	stateDir := testpath.Abs("home", "corneel", ".local", "state", "lighthouse", "dispatch")
	env := map[string]string{"HOME": testpath.Abs("home", "vscode"), "LIGHTHOUSE_CONTROLLER_RUN": run, "LIGHTHOUSE_STATE_DIR": stateDir}
	got, err := MarkerDir(func(k string) string { return env[k] })
	if err != nil || got != filepath.Join(stateDir, "resume") {
		t.Fatalf("MarkerDir in a container = %q, %v", got, err)
	}
	// The host (no controller run) reads the user dir and the state dir, so it finds that marker.
	host := map[string]string{"HOME": testpath.Abs("home", "corneel")}
	dirs, err := HostMarkerDirs(func(k string) string { return host[k] }, stateDir)
	if err != nil || len(dirs) != 2 || dirs[1] != got {
		t.Fatalf("HostMarkerDirs = %v, %v: want the state resume dir second", dirs, err)
	}
}

func TestHostReadsMarkerFromStateDirAndFlagsUnreadable(t *testing.T) {
	state, user := t.TempDir(), t.TempDir()
	dirs := []string{user, StateResumeDir(state)}
	if err := os.MkdirAll(dirs[1], 0o755); err != nil {
		t.Fatal(err)
	}
	good := `{"session_id":"s1","reset_at":"2026-10-31T11:30:00Z","created_at":"2026-10-31T11:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dirs[1], "s1.json"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs[1], "s2.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := ReadMarkersIn(dirs)
	if err != nil || len(ms) != 1 || ms[0].SessionID != "s1" {
		t.Fatalf("ReadMarkersIn = %v, %v", ms, err)
	}
	if got := UnreadableIn(dirs); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("UnreadableIn = %v, want [s2]", got)
	}
	if err := RemoveMarkerIn(dirs, "s1", ms[0].Key()); err != nil {
		t.Fatal(err)
	}
	if ms, _ := ReadMarkersIn(dirs); len(ms) != 0 {
		t.Fatalf("marker still there: %v", ms)
	}
}
