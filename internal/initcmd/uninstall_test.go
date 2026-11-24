package initcmd

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/created"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// runUninstall runs Uninstall with the options of run, --yes unless a mod says otherwise.
func runUninstall(t *testing.T, root string, mods ...func(*Options)) result {
	t.Helper()
	var out, errOut bytes.Buffer
	o := Options{
		Dir:     root,
		Version: "dev",
		Packs:   testPacks(),
		Deps:    allPresent(),
		In:      strings.NewReader(""),
		Out:     &out,
		Err:     &errOut,
		Yes:     true,
	}
	for _, m := range mods {
		m(&o)
	}
	code := Uninstall(o)
	return result{code, out.String(), errOut.String()}
}

func force(o *Options) { o.Force = true }

// treeSnapshot is snapshot plus every directory outside .git, so a directory left
// empty shows up.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := snapshot(t, root)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		snap[filepath.ToSlash(rel)+"/"] = "dir"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// diffSnapshots names every path that differs between a and b, except those in skip.
func diffSnapshots(a, b map[string]string, skip ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range skip {
		seen[s] = true
	}
	for k, v := range a {
		if !seen[k] && b[k] != v {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok && !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// Red when uninstall does not exist, writes something, or reports a different error.
func TestUninstallNotInstalled(t *testing.T) {
	root := newRepo(t)
	write(t, root, "AGENTS.md", "# mine\n")
	before := treeSnapshot(t, root)
	r := runUninstall(t, root)
	if r.code != 1 || !strings.Contains(r.err, "not installed") || !strings.HasPrefix(r.err, "lighthouse uninstall: ") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if d := diffSnapshots(before, treeSnapshot(t, root)); len(d) > 0 {
		t.Fatalf("changed: %v", d)
	}
}

// ownSettings is a project settings.json with its own hook and permission rule, in the
// canonical orderedjson layout so a round trip can compare bytes.
const ownSettings = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "./scripts/mine.sh"
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(npm test)"
    ]
  }
}
`

// Red when uninstall leaves a unit, removes project content, or leaves an empty directory.
func TestUninstallRoundTripKeepsProjectContent(t *testing.T) {
	root := newRepo(t)
	write(t, root, "AGENTS.md", "# Mine\n\nOwn text.\n")
	write(t, root, ".claude/settings.json", ownSettings)
	write(t, root, ".gitattributes", "*.png binary\n")
	if v, err := orderedjson.Parse([]byte(ownSettings)); err != nil {
		t.Fatal(err)
	} else if b, _ := orderedjson.Marshal(v); string(b) != ownSettings {
		t.Fatalf("fixture is not canonical:\n%s", b)
	}
	before := treeSnapshot(t, root)
	if r := run(t, root, "", packsWithPermissions()); r.code != 0 {
		t.Fatalf("init: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !strings.Contains(read(t, root, ".claude/settings.json"), "Bash(lighthouse status)") {
		t.Fatal("fixture: init added no lighthouse permission")
	}

	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	for _, rel := range []string{".claude/hooks/lighthouse", manifest.Path, created.Path, ".lighthouse"} {
		if exists(root, rel) {
			t.Errorf("%s still there", rel)
		}
	}
	if !strings.Contains(r.out, "Removed ") {
		t.Errorf("no summary:\n%s", r.out)
	}
	// lighthouse.toml and the .gitattributes lines are what created.toml reverts (task 5).
	after := treeSnapshot(t, root)
	if d := diffSnapshots(before, after, "lighthouse.toml", ".gitattributes"); len(d) > 0 {
		for _, p := range d {
			t.Errorf("differs: %s\nbefore: %q\nafter:  %q", p, before[p], after[p])
		}
	}
	if !strings.HasPrefix(read(t, root, ".gitattributes"), "*.png binary\n") {
		t.Errorf(".gitattributes lost its own line: %q", read(t, root, ".gitattributes"))
	}
}

// Red when uninstall removes a locally changed unit without --force, writes on a refusal,
// or does not remove it with --force.
func TestUninstallRefusesLocalEdit(t *testing.T) {
	edits := map[string]struct {
		edit func(t *testing.T, root string)
		show string
	}{
		"script line": {func(t *testing.T, root string) {
			write(t, root, ".claude/hooks/lighthouse/g.sh", read(t, root, ".claude/hooks/lighthouse/g.sh")+"echo LOCAL\n")
		}, "-echo LOCAL"},
		"block line": {func(t *testing.T, root string) {
			write(t, root, "AGENTS.md", strings.Replace(read(t, root, "AGENTS.md"), "- rule", "- rule LOCAL", 1))
		}, "- rule LOCAL"},
		"hook timeout": {func(t *testing.T, root string) {
			write(t, root, ".claude/settings.json", strings.Replace(read(t, root, ".claude/settings.json"), `"timeout": 15`, `"timeout": 99`, 1))
		}, "99"},
	}
	for name, c := range edits {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			initYes(t, root)
			c.edit(t, root)
			edited := treeSnapshot(t, root)
			r := runUninstall(t, root)
			if r.code != 1 || !strings.Contains(r.err, "refused: 1 locally modified unit(s)") {
				t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
			}
			if !strings.Contains(r.out, "conflict: ") || !strings.Contains(r.out, c.show) {
				t.Fatalf("no conflict diff showing %q:\n%s", c.show, r.out)
			}
			if d := diffSnapshots(edited, treeSnapshot(t, root)); len(d) > 0 {
				t.Fatalf("refused run changed %v", d)
			}
			r = runUninstall(t, root, force)
			if r.code != 0 {
				t.Fatalf("--force: rc %d\n%s%s", r.code, r.out, r.err)
			}
			if exists(root, ".claude/hooks/lighthouse") || exists(root, manifest.Path) {
				t.Error("--force left the hook dir or the manifest")
			}
			if s := read(t, root, "AGENTS.md"); strings.Contains(s, "lighthouse:begin") {
				t.Errorf("--force left the block: %q", s)
			}
			if s := read(t, root, ".claude/settings.json"); strings.Contains(s, "hooks/lighthouse") {
				t.Errorf("--force left the hook registration:\n%s", s)
			}
		})
	}
}

// Red when a unit that is already gone counts as a conflict, or stops the rest.
func TestUninstallDeletedScriptIsNoConflict(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	if err := os.Remove(filepath.Join(root, ".claude/hooks/lighthouse/g.sh")); err != nil {
		t.Fatal(err)
	}
	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	for _, rel := range []string{".lighthouse/scripts/a.py", manifest.Path, ".claude/hooks"} {
		if exists(root, rel) {
			t.Errorf("%s still there", rel)
		}
	}
	if s := read(t, root, ".claude/settings.json"); strings.Contains(s, "hooks/lighthouse") {
		t.Errorf("hook registration left:\n%s", s)
	}
}

// Red when a declined run writes anything or asks another question.
func TestUninstallDecline(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	before := treeSnapshot(t, root)
	r := runUninstall(t, root, func(o *Options) { o.Yes = false; o.In = strings.NewReader("n\n") })
	if r.code != 1 || !strings.Contains(r.out, "Remove lighthouse? [y/N] ") {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	q := strings.Index(r.out, "Remove lighthouse? [y/N] ")
	if !strings.Contains(r.out[:q], "+++ /dev/null") {
		t.Errorf("no diff before the question:\n%s", r.out)
	}
	if d := diffSnapshots(before, treeSnapshot(t, root)); len(d) > 0 {
		t.Fatalf("declined run changed %v", d)
	}
	r = runUninstall(t, root, func(o *Options) { o.Yes = false; o.In = strings.NewReader("y\n") })
	if r.code != 0 || exists(root, manifest.Path) {
		t.Fatalf("yes: rc %d\n%s%s", r.code, r.out, r.err)
	}
}

// Red when uninstall creates .claude/settings.json to remove registrations from it.
func TestUninstallNoSettingsFile(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	if err := os.Remove(filepath.Join(root, ".claude/settings.json")); err != nil {
		t.Fatal(err)
	}
	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	if exists(root, ".claude/settings.json") {
		t.Fatalf("settings.json created: %q", read(t, root, ".claude/settings.json"))
	}
	if exists(root, ".claude") {
		t.Error(".claude left empty")
	}
}

// Red when the manifest or created.toml goes before the other paths: a failure halfway
// would then leave a remainder no later uninstall sees.
func TestUninstallLastTwoPathsAndResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not stop a delete on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := newRepo(t)
	initYes(t, root)
	if !exists(root, created.Path) {
		t.Fatal("fixture: init wrote no created.toml")
	}
	// .lighthouse/scripts sorts after .lighthouse/created.toml and .lighthouse/manifest.toml.
	locked := filepath.Join(root, ".lighthouse/scripts")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	r := runUninstall(t, root)
	if r.code != 1 || !strings.HasPrefix(r.err, "lighthouse uninstall: ") {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	if exists(root, ".claude/hooks/lighthouse/g.sh") {
		t.Fatal("fixture: the failure came before the first path")
	}
	for _, rel := range []string{manifest.Path, created.Path} {
		if !exists(root, rel) {
			t.Errorf("%s removed before the failing path", rel)
		}
	}

	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	r = runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("second run: rc %d\n%s%s", r.code, r.out, r.err)
	}
	for _, rel := range []string{".lighthouse", ".claude/hooks"} {
		if exists(root, rel) {
			t.Errorf("%s still there after the second run", rel)
		}
	}
}

func TestUninstallPycacheAndOwnFileInHookDir(t *testing.T) {
	const pyc = ".claude/hooks/lighthouse/__pycache__/g.cpython-312.pyc"
	t.Run("pycache only", func(t *testing.T) {
		// Red when __pycache__ keeps the hook directory alive.
		root := newRepo(t)
		initYes(t, root)
		write(t, root, pyc, "x")
		if r := runUninstall(t, root); r.code != 0 {
			t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
		}
		if exists(root, ".claude/hooks") {
			t.Error(".claude/hooks survives on a __pycache__")
		}
	})
	t.Run("pycache and own file", func(t *testing.T) {
		// Red when uninstall removes a project file or the __pycache__ next to it.
		root := newRepo(t)
		initYes(t, root)
		write(t, root, pyc, "x")
		write(t, root, ".claude/hooks/lighthouse/mine.sh", "echo mine\n")
		if r := runUninstall(t, root); r.code != 0 {
			t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
		}
		for _, rel := range []string{".claude/hooks/lighthouse/mine.sh", pyc, ".claude"} {
			if !exists(root, rel) {
				t.Errorf("%s removed", rel)
			}
		}
		if exists(root, ".claude/hooks/lighthouse/g.sh") {
			t.Error("g.sh still there")
		}
	})
}

func TestRemoveEmptyDirs(t *testing.T) {
	cases := map[string]struct {
		files []string // present before, relative to root
		rel   string   // the removed file
		gone  []string
		stay  []string
	}{
		"empty chain up to root": {
			rel:  "a/b/c/x",
			gone: []string{"a"},
		},
		"stops at a directory with a file": {
			files: []string{"a/keep"},
			rel:   "a/b/c/x",
			gone:  []string{"a/b"},
			stay:  []string{"a/keep"},
		},
		"pycache only counts as empty": {
			files: []string{"a/b/__pycache__/x.pyc"},
			rel:   "a/b/x",
			gone:  []string{"a"},
		},
		"pycache next to a file stays": {
			files: []string{"a/b/__pycache__/x.pyc", "a/b/mine"},
			rel:   "a/b/x",
			stay:  []string{"a/b/__pycache__/x.pyc", "a/b/mine"},
		},
		"file at the root": {
			rel: "x",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for _, f := range c.files {
				write(t, root, f, "x")
			}
			if err := os.MkdirAll(filepath.Join(root, filepath.Dir(c.rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := removeEmptyDirs(root, c.rel); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("root removed: %v", err)
			}
			for _, g := range c.gone {
				if exists(root, g) {
					t.Errorf("%s still there", g)
				}
			}
			for _, s := range c.stay {
				if !exists(root, s) {
					t.Errorf("%s removed", s)
				}
			}
		})
	}
	t.Run("missing directory", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "a/keep", "x")
		if err := removeEmptyDirs(root, "a/b/x"); err != nil {
			t.Fatal(err)
		}
		if !exists(root, "a/keep") {
			t.Error("a/keep removed")
		}
	})
}
