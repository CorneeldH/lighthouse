package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/CorneeldH/lighthouse/internal/levels"
)

const levelsHooks = `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh main-commit-gate", "timeout": 15},
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh commit-test-gate pre", "timeout": 15}]}]}}`

const levelsToml = `order = ["light", "standard", "strict"]
base = ["run.sh"]
on = ["main-commit-gate", "skill:forge"]

[light.components]
commit-test-gate = "off"
"skill:fix-pr" = "off"

[standard.components]

[strict.components]
`

const skillMD = "---\nname: lighthouse-%s\n---\nbody\n"

// levelPacks is a core package with a levels.toml, two hooks and three skills; mystery is
// named by no level.
func levelPacks() fstest.MapFS {
	return fstest.MapFS{
		"core/hooks/run.sh":            file("#!/bin/bash\n"),
		"core/hooks/hooks.json":        file(levelsHooks),
		"core/levels.toml":             file(levelsToml),
		"core/instructions/AGENTS.md":  file("## core\n\n- rule\n"),
		"core/skills/fix-pr/SKILL.md":  file(strings.Replace(skillMD, "%s", "fix-pr", 1)),
		"core/skills/forge/SKILL.md":   file(strings.Replace(skillMD, "%s", "forge", 1)),
		"core/skills/mystery/SKILL.md": file(strings.Replace(skillMD, "%s", "mystery", 1)),
		"core/templates/labels.md":     file("# Labels\n\n| `blocked-issue` | x |\n"),
		"core/requires.toml":           file("jq = [\"main-commit-gate\"]\n"),
	}
}

func initLevels(t *testing.T, root, tomlText string, mod func(*Options)) result {
	t.Helper()
	if tomlText != "" {
		write(t, root, "lighthouse.toml", tomlText)
	}
	return run(t, root, "", func(o *Options) {
		o.Yes = true
		o.Packs = levelPacks()
		if mod != nil {
			mod(o)
		}
	})
}

// Red if selectUnits stops dropping units whose component is off.
func TestInitLightSkipsOffUnits(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "level = \"light\"\n", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("fix-pr is off at light but installed")
	}
	if !exists(root, ".claude/skills/lighthouse-forge/SKILL.md") {
		t.Error("forge is on at light but missing")
	}
	settings := read(t, root, ".claude/settings.json")
	if strings.Contains(settings, "commit-test-gate") {
		t.Errorf("commit-test-gate registered at light: %s", settings)
	}
	if !strings.Contains(settings, "main-commit-gate") {
		t.Errorf("main-commit-gate missing: %s", settings)
	}
	if !exists(root, ".claude/hooks/lighthouse/run.sh") {
		t.Error("base unit run.sh must always be installed")
	}
}

// Red if selectUnits drops something at strict, or the level default is no longer strict.
func TestInitStrictInstallsEverything(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "level = \"strict\"\n", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	for _, p := range []string{".claude/skills/lighthouse-fix-pr/SKILL.md", ".claude/skills/lighthouse-forge/SKILL.md"} {
		if !exists(root, p) {
			t.Errorf("%s missing at strict", p)
		}
	}
	if !strings.Contains(read(t, root, ".claude/settings.json"), "commit-test-gate") {
		t.Error("commit-test-gate missing at strict")
	}
}

// Red if componentsUnit is not added to desired, or lists a component that is on.
func TestInitWritesComponentsToml(t *testing.T) {
	root := newRepo(t)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	r := initLevels(t, root, "level = \"light\"\n", func(o *Options) { o.Now = func() time.Time { return now } })
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	want := "# Managed by lighthouse init. Do not edit by hand.\n" +
		"level = \"light\"\nsince = \"2026-11-22T10:00:00Z\"\n\n" +
		"[components]\n\"commit-test-gate\" = \"off\"\n\"skill:fix-pr\" = \"off\"\n"
	if got := read(t, root, levels.InstalledPath); got != want {
		t.Errorf("components.toml =\n%s\nwant\n%s", got, want)
	}
	info, err := os.Stat(filepath.Join(root, levels.InstalledPath))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v err %v", info, err)
	}
}

// Red if the default for a repo without `level` is anything but strict, or if upgrade
// writes the key to lighthouse.toml.
func TestInitRepoWithoutLevelActsAsStrict(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "[project]\nlanguage = \"en\"\n", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if !exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("a repo without level must install everything")
	}
	if !strings.Contains(read(t, root, levels.InstalledPath), "level = \"strict\"") {
		t.Errorf("components.toml = %s", read(t, root, levels.InstalledPath))
	}
	r = run(t, root, "", func(o *Options) { o.Packs = levelPacks(); o.Upgrade = true; o.Yes = true })
	if r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("upgrade: code %d out %q err %q", r.code, r.out, r.err)
	}
	if strings.Contains(read(t, root, "lighthouse.toml"), "level") {
		t.Errorf("upgrade wrote level: %s", read(t, root, "lighthouse.toml"))
	}
}

// Red if overrides from [components] are no longer applied after the level.
func TestOverrideWinsOverLevel(t *testing.T) {
	root := newRepo(t)
	cfg := "level = \"light\"\n\n[components]\n\"skill:fix-pr\" = \"on\"\n\"skill:forge\" = \"off\"\n"
	r := initLevels(t, root, cfg, nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if !exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("override on must install fix-pr at light")
	}
	if exists(root, ".claude/skills/lighthouse-forge/SKILL.md") {
		t.Error("override off must drop forge")
	}
}

// Red if a component the `on` list names is reported as unlisted.
func TestAlwaysOnListedComponentIsNotReported(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "level = \"light\"\n", nil)
	if strings.Contains(r.err, "main-commit-gate") || strings.Contains(r.err, "skill:forge") {
		t.Errorf("listed component reported: %s", r.err)
	}
}

// Red if selectUnits stops reporting a component no levels.toml names, reports it on
// stdout, or drops it.
func TestUnlistedComponentIsReportedOnStderr(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "level = \"light\"\n", nil)
	if n := strings.Count(r.err, "skill:mystery"); n != 1 {
		t.Errorf("want one stderr line for skill:mystery, got %d: %s", n, r.err)
	}
	if strings.Contains(r.out, "mystery") && strings.Contains(r.out, "levels.toml") {
		t.Errorf("report on stdout: %s", r.out)
	}
	if !exists(root, ".claude/skills/lighthouse-mystery/SKILL.md") {
		t.Error("an unlisted component stays on")
	}
	r = run(t, root, "", func(o *Options) { o.Packs = levelPacks(); o.Upgrade = true })
	if r.out != "No changes.\n" {
		t.Errorf("stdout on no change = %q", r.out)
	}
}

// Red if since is rewritten on a rerun at the same level, or kept when the level changes.
func TestSinceKeptWhileLevelUnchanged(t *testing.T) {
	root := newRepo(t)
	t1 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(48 * time.Hour)
	initLevels(t, root, "level = \"light\"\n", func(o *Options) { o.Now = func() time.Time { return t1 } })
	r := run(t, root, "", func(o *Options) {
		o.Packs = levelPacks()
		o.Upgrade, o.Yes = true, true
		o.Now = func() time.Time { return t2 }
	})
	if r.out != "No changes.\n" {
		t.Fatalf("same level: out %q", r.out)
	}
	write(t, root, "lighthouse.toml", "level = \"standard\"\n")
	run(t, root, "", func(o *Options) {
		o.Packs = levelPacks()
		o.Upgrade, o.Yes = true, true
		o.Now = func() time.Time { return t2 }
	})
	if got := read(t, root, levels.InstalledPath); !strings.Contains(got, "2026-11-24T10:00:00Z") || !strings.Contains(got, `level = "standard"`) {
		t.Errorf("components.toml = %s", got)
	}
}
