package kpi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type classifyRow struct {
	Fn    string `json:"fn"`
	Input string `json:"input"`
	Want  any    `json:"want"`
	From  string `json:"from"`
}

// knownDivergence lists the rows where Go deliberately differs from Python (see the package
// comment): Go's \b is ASCII-only, so a pattern that ends on a non-ASCII digit does not match.
// The value is what Go answers; the row's own "want" is Python's answer.
var knownDivergence = map[string]any{
	"fix_pr\x00fix ١٢٣": false,
}

func TestClassifyParity(t *testing.T) {
	fixtures, err := filepath.Abs("../../tests/pack/fixtures/kpi")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(fixtures, "headings.toml"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", fixtures)
	data, err := os.ReadFile(filepath.Join(fixtures, "classify.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []classifyRow
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("classify.json has no rows")
	}
	hr, err := loadHeadingRes()
	if err != nil {
		t.Fatal(err)
	}
	bash := func(cmd string) map[string]any { return map[string]any{"command": cmd} }
	exempt := 0
	for _, r := range rows {
		var got any
		switch r.Fn {
		case "pickup":
			got = isPickup(r.Input)
		case "fix_pr":
			got = isFixRequest(r.Input)
		case "names_skill":
			got = namesASkill(r.Input)
		case "has_issue_number":
			got = hasIssueNumber(r.Input)
		case "mutating":
			got = isMutating("Bash", bash(r.Input))
		case "diagnostic":
			got = isDiagnostic("Bash", bash(r.Input))
		case "gate":
			got = nil
			if g, ok := hr.gate(r.Input); ok {
				got = float64(g)
			}
		case "subgate":
			got = nil
			if s, ok := hr.subgate(r.Input); ok {
				got = s
			}
		case "skip_reason":
			got = hr.skipReasonOK(r.Input)
		default:
			t.Errorf("unknown fn %q (from %s)", r.Fn, r.From)
			continue
		}
		want := r.Want
		if v, ok := knownDivergence[r.Fn+"\x00"+r.Input]; ok {
			if r.Want != true {
				t.Errorf("%s(%q): row says %v, a divergence must start from Python's true", r.Fn, r.Input, r.Want)
			}
			want = v
			exempt++
		}
		if got != want {
			t.Errorf("%s(%q) = %v, want %v (from %s)", r.Fn, r.Input, got, want, r.From)
		}
	}
	if exempt != len(knownDivergence) {
		t.Errorf("%d of %d known divergences found in classify.json", exempt, len(knownDivergence))
	}
}

func TestToolClassification(t *testing.T) {
	for _, name := range []string{"Write", "Edit", "NotebookEdit"} {
		if !isMutating(name, nil) {
			t.Errorf("%s must be mutating", name)
		}
		if isDiagnostic(name, nil) {
			t.Errorf("%s must not be diagnostic", name)
		}
	}
	if isMutating("Read", map[string]any{"command": "git add ."}) {
		t.Error("a non-Bash tool with a command field is not mutating")
	}
	if isMutating("Bash", map[string]any{"command": 5.0}) || isDiagnostic("Bash", map[string]any{}) {
		t.Error("a missing or non-string command counts as empty")
	}
	if isDiagnostic("Bash", map[string]any{"command": "git status && git add ."}) {
		t.Error("a mutating command is never diagnostic")
	}
}

func TestIsDiagnosticForgePR(t *testing.T) {
	cases := []struct {
		command    string
		diagnostic bool
		mutating   bool
	}{
		{"lighthouse forge pr view 12", true, false},
		{"lighthouse forge pr checks 12 --failed-log", true, false},
		{"lighthouse forge pr create --title x", false, false},
		{"lighthouse forge api -X POST projects/1/jobs/2/retry", false, true},
		{"lighthouse forge pr comment 12 --body x", false, true},
		{"lighthouse forge pr view 12 && lighthouse forge pr comment 12", false, true},
	}
	for _, c := range cases {
		in := map[string]any{"command": c.command}
		if got := isDiagnostic("Bash", in); got != c.diagnostic {
			t.Errorf("isDiagnostic(%q) = %v, want %v", c.command, got, c.diagnostic)
		}
		if got := isMutating("Bash", in); got != c.mutating {
			t.Errorf("isMutating(%q) = %v, want %v", c.command, got, c.mutating)
		}
	}
}
