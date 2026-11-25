package packs

import (
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/pack"
)

// shipped loads every package of pack.Files, as init does.
func shipped(t *testing.T) map[string]Pack {
	t.Helper()
	entries, err := fs.ReadDir(pack.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Pack{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := fs.Sub(pack.Files, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		p, err := Load(sub, e.Name(), "dev")
		if err != nil {
			t.Fatalf("package %s: %v", e.Name(), err)
		}
		out[e.Name()] = p
	}
	return out
}

func hookUnit(event, matcher, command string) Unit {
	full := projectHooks + command
	return Unit{Kind: KindHook, Path: settingsPath, ID: HookID(event, matcher, full), Hook: &Registration{Event: event, Matcher: matcher}}
}

// Red when a unit of a shipped package has no component, no owner entry or no base place in its levels.toml.
func TestEveryUnitHasExactlyOnePlace(t *testing.T) {
	for name, p := range shipped(t) {
		for _, u := range p.Units {
			if _, _, err := Component(u, p.Levels); err != nil {
				t.Errorf("package %s: unit %s (%s): %v", name, u.Path, u.ID, err)
			}
		}
	}
}

// Red when Component derives another name or base flag from a registration, hook file, skill file, owner or fixed path.
func TestComponentDerivation(t *testing.T) {
	f := levels.File{
		Levels: map[string]levels.Table{},
		Base:   []string{"usage-gate", "headings-x"},
		Owners: map[string]string{"agents/scout.md": "skill:start-issue", "scripts/lib/x.sh": "base"},
	}
	file := func(p string) Unit { return Unit{Kind: KindFile, Path: p} }
	tests := []struct {
		name     string
		unit     Unit
		wantName string
		wantBase bool
	}{
		{"pre registration", hookUnit("PreToolUse", "Bash", "run.sh commit-test-gate pre"), "commit-test-gate", false},
		{"post failure registration", hookUnit("PostToolUseFailure", "Bash", "run.sh commit-test-gate post"), "commit-test-gate", false},
		{"matcher with a pipe", hookUnit("PreToolUse", "Edit|Write", "run.sh worktree-check"), "worktree-check", false},
		{"script registration", hookUnit("Stop", "", "go-lint-gate.sh"), "go-lint-gate", false},
		{"hook file", file(HookDir + "gofmt-check.sh"), "gofmt-check", false},
		{"run.sh file", file(HookDir + "run.sh"), "", true},
		{"blocking file", file(HookDir + "blocking"), "", true},
		{"base hook by name", hookUnit("Stop", "", "run.sh usage-gate stop"), "", true},
		{"claude skill file", file(".claude/skills/lighthouse-go-standard/SKILL.md"), "skill:go-standard", false},
		{"agents skill file", file(".agents/skills/lighthouse-go-standard/ref/a.md"), "skill:go-standard", false},
		{"agent by owner", file(".claude/agents/lighthouse-scout.md"), "skill:start-issue", false},
		{"script owned by base", file(".lighthouse/scripts/lib/x.sh"), "", true},
		{"permission", Unit{Kind: KindPermission, Path: settingsPath, ID: "Bash(x)"}, "", true},
		{"block", Unit{Kind: KindBlock, Path: "AGENTS.md", ID: "core"}, "", true},
		{"headings", file(HeadingsPath), "", true},
		{"pr template", file(PRTemplatePath), "", true},
		{"workflow", file(WorkflowPrefix + "unblock.yml"), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, base, err := Component(tc.unit, f)
			if err != nil {
				t.Fatal(err)
			}
			if name != tc.wantName || base != tc.wantBase {
				t.Errorf("Component = (%q, %v), want (%q, %v)", name, base, tc.wantName, tc.wantBase)
			}
		})
	}
}

// Red when Component returns no error for an unowned agent or script, a name in base and a level table, or a path it cannot place.
func TestComponentErrors(t *testing.T) {
	f := levels.File{
		Levels: map[string]levels.Table{"light": {Components: map[string]levels.Stand{"both": levels.StandOff}}},
		Base:   []string{"both"},
	}
	tests := map[string]Unit{
		"agent without owner":     {Kind: KindFile, Path: ".claude/agents/lighthouse-lost.md"},
		"script without owner":    {Kind: KindFile, Path: ".lighthouse/scripts/lost.sh"},
		"component and base":      hookUnit("Stop", "", "run.sh both"),
		"unknown file location":   {Kind: KindFile, Path: "somewhere/else.md"},
		"registration no hookdir": {Kind: KindHook, Path: settingsPath, ID: "Stop||x"},
	}
	for name, u := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Component(u, f); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// Red when Component errors on a package without levels.toml instead of calling every unit base.
func TestComponentWithoutLevelsFileIsBase(t *testing.T) {
	u := Unit{Kind: KindFile, Path: ".claude/agents/lighthouse-lost.md"}
	name, base, err := Component(u, levels.File{})
	if err != nil || !base || name != "" {
		t.Errorf("Component = (%q, %v, %v), want base without error", name, base, err)
	}
}

// named lists every component a levels.toml names in a level table or in on.
func named(f levels.File) map[string]bool {
	m := map[string]bool{}
	for _, c := range f.On {
		m[c] = true
	}
	for _, tb := range f.Levels {
		for c := range tb.Components {
			m[c] = true
		}
	}
	return m
}

// Red when a derived component is removed from every level table and from on in a shipped levels.toml.
func TestEveryDerivedComponentIsNamed(t *testing.T) {
	for pkgName, p := range shipped(t) {
		listed := named(p.Levels)
		for _, u := range p.Units {
			name, base, err := Component(u, p.Levels)
			if err != nil || base {
				continue
			}
			if !listed[name] {
				t.Errorf("package %s: component %q is in no level table, base or on", pkgName, name)
			}
		}
	}
}

// Red when a shipped levels.toml names a component that no unit of its package belongs to (a typo).
func TestLevelsNameOnlyRealComponents(t *testing.T) {
	for pkgName, p := range shipped(t) {
		real := map[string]bool{}
		for _, u := range p.Units {
			if name, base, err := Component(u, p.Levels); err == nil && !base {
				real[name] = true
			}
		}
		for c := range named(p.Levels) {
			if !real[c] {
				t.Errorf("package %s: levels.toml names %q, which no unit belongs to", pkgName, c)
			}
		}
	}
}

// Red when a stand, the order, the labels flag or a skill row of the core ladder is changed in pack/core/levels.toml.
func TestCoreLevelsTable(t *testing.T) {
	f := shipped(t)["core"].Levels
	if want := []string{"light", "standard", "strict"}; !reflect.DeepEqual(f.Order, want) {
		t.Errorf("order = %v, want %v", f.Order, want)
	}
	light := f.Levels["light"]
	for _, c := range []string{"commit-test-gate"} {
		if light.Components[c] != levels.StandOff {
			t.Errorf("light %s = %q, want off", c, light.Components[c])
		}
	}
	for _, c := range []string{"md-link-check", "code-language-check"} {
		if light.Components[c] != levels.StandLog {
			t.Errorf("light %s = %q, want log", c, light.Components[c])
		}
	}
	for _, c := range levels.CoreGates {
		if s, ok := light.Components[c]; ok && s != levels.StandOn {
			t.Errorf("light turns core gate %s %q", c, s)
		}
	}
	if light.Flags["labels"] || !f.Levels["standard"].Flags["labels"] || !f.Levels["strict"].Flags["labels"] {
		t.Errorf("labels flag: light %v standard %v strict %v", light.Flags, f.Levels["standard"].Flags, f.Levels["strict"].Flags)
	}
	if len(f.Levels["strict"].Components) != 0 {
		t.Errorf("strict components = %v, want none", f.Levels["strict"].Components)
	}
	for _, c := range []string{"skill:review-spec", "skill:review-plan"} {
		if f.Levels["standard"].Components[c] != levels.StandOff || f.Levels["strict"].Components[c] == levels.StandOff {
			t.Errorf("%s should be off at standard and on at strict", c)
		}
	}
}

// Red when the owner of a script changes in a levels.toml, or a script is added or removed in a pack without a table row.
func TestScriptOwnersTable(t *testing.T) {
	got := map[string]string{}
	for _, p := range shipped(t) {
		for path, owner := range p.Levels.Owners {
			if strings.HasPrefix(path, "scripts/") {
				got[path] = owner
			}
		}
	}
	want := map[string]string{
		"scripts/gate_python.py":            "base",
		"scripts/quality/__init__.py":       "base",
		"scripts/quality/baseline.py":       "base",
		"scripts/quality/diffcov.py":        "base",
		"scripts/quality/gitenv.py":         "base",
		"scripts/quality/prcov.py":          "base",
		"scripts/quality/prcov_issues.py":   "base",
		"scripts/quality/ratchet.py":        "base",
		"scripts/shellcheck-repo.sh":        "shell-lint-check",
		"scripts/lib/shellcheck-bin.sh":     "shell-lint-check",
		"scripts/psscriptanalyzer-repo.ps1": "powershell-lint-check",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("script owners = %v, want %v", got, want)
	}
	// Every script file of every package is in the table (TestEveryUnitHasExactlyOnePlace
	// covers the units; this pins that no script is missing from the files).
	var onDisk []string
	for _, p := range shipped(t) {
		for _, u := range p.Units {
			if rel, ok := strings.CutPrefix(u.Path, ".lighthouse/scripts/"); ok {
				onDisk = append(onDisk, "scripts/"+rel)
			}
		}
	}
	sort.Strings(onDisk)
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(onDisk, keys) {
		t.Errorf("scripts in packs = %v, table = %v", onDisk, keys)
	}
}

// Red when a shipped levels.toml has a [calls] key or value that is no component of that
// package (a typo): such a handover would silently never be checked.
func TestCallsNameOnlyKnownComponents(t *testing.T) {
	for pkgName, p := range shipped(t) {
		known := named(p.Levels)
		for from, tos := range p.Levels.Calls {
			if !known[from] {
				t.Errorf("package %s: [calls] key %q is no known component", pkgName, from)
			}
			for _, to := range tos {
				if !known[to] {
					t.Errorf("package %s: [calls] %q hands over to %q, which is no known component", pkgName, from, to)
				}
			}
		}
	}
}
