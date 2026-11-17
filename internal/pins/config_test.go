package pins

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeToml(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPins_defaults(t *testing.T) {
	p := writeToml(t, `
[pins.kenn-forge]
kind = "tool"
repo = "kenn-io/forge"
version = "0.5.1"
`)
	got, err := LoadPins(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pin{{
		Name: "kenn-forge", Kind: KindTool, Repo: "kenn-io/forge", Version: "0.5.1",
		Command: "kenn-forge", VersionArgs: []string{"--version"}, TagPrefix: "v",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadPins_explicitValuesAndSortedByName(t *testing.T) {
	p := writeToml(t, `
[pins.roborev]
kind = "tool"
repo = "roborev-dev/roborev"
version = "0.69.0"
version_args = ["version"]

[pins.codex]
kind = "tool"
repo = "openai/codex"
version = "0.157.0"
tag_prefix = "rust-v"
command = "codex-cli"

[pins.superpowers]
kind = "plugin"
repo = "obra/superpowers"
version = "7.0.2"
`)
	got, err := LoadPins(p)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, pin := range got {
		names = append(names, pin.Name)
	}
	if want := []string{"codex", "roborev", "superpowers"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("order = %v, want %v", names, want)
	}
	if got[0].TagPrefix != "rust-v" || got[0].Command != "codex-cli" {
		t.Errorf("codex = %+v", got[0])
	}
	if !reflect.DeepEqual(got[1].VersionArgs, []string{"version"}) {
		t.Errorf("roborev VersionArgs = %v", got[1].VersionArgs)
	}
	if got[2].Kind != KindPlugin {
		t.Errorf("superpowers kind = %q", got[2].Kind)
	}
}

func TestLoadPins_noSection(t *testing.T) {
	p := writeToml(t, "[project]\nlanguage = \"nl\"\n")
	got, err := LoadPins(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestLoadPins_invalidPinIsErrNotFatal(t *testing.T) {
	p := writeToml(t, `
[pins.good]
kind = "tool"
repo = "o/good"
version = "1.0.0"

[pins.badkind]
kind = "x"
repo = "o/badkind"
version = "1.0.0"

[pins.noversion]
kind = "plugin"
repo = "o/noversion"

[pins.norepo]
kind = "plugin"
version = "1.0.0"
`)
	got, err := LoadPins(p)
	if err != nil {
		t.Fatalf("LoadPins failed as a whole: %v", err)
	}
	byName := map[string]Pin{}
	for _, pin := range got {
		byName[pin.Name] = pin
	}
	if len(byName) != 4 {
		t.Fatalf("got %d pins, want 4", len(byName))
	}
	for _, n := range []string{"badkind", "noversion", "norepo"} {
		if byName[n].Err == nil {
			t.Errorf("pin %s has no Err", n)
		}
	}
	good := byName["good"]
	if good.Err != nil || good.Version != "1.0.0" || good.Repo != "o/good" {
		t.Errorf("good pin disturbed: %+v", good)
	}
}

func TestLoadPins_missingFileIsError(t *testing.T) {
	if _, err := LoadPins(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("want an error for a missing file")
	}
}
