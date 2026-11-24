package initcmd

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/internal/created"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
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

// sameTree fails t for every path that differs between before and the tree under root.
func sameTree(t *testing.T, root string, before map[string]string) {
	t.Helper()
	after := treeSnapshot(t, root)
	for _, p := range diffSnapshots(before, after) {
		t.Errorf("differs: %s\nbefore: %q\nafter:  %q", p, before[p], after[p])
	}
}

// jsonContent is the canonical form of the JSON in s, so two layouts of the same
// content compare equal.
func jsonContent(t *testing.T, s string) string {
	t.Helper()
	v, err := orderedjson.Parse([]byte(s))
	if err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	b, err := orderedjson.Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Red when uninstall leaves a unit, removes project content, leaves an empty directory,
// or leaves a line init added to .gitattributes.
func TestUninstallRoundTripWithOwnContent(t *testing.T) {
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
	if !exists(root, "lighthouse.toml") || !strings.Contains(read(t, root, ".gitattributes"), "eol=lf") {
		t.Fatal("fixture: init wrote no lighthouse.toml or .gitattributes lines")
	}

	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "Changed ") {
		t.Errorf("no summary:\n%s", r.out)
	}
	sameTree(t, root, before)
}

// Red when a round trip in an empty repo leaves a file init created, or keeps an edited
// lighthouse.toml.
func TestUninstallRoundTripEmptyRepo(t *testing.T) {
	root := newRepo(t)
	before := treeSnapshot(t, root)
	if r := run(t, root, "", packsWithPermissions()); r.code != 0 {
		t.Fatalf("init: rc %d\n%s%s", r.code, r.out, r.err)
	}
	for _, rel := range []string{"AGENTS.md", "CLAUDE.md", ".claude/settings.json", ".gitattributes", "lighthouse.toml"} {
		if !exists(root, rel) {
			t.Fatalf("fixture: init did not create %s", rel)
		}
	}
	// lighthouse.toml goes even when edited: it has no reader without lighthouse.
	write(t, root, "lighthouse.toml", read(t, root, "lighthouse.toml")+"# edited\n")

	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "# edited") {
		t.Errorf("diff does not show the edited lighthouse.toml:\n%s", r.out)
	}
	if strings.Contains(r.err, "kept: ") || strings.Contains(r.err, "left: ") {
		t.Errorf("notes for a clean round trip:\n%s", r.err)
	}
	sameTree(t, root, before)
}

// Red when uninstall rewrites a project's own .gitattributes beyond the lines init added.
func TestUninstallGitattributesOwnLines(t *testing.T) {
	for name, c := range map[string]struct{ before, after string }{
		"no final newline":     {"*.png binary", "*.png binary\n"},
		"look-alike sh line":   {"*.sh eol=lf text\n", "*.sh eol=lf text\n"},
		"own exact sh line":    {"*.sh eol=lf\n*.png binary\n", "*.sh eol=lf\n*.png binary\n"},
		"crlf":                 {"*.png binary\r\n", "*.png binary\r\n"},
		"look-alike hook line": {".claude/hooks/lighthouse/** -text\n", ".claude/hooks/lighthouse/** -text\n"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			write(t, root, ".gitattributes", c.before)
			initYes(t, root)
			if read(t, root, ".gitattributes") == c.before {
				t.Fatal("fixture: init added no line")
			}
			if r := runUninstall(t, root); r.code != 0 {
				t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
			}
			if got := read(t, root, ".gitattributes"); got != c.after {
				t.Errorf(".gitattributes = %q, want %q", got, c.after)
			}
		})
	}
}

// Red when uninstall leaves the "language" that --upgrade set, or changes the rest.
func TestUninstallRoundTripViaUpgrade(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".claude/settings.json", ownSettings)
	initYes(t, root)
	toml := read(t, root, "lighthouse.toml")
	if !strings.Contains(toml, `language = "en"`) {
		t.Fatalf("fixture: no language to change:\n%s", toml)
	}
	write(t, root, "lighthouse.toml", strings.Replace(toml, `language = "en"`, `language = "nl"`, 1))
	if r := run(t, root, "", func(o *Options) { o.Upgrade, o.Yes = true, true }); r.code != 0 {
		t.Fatalf("upgrade: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !strings.Contains(read(t, root, ".claude/settings.json"), `"language": "dutch"`) {
		t.Fatal("fixture: upgrade set no language")
	}
	if r := runUninstall(t, root); r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if got, want := jsonContent(t, read(t, root, ".claude/settings.json")), jsonContent(t, ownSettings); got != want {
		t.Errorf("settings.json = %s\nwant %s", got, want)
	}
}

// Red when uninstall removes a recorded thing that changed after init, or keeps it silently.
func TestUninstallKeepsDeviatingCreated(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"nl\"\n")
	initYes(t, root)
	c := readCreated(t, root)
	if c.SettingsLanguage != "dutch" || !sameList(c.Files, []string{"AGENTS.md", "CLAUDE.md", ".claude/settings.json", ".gitattributes"}) {
		t.Fatalf("fixture: created = %+v", c)
	}
	s := read(t, root, ".claude/settings.json")
	write(t, root, ".claude/settings.json", strings.Replace(s, `"language": "dutch"`, `"language": "english"`, 1))
	write(t, root, "CLAUDE.md", "@AGENTS.md\nmore\n")

	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if got := jsonContent(t, read(t, root, ".claude/settings.json")); got != `{"language":"english"}` {
		t.Errorf("settings.json = %s, want only the changed language", got)
	}
	if got := read(t, root, "CLAUDE.md"); got != "@AGENTS.md\nmore\n" {
		t.Errorf("CLAUDE.md = %q", got)
	}
	if exists(root, "AGENTS.md") {
		t.Errorf("AGENTS.md left: %q", read(t, root, "AGENTS.md"))
	}
	for _, want := range []string{"kept: .claude/settings.json", `"english"`, "kept: CLAUDE.md"} {
		if !strings.Contains(r.err, want) {
			t.Errorf("no note %q:\n%s", want, r.err)
		}
	}
	// The same CLAUDE.md, untouched, still stays when AGENTS.md stays.
	other := newRepo(t)
	initYes(t, other)
	write(t, other, "AGENTS.md", read(t, other, "AGENTS.md")+"own\n")
	r = runUninstall(t, other)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !exists(other, "AGENTS.md") || !strings.Contains(read(t, other, "AGENTS.md"), "own") {
		t.Errorf("AGENTS.md lost its own text: %q", read(t, other, "AGENTS.md"))
	}
	if !exists(other, "CLAUDE.md") || !strings.Contains(r.err, "kept: CLAUDE.md") {
		t.Errorf("CLAUDE.md removed although AGENTS.md stays, or no note:\n%s", r.err)
	}
}

// Red when uninstall without created.toml reverts anything outside the units, or does
// not name what it left.
func TestUninstallWithoutCreatedToml(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"nl\"\n")
	initYes(t, root)
	if err := os.Remove(filepath.Join(root, created.Path)); err != nil {
		t.Fatal(err)
	}
	ga := read(t, root, ".gitattributes")
	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if exists(root, ".claude/hooks/lighthouse") || exists(root, manifest.Path) {
		t.Error("units left")
	}
	if read(t, root, ".gitattributes") != ga || read(t, root, "CLAUDE.md") != "@AGENTS.md\n" || !exists(root, "lighthouse.toml") {
		t.Error("reverted without created.toml")
	}
	if !strings.Contains(read(t, root, ".claude/settings.json"), `"language": "dutch"`) {
		t.Error("language removed without created.toml")
	}
	const tail = " — init may have written it; remove it yourself if so\n"
	for _, p := range []string{"lighthouse.toml", "CLAUDE.md", ".gitattributes", `.claude/settings.json "language"`} {
		if !strings.Contains(r.err, "left: "+p+tail) {
			t.Errorf("no left line for %s:\n%s", p, r.err)
		}
	}
	if n := strings.Count(r.err, "left: "); n != 4 {
		t.Errorf("%d left lines, want 4:\n%s", n, r.err)
	}
}

// Red when a settings.json in another layout loses or gains content in a round trip.
func TestUninstallSettingsOddFormat(t *testing.T) {
	const odd = `{"permissions":{"allow":["Bash(npm test)"]},"model":"x"}`
	root := newRepo(t)
	write(t, root, ".claude/settings.json", odd)
	if r := run(t, root, "", packsWithPermissions()); r.code != 0 {
		t.Fatalf("init: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if r := runUninstall(t, root); r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if got, want := jsonContent(t, read(t, root, ".claude/settings.json")), jsonContent(t, odd); got != want {
		t.Errorf("settings.json = %s\nwant %s", got, want)
	}
}

// Red when a broken created.toml lets uninstall write anything.
func TestUninstallCorruptCreatedToml(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	write(t, root, created.Path, "this is = = not toml\n")
	before := treeSnapshot(t, root)
	r := runUninstall(t, root)
	if r.code != 1 || !strings.Contains(r.err, created.Path) {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	sameTree(t, root, before)
}

// Red when a settings.json that is no JSON object lets uninstall write anything.
func TestUninstallBrokenSettings(t *testing.T) {
	for name, body := range map[string]string{"broken": "{", "array": "[]", "string": `"x"`} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			initYes(t, root)
			write(t, root, ".claude/settings.json", body)
			before := treeSnapshot(t, root)
			r := runUninstall(t, root)
			if r.code != 1 || !strings.HasPrefix(r.err, "lighthouse uninstall: .claude/settings.json") {
				t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
			}
			sameTree(t, root, before)
		})
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
		}, `"timeout":99`},
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
			// init created both files here, so the round trip removes them.
			for _, rel := range []string{"AGENTS.md", ".claude/settings.json"} {
				if exists(root, rel) {
					t.Errorf("--force left %s:\n%s", rel, read(t, root, rel))
				}
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
	if exists(root, ".claude/settings.json") {
		t.Errorf("settings.json left:\n%s", read(t, root, ".claude/settings.json"))
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

// Red when removeLastLine removes another match than the last, a partial match, or
// leaves the \r of a CRLF line.
func TestRemoveLastLine(t *testing.T) {
	const r = "*.sh eol=lf"
	for in, want := range map[string]string{
		"a\n" + r + "\nb\n" + r + "\nc\n": "a\n" + r + "\nb\nc\n",
		"a\r\n" + r + "\r\nb\r\n":         "a\r\nb\r\n",
		"a\n" + r:                         "a\n",
		"a\n" + r + " text\n":             "a\n" + r + " text\n",
		"":                                "",
	} {
		if got := string(removeLastLine([]byte(in), r)); got != want {
			t.Errorf("removeLastLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// Red when the empty containers of a settings.json init created stay because
// settings_keys does not name them: the file in files covers all three.
func TestUninstallSettingsFileImpliesContainers(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", packsWithPermissions()); r.code != 0 {
		t.Fatalf("init: rc %d\n%s%s", r.code, r.out, r.err)
	}
	c := readCreated(t, root)
	if !slices.Contains(c.Files, ".claude/settings.json") {
		t.Fatalf("fixture: settings.json not in files: %+v", c)
	}
	c.SettingsKeys = nil
	write(t, root, created.Path, string(created.Encode(c)))
	if r := runUninstall(t, root); r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if exists(root, ".claude/settings.json") {
		t.Errorf("settings.json left:\n%s", read(t, root, ".claude/settings.json"))
	}
}

// standardPacks is levelPacks with the labels flag at standard and the real direction
// templates, so a standard-level init seeds a labels file and the direction documents.
func standardPacks(t *testing.T) fstest.MapFS {
	t.Helper()
	p := levelPacks()
	for k, v := range directionPacks(t) {
		if strings.HasPrefix(k, "core/templates/") || k == "core/headings.toml" {
			p[k] = v
		}
	}
	p["core/levels.toml"] = file(flagLevelsToml)
	return p
}

// standardDirectionInit runs init at level standard with labels and direction documents over
// levelPacks, so the run seeds a labels file and the direction documents.
func standardDirectionInit(t *testing.T, root string) {
	t.Helper()
	r := run(t, root, "", func(o *Options) {
		o.Level = "standard"
		o.Packs = standardPacks(t)
		o.Yes = true
		o.Direction = true
		o.SyncLabels = func(string, bool, io.Writer, io.Writer) error { return nil }
	})
	if r.code != 0 {
		t.Fatalf("init: rc %d\n%s%s", r.code, r.out, r.err)
	}
}

// Red when uninstall deletes a seeded file or stays silent about one.
func TestUninstallRoundTripStandardLevel(t *testing.T) {
	root := newRepo(t)
	before := treeSnapshot(t, root)
	standardDirectionInit(t, root)
	seeded := []string{taxRel, strategyRel, tenetsRel, areasRel}
	for _, rel := range seeded {
		if !exists(root, rel) {
			t.Fatalf("fixture: init seeded no %s", rel)
		}
	}

	r := runUninstall(t, root, func(o *Options) {
		o.Packs = standardPacks(t)
	})
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	for _, rel := range seeded {
		if !exists(root, rel) {
			t.Errorf("%s is gone", rel)
		}
		if want := "kept: " + rel + " (project file after seeding)\n"; !strings.Contains(r.err, want) {
			t.Errorf("no %q in:\n%s", want, r.err)
		}
	}
	// What remains beyond the tree of before is the seeded files and their directories.
	for rel := range treeSnapshot(t, root) {
		if _, ok := before[rel]; ok {
			continue
		}
		owned := false
		for _, s := range seeded {
			if rel == s || strings.HasPrefix(rel, s+"/") || strings.HasPrefix(s, rel) {
				owned = true
			}
		}
		if !owned {
			t.Errorf("uninstall left %s", rel)
		}
	}
	for rel := range before {
		if _, ok := treeSnapshot(t, root)[rel]; !ok {
			t.Errorf("uninstall removed %s", rel)
		}
	}
}

// Red when a seeded path that is not there is reported, or one that is there is not.
func TestUninstallSeededPathMissingIsNotReported(t *testing.T) {
	root := newRepo(t)
	standardDirectionInit(t, root)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(tenetsRel))); err != nil {
		t.Fatal(err)
	}
	r := runUninstall(t, root, func(o *Options) { o.Packs = standardPacks(t) })
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if strings.Contains(r.err, tenetsRel) {
		t.Errorf("reports the missing %s:\n%s", tenetsRel, r.err)
	}
	if !strings.Contains(r.err, "kept: "+strategyRel+" ") {
		t.Errorf("does not report %s:\n%s", strategyRel, r.err)
	}
}

// Red when a runtime directory is deleted or goes unreported, or a loose file is reported as one.
func TestUninstallReportsRuntimeDirs(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	write(t, root, ".lighthouse/run/x", "1\n")
	write(t, root, ".lighthouse/bin/y", "2\n")
	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	for _, rel := range []string{".lighthouse/run/x", ".lighthouse/bin/y"} {
		if !exists(root, rel) {
			t.Errorf("%s is gone", rel)
		}
	}
	for _, d := range []string{".lighthouse/run", ".lighthouse/bin"} {
		if want := "kept: " + d + " (runtime state, not installed by init)\n"; !strings.Contains(r.err, want) {
			t.Errorf("no %q in:\n%s", want, r.err)
		}
	}
}

// Red when the fixed lines vanish: labels on the forge, host.toml, the running session.
func TestUninstallReportsNotTouched(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	var forge, restart bool
	for _, l := range strings.Split(r.err, "\n") {
		if l == "not touched: labels on the forge, ~/"+hostconfig.HomeRel {
			forge = true
		}
		if strings.HasPrefix(l, "note:") && strings.Contains(l, "running Claude Code session") && strings.Contains(l, "restart") {
			restart = true
		}
	}
	if !forge || !restart {
		t.Errorf("forge line %v, restart line %v in:\n%s", forge, restart, r.err)
	}
}

// Red when a refused or declined run writes a report.
func TestUninstallLeavesNoReportOnRefusal(t *testing.T) {
	root := newRepo(t)
	standardDirectionInit(t, root)
	write(t, root, ".lighthouse/run/x", "1\n")
	p := standardPacks(t)
	write(t, root, ".claude/hooks/lighthouse/run.sh", read(t, root, ".claude/hooks/lighthouse/run.sh")+"# mine\n")
	refused := runUninstall(t, root, func(o *Options) { o.Packs = p })
	if refused.code != 1 || !strings.Contains(refused.err, "refused") {
		t.Fatalf("fixture: want a refusal, rc %d\n%s", refused.code, refused.err)
	}
	declined := runUninstall(t, root, func(o *Options) { o.Packs = p; o.Force = true; o.Yes = false; o.In = strings.NewReader("n\n") })
	if declined.code != 1 {
		t.Fatalf("fixture: want a decline, rc %d\n%s%s", declined.code, declined.out, declined.err)
	}
	for name, r := range map[string]result{"refused": refused, "declined": declined} {
		for _, w := range []string{"kept:", "not touched:", "left:"} {
			if strings.Contains(r.err, w) {
				t.Errorf("%s run reports %q:\n%s", name, w, r.err)
			}
		}
	}
}

// Red when an lighthouse.toml that existed before init is removed or goes unreported:
// init may have added keys to it, and created.toml does not record that.
func TestUninstallReportsPreExistingLighthouseToml(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"nl\"\n")
	initYes(t, root)
	if slices.Contains(readCreated(t, root).Files, "lighthouse.toml") {
		t.Fatal("fixture: created.toml records lighthouse.toml")
	}
	r := runUninstall(t, root)
	if r.code != 0 {
		t.Fatalf("uninstall: rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !exists(root, "lighthouse.toml") {
		t.Error("lighthouse.toml removed")
	}
	if want := "left: lighthouse.toml — init may have added keys to it; remove them yourself if so\n"; !strings.Contains(r.err, want) {
		t.Errorf("no %q in:\n%s", want, r.err)
	}
	// An lighthouse.toml that init created goes, without the note.
	other := newRepo(t)
	initYes(t, other)
	r = runUninstall(t, other)
	if strings.Contains(r.err, "left: lighthouse.toml") {
		t.Errorf("note for a created lighthouse.toml:\n%s", r.err)
	}
}
