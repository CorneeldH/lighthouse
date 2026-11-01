package triage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

func TestSchemaIsValidJSON(t *testing.T) {
	var s struct {
		Required             []string `json:"required"`
		AdditionalProperties bool     `json:"additionalProperties"`
	}
	if err := json.Unmarshal([]byte(Schema), &s); err != nil {
		t.Fatal(err)
	}
	want := []string{"route_line", "gate", "sub", "redundancy", "redundancy_issue",
		"redundancy_evidence", "headless", "headless_reason", "design_questions"}
	if !reflect.DeepEqual(s.Required, want) || s.AdditionalProperties {
		t.Errorf("required = %v additional = %v", s.Required, s.AdditionalProperties)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalog(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(repo, ".claude/skills/lighthouse-a/SKILL.md"),
		"---\nname: lighthouse-a\ndescription: 'It''s a skill — with quote'\n---\nbody\n")
	writeFile(t, filepath.Join(repo, ".claude/skills/lighthouse-b/SKILL.md"), "no frontmatter\n")
	userInstall := filepath.Join(root, "user")
	projInstall := filepath.Join(root, "proj")
	writeFile(t, filepath.Join(userInstall, "skills/stale/SKILL.md"), "---\nname: stale\ndescription: old\n---\n")
	writeFile(t, filepath.Join(projInstall, "skills/brainstorming/SKILL.md"),
		"---\nname: brainstorming\ndescription: \"Think first\"\n---\n")
	plugins := filepath.Join(root, "installed_plugins.json")
	writeFile(t, plugins, `{"version":2,"plugins":{
 "other@x":[{"scope":"user","installPath":"/nope"}],
 "superpowers@x":[
  {"scope":"user","installPath":"`+userInstall+`","version":"7.0.1"},
  {"scope":"project","projectPath":"`+repo+`","installPath":"`+projInstall+`","version":"7.0.1"}]}}`)

	got, warn, err := Catalog(repo, plugins)
	if err != nil || warn != "" {
		t.Fatalf("err = %v warn = %q", err, warn)
	}
	want := []Skill{
		{"lighthouse-a", "It's a skill — with quote"},
		{"superpowers:brainstorming", "Think first"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("project entry: got %v want %v", got, want)
	}

	// no project entry for this repo: the user entry is used
	other := filepath.Join(root, "other")
	writeFile(t, filepath.Join(other, ".claude/skills/x/SKILL.md"), "---\nname: x\ndescription: d\n---\n")
	got, _, _ = Catalog(other, plugins)
	want = []Skill{{"x", "d"}, {"superpowers:stale", "old"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("user entry: got %v want %v", got, want)
	}

	// no superpowers entry, and no file at all
	writeFile(t, plugins, `{"version":2,"plugins":{"other@x":[]}}`)
	for _, file := range []string{plugins, filepath.Join(root, "missing.json")} {
		got, warn, err = Catalog(repo, file)
		if err != nil || warn == "" {
			t.Errorf("%s: err = %v warn = %q, want a warning", file, err, warn)
		}
		if !reflect.DeepEqual(got, []Skill{{"lighthouse-a", "It's a skill — with quote"}}) {
			t.Errorf("%s: got %v, want only repo skills", file, got)
		}
	}
}

func facts7() Facts {
	return Facts{Number: 7, Title: "t", Labels: []string{"1-loc:container"}}
}

func ctxNL() Context {
	return Context{Language: "nl", Labels: repoconfig.Labels{
		BlockedIssue: "blocked-issue", BlockedExternal: "blocked-external",
		NeedsShaping: "needs-shaping", NeedsDecision: "needs-decision",
		NeedsHands: "needs-hands", NeedsTesting: "needs-testing",
	}}
}

func envelope(t *testing.T, out map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"is_error": false, "structured_output": out})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func verdict(over map[string]any) map[string]any {
	m := map[string]any{
		"route_line": "#7 Poort 4b: writing-plans — de AC leggen het HOE vast",
		"gate":       4, "sub": "b",
		"redundancy": "none", "redundancy_issue": 0, "redundancy_evidence": "",
		"headless": true, "headless_reason": "",
		"design_questions": []any{},
	}
	for k, v := range over {
		m[k] = v
	}
	return m
}

func questions(n, opts, rec int) []any {
	var qs []any
	for range n {
		o := make([]any, opts)
		for i := range o {
			o[i] = "opt"
		}
		qs = append(qs, map[string]any{"question": "q?", "options": o, "recommended": rec})
	}
	return qs
}

func TestValidateValid(t *testing.T) {
	tests := []struct {
		name     string
		over     map[string]any
		wantGate int
		wantSub  string
		wantQ    int
	}{
		{"4b", nil, 4, "b", 0},
		{"gate 1", map[string]any{
			"route_line": "#7 Poort 1: lighthouse-audit-worktrees — bekend recept",
			"gate":       1, "sub": ""}, 1, "", 0},
		{"4c two questions", map[string]any{
			"route_line": "#7 Poort 4c: superpowers:brainstorming — ontwerp open",
			"sub":        "c", "design_questions": append(questions(1, 2, 1), questions(1, 6, 5)...)}, 4, "c", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j, err := Validate(envelope(t, verdict(tc.over)), facts7(), ctxNL())
			if err != nil {
				t.Fatal(err)
			}
			if j.Gate != tc.wantGate || j.Sub != tc.wantSub || len(j.Questions) != tc.wantQ || !j.Headless {
				t.Errorf("got %+v", j)
			}
			if tc.wantQ == 2 && (j.Questions[1].Recommended != 5 || j.Questions[0].Text != "q?" || len(j.Questions[1].Options) != 6) {
				t.Errorf("questions = %+v", j.Questions)
			}
		})
	}
}

func TestValidateInvalid(t *testing.T) {
	needsDecision := facts7()
	needsDecision.Labels = append(needsDecision.Labels, "needs-decision")
	needsShaping := facts7()
	needsShaping.Labels = append(needsShaping.Labels, "needs-shaping")
	c4 := map[string]any{"route_line": "#7 Poort 4c: brainstorming — x", "sub": "c"}
	merge := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range c4 {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name    string
		f       Facts
		over    map[string]any
		wantErr string
	}{
		{"4 without sub", facts7(), map[string]any{"route_line": "#7 Poort 4: x — y", "sub": ""}, "sub-letter"},
		{"gate 3", facts7(), map[string]any{"route_line": "#7 Poort 3: x — y", "gate": 3, "sub": ""}, "gate 3"},
		{"gate field mismatch", facts7(), map[string]any{"gate": 2}, "field gate 2"},
		{"sub field mismatch", facts7(), map[string]any{"sub": "a"}, "field sub"},
		{"4b with needs-decision", needsDecision, nil, "needs-decision"},
		{"4b with needs-shaping", needsShaping, nil, "needs-shaping"},
		{"4c without questions", facts7(), c4, "4c needs 1 to 4"},
		{"4c five questions", facts7(), merge(map[string]any{"design_questions": questions(5, 2, 0)}), "4c needs 1 to 4"},
		{"4c one option", facts7(), merge(map[string]any{"design_questions": questions(1, 1, 0)}), "options"},
		{"4c seven options", facts7(), merge(map[string]any{"design_questions": questions(1, 7, 0)}), "options"},
		{"4c recommended out", facts7(), merge(map[string]any{"design_questions": questions(1, 3, 3)}), "recommended"},
		{"4c recommended negative", facts7(), merge(map[string]any{"design_questions": questions(1, 3, -1)}), "recommended"},
		{"questions at 4b", facts7(), map[string]any{"design_questions": questions(1, 2, 0)}, "only belong to 4c"},
		{"gate 0 without Go", facts7(), map[string]any{"route_line": "#7 Poort 0: stop — x", "gate": 0, "sub": ""}, "gate 0 where Go found no block"},
		{"duplicate without issue", facts7(), map[string]any{"redundancy": "duplicate"}, "without redundancy_issue"},
		{"duplicate of itself", facts7(), map[string]any{"redundancy": "duplicate", "redundancy_issue": 7}, "duplicate of #7 itself"},
		{"done without evidence", facts7(), map[string]any{"redundancy": "done"}, "without redundancy_evidence"},
		{"route over limit", facts7(), map[string]any{"route_line": "#7 Poort 4b: " + strings.Repeat("r", MaxRouteRunes+1) + " — x"}, "route"},
		{"reason over limit", facts7(), map[string]any{"route_line": "#7 Poort 4b: writing-plans — " + strings.Repeat("r", MaxRouteReasonRunes+1)}, "reason"},
		{"headless_reason over limit", facts7(), map[string]any{"headless": false, "headless_reason": strings.Repeat("h", MaxHeadlessReasonRunes+1)}, "headless_reason"},
		{"question over limit", facts7(), merge(map[string]any{"design_questions": []any{map[string]any{
			"question": strings.Repeat("q", MaxQuestionRunes+1), "options": []any{"a", "b"}, "recommended": 0}}}), "design_questions[0]"},
		{"option over limit", facts7(), merge(map[string]any{"design_questions": []any{map[string]any{
			"question": "q?", "options": []any{"a", strings.Repeat("o", MaxOptionRunes+1)}, "recommended": 0}}}), "options[1]"},
		{"evidence prose", factsSha(), done("fixed in abc1234"), "redundancy_evidence"},
		{"evidence unknown sha", factsSha(), done("fff0000"), "not in the git log"},
		{"evidence sha extended", factsSha(), done("abc1234ff"), "not in the git log"},
		{"evidence seven digit issue", factsSha(), done("#1234567"), "redundancy_evidence"},
		{"evidence six refs", factsSha(), done("#1, #2, #3, #4, #5, #6"), "redundancy_evidence"},
		{"evidence empty separator", factsSha(), done("#1 ,, ,#2"), "redundancy_evidence"},
		{"evidence over limit", factsSha(), done("#1" + strings.Repeat(" ", MaxEvidenceRunes) + "#2"), "redundancy_evidence"},
		{"headless false without reason", facts7(), map[string]any{"headless": false}, "without headless_reason"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Validate(envelope(t, verdict(tc.over)), tc.f, ctxNL())
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

const testSha = "abc1234def5678abc1234def5678abc1234def56"

func factsSha() Facts {
	f := facts7()
	f.GitShas = []string{testSha}
	return f
}

func done(evidence string) map[string]any {
	return map[string]any{"redundancy": "done", "redundancy_evidence": evidence}
}

func TestValidateLimitsValid(t *testing.T) {
	c4 := map[string]any{"route_line": "#7 Poort 4c: brainstorming — x", "sub": "c", "gate": 4}
	with := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range c4 {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	oneQuestion := func(q string, opts ...string) []any {
		o := make([]any, len(opts))
		for i, s := range opts {
			o[i] = s
		}
		return []any{map[string]any{"question": q, "options": o, "recommended": 0}}
	}
	tests := []struct {
		name string
		over map[string]any
	}{
		{"route at limit in runes", map[string]any{"route_line": "#7 Poort 4b: " + strings.Repeat("é", MaxRouteRunes) + " — x"}},
		{"reason at limit", map[string]any{"route_line": "#7 Poort 4b: writing-plans — " + strings.Repeat("r", MaxRouteReasonRunes)}},
		{"headless_reason at limit", map[string]any{"headless": false, "headless_reason": strings.Repeat("h", MaxHeadlessReasonRunes)}},
		{"question and option at limit", with(map[string]any{"design_questions": oneQuestion(strings.Repeat("q", MaxQuestionRunes), "a", strings.Repeat("o", MaxOptionRunes))})},
		{"done with evidence skips the design limits", with(map[string]any{"redundancy": "done", "redundancy_evidence": "#12",
			"design_questions": oneQuestion(strings.Repeat("q", 400), "a", "b")})},
		{"headless_reason over limit at gate 1", map[string]any{"route_line": "#7 Poort 1: x — y", "gate": 1, "sub": "",
			"headless": false, "headless_reason": strings.Repeat("h", 400)}},
		{"evidence issue and sha", done("#12, abc1234")},
		{"evidence upper case sha", done("ABC1234")},
		{"evidence space separated", done("#1 #2 #3 #4 #5")},
		{"empty evidence at duplicate", map[string]any{"redundancy": "duplicate", "redundancy_issue": 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Validate(envelope(t, verdict(tc.over)), factsSha(), ctxNL()); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestValidateEnvelope(t *testing.T) {
	for _, tc := range []struct{ name, env, want string }{
		{"is_error", `{"is_error":true,"structured_output":{}}`, "claude reported an error"},
		{"no output", `{"is_error":false,"result":"hi"}`, "no structured_output"},
		{"null output", `{"is_error":false,"structured_output":null}`, "no structured_output"},
		{"not json", `oops`, "envelope is not JSON"},
	} {
		_, err := Validate([]byte(tc.env), facts7(), ctxNL())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestValidateWrongNumber(t *testing.T) {
	_, err := Validate(envelope(t, verdict(map[string]any{"route_line": "#8 Poort 4b: writing-plans — x"})), facts7(), ctxNL())
	if err == nil || !strings.Contains(err.Error(), "#8") {
		t.Errorf("err = %v", err)
	}
}

func TestValidateRedundancyAndHeadless(t *testing.T) {
	j, err := Validate(envelope(t, verdict(map[string]any{
		"redundancy": "duplicate", "redundancy_issue": 3, "redundancy_evidence": "#3",
		"headless": false, "headless_reason": "needs sudo",
	})), facts7(), ctxNL())
	if err != nil {
		t.Fatal(err)
	}
	if j.Redundancy != "duplicate" || j.RedundancyIssue != 3 || j.RedundancyEvidence != "#3" ||
		j.Headless || j.HeadlessReason != "needs sudo" {
		t.Errorf("got %+v", j)
	}
}

func TestValidateGate0FromGo(t *testing.T) {
	c := ctxNL()
	c.Gate0 = &Gate0{Blockers: []int{5}}
	// the model says 4b, and even a malformed line: Go's stop wins
	for _, line := range []string{"#7 Poort 4b: writing-plans — x", "garbage"} {
		j, err := Validate(envelope(t, verdict(map[string]any{
			"route_line": line, "redundancy": "done", "redundancy_evidence": "abc1234",
			"headless": false, "headless_reason": "other host",
		})), factsSha(), c)
		if err != nil {
			t.Fatal(err)
		}
		if j.Gate != 0 || j.Sub != "" || j.RouteLine != "#7 Poort 0: stop — #5" {
			t.Errorf("gate 0: %+v", j)
		}
		if j.Redundancy != "done" || j.Headless {
			t.Errorf("model fields lost: %+v", j)
		}
	}
	c.Gate0 = &Gate0{Label: "blocked-external"}
	j, err := Validate(envelope(t, verdict(nil)), facts7(), c)
	if err != nil || j.RouteLine != "#7 Poort 0: stop — blocked-external" {
		t.Errorf("label: %+v %v", j, err)
	}
	c.Gate0 = &Gate0{Blockers: []int{5, 6}, Label: "3-blocked-external"}
	j, err = Validate(envelope(t, verdict(nil)), facts7(), c)
	if err != nil || j.RouteLine != "#7 Poort 0: stop — #5, #6, 3-blocked-external" {
		t.Errorf("both: %+v %v", j, err)
	}
	// the redundancy rules still hold under gate 0
	if _, err := Validate(envelope(t, verdict(map[string]any{"redundancy": "done"})), facts7(), c); err == nil {
		t.Error("done without evidence passed")
	}
}
