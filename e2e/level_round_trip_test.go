//go:build e2e

// Switching strict -> light -> strict on the real pack ends with the same installation (#532).
//
// settings.json keeps a registration where it stands and appends a new one (internal/settings
// Apply), so the hook entries may come back in another order: the file is compared as a set of
// commands, every other file byte for byte. components.toml records when the level was set
// (`since`), so a switch gives it new bytes: it is left out, and so is its hash in the manifest.
package e2e

import (
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const levelComponents = ".lighthouse/components.toml"

// levelSnapshot maps every file under root, except .git and components.toml, to its bytes. The
// manifest is reduced to its parsed form without the sha256 of components.toml.
func levelSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == levelComponents {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const manifest = ".lighthouse/manifest.toml"
	if raw, ok := files[manifest]; ok {
		var data map[string]any
		if _, err := toml.Decode(raw, &data); err != nil {
			t.Fatalf("%s: %v", manifest, err)
		}
		units, _ := data["unit"].([]map[string]any)
		for _, u := range units {
			if u["path"] == levelComponents {
				delete(u, "sha256")
			}
		}
		out, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		files[manifest] = string(out)
	}
	return files
}

// hookCommands is the sorted list of every hook command in a settings.json.
func hookCommands(t *testing.T, raw string) []string {
	t.Helper()
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Command != "" {
					cmds = append(cmds, h.Command)
				}
			}
		}
	}
	slices.Sort(cmds)
	return cmds
}

// Fails when `init --upgrade --level strict` after light does not restore what light removed, or
// leaves something of light behind: a file, its bytes, or a hook registration.
func TestStrictLightStrictGivesTheSameFiles(t *testing.T) {
	bin := buildLighthouse(t)
	repo := freshRepo(t)
	packs := []string{"--pack", "core", "--pack", "go", "--pack", "shell"}

	run(t, repo, bin, append([]string{"init", "--yes", "--level", "strict"}, packs...)...)
	first := levelSnapshot(t, repo)
	run(t, repo, bin, "init", "--yes", "--upgrade", "--level", "light")
	light := levelSnapshot(t, repo)
	run(t, repo, bin, "init", "--yes", "--upgrade", "--level", "strict")
	again := levelSnapshot(t, repo)

	if reflect.DeepEqual(light, first) {
		t.Fatal("light installed the same as strict: the switch did nothing")
	}
	if got, want := slices.Sorted(maps.Keys(again)), slices.Sorted(maps.Keys(first)); !slices.Equal(got, want) {
		t.Errorf("file set differs after the round trip:\ngot  %v\nwant %v", got, want)
	}
	const settings = ".claude/settings.json"
	for k, v := range first {
		if k != settings && again[k] != v {
			t.Errorf("%s differs after the round trip", k)
		}
	}
	if got, want := hookCommands(t, again[settings]), hookCommands(t, first[settings]); !slices.Equal(got, want) {
		t.Errorf("hook commands differ:\ngot  %v\nwant %v", got, want)
	}
}
