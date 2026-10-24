package initcmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/internal/deps"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

func hookJSON(event, matcher, script string) string {
	return `{"hooks": {"` + event + `": [{"matcher": "` + matcher + `", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/` + script + `", "timeout": 15}]}]}}`
}

// withPython adds a stub of pack/python; the real content comes in the package PR.
func withPython(o *Options) {
	m := o.Packs.(fstest.MapFS)
	m["python/detect.toml"] = file("files = [\"pyproject.toml\", \"setup.py\", \"setup.cfg\"]\n")
	m["python/lighthouse.toml.fragment"] = file("[test_gate]\ncommand = \"uv run pytest -q\"\nunchecked_exit_codes = [3, 4, 5]\n")
	m["python/hooks/hooks.json"] = file(hookJSON("PostToolUse", "Edit|Write", "ruff-format.sh"))
	m["python/hooks/ruff-format.sh"] = file("#!/bin/bash\necho ruff\n")
	m["python/requires.toml"] = file("ruff = [\"ruff-format\"]\n")
	m["python/instructions/AGENTS.md"] = file("## python\n\n- rule\n")
}

// withShell adds a stub of pack/shell.
func withShell(o *Options) {
	m := o.Packs.(fstest.MapFS)
	m["shell/detect.toml"] = file("files = [\"**/*.sh\", \"**/*.bash\"]\n")
	m["shell/hooks/hooks.json"] = file(hookJSON("PostToolUse", "Edit|Write", "shellcheck.sh"))
	m["shell/hooks/shellcheck.sh"] = file("#!/bin/bash\necho sc\n")
}

func addPack(name string) func(*Options) {
	return func(o *Options) { o.AddPacks = append(o.AddPacks, name) }
}

func dropPack(name string) func(*Options) {
	return func(o *Options) { o.DropPacks = append(o.DropPacks, name) }
}

func TestDetectRules(t *testing.T) {
	python := &packs.Detect{Files: []string{"pyproject.toml", "setup.py", "setup.cfg"}}
	shell := &packs.Detect{Files: []string{"**/*.sh", "**/*.bash"}}
	expo := &packs.Detect{Contains: []packs.Contains{{Files: []string{"**/package.json"}, Text: `"expo"`}}}
	cases := []struct {
		name  string
		files map[string]string
		rule  *packs.Detect
		match string
	}{
		{"root pyproject", map[string]string{"pyproject.toml": ""}, python, "pyproject.toml"},
		{"pyproject in a subdirectory", map[string]string{"sub/pyproject.toml": ""}, python, ""},
		{"ignored setup.py", map[string]string{".gitignore": "setup.py\n", "setup.py": ""}, python, ""},
		{"nested shell script", map[string]string{"ci/x.sh": ""}, shell, "ci/x.sh"},
		{"root shell script", map[string]string{"x.sh": ""}, shell, "x.sh"},
		{"expo app", map[string]string{"apps/a/package.json": `{"dependencies": {"expo": "~54.0.37"}}`}, expo, "apps/a/package.json"},
		{"package.json without expo", map[string]string{"package.json": `{"dependencies": {"react": "1"}}`}, expo, ""},
		{"expo only in ignored node_modules", map[string]string{".gitignore": "node_modules/\n", "node_modules/x/package.json": `{"expo": 1}`}, expo, ""},
		{"empty repo", nil, python, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newRepo(t)
			for rel, body := range c.files {
				write(t, root, rel, body)
			}
			match, ok, err := detect(root, c.rule)
			if err != nil {
				t.Fatal(err)
			}
			if ok != (c.match != "") || match != c.match {
				t.Fatalf("match %q ok %v, want %q", match, ok, c.match)
			}
		})
	}
}

func TestDetectTrackedFile(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	gitIn(t, root, "add", "pyproject.toml")
	if match, ok, err := detect(root, &packs.Detect{Files: []string{"pyproject.toml"}}); err != nil || !ok || match != "pyproject.toml" {
		t.Fatalf("match %q ok %v err %v", match, ok, err)
	}
}

func packages(t *testing.T, root string) string {
	t.Helper()
	m, err := manifest.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(m.Packages, ",")
}

func TestInitProposesPythonInPythonRepo(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "[project]\nname = \"x\"\n")
	r := run(t, root, "y\n", withPython)
	if r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if !strings.HasPrefix(r.out, "Packages: core, python (found pyproject.toml)\n") {
		t.Fatalf("out:\n%s", r.out)
	}
	if !exists(root, ".claude/hooks/lighthouse/ruff-format.sh") || packages(t, root) != "core,python" {
		t.Fatalf("not installed; packages %s", packages(t, root))
	}
	if !strings.Contains(read(t, root, "AGENTS.md"), "<!-- lighthouse:begin python -->") {
		t.Fatalf("AGENTS.md = %q", read(t, root, "AGENTS.md"))
	}
}

func TestInitWithoutMarkersIsCoreOnly(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, withPython))
	if r.code != 0 || !strings.HasPrefix(r.out, "Packages: core\n") {
		t.Fatalf("rc %d out:\n%s", r.code, r.out)
	}
	if strings.Contains(read(t, root, "lighthouse.toml"), "[test_gate]") || packages(t, root) != "core" {
		t.Fatalf("toml %q packages %s", read(t, root, "lighthouse.toml"), packages(t, root))
	}
}

func TestInitPackFlag(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, withPython, addPack("python")))
	if r.code != 0 || !strings.HasPrefix(r.out, "Packages: core, python (--pack)\n") || packages(t, root) != "core,python" {
		t.Fatalf("rc %d out:\n%s\nerr %s", r.code, r.out, r.err)
	}
}

func TestInitNoPackFlag(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	r := run(t, root, "", all(yes, withPython, dropPack("python")))
	if r.code != 0 || !strings.HasPrefix(r.out, "Packages: core\n") || packages(t, root) != "core" {
		t.Fatalf("rc %d out:\n%s", r.code, r.out)
	}
	if exists(root, ".claude/hooks/lighthouse/ruff-format.sh") {
		t.Fatal("python installed")
	}
}

func TestNoPackCoreFails(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	r := run(t, root, "", all(yes, dropPack("core")))
	if r.code != 1 || !strings.Contains(r.err, "core cannot be left out") || !sameSnapshot(before, snapshot(t, root)) {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestUnknownPackFails(t *testing.T) {
	for _, mod := range []func(*Options){addPack("rust"), dropPack("rust")} {
		root := newRepo(t)
		before := snapshot(t, root)
		r := run(t, root, "", all(yes, mod))
		if r.code != 1 || !strings.Contains(r.err, "package rust is not in this binary") || !sameSnapshot(before, snapshot(t, root)) {
			t.Fatalf("rc %d err %q", r.code, r.err)
		}
	}
}

func TestUpgradeSuggestsFittingPack(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", all(yes, withPython)); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	write(t, root, "pyproject.toml", "")
	before := snapshot(t, root)
	r := run(t, root, "", all(upgrade, withPython))
	if r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("rc %d out %q err %q", r.code, r.out, r.err)
	}
	if !strings.Contains(r.err, "package python fits this repository (found pyproject.toml); add it with: lighthouse init --upgrade --pack python") {
		t.Fatalf("err %q", r.err)
	}
	if !sameSnapshot(before, snapshot(t, root)) {
		t.Fatal("files changed")
	}
}

func TestUpgradeAddsPack(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", all(yes, withPython)); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	r := run(t, root, "", all(upgrade, yes, withPython, addPack("python")))
	if r.code != 0 || !strings.HasPrefix(r.out, "Adding package python.\n") {
		t.Fatalf("rc %d out:\n%s\nerr %s", r.code, r.out, r.err)
	}
	if !exists(root, ".claude/hooks/lighthouse/ruff-format.sh") || packages(t, root) != "core,python" {
		t.Fatalf("packages %s", packages(t, root))
	}
	if r := run(t, root, "", all(upgrade, withPython)); r.out != "No changes.\n" {
		t.Fatalf("second upgrade: %q %q", r.out, r.err)
	}
}

// initWithPython installs core and the python stub in a Python repository.
func initWithPython(t *testing.T) string {
	t.Helper()
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	if r := run(t, root, "", all(yes, withPython)); r.code != 0 || packages(t, root) != "core,python" {
		t.Fatalf("init: rc %d %s", r.code, r.err)
	}
	return root
}

func TestUpgradeDropsPack(t *testing.T) {
	root := initWithPython(t)
	r := run(t, root, "", all(upgrade, yes, withPython, dropPack("python")))
	if r.code != 0 || !strings.HasPrefix(r.out, "Removing package python.\n") {
		t.Fatalf("rc %d out:\n%s\nerr %s", r.code, r.out, r.err)
	}
	if exists(root, ".claude/hooks/lighthouse/ruff-format.sh") || packages(t, root) != "core" {
		t.Fatalf("still installed; packages %s", packages(t, root))
	}
	if strings.Contains(read(t, root, ".claude/settings.json"), "ruff-format.sh") || strings.Contains(read(t, root, "AGENTS.md"), "python") {
		t.Fatalf("settings or AGENTS.md keep python:\n%s\n%s", read(t, root, ".claude/settings.json"), read(t, root, "AGENTS.md"))
	}
	if !strings.Contains(r.err, "lighthouse.toml: the keys package python added stay; remove them by hand if you no longer want them") {
		t.Fatalf("err %q", r.err)
	}
	if r := run(t, root, "", all(upgrade, withPython)); r.out != "No changes.\n" {
		t.Fatalf("second upgrade: %q %q", r.out, r.err)
	}
}

func TestNoPackRefusesLocalEdit(t *testing.T) {
	root := initWithPython(t)
	write(t, root, ".claude/hooks/lighthouse/ruff-format.sh", "#!/bin/bash\necho mine\n")
	r := run(t, root, "", all(upgrade, yes, withPython, dropPack("python")))
	if r.code != 1 || !strings.Contains(r.out, "conflict:") || !exists(root, ".claude/hooks/lighthouse/ruff-format.sh") {
		t.Fatalf("rc %d out:\n%s", r.code, r.out)
	}
	r = run(t, root, "", all(upgrade, yes, withPython, dropPack("python"), func(o *Options) { o.Force = true }))
	if r.code != 0 || exists(root, ".claude/hooks/lighthouse/ruff-format.sh") {
		t.Fatalf("force: rc %d\n%s", r.code, r.err)
	}
}

func TestPackOrderIsCoreThenAlphabetical(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	write(t, root, "x.sh", "")
	r := run(t, root, "", all(yes, withShell, withPython))
	if r.code != 0 || !strings.HasPrefix(r.out, "Packages: core, python (found pyproject.toml), shell (found x.sh)\n") {
		t.Fatalf("rc %d out:\n%s\nerr %s", r.code, r.out, r.err)
	}
	if packages(t, root) != "core,python,shell" {
		t.Fatalf("packages %s", packages(t, root))
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string }
		}
	}
	if err := json.Unmarshal([]byte(read(t, root, ".claude/settings.json")), &doc); err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, g := range doc.Hooks["PostToolUse"] {
		for _, h := range g.Hooks {
			cmds = append(cmds, h.Command)
		}
	}
	if len(cmds) != 2 || !strings.HasSuffix(cmds[0], "ruff-format.sh") || !strings.HasSuffix(cmds[1], "shellcheck.sh") {
		t.Fatalf("PostToolUse commands %v", cmds)
	}
}

func TestRequiresMergedPerTool(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	r := run(t, root, "", all(yes, withPython, func(o *Options) {
		o.Packs.(fstest.MapFS)["python/requires.toml"] = file("jq = [\"ruff-format\"]\n")
		o.Deps = deps.Checker{
			LookPath: func(name string) (string, error) {
				if name == "jq" {
					return "", errors.New("no")
				}
				return "/usr/bin/" + name, nil
			},
			Run: func(string, ...string) error { return nil },
		}
	}))
	if r.code != 0 || strings.Count(r.err, "missing dependency: jq") != 1 || !strings.Contains(r.err, "missing dependency: jq (needed by: main-commit-guard, ruff-format)") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestBrokenUnchosenPackFails(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, func(o *Options) { o.Packs.(fstest.MapFS)["rust/notes.txt"] = file("x") }))
	if r.code != 1 || !strings.Contains(r.err, "package rust") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func withExpo(o *Options) {
	o.Packs.(fstest.MapFS)["expo/detect.toml"] = file("[[contains]]\nfiles = [\"**/package.json\"]\ntext = '\"expo\"'\n")
	o.Packs.(fstest.MapFS)["expo/lib/e.py"] = file("x = 1\n")
}

// A tracked file whose directory became a file is gone, like a deleted one.
func TestDetectSkipsPathUnderAFile(t *testing.T) {
	root := newRepo(t)
	write(t, root, "a/package.json", `{"expo": 1}`)
	gitIn(t, root, "add", "a/package.json")
	if err := os.RemoveAll(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "a", "now a file")
	rule := &packs.Detect{Contains: []packs.Contains{{Files: []string{"**/package.json"}, Text: `"expo"`}}}
	if match, ok, err := detect(root, rule); err != nil || ok {
		t.Fatalf("match %q ok %v err %v", match, ok, err)
	}
}

// On --upgrade detection only suggests, so it cannot stop the upgrade.
func TestUpgradeDetectionErrorIsANote(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with mode 000")
	}
	root := newRepo(t)
	if r := run(t, root, "", all(yes, withExpo)); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	write(t, root, "x/package.json", `{"expo": 1}`)
	if err := os.Chmod(filepath.Join(root, "x/package.json"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "x/package.json"), 0o644) })
	r := run(t, root, "", all(upgrade, withExpo))
	if r.code != 0 || r.out != "No changes.\n" || !strings.Contains(r.err, "package expo: cannot check whether it fits:") {
		t.Fatalf("rc %d out %q err %q", r.code, r.out, r.err)
	}
}

// The files lighthouse itself installs do not make a package fit: a core-only repo
// has shell hooks under .claude/hooks/lighthouse/, and is no shell repo for that.
func TestDetectIgnoresInstalledFiles(t *testing.T) {
	root := newRepo(t)
	write(t, root, "README.md", "# x\n")
	realPacks := func(o *Options) { o.Packs = nil }
	r := run(t, root, "", all(yes, realPacks))
	if r.code != 0 || !strings.HasPrefix(r.out, "Packages: core\n") {
		t.Fatalf("init: rc %d\nout:\n%s\nerr:\n%s", r.code, r.out, r.err)
	}
	if !exists(root, ".claude/hooks/lighthouse/main-commit-guard.sh") {
		t.Fatal("core hooks not installed")
	}
	r = run(t, root, "", all(upgrade, realPacks))
	if r.code != 0 || r.out != "No changes.\n" || strings.Contains(r.err, "package shell fits") {
		t.Fatalf("upgrade: rc %d out %q err %q", r.code, r.out, r.err)
	}
}
