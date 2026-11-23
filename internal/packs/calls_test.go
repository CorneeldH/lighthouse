package packs

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/pack"
)

// skillRef is a hard reference to a lighthouse skill in a skill text: the slash command
// `/lighthouse-<x>` or the name in code, `lighthouse-<x>`.
var skillRef = regexp.MustCompile("[/`]lighthouse-([a-z][a-z-]*[a-z])")

// conditionalPhrase is what turns a reference to a skill that may be off into a soft one.
const conditionalPhrase = "if installed"

// loadAllPacks loads every package under pack.Files, core included.
func loadAllPacks(t *testing.T) []Pack {
	t.Helper()
	entries, err := fs.ReadDir(pack.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	var packs []Pack
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
			t.Fatal(err)
		}
		packs = append(packs, p)
	}
	return packs
}

// resolveEveryLevel resolves each level of the core order over all packs together.
func resolveEveryLevel(t *testing.T, packs []Pack) map[string]levels.Resolved {
	t.Helper()
	var files []levels.File
	var order []string
	for _, p := range packs {
		files = append(files, p.Levels)
		if p.Name == "core" {
			order = p.Levels.Order
		}
	}
	if len(order) == 0 {
		t.Fatal("core levels.toml has no order")
	}
	out := map[string]levels.Resolved{}
	for _, lvl := range order {
		r, err := levels.Resolve(files, lvl, nil)
		if err != nil {
			t.Fatal(err)
		}
		out[lvl] = r
	}
	return out
}

// skillTexts returns the markdown units of every skill with the component they belong to.
func skillTexts(t *testing.T, packs []Pack) []struct {
	Unit      Unit
	Component string
} {
	t.Helper()
	var out []struct {
		Unit      Unit
		Component string
	}
	for _, p := range packs {
		for _, u := range p.Units {
			if u.Kind != KindFile || !strings.HasSuffix(u.Path, ".md") {
				continue
			}
			name, base, err := Component(u, p.Levels)
			if err != nil {
				t.Fatal(err)
			}
			if base || !strings.HasPrefix(name, "skill:") {
				continue
			}
			out = append(out, struct {
				Unit      Unit
				Component string
			}{u, name})
		}
	}
	return out
}

func allCalls(packs []Pack) map[string][]string {
	calls := map[string][]string{}
	for _, p := range packs {
		for caller, callees := range p.Levels.Calls {
			calls[caller] = append(calls[caller], callees...)
		}
	}
	return calls
}

// Fails when a skill's [calls] in a levels.toml names a skill that a level table
// switches off where the caller is on, or when the level table of a callee is cut loose
// from the caller: the handover would point at nothing.
func TestSkillsOnlyCallSkillsThatAreOn(t *testing.T) {
	packs := loadAllPacks(t)
	for lvl, r := range resolveEveryLevel(t, packs) {
		for caller, callees := range allCalls(packs) {
			if r.Stand(caller) != levels.StandOn {
				continue
			}
			for _, callee := range callees {
				if r.Stand(callee) != levels.StandOn {
					t.Errorf("level %s: %s is on and calls %s, which is %s", lvl, caller, callee, r.Stand(callee))
				}
			}
		}
	}
}

// Fails when a skill text names /lighthouse-<x> on a line without the phrase "if installed",
// where <x> is off at a level in which the skill is on and the skill's [calls] does not list
// it: such a text sends the agent to a skill that is not there.
func TestHardReferencesToOffSkillsAreConditional(t *testing.T) {
	packs := loadAllPacks(t)
	calls := allCalls(packs)
	resolved := resolveEveryLevel(t, packs)
	for _, st := range skillTexts(t, packs) {
		for lvl, r := range resolved {
			if r.Stand(st.Component) != levels.StandOn {
				continue
			}
			for i, line := range bytes.Split(st.Unit.Content, []byte("\n")) {
				for _, m := range skillRef.FindAllSubmatch(line, -1) {
					target := "skill:" + string(m[1])
					if target == st.Component || r.Stand(target) != levels.StandOff {
						continue
					}
					if containsString(calls[st.Component], target) {
						continue
					}
					if strings.Contains(strings.ToLower(string(line)), conditionalPhrase) {
						continue
					}
					t.Errorf("%s:%d (level %s): lighthouse-%s is off, the line lacks %q", st.Unit.Path, i+1, lvl, m[1], conditionalPhrase)
				}
			}
		}
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
