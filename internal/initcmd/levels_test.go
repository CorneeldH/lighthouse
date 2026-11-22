package initcmd

import (
	"io/fs"
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

// Red if the default for an initialised repo without `level` is anything but strict.
func TestInitRepoWithoutLevelActsAsStrict(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "level = \"light\"\n", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"en\"\n")
	r = run(t, root, "", func(o *Options) { o.Packs = levelPacks(); o.Upgrade = true; o.Yes = true })
	if r.code != 0 {
		t.Fatalf("upgrade: code %d out %q err %q", r.code, r.out, r.err)
	}
	if !exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("a repo without level must install everything")
	}
	if !strings.Contains(read(t, root, levels.InstalledPath), "level = \"strict\"") {
		t.Errorf("components.toml = %s", read(t, root, levels.InstalledPath))
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

// shellHookPacks adds a shell hook (gofmt-check.sh) to levelPacks, listed at no level.
func shellHookPacks() fstest.MapFS {
	m := levelPacks()
	m["core/hooks/hooks.json"] = file(`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh main-commit-gate", "timeout": 15},
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh commit-test-gate pre", "timeout": 15},
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh md-link-check", "timeout": 15},
 {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/gofmt-check.sh", "timeout": 15}]}]}}`)
	m["core/hooks/gofmt-check.sh"] = file("#!/bin/bash\n")
	m["core/levels.toml"] = file(strings.Replace(levelsToml, `on = [`, `on = ["md-link-check", "gofmt-check", `, 1))
	return m
}

func initWith(t *testing.T, root, cfg string, packs fstest.MapFS) result {
	t.Helper()
	write(t, root, "lighthouse.toml", cfg)
	return run(t, root, "", func(o *Options) { o.Yes = true; o.Packs = packs })
}

// Red if init stops rejecting an unknown level or its error stops naming the valid ones.
func TestUnknownLevelNamesTheValidOnes(t *testing.T) {
	r := initLevels(t, newRepo(t), "level = \"mild\"\n", nil)
	if r.code == 0 || !strings.Contains(r.err, "mild") || !strings.Contains(r.err, "light") || !strings.Contains(r.err, "strict") {
		t.Errorf("code %d err %q", r.code, r.err)
	}
}

// Red if validate stops checking override names against the components the packs name.
func TestUnknownComponentIsAnError(t *testing.T) {
	r := initLevels(t, newRepo(t), "level = \"light\"\n\n[components]\n\"skill:nope\" = \"off\"\n", nil)
	if r.code == 0 || !strings.Contains(r.err, "skill:nope") || !strings.Contains(r.err, "skill:forge") {
		t.Errorf("code %d err %q", r.code, r.err)
	}
}

// Red if validate accepts an override on a base component.
func TestOverrideOnBaseIsRefused(t *testing.T) {
	r := initLevels(t, newRepo(t), "[components]\n\"run.sh\" = \"off\"\n", nil)
	if r.code == 0 || !strings.Contains(r.err, "run.sh") || !strings.Contains(r.err, "base") {
		t.Errorf("code %d err %q", r.code, r.err)
	}
}

// Red if validate lets log through for a skill.
func TestLogOnSkillIsRefused(t *testing.T) {
	r := initLevels(t, newRepo(t), "[components]\n\"skill:fix-pr\" = \"log\"\n", nil)
	if r.code == 0 || !strings.Contains(r.err, "skill:fix-pr") {
		t.Errorf("code %d err %q", r.code, r.err)
	}
}

// Red if validate lets log through for a hook that does not go through run.sh.
func TestLogOnShellHookIsRefused(t *testing.T) {
	r := initWith(t, newRepo(t), "[components]\ngofmt-check = \"log\"\n", shellHookPacks())
	if r.code == 0 || !strings.Contains(r.err, "gofmt-check") {
		t.Errorf("code %d err %q", r.code, r.err)
	}
}

// Red if validate refuses log on a Go hook, or loggable stops being derived from run.sh.
func TestLogOnMdLinkCheckIsAccepted(t *testing.T) {
	r := initWith(t, newRepo(t), "[components]\nmd-link-check = \"log\"\n", shellHookPacks())
	if r.code != 0 {
		t.Errorf("code %d err %q", r.code, r.err)
	}
}

// Red if a core gate set off is refused instead of warned about, the warning goes to
// stdout, or the gate stays registered.
func TestCoreGateOffWarnsOnStderrAndInstallsWithoutIt(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "level = \"strict\"\n\n[components]\nmain-commit-gate = \"off\"\n", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.err)
	}
	want := "lighthouse: main-commit-gate is off — this repo works outside the chain"
	if !strings.Contains(r.err, want) || strings.Contains(r.out, "outside the chain") {
		t.Errorf("stderr %q stdout %q", r.err, r.out)
	}
	if strings.Contains(read(t, root, ".claude/settings.json"), "main-commit-gate") {
		t.Error("main-commit-gate still registered")
	}
}

// Red if validation moves after planning or writing.
func TestInitWithBadNameWritesNothing(t *testing.T) {
	root := newRepo(t)
	r := initLevels(t, root, "[components]\n\"skill:nope\" = \"off\"\n", nil)
	if r.code == 0 {
		t.Fatal("want failure")
	}
	for _, p := range []string{".claude", ".lighthouse", "AGENTS.md", "CLAUDE.md"} {
		if exists(root, p) {
			t.Errorf("%s was written", p)
		}
	}
}

// Red if validate stops warning on a core gate that is not on.
func TestValidateWarnsPerCoreGate(t *testing.T) {
	f := levels.File{Levels: map[string]levels.Table{"strict": {}}, On: levels.CoreGates}
	w, err := validate([]levels.File{f}, "strict", map[string]levels.Stand{"worktree-check": levels.StandOff, "pr-create-gate": levels.StandLog}, func(string) bool { return true })
	if err != nil || len(w) != 2 {
		t.Fatalf("w=%v err=%v", w, err)
	}
}

// Red if validate stops refusing off on a skill that an on skill hands over to.
func TestValidateOffOnCalledSkillIsRefused(t *testing.T) {
	f := levels.File{
		Levels: map[string]levels.Table{"strict": {}},
		On:     []string{"skill:a", "skill:b"},
		Calls:  map[string][]string{"skill:a": {"skill:b"}},
	}
	_, err := validate([]levels.File{f}, "strict", map[string]levels.Stand{"skill:b": levels.StandOff}, func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "skill:a") || !strings.Contains(err.Error(), "skill:b") {
		t.Errorf("err %v", err)
	}
}

// Red if validate also refuses off on a skill that only an off skill hands over to.
func TestValidateOffCallerMayCallOffSkill(t *testing.T) {
	f := levels.File{
		Levels: map[string]levels.Table{"light": {Components: map[string]levels.Stand{"skill:a": levels.StandOff}}},
		On:     []string{"skill:b"},
		Calls:  map[string][]string{"skill:a": {"skill:b"}},
	}
	if _, err := validate([]levels.File{f}, "light", map[string]levels.Stand{"skill:b": levels.StandOff}, func(string) bool { return false }); err != nil {
		t.Errorf("err %v", err)
	}
}

// tree reads every file under root except .git, by slash path.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
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
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ticking is a clock that advances a day per call.
func ticking() func() time.Time {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	return func() time.Time { at = at.Add(24 * time.Hour); return at }
}

func switchTo(t *testing.T, root, level string, now func() time.Time) result {
	t.Helper()
	return run(t, root, "", func(o *Options) {
		o.Packs = levelPacks()
		o.Upgrade, o.Yes = true, true
		o.Level = level
		o.Now = now
	})
}

// Red if resolveLevels stops defaulting a first init to light, or planConfig stops
// writing the level line into the lighthouse.toml diff (the single y then shows no level).
func TestNewRepoChoosesLightAndWritesLevel(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "y\n", func(o *Options) { o.Packs = levelPacks() })
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "+level = \"light\"") {
		t.Errorf("diff lacks the level line:\n%s", r.out)
	}
	if !strings.HasPrefix(read(t, root, "lighthouse.toml"), "level = \"light\"\n") {
		t.Errorf("lighthouse.toml = %q", read(t, root, "lighthouse.toml"))
	}
	if exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("light installed fix-pr")
	}
}

// Red if --upgrade writes `level` on its own initiative (resolveLevels' write condition
// drops the initialized check), or if the effective level stops being strict.
func TestUpgradeWithoutLevelDoesNotWriteIt(t *testing.T) {
	root := newRepo(t)
	initLevels(t, root, "", nil)
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"en\"\n")
	r := run(t, root, "", func(o *Options) { o.Packs = levelPacks(); o.Upgrade, o.Yes = true, true })
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if strings.Contains(read(t, root, "lighthouse.toml"), "level") {
		t.Errorf("upgrade wrote level: %s", read(t, root, "lighthouse.toml"))
	}
	if !exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("without level the repo acts as strict")
	}
}

// Red if --level on an upgrade no longer rewrites lighthouse.toml, or the switch no
// longer goes through reconcile (the off units stay behind).
func TestUpgradeLevelFlagSwitchesAndRemovesUnits(t *testing.T) {
	root := newRepo(t)
	initLevels(t, root, "level = \"strict\"\n", nil)
	r := switchTo(t, root, "light", nil)
	if r.code != 0 {
		t.Fatalf("code %d: %s %s", r.code, r.out, r.err)
	}
	if !strings.Contains(read(t, root, "lighthouse.toml"), "level = \"light\"") {
		t.Errorf("lighthouse.toml = %s", read(t, root, "lighthouse.toml"))
	}
	if exists(root, ".claude/skills/lighthouse-fix-pr/SKILL.md") {
		t.Error("fix-pr survived the switch to light")
	}
	if strings.Contains(read(t, root, ".claude/settings.json"), "commit-test-gate") {
		t.Error("commit-test-gate still registered")
	}
	if !strings.Contains(r.err, "level: switching from strict to light") {
		t.Errorf("no switch message on stderr: %s", r.err)
	}
}

// Red if a hand-edited `level` and --level give different trees: the flag must only be
// a shortcut for the edit.
func TestHandEditedLevelThenUpgradeEqualsLevelFlag(t *testing.T) {
	a, b := newRepo(t), newRepo(t)
	for _, root := range []string{a, b} {
		initLevels(t, root, "level = \"strict\"\n", func(o *Options) { o.Now = ticking() })
	}
	switchTo(t, a, "light", ticking())
	write(t, b, "lighthouse.toml", "level = \"light\"\n")
	switchTo(t, b, "", ticking())
	ta, tb := tree(t, a), tree(t, b)
	for p, c := range ta {
		if tb[p] != c {
			t.Errorf("%s differs:\n%s\n---\n%s", p, c, tb[p])
		}
	}
	for p := range tb {
		if _, ok := ta[p]; !ok {
			t.Errorf("%s only in the hand-edited tree", p)
		}
	}
}

// Red if a strict -> light -> strict round trip leaves other files than the start: only
// components.toml (since) and its sha256 line in the manifest may differ.
func TestSwitchRoundTripIsIdentical(t *testing.T) {
	root := newRepo(t)
	now := ticking()
	initLevels(t, root, "level = \"strict\"\n", func(o *Options) { o.Now = now })
	start := tree(t, root)
	switchTo(t, root, "light", now)
	switchTo(t, root, "strict", now)
	end := tree(t, root)
	for p, c := range start {
		got, ok := end[p]
		switch {
		case !ok:
			t.Errorf("%s missing after the round trip", p)
		case p == levels.InstalledPath, p == ".lighthouse/manifest.toml":
		case got != c:
			t.Errorf("%s differs after the round trip", p)
		}
	}
	for p := range end {
		if _, ok := start[p]; !ok {
			t.Errorf("%s appeared in the round trip", p)
		}
	}
	line := func(s, prefix string) []string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.HasPrefix(l, prefix) {
				out = append(out, l)
			}
		}
		return out
	}
	if a, b := line(start[levels.InstalledPath], "since"), line(end[levels.InstalledPath], "since"); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("components.toml differs beyond since:\n%s\n---\n%s", start[levels.InstalledPath], end[levels.InstalledPath])
	}
	if start[levels.InstalledPath] == end[levels.InstalledPath] {
		t.Error("since must differ after a round trip")
	}
	// The manifest differs in one line only: the sha256 of components.toml.
	ms, me := strings.Split(start[".lighthouse/manifest.toml"], "\n"), strings.Split(end[".lighthouse/manifest.toml"], "\n")
	if len(ms) != len(me) {
		t.Fatalf("manifest length %d -> %d", len(ms), len(me))
	}
	diff := 0
	for i := range ms {
		if ms[i] != me[i] {
			diff++
			if !strings.Contains(ms[i], "sha256") {
				t.Errorf("manifest line %d differs and is no sha256 line: %q", i, ms[i])
			}
		}
	}
	if diff != 1 {
		t.Errorf("manifest differs in %d lines, want 1 (components.toml)", diff)
	}
}

// Red if a switch overwrites a locally changed unit without --force: reconcile's conflict
// refusal must apply to units the downgrade removes.
func TestDowngradeConflictRefusesWithoutForce(t *testing.T) {
	root := newRepo(t)
	initLevels(t, root, "level = \"strict\"\n", nil)
	write(t, root, ".claude/skills/lighthouse-fix-pr/SKILL.md", "mine\n")
	r := switchTo(t, root, "light", nil)
	if r.code == 0 || !strings.Contains(r.err, "refused: 1 locally modified unit(s)") {
		t.Fatalf("code %d err %q", r.code, r.err)
	}
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, "strict") {
		t.Errorf("lighthouse.toml changed on a refusal: %s", got)
	}
	if read(t, root, ".claude/skills/lighthouse-fix-pr/SKILL.md") != "mine\n" {
		t.Error("the local edit was lost")
	}
}

// Red if the summary stops naming the level or the next one, goes to stderr, or shows
// when nothing is written.
func TestSummaryNamesLevelAndHowToScaleUp(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", func(o *Options) { o.Packs = levelPacks(); o.Yes = true })
	want := "Level: light. Scale up with: lighthouse init --upgrade --level standard\n"
	if !strings.Contains(r.out, want) || strings.Contains(r.err, "Scale up") {
		t.Errorf("stdout %q stderr %q", r.out, r.err)
	}
	r = run(t, root, "", func(o *Options) { o.Packs = levelPacks(); o.Upgrade, o.Yes = true, true })
	if r.out != "No changes.\n" {
		t.Errorf("no-change stdout = %q", r.out)
	}
	r = switchTo(t, root, "strict", nil)
	if !strings.Contains(r.out, "Level: strict. Scale down with: lighthouse init --upgrade --level standard\n") {
		t.Errorf("strict summary: %q", r.out)
	}
}

// Red if switched stops treating a missing components.toml as a switch, or a recorded
// level equal to the effective one as none.
func TestSwitched(t *testing.T) {
	in := levels.Installed{Level: "light"}
	for _, c := range []struct {
		in    levels.Installed
		found bool
		level string
		want  bool
	}{
		{levels.Installed{}, false, "light", true},
		{in, true, "light", false},
		{in, true, "strict", true},
	} {
		if got := switched(c.in, c.found, c.level); got != c.want {
			t.Errorf("switched(%+v, %v, %q) = %v", c.in, c.found, c.level, got)
		}
	}
}
