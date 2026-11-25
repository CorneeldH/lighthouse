package initcmd

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/internal/automemory"
)

const memoryHooks = `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh main-commit-gate", "timeout": 15},
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh memory-gate", "timeout": 15}]}]}}`

const memoryLevels = `order = ["light", "standard", "strict"]
base = ["run.sh"]
on = ["main-commit-gate"]

[light.components]
memory-gate = "log"

[standard.components]

[strict.components]
`

func memoryPacks() fstest.MapFS {
	return fstest.MapFS{
		"core/hooks/run.sh":           file("#!/bin/bash\n"),
		"core/hooks/hooks.json":       file(memoryHooks),
		"core/levels.toml":            file(memoryLevels),
		"core/instructions/AGENTS.md": file("## core\n\n- rule\n"),
	}
}

// isolateMemory points CLAUDE_CONFIG_DIR at an empty temp dir, so a test never reads the real
// ~/.claude, and returns it.
func isolateMemory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	return dir
}

func initMemory(t *testing.T, root, level string, mod func(*Options)) result {
	t.Helper()
	return run(t, root, "", func(o *Options) {
		o.Yes = true
		o.Packs = memoryPacks()
		o.Level = level
		if mod != nil {
			mod(o)
		}
	})
}

func settingsHasMemory(t *testing.T, root, want string) {
	t.Helper()
	if got := read(t, root, ".claude/settings.json"); !strings.Contains(got, `"autoMemoryEnabled": `+want) {
		t.Errorf("settings.json lacks autoMemoryEnabled %s:\n%s", want, got)
	}
}

// Red if init stops writing autoMemoryEnabled into a settings.json that lacks it, at any stand.
func TestInitAutoMemoryLight(t *testing.T) {
	isolateMemory(t)
	root := newRepo(t)
	r := initMemory(t, root, "light", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	settingsHasMemory(t, root, "false")
	if strings.Contains(r.err, "autoMemoryEnabled") {
		t.Errorf("stderr mentions autoMemoryEnabled: %s", r.err)
	}
}

// Red if a true value is no longer overwritten when memory-gate is on, or the note goes.
func TestInitAutoMemoryStrictOverwrites(t *testing.T) {
	isolateMemory(t)
	root := newRepo(t)
	write(t, root, ".claude/settings.json", `{"autoMemoryEnabled": true}`+"\n")
	r := initMemory(t, root, "strict", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	settingsHasMemory(t, root, "false")
	want := "note: .claude/settings.json had autoMemoryEnabled true; memory-gate is on, so it is now false"
	if !strings.Contains(r.err, want) {
		t.Errorf("stderr lacks %q:\n%s", want, r.err)
	}
}

// Red if a true value is overwritten at log, or the warning goes.
func TestInitAutoMemoryLightKeeps(t *testing.T) {
	isolateMemory(t)
	root := newRepo(t)
	write(t, root, ".claude/settings.json", `{"autoMemoryEnabled": true}`+"\n")
	r := initMemory(t, root, "light", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	settingsHasMemory(t, root, "true")
	want := "warning: .claude/settings.json has autoMemoryEnabled true; memory-gate is at log, so it stays and auto-memory stays on"
	if !strings.Contains(r.err, want) {
		t.Errorf("stderr lacks %q:\n%s", want, r.err)
	}
}

// Red if an off memory-gate still changes or mentions autoMemoryEnabled.
func TestInitAutoMemoryOffOverride(t *testing.T) {
	isolateMemory(t)
	root := newRepo(t)
	write(t, root, ".claude/settings.json", `{"autoMemoryEnabled": true}`+"\n")
	write(t, root, "lighthouse.toml", "[components]\nmemory-gate = \"off\"\n")
	r := initMemory(t, root, "strict", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	settingsHasMemory(t, root, "true")
	if strings.Contains(r.err, "autoMemoryEnabled") {
		t.Errorf("stderr mentions autoMemoryEnabled: %s", r.err)
	}
}

// Red if a found memory file makes the second upgrade write, or the note leaves stderr.
func TestInitUpgradeNoChangesWithMemory(t *testing.T) {
	cfg := isolateMemory(t)
	root := newRepo(t)
	mem := filepath.Join("projects", automemory.EncodeProject(root), "memory")
	write(t, cfg, filepath.Join(mem, "a.md"), "---\nname: a\n---\nbody\n")
	if r := initMemory(t, root, "strict", nil); r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	r := initMemory(t, root, "", func(o *Options) { o.Upgrade = true })
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "No changes.") {
		t.Errorf("stdout lacks No changes.:\n%s", r.out)
	}
	if strings.Contains(r.out, "auto-memory") {
		t.Errorf("stdout carries the memory note:\n%s", r.out)
	}
	want := "note: 1 auto-memory files found for this repo; move them with /lighthouse-migrate-memory"
	if !strings.Contains(r.err, want) {
		t.Errorf("stderr lacks %q:\n%s", want, r.err)
	}
}

// Red if the scan uses the worktree path instead of the main checkout.
func TestInitMemoryNoteFromWorktree(t *testing.T) {
	cfg := isolateMemory(t)
	root := newRepo(t)
	gitIn(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x")
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, root, "worktree", "add", "-q", "-b", "side", wt)
	write(t, cfg, filepath.Join("projects", automemory.EncodeProject(root), "memory", "a.md"), "body\n")
	r := initMemory(t, wt, "strict", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.err, "note: 1 auto-memory files found") {
		t.Errorf("stderr lacks the memory note:\n%s", r.err)
	}
}
