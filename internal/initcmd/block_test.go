package initcmd

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/agentsmd"
	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/pack"
)

func blockUnit(pkg, body string) packs.Unit {
	return packs.Unit{Kind: packs.KindBlock, Package: pkg, Path: agentsMDPath, ID: pkg, Content: []byte(body)}
}

func standing(c map[string]levels.Stand) levels.Resolved {
	return levels.Resolved{Components: c}
}

const markedCore = "## core\n\n" +
	"- Test gate: `commit-test-gate` runs it. <!--level:commit-test-gate-->\n" +
	"- Always here.\n\n" +
	"Skills:\n\n" +
	"- `/lighthouse-fix-pr`: fix. <!--level:skill:fix-pr-->\n" +
	"- `/lighthouse-forge`: forge. <!--level:skill:forge-->\n"

// Red if filterBlock stops dropping a marked line whose component is off, or drops an
// unmarked line.
func TestBlockOnLightLacksOffSkillsAndTestGate(t *testing.T) {
	r := standing(map[string]levels.Stand{"commit-test-gate": levels.StandOff, "skill:fix-pr": levels.StandOff})
	got := string(filterBlock(blockUnit("core", markedCore), r).Content)
	for _, gone := range []string{"commit-test-gate", "fix-pr"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s stays at light:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"- Always here.", "- `/lighthouse-forge`: forge.", "Skills:"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%q dropped:\n%s", kept, got)
		}
	}
}

// Red if filterBlock leaves a marker on a line that stays, whatever the stand, or leaves
// trailing whitespace where the marker was.
func TestBlockLeavesNoMarkerBehind(t *testing.T) {
	for _, r := range []levels.Resolved{
		standing(nil),
		standing(map[string]levels.Stand{"skill:fix-pr": levels.StandOff}),
		standing(map[string]levels.Stand{"skill:forge": levels.StandLog}),
	} {
		got := string(filterBlock(blockUnit("core", markedCore), r).Content)
		if strings.Contains(got, "<!--level:") {
			t.Errorf("marker left behind:\n%s", got)
		}
		for _, line := range strings.Split(got, "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Errorf("trailing space in %q", line)
			}
		}
	}
}

// Red if a line with two markers drops while one of its components is still on, or stays
// when both are off.
func TestBlockLineWithTwoMarkersStaysWhileOneIsOn(t *testing.T) {
	body := "## go\n\n- Two hooks. <!--level:gofmt-check--> <!--level:go-lint-gate-->\n"
	one := standing(map[string]levels.Stand{"gofmt-check": levels.StandOff})
	if got := string(filterBlock(blockUnit("go", body), one).Content); got != "## go\n\n- Two hooks.\n" {
		t.Errorf("one on: %q", got)
	}
	both := standing(map[string]levels.Stand{"gofmt-check": levels.StandOff, "go-lint-gate": levels.StandOff})
	if got := string(filterBlock(blockUnit("go", body), both).Content); got != "## go\n\n" {
		t.Errorf("both off: %q", got)
	}
}

// Red if the warning line changes wording, is left out for a disabled core gate, is added
// for a gate that is on, or lands in another block than core.
func TestBlockWarnsPerDisabledCoreGate(t *testing.T) {
	r := standing(map[string]levels.Stand{"main-commit-gate": levels.StandOff, "pr-create-gate": levels.StandLog})
	got := string(filterBlock(blockUnit("core", "## core\n\n- rule\n"), r).Content)
	want := "## core\n\n- rule\n" +
		"- Warning: main-commit-gate is off in this repo (override in [components]): it works outside the chain.\n" +
		"- Warning: pr-create-gate is log in this repo (override in [components]): it works outside the chain.\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "worktree-check") {
		t.Error("warning for a gate that is on")
	}
	if other := string(filterBlock(blockUnit("go", "## go\n"), r).Content); other != "## go\n" {
		t.Errorf("warning in the go block: %q", other)
	}
}

// Red if filterBlock touches a unit that is not a block.
func TestFilterBlockLeavesOtherKindsAlone(t *testing.T) {
	u := packs.Unit{Kind: packs.KindFile, Package: "core", Path: "x", Content: []byte("a <!--level:skill:fix-pr-->\n")}
	if got := filterBlock(u, standing(map[string]levels.Stand{"skill:fix-pr": levels.StandOff})); string(got.Content) != string(u.Content) {
		t.Errorf("file changed: %q", got.Content)
	}
}

var markerRe = regexp.MustCompile(` ?<!--level:[^ >]+-->`)

// Red if a pack block gets a marker that filterBlock does not strip at strict, or if the
// installed AGENTS.md of this repo (the dogfood copy) drifts from the pack source: strict
// must equal today's block.
func TestBlockOnStrictEqualsTodaysBlock(t *testing.T) {
	installed, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"core", "go"} {
		src, err := fs.ReadFile(pack.Files, name+"/instructions/AGENTS.md")
		if err != nil {
			t.Fatal(err)
		}
		got := string(filterBlock(blockUnit(name, string(src)), standing(nil)).Content)
		if want := markerRe.ReplaceAllString(string(src), ""); got != want {
			t.Errorf("%s: strict block differs from the source without markers:\n%s", name, got)
		}
		begin := strings.Index(string(installed), agentsmd.Begin(name))
		end := strings.Index(string(installed), agentsmd.End(name))
		if begin < 0 || end < 0 {
			t.Fatalf("no %s block in AGENTS.md", name)
		}
		block := strings.TrimSpace(string(installed)[begin+len(agentsmd.Begin(name)) : end])
		if block != strings.TrimSpace(got) {
			t.Errorf("%s: installed block differs from strict output", name)
		}
	}
}

// Red if a skill or gate line of a pack block is not marked, or marks a component that no
// levels.toml names (the marker would never drop its line).
func TestPackBlockMarkersNameKnownComponents(t *testing.T) {
	all, err := loadAll(pack.Files, "dev")
	if err != nil {
		t.Fatal(err)
	}
	var chosen []packs.Pack
	for _, p := range all {
		chosen = append(chosen, p)
	}
	files := levelFiles(chosen)
	r, err := levels.Resolve(fileList(files), "strict", nil)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, n := range r.Names() {
		known[n] = true
	}
	for _, f := range files {
		for _, n := range f.On {
			known[n] = true
		}
	}
	for _, p := range chosen {
		for _, u := range p.Units {
			if u.Kind != packs.KindBlock {
				continue
			}
			for _, m := range regexp.MustCompile(`<!--level:([^ >]+)-->`).FindAllStringSubmatch(string(u.Content), -1) {
				if !known[m[1]] {
					t.Errorf("%s block marks unknown component %q", p.Name, m[1])
				}
			}
		}
	}
	core := string(blockContent(t, all["core"]))
	for _, skill := range []string{"fix-pr", "start-issue", "upgrade"} {
		if !strings.Contains(core, "<!--level:skill:"+skill+"-->") {
			t.Errorf("core block has no marker for skill %s", skill)
		}
	}
	if !strings.Contains(core, "<!--level:commit-test-gate-->") {
		t.Error("core block has no marker for commit-test-gate")
	}
}

func blockContent(t *testing.T, p packs.Pack) []byte {
	t.Helper()
	for _, u := range p.Units {
		if u.Kind == packs.KindBlock {
			return u.Content
		}
	}
	t.Fatalf("no block in %s", p.Name)
	return nil
}

// Red if the `--no-verify` rule of pack/core's block is marked (or merged into the marked
// commit-test-gate line) again: at light, where commit-test-gate is off, Codex and OpenCode
// would lose the one rule that no hook protects there.
func TestBlockOnLightKeepsNoVerifyRule(t *testing.T) {
	body, err := fs.ReadFile(pack.Files, "core/instructions/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	r := standing(map[string]levels.Stand{"commit-test-gate": levels.StandOff})
	got := string(filterBlock(blockUnit("core", string(body)), r).Content)
	if strings.Contains(got, "`commit-test-gate`") {
		t.Fatalf("fixture: commit-test-gate line stays at light:\n%s", got)
	}
	if !strings.Contains(got, "`--no-verify`") {
		t.Errorf("the --no-verify rule is gone from the core block at light:\n%s", got)
	}
}
