package agentaudit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

const sum64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// manifestWith is a valid manifest listing one installed file unit per path.
func manifestWith(paths ...string) string {
	var b strings.Builder
	b.WriteString("version = \"dev\"\npackages = [\"core\"]\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "\n[[unit]]\nkind = \"file\"\npath = %q\npackage = \"core\"\nversion = \"dev\"\nsha256 = %q\n", p, sum64)
	}
	return b.String()
}

func hookRows(inv *Inventory) []string {
	var rows []string
	for _, h := range inv.Hooks {
		rows = append(rows, fmt.Sprintf("%s %s %q %s", h.Source, h.Event, h.Matcher, h.Command))
	}
	return rows
}

func skillRows(inv *Inventory) []string {
	var rows []string
	for _, s := range inv.Skills {
		rows = append(rows, fmt.Sprintf("%s %s %s %s %s name=%d desc=%d", s.Harness, s.Source, s.Kind, s.Name, s.Path, s.NameChars, s.DescChars))
	}
	return rows
}

func permRows(inv *Inventory) []string {
	var rows []string
	for _, p := range inv.Permissions {
		rows = append(rows, p.Source+" "+p.Kind+" "+p.Rule)
	}
	return rows
}

func mcpRows(inv *Inventory) []string {
	var rows []string
	for _, m := range inv.MCP {
		rows = append(rows, m.Source+" "+m.Name+" "+m.Origin)
	}
	return rows
}

func status(inv *Inventory, source string) Status {
	for _, s := range inv.Statuses {
		if s.Source == source {
			return s
		}
	}
	return Status{}
}

func assertRows(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\ngot  %q\nwant %q", what, got, want)
	}
}

func TestClaudeJSONHonoursConfigDir(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("set", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", dir)
		env, err := DefaultEnv(root)
		if err != nil {
			t.Fatal(err)
		}
		if env.ConfigDir != dir || env.ClaudeJSON != filepath.Join(dir, ".claude.json") {
			t.Errorf("ConfigDir %q, ClaudeJSON %q; want %q and %q", env.ConfigDir, env.ClaudeJSON, dir, filepath.Join(dir, ".claude.json"))
		}
	})
	t.Run("unset", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		env, err := DefaultEnv(root)
		if err != nil {
			t.Fatal(err)
		}
		if env.ConfigDir != filepath.Join(home, ".claude") || env.ClaudeJSON != filepath.Join(home, ".claude.json") {
			t.Errorf("ConfigDir %q, ClaudeJSON %q; want %q and %q", env.ConfigDir, env.ClaudeJSON, filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json"))
		}
		if env.Home != home {
			t.Errorf("Home %q; want %q", env.Home, home)
		}
		if env.MainRoot != root || env.Root != root {
			t.Errorf("outside git: Root %q, MainRoot %q; want both %q", env.Root, env.MainRoot, root)
		}
	})
}

func TestDefaultEnvMainRootFromWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	hostless(t)
	// Resolved, as git prints them: on macOS the temp dir is /var/..., a link to /private/var/...
	main := resolvedTempDir(t)
	wt := filepath.Join(resolvedTempDir(t), "wt")
	for _, args := range [][]string{
		{"init", "-q", main},
		{"-C", main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"},
		{"-C", main, "worktree", "add", "-q", wt},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	env, err := DefaultEnv(wt)
	if err != nil {
		t.Fatal(err)
	}
	if env.Root != wt || env.MainRoot != main {
		t.Errorf("Root %q, MainRoot %q; want %q and %q", env.Root, env.MainRoot, wt, main)
	}
}

// resolvedTempDir is t.TempDir with its symlinks resolved.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCollectAttributesSources(t *testing.T) {
	home, configDir := hostless(t)
	mainRoot := t.TempDir()
	root := tree(t, map[string]string{
		".lighthouse/manifest.toml": manifestWith(".claude/skills/lighthouse-a/SKILL.md", ".agents/skills/lighthouse-a/SKILL.md"),
		".claude/settings.json": `{
  "permissions": {
    "allow": ["Bash(lighthouse review-run:*)", "Bash(make:*)"],
    "deny": ["Bash(lighthouse agent-audit:*)"]
  },
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [
    {"type": "command", "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh main-commit-gate"},
    {"type": "command", "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/check.sh"}
  ]}]}
}`,
		".claude/settings.local.json": `{"permissions": {"deny": ["Bash(rm:*)"]},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh main-commit-gate"}]}]}}`,
		".claude/hooks/lighthouse/run.sh":      "#!/bin/sh\nexit 2\n",
		".agents/skills/lighthouse-a/SKILL.md": "---\nname: lighthouse-a\ndescription: abcd\n---\n",
		".agents/skills/d/SKILL.md":            "---\nname: dee\ndescription: abcde\n---\n",
		".claude/hooks/check.sh":               "#!/bin/sh\necho check\n",
		".claude/hooks/old.sh":                 "#!/bin/sh\n",
		".claude/skills/lighthouse-a/SKILL.md": "---\nname: lighthouse-a\ndescription: abcd\n---\n",
		".claude/skills/b/SKILL.md":            "---\nname: bee\ndescription: ab\n---\n",
		".claude/agents/ag.md":                 "---\nname: agent-x\ndescription: abc\n---\n",
		".claude/rules/r.md":                   "---\npaths:\n  - \"src/**\"\n---\nRule.\n",
		".claude/rules/plain.md":               "Always.\n",
		".mcp.json":                            `{"mcpServers": {"m": {"command": "m"}}}`,
	})
	writeFiles(t, home, map[string]string{
		".claude/settings.json":     `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "~/.claude/hooks/u.sh"}, {"type": "command", "command": ".claude/hooks/lighthouse/run.sh main-commit-gate"}]}]}}`,
		".claude/hooks/u.sh":        "#!/bin/sh\n",
		".claude/skills/c/SKILL.md": "---\ndescription: c-désc\n---\n",
		".claude.json":              `{"mcpServers": {"u": {}}, "projects": {"` + mainRoot + `": {"mcpServers": {"l": {}}}}}`,
	})
	env := Env{Root: root, MainRoot: mainRoot, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home}
	before := treeHash(t, root, home)
	t.Setenv("HOME", t.TempDir()) // Collect takes home from Env.Home, not from the process

	inv, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}

	if after := treeHash(t, root, home); after != before {
		t.Error("Collect changed the tree")
	}
	assertRows(t, "statuses", statusRows(inv), []string{"lighthouse ok", "project ok", "lokaal ok", "gebruiker ok", "managed COULD NOT CHECK — managed settings are not read"})
	assertRows(t, "hooks", hookRows(inv), []string{
		`lighthouse PreToolUse "Bash" "$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh main-commit-gate`,
		`project PreToolUse "Bash" "$CLAUDE_PROJECT_DIR"/.claude/hooks/check.sh`,
		`lighthouse PreToolUse "Bash" "$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh main-commit-gate`,
		`gebruiker Stop "" ~/.claude/hooks/u.sh`,
		`lighthouse Stop "" .claude/hooks/lighthouse/run.sh main-commit-gate`,
	})
	if len(inv.Hooks) != 5 {
		t.Fatalf("want 5 hooks, got %d", len(inv.Hooks))
	}
	var files []string
	for _, h := range inv.Hooks {
		files = append(files, fmt.Sprintf("%s %t", h.File, h.Blocking))
	}
	// run.sh holds exit 2, but a lighthouse hook blocks only by name: no Packs, so none does.
	assertRows(t, "registering files", files, []string{
		".claude/settings.json false",
		".claude/settings.json false",
		".claude/settings.local.json false",
		"~/.claude/settings.json false",
		"~/.claude/settings.json false",
	})
	if got := inv.Hooks[1].Script; got != filepath.Join(root, ".claude", "hooks", "check.sh") {
		t.Errorf("project hook script %q", got)
	}
	if got := string(inv.Hooks[1].Content); got != "#!/bin/sh\necho check\n" {
		t.Errorf("project hook content %q", got)
	}
	if got := inv.Hooks[3].Script; got != filepath.Join(home, ".claude", "hooks", "u.sh") {
		t.Errorf("user hook script %q", got)
	}
	assertRows(t, "permissions", permRows(inv), []string{
		"lighthouse allow Bash(lighthouse review-run:*)",
		"project allow Bash(make:*)",
		"project deny Bash(lighthouse agent-audit:*)",
		"lokaal deny Bash(rm:*)",
	})
	assertRows(t, "skills", skillRows(inv), []string{
		"claude project skill bee .claude/skills/b/SKILL.md name=3 desc=2",
		"claude lighthouse skill lighthouse-a .claude/skills/lighthouse-a/SKILL.md name=12 desc=4",
		"claude project agent agent-x .claude/agents/ag.md name=7 desc=3",
		"codex project skill dee .agents/skills/d/SKILL.md name=3 desc=5",
		"codex lighthouse skill lighthouse-a .agents/skills/lighthouse-a/SKILL.md name=12 desc=4",
		"claude gebruiker skill c ~/.claude/skills/c/SKILL.md name=1 desc=6",
	})
	assertRows(t, "rules", ruleRows(inv), []string{"project .claude/rules/plain.md false", "project .claude/rules/r.md true"})
	assertRows(t, "mcp", mcpRows(inv), []string{"project m .mcp.json", "lokaal l ~/.claude.json", "gebruiker u ~/.claude.json"})
	assertRows(t, "scripts", inv.Scripts, []string{"project/.claude/hooks/check.sh", "project/.claude/hooks/old.sh", "gebruiker/~/.claude/hooks/u.sh"})
}

func statusRows(inv *Inventory) []string {
	var rows []string
	for _, s := range inv.Statuses {
		row := s.Source + " " + s.State
		if s.Reason != "" {
			row += " — " + s.Reason
		}
		rows = append(rows, row)
	}
	return rows
}

func ruleRows(inv *Inventory) []string {
	var rows []string
	for _, r := range inv.Rules {
		rows = append(rows, fmt.Sprintf("%s %s %t", r.Source, r.Path, r.HasPaths))
	}
	return rows
}

func TestCollectAbsentSources(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{"README.md": "x\n"})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "statuses", statusRows(inv), []string{"lighthouse afwezig", "project afwezig", "lokaal afwezig", "gebruiker afwezig", "managed COULD NOT CHECK — managed settings are not read"})
}

func TestCollectMissingRoot(t *testing.T) {
	hostless(t)
	if _, err := Collect(Env{Root: filepath.Join(t.TempDir(), "gone")}); err == nil {
		t.Fatal("Collect on a missing root: no error")
	}
}

func TestCollectInvalidUserSettings(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/settings.json": `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo project"}]}]}}`,
	})
	writeFiles(t, home, map[string]string{".claude/settings.json": "{not json"})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	user := status(inv, "gebruiker")
	if user.State != "COULD NOT CHECK" || !strings.Contains(user.Reason, "settings.json") {
		t.Errorf("gebruiker status %+v; want COULD NOT CHECK naming settings.json", user)
	}
	if got := status(inv, "project").State; got != "ok" {
		t.Errorf("project status %q; want ok", got)
	}
	assertRows(t, "hooks", hookRows(inv), []string{`project Stop "" echo project`})
}

func TestCollectInvalidClaudeJSON(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{".mcp.json": `{"mcpServers": {"m": {}}}`})
	writeFiles(t, home, map[string]string{".claude.json": "[1,"})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"gebruiker", "lokaal"} {
		if got := status(inv, source); got.State != "COULD NOT CHECK" || !strings.Contains(got.Reason, ".claude.json") {
			t.Errorf("%s status %+v; want COULD NOT CHECK naming .claude.json", source, got)
		}
	}
	assertRows(t, "mcp", mcpRows(inv), []string{"project m .mcp.json"})
}

func TestCollectScriptsOnlyScripts(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/hooks/hook.sh":                           "#!/bin/sh\n",
		".claude/hooks/guard.py":                          "",
		".claude/hooks/check.ts":                          "",
		".claude/hooks/lib.mjs":                           "",
		".claude/hooks/old.cjs":                           "",
		".claude/hooks/helper.js":                         "",
		".claude/hooks/notes":                             "plain text\n",
		".claude/hooks/package.json":                      "{}\n",
		".claude/hooks/package-lock.json":                 "{}\n",
		".claude/hooks/yarn.lock":                         "\n",
		".claude/hooks/todo.txt":                          "\n",
		".claude/hooks/node_modules/dep/index.js":         "",
		".claude/hooks/__pycache__/guard.cpython-312.pyc": "",
		".claude/hooks/stray.pyc":                         "",
		".claude/hooks/.cache/x.sh":                       "#!/bin/sh\n",
		".claude/hooks/sub/deeper.sh":                     "#!/bin/sh\n",
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home, RepoOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "scripts", inv.Scripts, []string{
		"project/.claude/hooks/check.ts",
		"project/.claude/hooks/guard.py",
		"project/.claude/hooks/helper.js",
		"project/.claude/hooks/hook.sh",
		"project/.claude/hooks/lib.mjs",
		"project/.claude/hooks/old.cjs",
		"project/.claude/hooks/sub/deeper.sh",
	})
}

func TestCollectScriptsCountsAnExecutable(t *testing.T) {
	skipOnWindows(t, "NTFS has no exec bit")
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/hooks/run-me":    "#!/bin/sh\n", // executable below
		".claude/hooks/notes":     "plain text\n",
		".claude/hooks/README.md": "# hooks\n",
	})
	// An executable file counts; an executable README.md stays a data file.
	for _, name := range []string{"run-me", "README.md"} {
		if err := os.Chmod(filepath.Join(root, ".claude", "hooks", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home, RepoOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "scripts", inv.Scripts, []string{"project/.claude/hooks/run-me"})
}

func TestCollectRepoOnly(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/settings.local.json": `{"permissions": {"allow": ["Bash(ls:*)"]}}`,
	})
	writeFiles(t, home, map[string]string{
		".claude/settings.json":     `{"permissions": {"deny": ["Bash(x:*)"]}, "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo u"}]}]}}`,
		".claude/skills/c/SKILL.md": "---\nname: c\ndescription: d\n---\n",
		".claude/hooks/u.sh":        "#!/bin/sh\n",
		".claude.json":              `{"mcpServers": {"u": {}}, "projects": {"` + root + `": {"mcpServers": {"l": {}}}}}`,
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home, RepoOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "statuses", statusRows(inv), []string{
		"lighthouse afwezig", "project afwezig", "lokaal ok", "gebruiker overgeslagen (--repo-only)",
		"plugin:* overgeslagen (--repo-only)", "managed overgeslagen (--repo-only)",
	})
	assertRows(t, "permissions", permRows(inv), []string{"lokaal allow Bash(ls:*)"})
	assertRows(t, "hooks", hookRows(inv), nil)
	assertRows(t, "skills", skillRows(inv), nil)
	assertRows(t, "mcp", mcpRows(inv), nil)
	assertRows(t, "scripts", inv.Scripts, nil)
}

func TestCollectPermissionsWrongShape(t *testing.T) {
	hook := `"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo h"}]}]}`
	cases := []struct {
		name     string
		settings string
		status   string
		hooks    []string
		perms    []string
	}{
		{
			name:     "hooks without permissions",
			settings: `{` + hook + `}`,
			status:   "project ok",
			hooks:    []string{`project Stop "" echo h`},
		},
		{
			name:     "permissions as a list",
			settings: `{"permissions": ["Bash(x:*)"], ` + hook + `}`,
			status:   "project COULD NOT CHECK — .claude/settings.json: \"permissions\" is not an object",
			hooks:    []string{`project Stop "" echo h`},
		},
		{
			name:     "allow not a list",
			settings: `{"permissions": {"allow": "Bash(x:*)", "deny": ["Bash(y:*)"]}, ` + hook + `}`,
			status:   "project COULD NOT CHECK — .claude/settings.json: permissions.allow is not an array of strings",
			hooks:    []string{`project Stop "" echo h`},
			perms:    []string{"project deny Bash(y:*)"},
		},
		{
			name:     "hooks wrong, permissions fine",
			settings: `{"permissions": {"allow": ["Bash(x:*)"]}, "hooks": []}`,
			status:   "project COULD NOT CHECK — .claude/settings.json: \"hooks\" is not an object",
			perms:    []string{"project allow Bash(x:*)"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, configDir := hostless(t)
			root := tree(t, map[string]string{".claude/settings.json": c.settings})
			inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
			if err != nil {
				t.Fatal(err)
			}
			assertRows(t, "project status", statusRows(&Inventory{Statuses: []Status{status(inv, "project")}}), []string{c.status})
			assertRows(t, "hooks", hookRows(inv), c.hooks)
			assertRows(t, "permissions", permRows(inv), c.perms)
		})
	}
}

func TestCollectBlocking(t *testing.T) {
	home, configDir := hostless(t)
	cmd := func(c string) string { return `{"type": "command", "command": "` + c + `"}` }
	root := tree(t, map[string]string{
		".claude/settings.json": `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [` + strings.Join([]string{
			cmd(`.claude/hooks/exit2.sh`),
			cmd(`.claude/hooks/deny.sh`),
			cmd(`.claude/hooks/block.sh`),
			cmd(`.claude/hooks/plain.sh`),
			cmd(`echo inline; exit 2`),
			cmd(`\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh listed-gate`),
			cmd(`\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh guarded-gate`),
			cmd(`\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh quiet-hook`),
			cmd(`${CLAUDE_PROJECT_DIR}/.claude/hooks/lighthouse/script-gate.sh`),
		}, ",") + `]}]}}`,
		".claude/hooks/exit2.sh":                  "#!/bin/sh\necho no >&2\nexit 2\n",
		".claude/hooks/deny.sh":                   "#!/bin/sh\necho '{\"hookSpecificOutput\": {\"permissionDecision\": \"deny\"}}'\n",
		".claude/hooks/block.sh":                  "#!/bin/sh\necho '{\"decision\":\"block\"}'\n",
		".claude/hooks/plain.sh":                  "#!/bin/sh\necho fine\nexit 0\nexit 20\n",
		".claude/hooks/lighthouse/run.sh":         "#!/bin/sh\nexit 2\n",
		".claude/hooks/lighthouse/script-gate.sh": "#!/bin/sh\n",
	})
	env := Env{
		Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home,
		Packs: []packs.Pack{{
			Name:   "core",
			Units:  []packs.Unit{{Kind: packs.KindFile, Path: packs.HookDir + "blocking", Content: []byte("listed-gate\nscript-gate\n")}},
			Guards: []packs.Guard{{Name: "guarded-gate"}},
		}},
	}
	inv, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range inv.Hooks {
		got = append(got, fmt.Sprintf("%s %t", h.Command, h.Blocking))
	}
	assertRows(t, "blocking", got, []string{
		".claude/hooks/exit2.sh true",
		".claude/hooks/deny.sh true",
		".claude/hooks/block.sh true",
		".claude/hooks/plain.sh false",
		"echo inline; exit 2 true",
		`"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh listed-gate true`,
		`"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh guarded-gate true`,
		`"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh quiet-hook false`,
		"${CLAUDE_PROJECT_DIR}/.claude/hooks/lighthouse/script-gate.sh true",
	})
}

func TestCollectUnreadableScript(t *testing.T) {
	skipOnWindows(t, "chmod 000 does not make a file unreadable on NTFS")
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/settings.json":   `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": ".claude/hooks/locked.sh"}]}]}}`,
		".claude/hooks/locked.sh": "#!/bin/sh\nexit 2\n",
	})
	locked := filepath.Join(root, ".claude", "hooks", "locked.sh")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Hooks) != 1 {
		t.Fatalf("hooks %v", hookRows(inv))
	}
	h := inv.Hooks[0]
	if h.Script != locked || h.Unreadable == "" || h.Blocking {
		t.Errorf("hook %+v; want script %s, a reason in Unreadable, not blocking", h, locked)
	}
}
