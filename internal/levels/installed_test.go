package levels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// red if an absent components.toml is reported as an error or as present.
func TestReadInstalledAbsent(t *testing.T) {
	_, ok, err := ReadInstalled(t.TempDir())
	if ok || err != nil {
		t.Fatalf("ok=%v err=%v, want false, nil", ok, err)
	}
}

// red if a corrupt components.toml is silently read as absent or empty.
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

// red if Marshal writes on entries, loses the header, stops sorting, or writes something ReadInstalled cannot read back.
func TestMarshalRoundTrip(t *testing.T) {
	in := Installed{
		Level: "light",
		Since: "2026-11-22T10:00:00Z",
		Components: map[string]Stand{
			"md-link-check": StandOff,
			"usage-gate":    StandLog,
			"zeta-gate":     StandOff,
			"alpha-check":   StandLog,
			"stays-on":      StandOn,
		},
	}
	b := in.Marshal()
	if !strings.HasPrefix(string(b), "# Managed by lighthouse init. Do not edit by hand.\n") {
		t.Errorf("missing header:\n%s", b)
	}
	wantBytes := "# Managed by lighthouse init. Do not edit by hand.\n" +
		"level = \"light\"\n" +
		"since = \"2026-11-22T10:00:00Z\"\n" +
		"\n[components]\n" +
		"\"alpha-check\" = \"log\"\n" +
		"\"md-link-check\" = \"off\"\n" +
		"\"usage-gate\" = \"log\"\n" +
		"\"zeta-gate\" = \"off\"\n"
	if string(b) != wantBytes {
		t.Errorf("Marshal =\n%s\nwant\n%s", b, wantBytes)
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
	if len(got.Components) != 4 {
		t.Errorf("Components = %v, want 4 entries", got.Components)
	}
}

// red if Installed.Stand defaults an unlisted component to anything but on.
func TestInstalledStandUnlistedIsOn(t *testing.T) {
	if got := (Installed{}).Stand("x"); got != StandOn {
		t.Errorf("Stand = %q", got)
	}
}
