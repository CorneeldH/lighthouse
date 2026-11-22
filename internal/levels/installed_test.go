package levels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadInstalledAbsent(t *testing.T) {
	_, ok, err := ReadInstalled(t.TempDir())
	if ok || err != nil {
		t.Fatalf("ok=%v err=%v, want false, nil", ok, err)
	}
}

func TestReadInstalledCorrupt(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, InstalledPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("level = = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadInstalled(root); err == nil {
		t.Fatal("want error for corrupt file")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := Installed{
		Level: "light",
		Since: "2026-11-22T10:00:00Z",
		Components: map[string]Stand{
			"md-link-check": StandOff,
			"usage-gate":    StandLog,
			"stays-on":      StandOn,
		},
	}
	b := in.Marshal()
	if !strings.HasPrefix(string(b), "# Managed by lighthouse init. Do not edit by hand.\n") {
		t.Errorf("missing header:\n%s", b)
	}
	if strings.Contains(string(b), "stays-on") || strings.Contains(string(b), `"on"`) {
		t.Errorf("on entry written:\n%s", b)
	}
	if string(in.Marshal()) != string(b) {
		t.Error("Marshal is not deterministic")
	}
	root := t.TempDir()
	p := filepath.Join(root, InstalledPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadInstalled(root)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.Level != "light" || got.Since != in.Since {
		t.Errorf("got %+v", got)
	}
	if got.Stand("md-link-check") != StandOff || got.Stand("usage-gate") != StandLog || got.Stand("stays-on") != StandOn {
		t.Errorf("stands wrong: %v", got.Components)
	}
	if len(got.Components) != 2 {
		t.Errorf("Components = %v, want 2 entries", got.Components)
	}
}

func TestInstalledStandUnlistedIsOn(t *testing.T) {
	if got := (Installed{}).Stand("x"); got != StandOn {
		t.Errorf("Stand = %q", got)
	}
}
