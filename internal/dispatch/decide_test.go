package dispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// fixture is one file of testdata/decide: an input and the actions Decide must return.
type fixture struct {
	Input Input    `json:"input"`
	Want  []Action `json:"want"`
}

// decideFixtures reads every file of testdata/decide, by name without .json.
func decideFixtures(t *testing.T) map[string]fixture {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "decide", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixtures in testdata/decide")
	}
	out := make(map[string]fixture, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f fixture
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if f.Want == nil {
			t.Fatalf("%s has no want", path)
		}
		out[strings.TrimSuffix(filepath.Base(path), ".json")] = f
	}
	return out
}

func TestDecideFixtures(t *testing.T) {
	for name, f := range decideFixtures(t) {
		t.Run(name, func(t *testing.T) {
			got := Decide(f.Input)
			if !reflect.DeepEqual(got, f.Want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(f.Want, "", "  ")
				t.Errorf("Decide:\ngot  %s\nwant %s", gotJSON, wantJSON)
			}
		})
	}
}

// Apply posts Reason and Skip through protocol.Heading, which panics on an unknown key. Every
// key a fixture expects must be in the headings table, in nl and en. A defer carries the
// literal night, which apply never looks up.
func TestDecideKeysAreHeadings(t *testing.T) {
	keys := map[string]bool{}
	for _, f := range decideFixtures(t) {
		for _, a := range f.Want {
			if a.Reason != "" && a.Kind != KindDefer {
				keys[a.Reason] = true
			}
			if a.Skip != "" {
				keys[a.Skip] = true
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no Reason or Skip in any fixture")
	}
	for key := range keys {
		for _, language := range []string{"nl", "en"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Heading(%q, %q) panics: %v", key, language, r)
					}
				}()
				if protocol.Heading(key, language) == "" {
					t.Errorf("Heading(%q, %q) is empty", key, language)
				}
			}()
		}
	}
}

// A handoff branch reaches git as an argument: only a plain ref name passes.
func TestValidBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"issue-7-bouw-de-keten", true},
		{"feature/a.b_c-1", true},
		{"", false},
		{"-x", false},
		{"--upload-pack=x", false},
		{"a..b", false},
		{"a b", false},
		{"a;b", false},
		{"$(x)", false},
		{"a`b`", false},
		{"a\nb", false},
	}
	for _, tt := range tests {
		if got := validBranch(tt.branch); got != tt.want {
			t.Errorf("validBranch(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

// fixPRRuns counts the fix-pr runs of one PR towards the ceiling: an open run counts, a failed
// start does not, nor a run of another PR or another issue.
func TestFixPRRuns(t *testing.T) {
	fix := func(issue int, command string, open bool, o runs.Outcome) runs.Row {
		return runs.Row{Issue: issue, Command: command, Open: open, Outcome: o}
	}
	tests := []struct {
		name    string
		history []runs.Row
		want    int
	}{
		{"none", nil, 0},
		{"closed handoff and done", []runs.Row{
			fix(7, "/lighthouse-fix-pr 12", false, runs.Handoff), fix(7, "/lighthouse-fix-pr 12", false, runs.Done),
		}, 2},
		{"open row counts", []runs.Row{
			fix(7, "/lighthouse-fix-pr 12", false, runs.Returned), fix(7, "/lighthouse-fix-pr 12", true, ""),
		}, 2},
		{"start_failed does not count", []runs.Row{
			fix(7, "/lighthouse-fix-pr 12", false, runs.StartFailed), fix(7, "/lighthouse-fix-pr 12", false, runs.Done),
		}, 1},
		{"other PR does not count", []runs.Row{
			fix(7, "/lighthouse-fix-pr 13", false, runs.Done), fix(7, "/lighthouse-fix-pr 120", false, runs.Done),
		}, 0},
		{"other issue does not count", []runs.Row{fix(8, "/lighthouse-fix-pr 12", false, runs.Done)}, 0},
		{"other command does not count", []runs.Row{fix(7, "/lighthouse-start-issue 7", false, runs.Done)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixPRRuns(tt.history, 7, 12); got != tt.want {
				t.Errorf("fixPRRuns = %d, want %d", got, tt.want)
			}
		})
	}
}

// Only a fix-pr start of the same repo and issue keeps a fix-pr close a handoff; any other
// close passes untouched.
func TestReturnUnstartedFixPR(t *testing.T) {
	const session = "11111111-2222-4333-8444-555555555555"
	fixClose := Action{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff,
		SessionID: session, ReasonArg: "fixpr:12:abc123", Language: "nl"}
	returned := Action{Kind: KindReturn, Repo: "o/r", Issue: 7, RunID: 5, SessionID: session,
		Reason: "reason_pr_red", Language: "nl"}
	fixStart := Action{Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: "fixpr", TriggerKey: "fixpr:12:abc123"}
	tests := []struct {
		name string
		in   []Action
		want []Action
	}{
		{"own fix-pr start keeps the close", []Action{fixClose, fixStart}, []Action{fixClose, fixStart}},
		{"no start returns", []Action{fixClose}, []Action{returned}},
		{"other trigger returns",
			[]Action{fixClose, {Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: "handoff", TriggerKey: "handoff:50"}},
			[]Action{returned, {Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: "handoff", TriggerKey: "handoff:50"}}},
		{"fix-pr start of another issue returns",
			[]Action{fixClose, {Kind: KindStart, Repo: "o/r", Issue: 8, Trigger: "fixpr"}},
			[]Action{returned, {Kind: KindStart, Repo: "o/r", Issue: 8, Trigger: "fixpr"}}},
		{"fix-pr start of another repo returns",
			[]Action{fixClose, {Kind: KindStart, Repo: "o/s", Issue: 7, Trigger: "fixpr"}},
			[]Action{returned, {Kind: KindStart, Repo: "o/s", Issue: 7, Trigger: "fixpr"}}},
		{"plain handoff close passes",
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff}},
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff}}},
		{"start_failed close with a fixpr arg passes",
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.StartFailed, ReasonArg: "fixpr:x"}},
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.StartFailed, ReasonArg: "fixpr:x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps []planned
			for _, a := range tt.in {
				ps = append(ps, planned{Action: a})
			}
			var got []Action
			for _, p := range returnUnstartedFixPR(ps) {
				got = append(got, p.Action)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
