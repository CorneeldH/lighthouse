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
		"skills":                  func(m fstest.MapFS) { m["skills/x/SKILL.md"] = file("---\nname: x\n---\n") },
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
			if name == "skills" && !strings.Contains(err.Error(), "skills/ is not supported yet") {
				t.Fatalf("error = %v", err)
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
