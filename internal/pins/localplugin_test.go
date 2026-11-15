package pins

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type pluginEntry struct {
	Scope       string `json:"scope"`
	ProjectPath string `json:"projectPath,omitempty"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
}

// writeInstalled writes an installed_plugins.json in the shape Claude Code uses (version 2).
func writeInstalled(t *testing.T, plugins map[string][]pluginEntry) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": 2, "plugins": plugins})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "installed_plugins.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repoWithWorktree returns the real paths of a main checkout and a linked worktree of it.
func repoWithWorktree(t *testing.T) (main, worktree string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main = filepath.Join(root, "main")
	worktree = filepath.Join(root, "wt")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, main, "init", "-q", "-b", "main")
	gitIn(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	gitIn(t, main, "worktree", "add", "-q", worktree, "-b", "feature")
	return main, worktree
}

func superpowers() Pin { return Pin{Name: "superpowers", Kind: KindPlugin, Version: "7.0.2"} }

func TestMeasurePlugin_scopeOrder(t *testing.T) {
	main, wt := repoWithWorktree(t)
	user := pluginEntry{Scope: "user", Version: "7.0.2"}
	inMain := pluginEntry{Scope: "project", ProjectPath: main, Version: "7.0.1"}
	inWt := pluginEntry{Scope: "project", ProjectPath: wt, Version: "7.0.0"}
	other := pluginEntry{Scope: "project", ProjectPath: "/elsewhere", Version: "9.9.9"}

	cases := []struct {
		name    string
		entries []pluginEntry
		status  Status
		version string
		scope   string
	}{
		{"worktree wins", []pluginEntry{user, inMain, inWt, other}, StatusBehind, "7.0.0", "project (worktree)"},
		{"main checkout when worktree missing", []pluginEntry{user, inMain, other}, StatusBehind, "7.0.1", "project (main checkout)"},
		{"user when no project matches", []pluginEntry{user, other}, StatusCurrent, "7.0.2", "user"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeInstalled(t, map[string][]pluginEntry{"superpowers@sp": c.entries})
			got := MeasurePlugin(superpowers(), path, wt)
			if got.Status != c.status {
				t.Fatalf("status = %s (%s), want %s", got.Status, got.Detail, c.status)
			}
			for _, want := range []string{c.version, "7.0.2", c.scope} {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("detail %q lacks %q", got.Detail, want)
				}
			}
		})
	}
}

func TestMeasurePlugin_aheadIsNotCurrent(t *testing.T) {
	path := writeInstalled(t, map[string][]pluginEntry{
		"superpowers@sp": {{Scope: "user", Version: "7.1.0"}},
	})
	got := MeasurePlugin(superpowers(), path, t.TempDir())
	if got.Status != StatusAhead {
		t.Fatalf("status = %s (%s), want ahead", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "7.1.0") || !strings.Contains(got.Detail, "7.0.2") {
		t.Errorf("detail %q lacks both versions", got.Detail)
	}
}

func TestMeasurePlugin_multipleMarketplacesIsError(t *testing.T) {
	path := writeInstalled(t, map[string][]pluginEntry{
		"superpowers@alpha": {{Scope: "user", Version: "7.0.2"}},
		"superpowers@beta":  {{Scope: "user", Version: "7.0.1"}},
	})
	got := MeasurePlugin(superpowers(), path, t.TempDir())
	if got.Status != StatusError {
		t.Fatalf("status = %s (%s), want error", got.Status, got.Detail)
	}
	for _, want := range []string{"superpowers@alpha", "superpowers@beta"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail %q lacks %q", got.Detail, want)
		}
	}
}

func TestMeasurePlugin_otherPluginWithSamePrefixIsIgnored(t *testing.T) {
	path := writeInstalled(t, map[string][]pluginEntry{
		"superpowers-extra@x": {{Scope: "user", Version: "1.0.0"}},
		"superpowers@sp":      {{Scope: "user", Version: "7.0.2"}},
	})
	if got := MeasurePlugin(superpowers(), path, t.TempDir()); got.Status != StatusCurrent {
		t.Fatalf("status = %s (%s), want current", got.Status, got.Detail)
	}
}

func TestMeasurePlugin_notAGitDirSkipsMainCheckoutScope(t *testing.T) {
	dir := t.TempDir() // not a repository: git fails, the measurement must go on to user
	path := writeInstalled(t, map[string][]pluginEntry{
		"superpowers@sp": {{Scope: "user", Version: "7.0.2"}},
	})
	if got := MeasurePlugin(superpowers(), path, dir); got.Status != StatusCurrent {
		t.Fatalf("status = %s (%s), want current", got.Status, got.Detail)
	}
}

func TestMeasurePlugin_missingFileIsError(t *testing.T) {
	got := MeasurePlugin(superpowers(), filepath.Join(t.TempDir(), "nope.json"), t.TempDir())
	if got.Status != StatusError {
		t.Fatalf("status = %s (%s), want error", got.Status, got.Detail)
	}
}

func TestMeasurePlugin_noMatchingEntryIsError(t *testing.T) {
	path := writeInstalled(t, map[string][]pluginEntry{
		"caveman@caveman": {{Scope: "user", Version: "4.1.0"}},
		"superpowers@sp":  {{Scope: "project", ProjectPath: "/elsewhere", Version: "7.0.2"}},
	})
	got := MeasurePlugin(superpowers(), path, t.TempDir())
	if got.Status != StatusError {
		t.Fatalf("status = %s (%s), want error", got.Status, got.Detail)
	}
}

func TestMeasurePlugin_malformedJSONIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed_plugins.json")
	if err := os.WriteFile(path, []byte(`{"plugins": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := MeasurePlugin(superpowers(), path, t.TempDir()); got.Status != StatusError {
		t.Fatalf("status = %s (%s), want error", got.Status, got.Detail)
	}
}

func TestMeasurePlugin_unparsableVersionsAreErrors(t *testing.T) {
	path := writeInstalled(t, map[string][]pluginEntry{
		"superpowers@sp": {{Scope: "user", Version: "unknown"}},
	})
	if got := MeasurePlugin(superpowers(), path, t.TempDir()); got.Status != StatusError {
		t.Fatalf("installed: status = %s (%s), want error", got.Status, got.Detail)
	}
	path = writeInstalled(t, map[string][]pluginEntry{
		"superpowers@sp": {{Scope: "user", Version: "7.0.2"}},
	})
	if got := MeasurePlugin(Pin{Name: "superpowers", Version: "x"}, path, t.TempDir()); got.Status != StatusError {
		t.Fatalf("pin: status = %s (%s), want error", got.Status, got.Detail)
	}
}
