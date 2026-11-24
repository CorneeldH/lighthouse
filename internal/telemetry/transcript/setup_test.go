package transcript

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func strVal(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// line builds a bare line with extra top-level JSON members spliced in.
func line(typ, at, extra string, content string) string {
	return fmt.Sprintf(`{"type":%q,%s"sessionId":"s1","timestamp":%q,"message":{"content":%s}}`, typ, extra, at, content)
}

func skillText(base, body, args string) string {
	t := "Base directory for this skill: " + base + "\n\n" + body
	if args != "" {
		t += "\n\nARGUMENTS: " + args
	}
	return fmt.Sprintf("[{\"type\":\"text\",\"text\":%q}]", t)
}

func command(name string) string {
	return fmt.Sprintf("%q", "<command-message>x</command-message>\n<command-name>/"+name+"</command-name>")
}

func TestSetupModelByOutputTokens(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		asst("m1", "", "model-a", "2026-01-01T10:00:00Z", use(1, 10, 0, 0), ""),
		asst("m2", "", "model-b", "2026-01-01T10:01:00Z", use(1, 50, 0, 0), ""),
		asst("m3", "", "model-a", "2026-01-01T10:02:00Z", use(1, 20, 0, 0), ""),
		asst("m4", "", "model-c", "2026-01-01T10:03:00Z", use(1, 500, 0, 0), `"isSidechain":true,`),
		asst("m5", "", "<synthetic>", "2026-01-01T10:04:00Z", use(1, 900, 0, 0), ""),
	))
	// model-a: 10+20 = 30; model-b: 50; the sidechain and synthetic lines do not count.
	if got := strVal(r.SetupBetween(time.Time{}, time.Time{}).Model); got != "model-b" {
		t.Fatalf("Model = %q, want model-b", got)
	}
}

func TestSetupEffortLast(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		asst("m1", "", "model-a", "2026-01-01T10:00:00Z", use(1, 1, 0, 0), `"effort":"low",`),
		asst("m2", "", "model-a", "2026-01-01T10:01:00Z", use(1, 1, 0, 0), `"effort":"high",`),
		asst("m3", "", "model-a", "2026-01-01T10:02:00Z", use(1, 1, 0, 0), ""),
		asst("m4", "", "model-a", "2026-01-01T10:03:00Z", use(1, 1, 0, 0), `"effort":"max","isSidechain":true,`),
	))
	s := r.SetupBetween(time.Time{}, time.Time{})
	if strVal(s.Effort) != "high" {
		t.Fatalf("Effort = %s", strVal(s.Effort))
	}
	w := r.SetupBetween(ts("2026-01-01T10:00:30Z"), time.Time{})
	if strVal(w.Effort) != "high" {
		t.Fatalf("window Effort = %s", strVal(w.Effort))
	}
	w = r.SetupBetween(time.Time{}, ts("2026-01-01T10:00:30Z"))
	if strVal(w.Effort) != "low" {
		t.Fatalf("early window Effort = %s", strVal(w.Effort))
	}
}

func TestSetupHarnessVersionLast(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", `"version":"1.0.0",`, `"hi"`),
		line("user", "2026-01-01T10:01:00Z", `"version":"1.0.1",`, `"hi"`),
	))
	s := r.SetupBetween(time.Time{}, time.Time{})
	if strVal(s.Harness) != "claude-code" || strVal(s.HarnessVersion) != "1.0.1" {
		t.Fatalf("harness = %s %s", strVal(s.Harness), strVal(s.HarnessVersion))
	}
}

func TestSetupEmptyIsNil(t *testing.T) {
	r := mustParse(t, f("s.jsonl", line("user", "2026-01-01T10:00:00Z", "", `"hi"`)))
	s := r.SetupBetween(ts("2027-01-01T00:00:00Z"), time.Time{}) // empty window
	for name, p := range map[string]*string{
		"Model": s.Model, "Effort": s.Effort, "HarnessVersion": s.HarnessVersion,
		"SuperpowersVersion": s.SuperpowersVersion, "SkillsHash": s.SkillsHash, "Lane": s.Lane,
		"Harness": s.Harness,
	} {
		if p != nil {
			t.Errorf("%s = %q, want nil", name, *p)
		}
	}
}

func TestSetupSuperpowersVersionFromBaseDirectory(t *testing.T) {
	one := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", command("superpowers:brainstorming")),
		line("user", "2026-01-01T10:00:01Z", "", skillText("/h/superpowers/7.0.2/skills/brainstorming", "body", "")),
	))
	if got := strVal(one.SetupBetween(time.Time{}, time.Time{}).SuperpowersVersion); got != "7.0.2" {
		t.Fatalf("version = %s", got)
	}
	two := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", command("superpowers:brainstorming")),
		line("user", "2026-01-01T10:00:01Z", "", skillText("/h/superpowers/7.0.1/skills/brainstorming", "body", "")),
		line("user", "2026-01-01T10:00:02Z", "", command("superpowers:writing-plans")),
		line("user", "2026-01-01T10:00:03Z", "", skillText("/h/superpowers/7.0.2/skills/writing-plans", "body", "")),
	))
	if got := strVal(two.SetupBetween(time.Time{}, time.Time{}).SuperpowersVersion); got != "7.0.2" {
		t.Fatalf("last version = %s", got)
	}
	none := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", command("lighthouse-write-pr")),
		line("user", "2026-01-01T10:00:01Z", "", skillText("/h/.claude/skills/lighthouse-write-pr", "body", "")),
	))
	if s := none.SetupBetween(time.Time{}, time.Time{}); s.SuperpowersVersion != nil {
		t.Fatalf("version = %s, want nil", strVal(s.SuperpowersVersion))
	}
}

func TestSetupLane(t *testing.T) {
	load := func(at, name string) []string {
		short := name[strings.Index(name, ":")+1:]
		return []string{
			line("user", at, "", command(name)),
			line("user", at, "", skillText("/h/superpowers/7.0.2/skills/"+short, "body "+short, "")),
		}
	}
	lane := func(lines ...string) string {
		return strVal(mustParse(t, f("s.jsonl", lines...)).SetupBetween(time.Time{}, time.Time{}).Lane)
	}
	sdd := load("2026-01-01T10:00:00Z", "superpowers:subagent-driven-development")
	nat := load("2026-01-01T10:01:00Z", "superpowers:executing-plans")
	if got := lane(sdd...); got != "sdd" {
		t.Errorf("sdd lane = %s", got)
	}
	if got := lane(nat...); got != "native" {
		t.Errorf("native lane = %s", got)
	}
	if got := lane(append(append([]string{}, sdd...), nat...)...); got != "native" {
		t.Errorf("last wins (native) = %s", got)
	}
	if got := lane(append(append([]string{}, nat...), load("2026-01-01T10:02:00Z", "superpowers:subagent-driven-development")...)...); got != "sdd" {
		t.Errorf("last wins (sdd) = %s", got)
	}
	if got := lane(load("2026-01-01T10:00:00Z", "superpowers:brainstorming")...); got != "<nil>" {
		t.Errorf("no lane skill = %s", got)
	}
}

func TestSetupLaneFromSkillToolUse(t *testing.T) {
	tu := `[{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:subagent-driven-development"}}]`
	r := mustParse(t, f("s.jsonl",
		line("assistant", "2026-01-01T10:00:00Z", "", tu),
		line("user", "2026-01-01T10:00:01Z", "", `[{"type":"tool_result","content":"Launching skill"}]`),
		line("user", "2026-01-01T10:00:02Z", "", skillText("/h/superpowers/7.0.2/skills/subagent-driven-development", "body", "")),
	))
	if got := strVal(r.SetupBetween(time.Time{}, time.Time{}).Lane); got != "sdd" {
		t.Fatalf("lane = %s", got)
	}
}

func hashOf(t *testing.T, lines ...string) string {
	t.Helper()
	s := mustParse(t, f("s.jsonl", lines...)).SetupBetween(time.Time{}, time.Time{})
	if s.SkillsHash == nil {
		t.Fatal("SkillsHash nil")
	}
	return *s.SkillsHash
}

func TestSkillsHashIgnoresWorktreeAndArguments(t *testing.T) {
	mk := func(base, body, args string) []string {
		return []string{
			line("user", "2026-01-01T10:00:00Z", "", command("lighthouse-write-pr")),
			line("user", "2026-01-01T10:00:01Z", "", skillText(base, body, args)),
		}
	}
	a := hashOf(t, mk("/wt/one/.claude/skills/x", "same text", "123")...)
	b := hashOf(t, mk("/wt/two/.claude/skills/x", "same text", "456 other")...)
	c := hashOf(t, mk("/wt/one/.claude/skills/x", "changed text", "123")...)
	if a != b {
		t.Errorf("hash differs for other path and arguments: %s vs %s", a, b)
	}
	if a == c {
		t.Error("hash equal for changed skill text")
	}
}

func TestSetupInheritsSkillsFromEarlierWindow(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", command("superpowers:executing-plans")),
		line("user", "2026-01-01T10:00:01Z", "", skillText("/h/superpowers/7.0.2/skills/executing-plans", "body", "")),
		asst("m1", "", "model-a", "2026-01-01T11:00:00Z", use(1, 5, 0, 0), ""),
	))
	s := r.SetupBetween(ts("2026-01-01T10:30:00Z"), time.Time{})
	if strVal(s.Lane) != "native" || s.SkillsHash == nil || strVal(s.SuperpowersVersion) != "7.0.2" {
		t.Fatalf("window did not inherit: lane=%s hash=%v sp=%s", strVal(s.Lane), s.SkillsHash, strVal(s.SuperpowersVersion))
	}
	// A skill loaded after the window end is not part of it.
	early := r.SetupBetween(time.Time{}, ts("2026-01-01T10:00:00Z"))
	if early.Lane != nil || early.SkillsHash != nil {
		t.Fatalf("skills leaked from after the window: %v", early)
	}
}

func TestSetupSkillLoadFixture(t *testing.T) {
	fh, err := os.Open("testdata/skill-load.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fh.Close() })
	s := mustParse(t, File{Name: "sess-skill.jsonl", R: fh}).SetupBetween(time.Time{}, time.Time{})
	if strVal(s.Lane) != "native" || strVal(s.SuperpowersVersion) != "7.0.2" || s.SkillsHash == nil ||
		strVal(s.Effort) != "medium" || strVal(s.HarnessVersion) != "2.1.318" || strVal(s.Model) != "model-a" {
		t.Fatalf("setup = lane %s sp %s effort %s ver %s model %s", strVal(s.Lane), strVal(s.SuperpowersVersion),
			strVal(s.Effort), strVal(s.HarnessVersion), strVal(s.Model))
	}
}

func TestSetupStaleCommandDoesNotStealSkillName(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", command("clear")),
		line("user", "2026-01-01T10:00:01Z", "", command("superpowers:executing-plans")),
		line("user", "2026-01-01T10:00:02Z", "", skillText("/h/superpowers/7.0.2/skills/executing-plans", "body", "")),
	))
	if got := strVal(r.SetupBetween(time.Time{}, time.Time{}).Lane); got != "native" {
		t.Fatalf("lane = %s, want native", got)
	}
}

func TestSetupLaneFromBaseDirectoryFallback(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", skillText("/h/superpowers/7.0.2/skills/subagent-driven-development", "body", "")),
	))
	if got := strVal(r.SetupBetween(time.Time{}, time.Time{}).Lane); got != "sdd" {
		t.Fatalf("lane = %s, want sdd", got)
	}
	other := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", skillText("/h/.claude/skills/executing-plans", "body", "")),
	))
	if got := strVal(other.SetupBetween(time.Time{}, time.Time{}).Lane); got != "<nil>" {
		t.Fatalf("non-superpowers skill gave lane %s", got)
	}
}

func TestSetupWindowBoundaries(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		asst("m1", "", "model-a", "2026-01-01T10:00:00Z", use(1, 5, 0, 0), `"effort":"low",`),
		asst("m2", "", "model-b", "2026-01-01T11:00:00Z", use(1, 9, 0, 0), `"effort":"high",`),
	))
	s := r.SetupBetween(ts("2026-01-01T10:00:00Z"), ts("2026-01-01T11:00:00Z"))
	if strVal(s.Model) != "model-a" || strVal(s.Effort) != "low" {
		t.Fatalf("from inclusive / to exclusive violated: %s %s", strVal(s.Model), strVal(s.Effort))
	}
}
