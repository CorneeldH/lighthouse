package dispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// loadInput decodes the input of testdata/decide/<name>.json afresh on every call, so a case
// can mutate it freely.
func loadInput(t *testing.T, name string) Input {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "decide", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f fixture
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return f.Input
}

// summary gives kind:reason:reason_arg per action.
func summary(got []Action) []string {
	out := []string{}
	for _, a := range got {
		out = append(out, string(a.Kind)+":"+a.Reason+":"+a.ReasonArg)
	}
	return out
}

// The base of the merge tests, settle-green-pr-done: run 5 done (stand 2), its own PR #12 green
// on abc123, the item in Controller, no brake.
func pr12(in *Input) *forge.PR     { return &in.Repos[0].Snapshot.Items[0].PRs[0] }
func row5(in *Input) *runs.Row     { return &in.Repos[0].History[0] }
func item7(in *Input) *Item        { return &in.Repos[0].Snapshot.Items[0] }
func addPR(in *Input, pr forge.PR) { item7(in).PRs = append(item7(in).PRs, pr) }
func addRows(in *Input, r ...runs.Row) {
	in.Repos[0].History = append(r, in.Repos[0].History...)
}

// fixRows are closed fix-pr runs of PR #12 on issue 7 from before run 5, one per key.
func fixRows(keys ...string) []runs.Row {
	var out []runs.Row
	for i, key := range keys {
		out = append(out, runs.Row{
			ID: int64(i + 1), Issue: 7, Outcome: runs.Handoff, Command: "/lighthouse-fix-pr 12", TriggerKey: key,
			Branch: "issue-7-bouw-de-keten", StartedAt: time.Date(2026, 10, 31, 9, i, 0, 0, time.UTC),
		})
	}
	return out
}

var (
	red       = func(in *Input) { pr12(in).Checks = "FAILURE" }
	conflict  = func(in *Input) { pr12(in).Mergeable = "CONFLICTING" }
	roborev   = func(in *Input) { pr12(in).Roborev = forge.RoborevOpen }
	mergeNote = func(in *Input) {
		row5(in).MergeSHA, row5(in).MergeError = "abc123", "Pull Request is not mergeable"
	}
	ceiling = func(in *Input) { addRows(in, fixRows("fixpr:12:s1", "fixpr:12:s2", "fixpr:12:s3")...) }
	spent   = func(in *Input) { addRows(in, fixRows("fixpr:12:abc123")...) }
	blocker = func(in *Input) {
		item7(in).BlockedBy = []forge.Blocker{{Repo: "o/r", Issue: 3, State: "OPEN"}}
	}
)

func all(ms ...func(*Input)) func(*Input) {
	return func(in *Input) {
		for _, m := range ms {
			m(in)
		}
	}
}

// checkCases runs Decide on the mutated base and compares the summary; a start must be the
// fix-pr start of PR #12 on abc123.
func checkCases(t *testing.T, tests []struct {
	name   string
	mutate func(*Input)
	want   []string
}) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := loadInput(t, "settle-green-pr-done")
			tt.mutate(&in)
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
			for _, a := range got {
				if a.Kind != KindStart {
					continue
				}
				if a.TriggerKey != "fixpr:12:abc123" || a.Command != "/lighthouse-fix-pr 12" ||
					a.Branch != "issue-7-bouw-de-keten" || a.Trigger != "fixpr" {
					t.Errorf("start = %+v, want the fix-pr start of PR 12 on abc123", a)
				}
			}
		})
	}
}

const fixClose12 = "close::fixpr:12:abc123"

// Every cause of a fix-pr start on the own PR of a Done run, the first cause winning, and the
// gates that hand the issue back with the cause's own reason.
func TestFixPRCauses(t *testing.T) {
	checkCases(t, []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{"red", red, []string{fixClose12, "start:reason_pr_red:"}},
		{"conflict", conflict, []string{fixClose12, "start:reason_pr_conflict:"}},
		{"merge failed", mergeNote,
			[]string{fixClose12, "start:reason_merge_failed:Pull Request is not mergeable"}},
		{"roborev", roborev, []string{fixClose12, "start:reason_pr_roborev:"}},
		{"red wins over conflict", all(red, conflict), []string{fixClose12, "start:reason_pr_red:"}},
		{"unknown", func(in *Input) { pr12(in).Mergeable = "UNKNOWN" }, []string{}},
		{"conflict at ceiling", all(conflict, ceiling), []string{"return:reason_pr_conflict_ceiling:"}},
		{"merge on spent sha", all(mergeNote, spent),
			[]string{"return:reason_merge_failed:Pull Request is not mergeable"}},
		{"roborev waits for pending checks", all(roborev, func(in *Input) { pr12(in).Checks = "PENDING" }), []string{}},
		{"roborev at ceiling", all(roborev, ceiling), []string{"return:reason_pr_roborev_ceiling:"}},
		{"roborev on spent sha", all(roborev, spent), []string{"return:reason_pr_roborev:"}},
		{"conflict with blocker", all(conflict, blocker), []string{"return:reason_pr_conflict:"}},
	})
}

// A Done run with a green own PR merges it, unless something holds it back; every hold either
// leaves the run open or hands it to the fix-pr or done rule.
func TestMergeReady(t *testing.T) {
	merge := []string{"merge::"}
	checkCases(t, []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{"merges", func(*Input) {}, merge},
		{"not done", func(in *Input) { in.Repos[0].Stands[5] = workplace.Working }, []string{}},
		{"draft", func(in *Input) { pr12(in).Draft = true }, []string{"return:reason_pr_draft:"}},
		{"conflict", conflict, []string{fixClose12, "start:reason_pr_conflict:"}},
		{"unknown", func(in *Input) { pr12(in).Mergeable = "UNKNOWN" }, []string{}},
		{"checks pending", func(in *Input) { pr12(in).Checks = "PENDING" }, []string{}},
		{"roborev open", roborev, []string{fixClose12, "start:reason_pr_roborev:"}},
		{"roborev processed", func(in *Input) { pr12(in).Roborev = forge.RoborevProcessed }, merge},
		{"note on this head", mergeNote,
			[]string{fixClose12, "start:reason_merge_failed:Pull Request is not mergeable"}},
		{"note on an old head", func(in *Input) { row5(in).MergeSHA = "old1234" }, merge},
		{"other branch", func(in *Input) { pr12(in).HeadRef = "other-branch" }, []string{"done::"}},
		{"fork", func(in *Input) { pr12(in).CrossRepo = true }, []string{"done::"}},
		{"default branch", func(in *Input) { pr12(in).HeadRef, row5(in).Branch = "main", "main" },
			[]string{"done::"}},
		{"paused", func(in *Input) { in.Brake = &Brake{Paused: true} }, []string{}},
		{"not in Controller", func(in *Input) { item7(in).Status = "On hold" }, []string{}},
		{"active blocker", blocker, []string{}},
		{"blocker with a merged PR", func(in *Input) {
			item7(in).BlockedBy = []forge.Blocker{{Repo: "o/r", Issue: 3, State: "OPEN", MergedPR: true}}
		}, merge},
		{"two own PRs, first conflicting", func(in *Input) {
			conflict(in)
			pr := *pr12(in)
			pr.Number, pr.HeadSHA, pr.Mergeable, pr.URL = 14, "def456", "MERGEABLE", "https://github.com/o/r/pull/14"
			addPR(in, pr)
		}, []string{fixClose12, "start:reason_pr_conflict:"}},
		{"foreign green next to own conflict", func(in *Input) {
			conflict(in)
			pr := *pr12(in)
			pr.Number, pr.HeadRef, pr.Mergeable, pr.URL = 13, "other", "MERGEABLE", "https://github.com/o/r/pull/13"
			addPR(in, pr)
		}, []string{fixClose12, "start:reason_pr_conflict:"}},
		{"foreign conflict next to own green, paused", func(in *Input) {
			pr := *pr12(in)
			pr.Number, pr.HeadRef, pr.Mergeable, pr.URL = 13, "other", "CONFLICTING", "https://github.com/o/r/pull/13"
			addPR(in, pr)
			in.Brake = &Brake{Paused: true}
		}, []string{"return:reason_pr_conflict:"}},
	})
}

// The merge action carries what apply needs: the run, its session, the PR and its measured head.
func TestMergeReadyAction(t *testing.T) {
	got := Decide(loadInput(t, "settle-green-pr-done"))
	want := []Action{{
		Kind: KindMerge, Repo: "o/r", Issue: 7, RunID: 5, SessionID: "11111111-2222-4333-8444-555555555555",
		PR: 12, HeadSHA: "abc123", PRURL: "https://github.com/o/r/pull/12", Language: "nl", End: true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}
