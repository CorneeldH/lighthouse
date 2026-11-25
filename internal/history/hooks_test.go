package history

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/agentaudit"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/pack"
)

func coreGuards(t *testing.T) []packs.Guard {
	t.Helper()
	sub, err := fs.Sub(pack.Files, "core")
	if err != nil {
		t.Fatal(err)
	}
	p, err := packs.Load(sub, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	return p.Guards
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

const ownGuard = `#!/usr/bin/env bash
# Refuses a git commit straight onto main, so work happens on a branch.
command=$(jq -r '.tool_input.command // empty')
case "$command" in *"git commit"*) ;; *) exit 0 ;; esac
branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = "main" ] && exit 2
exit 0
`

func settingsFor(command string) string {
	return `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "` + command + `"}]}]}}`
}

func TestOverlapFromAudit(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".claude")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	root := t.TempDir()
	plug := filepath.Join(home, "plug", "p")
	writeTree(t, root, map[string]string{
		".claude/settings.json":       settingsFor(".claude/hooks/my-guard.sh"),
		".claude/hooks/my-guard.sh":   ownGuard,
		".claude/settings.local.json": `{"enabledPlugins": {"p@m": true}}`,
	})
	writeTree(t, home, map[string]string{
		".claude/plugins/installed_plugins.json": `{"version": 2, "plugins": {"p@m": [{"scope": "user", "installPath": "` + plug + `", "version": "1"}]}}`,
	})
	writeTree(t, plug, map[string]string{
		"hooks/hooks.json": `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/guard.sh"}]}]}}`,
		"guard.sh":         ownGuard,
	})
	env := agentaudit.Env{Root: root, MainRoot: root, Home: home, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json")}
	got, err := OverlapFromAudit(env, coreGuards(t))
	if err != nil {
		t.Fatal(err)
	}
	want := HookOverlap{Hook: "project/.claude/hooks/my-guard.sh", Guard: "main-commit-gate"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want only %+v (plugin hook excluded)", got, want)
	}
}

func TestOverlapFromAudit_MissingRoot(t *testing.T) {
	if _, err := OverlapFromAudit(agentaudit.Env{Root: filepath.Join(t.TempDir(), "nope")}, nil); err == nil {
		t.Error("want an error for a missing root")
	}
}

func blockRec(cmd string) Record {
	return Record{File: "f.jsonl", Line: 1, Type: "attachment", HookName: "PreToolUse:Bash", HookCommand: cmd}
}

func hooksFinding(env Env, recs ...Record) Finding {
	d := newHooks(env)
	feed(d, recs...)
	return d.Finish(env)
}

func TestHooks_BlockingByHook(t *testing.T) {
	f := hooksFinding(Env{},
		blockRec("/x/y/run.sh"), blockRec("/x/y/run.sh"), blockRec("/x/y/run.sh"),
		blockRec(`"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/usage-gate.sh tool`),
		blockRec(`C:\Users\Alice\hooks\run.ps1`),
		blockRec(`"C:\Program Files\Alice\hooks\quoted.ps1" -x`),
		blockRec(""),
	)
	by := f.Measured["blocking_by_hook"].(map[string]int)
	if by["run.sh"] != 3 || by["usage-gate.sh"] != 1 || by["run.ps1"] != 1 || by["quoted.ps1"] != 1 || len(by) != 4 {
		t.Errorf("blocking_by_hook %v", by)
	}
	if f.Key != "hooks" || f.Status != "found" {
		t.Errorf("finding %+v", f)
	}
	if s := toJSON(t, f); strings.Contains(s, "Alice") || strings.Contains(s, "Users") {
		t.Errorf("leak: %s", s)
	}
}

func TestHooks_MeasuredKeys(t *testing.T) {
	f := hooksFinding(Env{}, blockRec("/x/run.sh"))
	if len(f.Measured) != 2 || f.Measured["overlapping"] == nil || f.Measured["blocking_by_hook"] == nil {
		t.Errorf("measured %v", f.Measured)
	}
}

func TestHooks_Overlap(t *testing.T) {
	env := Env{AuditHooks: []HookOverlap{{Hook: "project/.claude/hooks/my-guard.sh", Guard: "main-commit-gate"}}}
	f := hooksFinding(env)
	if f.Status != "found" {
		t.Fatalf("status %s", f.Status)
	}
	ov := f.Measured["overlapping"].([]map[string]string)
	if len(ov) != 1 || ov[0]["hook"] != "project/.claude/hooks/my-guard.sh" || ov[0]["guard"] != "main-commit-gate" {
		t.Errorf("overlapping %v", ov)
	}
	if f.Proposal.Kind != "advice" || !strings.Contains(f.Proposal.Text, "my-guard.sh") || !strings.Contains(f.Proposal.Text, "/lighthouse-audit-agent-config") {
		t.Errorf("proposal %+v", f.Proposal)
	}
}

func TestHooks_NoneAndNoData(t *testing.T) {
	if f := hooksFinding(Env{}); f.Status != "none" {
		t.Errorf("status %s", f.Status)
	}
	f := hooksFinding(Env{AuditErr: errors.New("boom")}, blockRec("/x/run.sh"))
	if f.Status != "no_data" {
		t.Errorf("status %s", f.Status)
	}
	if strings.Contains(toJSON(t, f), "boom") {
		t.Error("error text in finding")
	}
}

func TestOverlapFromAudit_NoLeak(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".claude")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	root := t.TempDir()
	outside := t.TempDir() // neither under the repo nor under home
	reg := func(cmds ...string) string {
		var hs []string
		for _, c := range cmds {
			hs = append(hs, `{"type": "command", "command": "`+c+`"}`)
		}
		return `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [` + strings.Join(hs, ",") + `]}]}}`
	}
	writeTree(t, home, map[string]string{
		".claude/settings.json": reg(filepath.Join(home, "bin", "alice-guard.sh")),
		"bin/alice-guard.sh":    ownGuard,
	})
	writeTree(t, root, map[string]string{
		".claude/settings.local.json": reg(filepath.Join(outside, "bobsecret", "abs-guard.sh"), "bash -c 'git commit main' --token=hunter2"),
	})
	writeTree(t, outside, map[string]string{"bobsecret/abs-guard.sh": ownGuard})
	env := agentaudit.Env{Root: root, MainRoot: root, Home: home, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json")}
	got, err := OverlapFromAudit(env, coreGuards(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("want the home and the absolute hook, got %+v", got)
	}
	s := toJSON(t, got)
	for _, bad := range []string{"bobsecret", "hunter2", outside, home, "--token"} {
		if strings.Contains(s, bad) {
			t.Errorf("%q leaks in %s", bad, s)
		}
	}
	var sawHome, sawAbs bool
	for _, o := range got {
		sawHome = sawHome || o.Hook == "gebruiker/~/bin/alice-guard.sh"
		sawAbs = sawAbs || o.Hook == "lokaal/abs-guard.sh"
	}
	if !sawHome || !sawAbs {
		t.Errorf("names %+v", got)
	}
}

func TestSafeHookPath(t *testing.T) {
	for in, want := range map[string]string{
		".claude/hooks/x.sh":           ".claude/hooks/x.sh",
		"~/bin/x.sh":                   "~/bin/x.sh",
		"/home/alice/x.sh":             "x.sh",
		"~alice/x.sh":                  "x.sh",
		`C:\Users\alice\x.ps1`:         "x.ps1",
		"curl http://u:pw@h/x --k=abc": "curl",
	} {
		if got := safeHookPath(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
