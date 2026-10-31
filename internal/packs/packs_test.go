package packs

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const hooksJSON = `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/g.sh", "timeout": 15}]}]}}`

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func basePack() fstest.MapFS {
	return fstest.MapFS{
		"hooks/g.sh":             file("#!/bin/bash\necho g\n"),
		"hooks/hooks.json":       file(hooksJSON),
		"lib/a.py":               file("x = 1\n"),
		"scripts/s.py":           file("print(1)\n"),
		"agents/scout.md":        file("---\nname: scout\n---\nbody\n"),
		"instructions/AGENTS.md": file("## core\n\n- rule"),
		"requires.toml":          file("git = [\"g\"]\njq = [\"g\", \"h\"]\n"),
	}
}

func byPath(t *testing.T, p Pack) map[string]Unit {
	t.Helper()
	m := map[string]Unit{}
	for _, u := range p.Units {
		if u.Kind == KindFile {
			m[u.Path] = u
		}
	}
	return m
}

func TestLoadMapsPaths(t *testing.T) {
	p, err := Load(basePack(), "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	files := byPath(t, p)
	for _, want := range []string{
		".claude/hooks/lighthouse/g.sh",
		".lighthouse/lib/lighthouse_lib/a.py",
		".lighthouse/scripts/s.py",
		".claude/agents/lighthouse-scout.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s; have %v", want, keys(files))
		}
	}
	if len(files) != 4 {
		t.Errorf("files = %v", keys(files))
	}
	var blocks []Unit
	for _, u := range p.Units {
		if u.Kind == KindBlock {
			blocks = append(blocks, u)
		}
	}
	if len(blocks) != 1 || blocks[0].Path != "AGENTS.md" || blocks[0].ID != "core" || string(blocks[0].Content) != "## core\n\n- rule\n" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if files[".claude/agents/lighthouse-scout.md"].Package != "core" {
		t.Error("package not set")
	}
}

func keys(m map[string]Unit) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLoadHooksJSON(t *testing.T) {
	p, err := Load(basePack(), "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	var hooks []Unit
	for _, u := range p.Units {
		if u.Kind == KindHook {
			hooks = append(hooks, u)
		}
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks = %+v", hooks)
	}
	h := hooks[0]
	const cmd = `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/g.sh`
	if h.Path != ".claude/settings.json" || h.Hook.Event != "PreToolUse" || h.Hook.Matcher != "Bash" {
		t.Fatalf("hook = %+v", h)
	}
	if got, _ := h.Hook.Entry.Get("command"); got != cmd {
		t.Fatalf("command = %v", got)
	}
	if h.ID != HookID("PreToolUse", "Bash", cmd) {
		t.Fatalf("id = %q", h.ID)
	}
	if want := `{"command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh","timeout":15,"type":"command"}`; string(h.Content) != want {
		t.Fatalf("content = %s", h.Content)
	}
	if !IsLighthouseCommand(cmd) || IsLighthouseCommand(`"$CLAUDE_PROJECT_DIR"/.claude/hooks/own.sh`) {
		t.Fatal("IsLighthouseCommand")
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]func(fstest.MapFS){
		"command without plugin root": func(m fstest.MapFS) {
			m["hooks/hooks.json"] = file(strings.Replace(hooksJSON, "${CLAUDE_PLUGIN_ROOT}/hooks/g.sh", "/usr/bin/g.sh", 1))
		},
		"command to missing script": func(m fstest.MapFS) {
			m["hooks/hooks.json"] = file(strings.Replace(hooksJSON, "g.sh", "nope.sh", 1))
		},
		"invalid hooks.json":      func(m fstest.MapFS) { m["hooks/hooks.json"] = file("{") },
		"unknown dir":             func(m fstest.MapFS) { m["weird/x"] = file("x") },
		"loose top-level file":    func(m fstest.MapFS) { m["notes.txt"] = file("x") },
		"invalid requires.toml":   func(m fstest.MapFS) { m["requires.toml"] = file("git = 1\n") },
		"agent outside .md":       func(m fstest.MapFS) { m["agents/x.txt"] = file("x") },
		"instructions other file": func(m fstest.MapFS) { m["instructions/CLAUDE.md"] = file("x") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := basePack()
			mutate(m)
			_, err := Load(m, "core", "dev")
			if err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestLoadSkipsJunk(t *testing.T) {
	m := basePack()
	m["lib/__pycache__/a.cpython-314.pyc"] = file("x")
	m["lib/x.pyc"] = file("x")
	m[".DS_Store"] = file("x")
	m["lib/.DS_Store"] = file("x")
	m[".pytest_cache/v/x"] = file("x")
	p, err := Load(m, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if files := byPath(t, p); len(files) != 4 {
		t.Fatalf("files = %v", keys(files))
	}
}

func TestLoadModes(t *testing.T) {
	p, err := Load(basePack(), "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	files := byPath(t, p)
	if m := files[".claude/hooks/lighthouse/g.sh"].Mode; m != fs.FileMode(0o755) {
		t.Errorf("g.sh mode %o", m)
	}
	if m := files[".lighthouse/lib/lighthouse_lib/a.py"].Mode; m != fs.FileMode(0o644) {
		t.Errorf("a.py mode %o", m)
	}
}

func TestLoadMarks(t *testing.T) {
	p, err := Load(basePack(), "core", "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if c := string(byPath(t, p)[".claude/hooks/lighthouse/g.sh"].Content); c != "#!/bin/bash\n# lighthouse: core@v9.9.9\necho g\n" {
		t.Fatalf("content %q", c)
	}
}

func TestLoadRequires(t *testing.T) {
	p, err := Load(basePack(), "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Requires) != 2 || p.Requires[0].Tool != "git" || p.Requires[1].Tool != "jq" || strings.Join(p.Requires[1].NeededBy, ",") != "g,h" {
		t.Fatalf("requires = %+v", p.Requires)
	}
}

func skillFiles(t *testing.T, p Pack) map[string]Unit {
	t.Helper()
	m := map[string]Unit{}
	for _, u := range p.Units {
		if u.Kind == KindFile && (strings.HasPrefix(u.Path, ".claude/skills/") || strings.HasPrefix(u.Path, ".agents/skills/")) {
			m[u.Path] = u
		}
	}
	return m
}

func TestLoadSkillShared(t *testing.T) {
	m := basePack()
	m["skills/x/SKILL.md"] = file("---\nname: lighthouse-x\n---\nbody\n")
	m["skills/x/review.sh"] = file("#!/bin/bash\necho r\n")
	p, err := Load(m, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	files := skillFiles(t, p)
	for _, want := range []string{
		".claude/skills/lighthouse-x/SKILL.md",
		".agents/skills/lighthouse-x/SKILL.md",
		".claude/skills/lighthouse-x/review.sh",
		".agents/skills/lighthouse-x/review.sh",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s; have %v", want, keys(files))
		}
	}
	if len(files) != 4 {
		t.Errorf("files = %v", keys(files))
	}
	sh := files[".agents/skills/lighthouse-x/review.sh"]
	if sh.Mode != fs.FileMode(0o755) || !strings.Contains(string(sh.Content), "# lighthouse: core@dev") {
		t.Errorf("review.sh mode %o content %q", sh.Mode, sh.Content)
	}
	if c := string(files[".claude/skills/lighthouse-x/SKILL.md"].Content); !strings.Contains(c, `lighthouse: "core@dev"`) {
		t.Errorf("SKILL.md without marker: %q", c)
	}
}

func TestLoadSkillVariants(t *testing.T) {
	m := basePack()
	m["skills/x/claude/SKILL.md"] = file("---\nname: lighthouse-x\n---\nclaude\n")
	m["skills/x/agents/SKILL.md"] = file("---\nname: lighthouse-x\n---\nagents\n")
	m["skills/x/prompt.md"] = file("shared\n")
	p, err := Load(m, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	files := skillFiles(t, p)
	if len(files) != 4 {
		t.Fatalf("files = %v", keys(files))
	}
	if c := string(files[".claude/skills/lighthouse-x/SKILL.md"].Content); !strings.HasSuffix(c, "claude\n") {
		t.Errorf("claude SKILL.md = %q", c)
	}
	if c := string(files[".agents/skills/lighthouse-x/SKILL.md"].Content); !strings.HasSuffix(c, "agents\n") {
		t.Errorf("agents SKILL.md = %q", c)
	}
	if _, ok := files[".claude/skills/lighthouse-x/claude/SKILL.md"]; ok {
		t.Error("variant dir copied as is")
	}
}

func TestLoadSkillRejects(t *testing.T) {
	skill := func(name string) *fstest.MapFile { return file("---\nname: " + name + "\n---\nbody\n") }
	cases := map[string]fstest.MapFS{
		"wrong name":             {"skills/x/SKILL.md": skill("x")},
		"no name":                {"skills/x/SKILL.md": file("---\ndescription: d\n---\n")},
		"shared next to variant": {"skills/x/SKILL.md": skill("lighthouse-x"), "skills/x/claude/SKILL.md": skill("lighthouse-x")},
		"agents only":            {"skills/x/agents/SKILL.md": skill("lighthouse-x")},
		"no SKILL.md":            {"skills/x/prompt.md": file("p\n")},
		"other file in variant":  {"skills/x/claude/SKILL.md": skill("lighthouse-x"), "skills/x/claude/extra.md": file("e\n")},
		"loose file in skills":   {"skills/README.md": file("r\n")},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			m := basePack()
			for k, v := range extra {
				m[k] = v
			}
			_, err := Load(m, "core", "dev")
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), "skills/") {
				t.Fatalf("error does not name the path: %v", err)
			}
		})
	}
}

func TestIsInstallPathSkills(t *testing.T) {
	for p, want := range map[string]bool{
		".claude/skills/lighthouse-x/SKILL.md":         true,
		".agents/skills/lighthouse-x/sub/prompt.md":    true,
		".claude/skills/lighthouse-x":                  false,
		".claude/skills/lighthouse-/SKILL.md":          false,
		".claude/skills/other/SKILL.md":                false,
		".agents/skills/lighthouse-x/../../evil.md":    false,
		".claude/skills/lighthouse-x/SKILL.md/../x.md": false,
	} {
		if got := IsInstallPath(KindFile, p); got != want {
			t.Errorf("IsInstallPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestLoadMapsTemplatesAndWorkflows(t *testing.T) {
	m := basePack()
	m["headings.toml"] = file("[x]\nnl = \"X\"\nen = \"X\"\n")
	m["templates/pull_request_template.md"] = file("---\na: b\n---\nbody\n")
	m["workflows/unblock.yml"] = file("on: push\n")
	p, err := Load(m, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	files := byPath(t, p)
	if c := string(files[".lighthouse/headings.toml"].Content); c != "[x]\nnl = \"X\"\nen = \"X\"\n" {
		t.Errorf("headings.toml = %q", c)
	}
	if c := string(files[".github/pull_request_template.md"].Content); c != "---\na: b\n---\nbody\n" {
		t.Errorf("PR template = %q", c)
	}
	if c := string(files[".github/workflows/lighthouse-unblock.yml"].Content); c != "# lighthouse: core@dev\non: push\n" {
		t.Errorf("workflow = %q", c)
	}
}

func TestLoadRejectsOtherTemplatesAndWorkflows(t *testing.T) {
	for name, extra := range map[string]string{
		"other template":    "templates/ISSUE.md",
		"nested workflow":   "workflows/a/b.yml",
		"workflow not yml":  "workflows/x.txt",
		"loose file in .lh": "other.toml",
	} {
		t.Run(name, func(t *testing.T) {
			m := basePack()
			m[extra] = file("x\n")
			if _, err := Load(m, "core", "dev"); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestIsInstallPathGitHub(t *testing.T) {
	for p, want := range map[string]bool{
		".lighthouse/headings.toml":              true,
		".github/pull_request_template.md":       true,
		".github/workflows/lighthouse-x.yml":     true,
		".github/workflows/ci.yml":               false,
		".github/workflows/lighthouse-/x.yml":    false,
		".github/CODEOWNERS":                     false,
		".github/workflows/lighthouse-x.yml.bak": false,
	} {
		if got := IsInstallPath(KindFile, p); got != want {
			t.Errorf("IsInstallPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestLoadPermissions(t *testing.T) {
	m := basePack()
	m["permissions.json"] = file(`{"allow": ["Bash(.claude/skills/lighthouse-a/run.sh:*)", "Bash(.claude/hooks/lighthouse/b.sh:*)"]}`)
	p, err := Load(m, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range p.Units {
		if u.Kind == KindPermission {
			if u.Path != ".claude/settings.json" || string(u.Content) != u.ID {
				t.Errorf("unit %+v", u)
			}
			got = append(got, u.ID)
		}
	}
	if strings.Join(got, ",") != "Bash(.claude/skills/lighthouse-a/run.sh:*),Bash(.claude/hooks/lighthouse/b.sh:*)" {
		t.Fatalf("permissions = %v", got)
	}
}

// Decision 22: a pack rule must point into a lighthouse directory, or init could not
// tell it from a project rule.
func TestLoadRefusesRuleOutsidePattern(t *testing.T) {
	m := basePack()
	m["permissions.json"] = file(`{"allow": ["Bash(make test)"]}`)
	_, err := Load(m, "core", "dev")
	if err == nil || !strings.Contains(err.Error(), "Bash(make test)") {
		t.Fatalf("err = %v", err)
	}
}

func TestIsLighthouseRule(t *testing.T) {
	for rule, want := range map[string]bool{
		"Bash(.claude/skills/lighthouse-review-spec/review.sh:*)": true,
		"Bash(.claude/hooks/lighthouse/x.sh)":                     true,
		"Bash(make test)":                                         false,
		"Bash(.claude/skills/other/review.sh:*)":                  false,
		"Bash(.agents/skills/lighthouse-x/review.sh:*)":           false,
		"Bash(.claude/hooks/x.sh)":                                false,
	} {
		if got := IsLighthouseRule(rule); got != want {
			t.Errorf("IsLighthouseRule(%q) = %v", rule, got)
		}
	}
}

func TestLoadRejectsPermissions(t *testing.T) {
	for _, body := range []string{`{"allow": "x"}`, `{"allow": [1]}`, `{"allow": [""]}`, `{"deny": ["x"]}`, `[`, `{"allow": ["a", "a"]}`} {
		m := basePack()
		m["permissions.json"] = file(body)
		if _, err := Load(m, "core", "dev"); err == nil {
			t.Errorf("%s: no error", body)
		}
	}
}

const detectTOML = "files = [\"pyproject.toml\"]\n\n[[contains]]\nfiles = [\"**/package.json\"]\ntext = '\"expo\"'\n"

func TestLoadDetectAndFragment(t *testing.T) {
	m := basePack()
	m["detect.toml"] = file(detectTOML)
	m["lighthouse.toml.fragment"] = file("[test_gate]\ncommand = \"uv run pytest -q\"\n")
	p, err := Load(m, "python", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if p.Detect == nil || len(p.Detect.Files) != 1 || p.Detect.Files[0] != "pyproject.toml" {
		t.Fatalf("detect %#v", p.Detect)
	}
	if len(p.Detect.Contains) != 1 || p.Detect.Contains[0].Files[0] != "**/package.json" || p.Detect.Contains[0].Text != `"expo"` {
		t.Fatalf("contains %#v", p.Detect.Contains)
	}
	if len(p.Config) != 1 || p.Config[0].Name != "test_gate" || p.Config[0].Keys[0].Value != `"uv run pytest -q"` {
		t.Fatalf("config %#v", p.Config)
	}
	for _, u := range p.Units {
		if strings.Contains(u.Path, "detect") || strings.Contains(u.Path, "fragment") {
			t.Fatalf("unit for %s", u.Path)
		}
	}
}

func TestLoadWithoutDetectOrFragment(t *testing.T) {
	p, err := Load(basePack(), "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if p.Detect != nil || p.Config != nil {
		t.Fatalf("detect %#v config %#v", p.Detect, p.Config)
	}
}

func TestLoadRejectsDetect(t *testing.T) {
	cases := map[string]string{
		"absolute":        "files = [\"/x\"]\n",
		"magic":           "files = [\":(top)x\"]\n",
		"dot-dot":         "files = [\"a/../b\"]\n",
		"empty pathspec":  "files = [\"\"]\n",
		"unknown key":     "files = [\"x\"]\npaths = []\n",
		"not a list":      "files = \"x\"\n",
		"no text":         "[[contains]]\nfiles = [\"x\"]\n",
		"contains files":  "[[contains]]\ntext = \"x\"\n",
		"empty file":      "",
		"unknown in rule": "[[contains]]\nfiles = [\"x\"]\ntext = \"y\"\nother = 1\n",
		"invalid toml":    "files = [\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			m := basePack()
			m["detect.toml"] = file(body)
			_, err := Load(m, "python", "dev")
			if err == nil || !strings.Contains(err.Error(), "detect.toml") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestLoadRejectsFragment(t *testing.T) {
	m := basePack()
	m["lighthouse.toml.fragment"] = file("[t]\na = [1,\n2]\n")
	_, err := Load(m, "python", "dev")
	if err == nil || !strings.Contains(err.Error(), "lighthouse.toml.fragment") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
}
