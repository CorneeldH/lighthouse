package initcmd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/internal/deps"
	"github.com/CorneeldH/lighthouse/internal/manifest"
)

const hooksJSON = `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/g.sh", "timeout": 15}]}]}}`

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func testPacks() fstest.MapFS {
	return fstest.MapFS{
		"core/hooks/g.sh":             file("#!/bin/bash\necho g\n"),
		"core/hooks/hooks.json":       file(hooksJSON),
		"core/lib/a.py":               file("x = 1\n"),
		"core/instructions/AGENTS.md": file("## core\n\n- rule\n"),
		"core/requires.toml":          file("jq = [\"main-commit-guard\"]\n"),
	}
}

func allPresent() deps.Checker {
	return deps.Checker{
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Run:      func(string, ...string) error { return nil },
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type result struct {
	code     int
	out, err string
}

func run(t *testing.T, dir string, in string, mod func(*Options)) result {
	t.Helper()
	var out, errOut bytes.Buffer
	o := Options{
		Dir:     dir,
		Version: "dev",
		Packs:   testPacks(),
		Deps:    allPresent(),
		In:      strings.NewReader(in),
		Out:     &out,
		Err:     &errOut,
	}
	if mod != nil {
		mod(&o)
	}
	code := Run(o)
	return result{code, out.String(), errOut.String()}
}

func yes(o *Options)     { o.Yes = true }
func upgrade(o *Options) { o.Upgrade = true }

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

// snapshot maps every file outside .git to its content and mode.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		snap[rel] = info.Mode().String() + "\n" + string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func initYes(t *testing.T, root string) {
	t.Helper()
	if r := run(t, root, "", yes); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
}

func TestInitFresh(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)

	guard := read(t, root, ".claude/hooks/lighthouse/g.sh")
	if guard != "#!/bin/bash\n# lighthouse: core@dev\necho g\n" {
		t.Fatalf("guard = %q", guard)
	}
	info, _ := os.Stat(filepath.Join(root, ".claude/hooks/lighthouse/g.sh"))
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if !strings.Contains(read(t, root, ".claude/settings.json"), `"command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh"`) {
		t.Fatalf("settings:\n%s", read(t, root, ".claude/settings.json"))
	}
	if read(t, root, "AGENTS.md") != "<!-- lighthouse:begin core -->\n## core\n\n- rule\n<!-- lighthouse:end core -->\n" {
		t.Fatalf("AGENTS.md = %q", read(t, root, "AGENTS.md"))
	}
	if read(t, root, "CLAUDE.md") != "@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md = %q", read(t, root, "CLAUDE.md"))
	}
	if !strings.Contains(read(t, root, "lighthouse.toml"), "[project]\nlanguage = \"en\"\n") {
		t.Fatalf("lighthouse.toml = %q", read(t, root, "lighthouse.toml"))
	}
	m, err := manifest.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Units) != 4 || m.Version != "dev" || strings.Join(m.Packages, ",") != "core" {
		t.Fatalf("manifest %+v", m)
	}
	for _, e := range m.Units {
		if e.Path == ".claude/hooks/lighthouse/g.sh" && e.SHA256 != manifest.Sum([]byte(guard)) {
			t.Fatal("hash of g.sh does not match the file")
		}
	}
}

func TestInitAsksFirst(t *testing.T) {
	for _, in := range []string{"n\n", "", "nope\n"} {
		root := newRepo(t)
		before := snapshot(t, root)
		r := run(t, root, in, nil)
		if r.code != 1 || !sameSnapshot(before, snapshot(t, root)) {
			t.Fatalf("in %q: rc %d, files changed", in, r.code)
		}
	}
	root := newRepo(t)
	r := run(t, root, "Y\n", nil)
	if r.code != 0 || !exists(root, ".claude/hooks/lighthouse/g.sh") {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	q := strings.Index(r.out, "Apply these changes? [y/N] ")
	if q < 0 || !strings.Contains(r.out[:q], "--- /dev/null") || !strings.Contains(r.out[:q], "+++ b/.claude/hooks/lighthouse/g.sh") {
		t.Fatalf("diff not before the question:\n%s", r.out)
	}
}

func TestInitTwiceRefuses(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	if r := run(t, root, "", yes); r.code != 1 || !strings.Contains(r.err, "--upgrade") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestUpgradeWithoutManifest(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", upgrade); r.code != 1 || !strings.Contains(r.err, "lighthouse init") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

type countingReader struct{ n int }

func (c *countingReader) Read(p []byte) (int, error) { c.n++; return 0, errors.New("must not read") }

func TestUpgradeRightAfterInitIsEmpty(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	before := snapshot(t, root)
	in := &countingReader{}
	r := run(t, root, "", func(o *Options) { o.Upgrade = true; o.In = in })
	if r.code != 0 || r.out != "No changes.\n" || in.n != 0 {
		t.Fatalf("rc %d reads %d out %q err %q", r.code, in.n, r.out, r.err)
	}
	if !sameSnapshot(before, snapshot(t, root)) {
		t.Fatal("files changed")
	}
}

func TestUpgradeRefusesLocalEdit(t *testing.T) {
	edits := map[string]func(t *testing.T, root string){
		"script line": func(t *testing.T, root string) {
			write(t, root, ".claude/hooks/lighthouse/g.sh", read(t, root, ".claude/hooks/lighthouse/g.sh")+"echo LOCAL\n")
		},
		"block line": func(t *testing.T, root string) {
			write(t, root, "AGENTS.md", strings.Replace(read(t, root, "AGENTS.md"), "- rule", "- rule LOCAL", 1))
		},
		"hook timeout": func(t *testing.T, root string) {
			write(t, root, ".claude/settings.json", strings.Replace(read(t, root, ".claude/settings.json"), `"timeout": 15`, `"timeout": 99`, 1))
		},
		"deleted script": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, ".claude/hooks/lighthouse/g.sh")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			initYes(t, root)
			fresh := snapshot(t, root)
			edit(t, root)
			edited := snapshot(t, root)
			r := run(t, root, "", func(o *Options) { o.Upgrade = true; o.Yes = true })
			if r.code != 1 || !strings.Contains(r.out+r.err, "--force") {
				t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
			}
			if name != "deleted script" && !strings.Contains(r.out, "LOCAL") && !strings.Contains(r.out, "99") {
				t.Fatalf("diff does not show the edit:\n%s", r.out)
			}
			if !sameSnapshot(edited, snapshot(t, root)) {
				t.Fatal("refused run changed files")
			}
			r = run(t, root, "", func(o *Options) { o.Upgrade = true; o.Yes = true; o.Force = true })
			if r.code != 0 || !sameSnapshot(fresh, snapshot(t, root)) {
				t.Fatalf("--force did not restore: rc %d\n%s%s", r.code, r.out, r.err)
			}
		})
	}
}

func TestUpgradeFromDevToTag(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	r := run(t, root, "", func(o *Options) { o.Upgrade = true; o.Yes = true; o.Version = "v0.1.0" })
	if r.code != 0 {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	if !strings.Contains(read(t, root, ".claude/hooks/lighthouse/g.sh"), "# lighthouse: core@v0.1.0") {
		t.Fatal("marker not updated")
	}
	if m, _ := manifest.Read(root); m.Version != "v0.1.0" {
		t.Fatalf("manifest version %q", m.Version)
	}
}

func TestUpgradeRemovesDroppedUnit(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	r := run(t, root, "", func(o *Options) {
		o.Upgrade = true
		o.Yes = true
		p := testPacks()
		delete(p, "core/lib/a.py")
		o.Packs = p
	})
	if r.code != 0 || exists(root, ".lighthouse/lib/lighthouse_lib/a.py") {
		t.Fatalf("rc %d, a.py still there\n%s%s", r.code, r.out, r.err)
	}
	m, _ := manifest.Read(root)
	for _, e := range m.Units {
		if strings.HasSuffix(e.Path, "a.py") {
			t.Fatal("a.py still in the manifest")
		}
	}
}

const projectSettings = `{
  "permissions": {
    "allow": ["Bash(ls)"]
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/own.sh"
          }
        ]
      }
    ]
  }
}
`

func TestKeepsProjectContent(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".claude/settings.json", projectSettings)
	write(t, root, ".claude/hooks/own.sh", "#!/bin/sh\n")
	write(t, root, "AGENTS.md", "# Rules\n\nOwn text.\n")
	write(t, root, "CLAUDE.md", "# Claude\n")
	toml := "# own comment\n[controller]\nstate_dir = \"x\"\n"
	write(t, root, "lighthouse.toml", toml)
	r := run(t, root, "", yes)
	if r.code != 0 {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	settings := read(t, root, ".claude/settings.json")
	own := strings.Index(settings, "own.sh")
	lh := strings.Index(settings, "lighthouse/g.sh")
	if own < 0 || lh < own || strings.Count(settings, `"matcher": "Bash"`) != 1 || !strings.HasPrefix(settings, "{\n  \"permissions\"") {
		t.Fatalf("settings:\n%s", settings)
	}
	if !strings.HasPrefix(read(t, root, "AGENTS.md"), "# Rules\n\nOwn text.\n\n<!-- lighthouse:begin core -->") {
		t.Fatalf("AGENTS.md = %q", read(t, root, "AGENTS.md"))
	}
	if read(t, root, "CLAUDE.md") != "# Claude\n" || !strings.Contains(r.err, "@AGENTS.md") {
		t.Fatalf("CLAUDE.md changed or no warning; err %q", r.err)
	}
	if !strings.HasPrefix(read(t, root, "lighthouse.toml"), toml) {
		t.Fatalf("lighthouse.toml = %q", read(t, root, "lighthouse.toml"))
	}
}

func TestFromSubdirectory(t *testing.T) {
	root := newRepo(t)
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := run(t, sub, "", yes); r.code != 0 || !exists(root, ".lighthouse/manifest.toml") || exists(sub, ".lighthouse") {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
}

func TestNotARepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if r := run(t, dir, "", yes); r.code != 1 || !strings.Contains(r.err, "not a git repository") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestCorruptManifest(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".lighthouse/manifest.toml", "version = \n")
	before := snapshot(t, root)
	for _, mod := range []func(*Options){yes, func(o *Options) { o.Upgrade = true; o.Yes = true }} {
		if r := run(t, root, "", mod); r.code != 1 || !sameSnapshot(before, snapshot(t, root)) {
			t.Fatalf("rc %d", r.code)
		}
	}
}

func TestUnparseableSettings(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".claude/settings.json", "{\n  // comment\n}\n")
	before := snapshot(t, root)
	if r := run(t, root, "", yes); r.code != 1 || !sameSnapshot(before, snapshot(t, root)) || !strings.Contains(r.err, "settings.json") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestWarnsMissingScript(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".claude/settings.json", strings.Replace(projectSettings, "own.sh", "gone.sh", 1))
	r := run(t, root, "", yes)
	if r.code != 0 || !strings.Contains(r.err, ".claude/hooks/gone.sh") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if strings.Contains(r.err, "lighthouse/g.sh") {
		t.Fatalf("warned about an installed script: %q", r.err)
	}
}

func TestReportsMissingDeps(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", func(o *Options) {
		o.Yes = true
		o.Deps = deps.Checker{
			LookPath: func(name string) (string, error) {
				if name == "jq" {
					return "", errors.New("no")
				}
				return "/usr/bin/" + name, nil
			},
			Run: func(string, ...string) error { return nil },
		}
	})
	if r.code != 0 || !strings.Contains(r.err, "missing dependency: jq (needed by: main-commit-guard)") || !exists(root, ".lighthouse/manifest.toml") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestNoForgeWithoutGitHubRemote(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", yes)
	if strings.Contains(read(t, root, "lighthouse.toml"), "[forge]") || !strings.Contains(r.err, "[forge]") {
		t.Fatalf("toml %q err %q", read(t, root, "lighthouse.toml"), r.err)
	}
}

func TestForgeFromGitHubRemote(t *testing.T) {
	root := newRepo(t)
	gitIn(t, root, "remote", "add", "origin", "git@github.com:acme/widgets.git")
	if r := run(t, root, "", yes); r.code != 0 {
		t.Fatal(r.err)
	}
	if !strings.Contains(read(t, root, "lighthouse.toml"), "[forge]\nkind = \"github\"\nrepo = \"acme/widgets\"\n") {
		t.Fatalf("toml %q", read(t, root, "lighthouse.toml"))
	}
}

func TestRestoresExecBit(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	if err := os.Chmod(filepath.Join(root, ".claude/hooks/lighthouse/g.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(t, root, "", func(o *Options) { o.Upgrade = true; o.Yes = true })
	info, _ := os.Stat(filepath.Join(root, ".claude/hooks/lighthouse/g.sh"))
	if r.code != 0 || info.Mode().Perm() != 0o755 {
		t.Fatalf("rc %d mode %o\n%s", r.code, info.Mode().Perm(), r.out)
	}
}

func TestInitInstallsRealCore(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", func(o *Options) { o.Yes = true; o.Packs = nil })
	if r.code != 0 || !exists(root, ".lighthouse/lib/lighthouse_lib/config.py") {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
}

func TestNewFileDiffHasNoPhantomLine(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", yes)
	if strings.Contains(r.out, "@@ -1 +1,") || strings.Contains(r.out, "\n-\n") {
		t.Fatalf("phantom removed line in a new-file diff:\n%s", r.out)
	}
	if !strings.Contains(r.out, "@@ -0,0 +1,") {
		t.Fatalf("new-file hunk header missing:\n%s", r.out)
	}
}

func TestUpgradeNeverDeletesOutsideTheRepo(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	write(t, filepath.Dir(outside), "outside.txt", "secret\n")
	rel, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	m := read(t, root, ".lighthouse/manifest.toml")
	m += "\n[[unit]]\nkind = \"file\"\npath = \"" + filepath.ToSlash(rel) + "\"\npackage = \"core\"\nversion = \"dev\"\nsha256 = \"" + manifest.Sum([]byte("secret\n")) + "\"\n"
	write(t, root, ".lighthouse/manifest.toml", m)
	r := run(t, root, "", func(o *Options) { o.Upgrade = true; o.Yes = true })
	if r.code != 1 || !exists(filepath.Dir(outside), "outside.txt") {
		t.Fatalf("rc %d, outside file exists: %v\n%s%s", r.code, exists(filepath.Dir(outside), "outside.txt"), r.out, r.err)
	}
}

func TestInitOnCRLFAgentsMD(t *testing.T) {
	root := newRepo(t)
	write(t, root, "AGENTS.md", "# Rules\r\n\r\nBe nice.\r\n")
	initYes(t, root)
	doc := read(t, root, "AGENTS.md")
	if strings.Count(doc, "\n") != strings.Count(doc, "\r\n") {
		t.Fatalf("mixed line endings: %q", doc)
	}
	if r := run(t, root, "", upgrade); r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("upgrade: rc %d out %q err %q", r.code, r.out, r.err)
	}
}

func TestUpgradeAfterCRLFNormalization(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	write(t, root, "AGENTS.md", strings.ReplaceAll(read(t, root, "AGENTS.md"), "\n", "\r\n"))
	if r := run(t, root, "", upgrade); r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("upgrade: rc %d out %q err %q", r.code, r.out, r.err)
	}
}
