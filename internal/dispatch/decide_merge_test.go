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

// openBrake is a brake with every gate open: usage measured and low, ample memory, free slots.
func openBrake() *Brake {
	mem := 32.0
	return &Brake{
		UsageMeasured: true, SessionPct: 10, WeekPct: 10, HaltPctSession: 85, HaltPctWeek: 90,
		MemAvailableGiB: &mem, MinMemAvailableGB: 8, ReservePerRunGB: 3, Slots: 4, MaxSoftware: 2,
	}
}

// A fix-pr start replaces run 5, which settle closes in the same tick and which InUse and
// SoftwareInUse already count, so it passes full slots and a full software limit (#334).
// Counted as a new run it defers, settleUnstartedFixPR keeps run 5 open, and with every slot
// held by such a run no run ever closes.
func TestFixPRStartPassesFullSlots(t *testing.T) {
	causes := []struct {
		name   string
		mutate func(*Input)
		start  string
	}{
		{"red", red, "start:reason_pr_red:"},
		{"conflict", conflict, "start:reason_pr_conflict:"},
		{"roborev", roborev, "start:reason_pr_roborev:"},
		{"merge failed", mergeNote, "start:reason_merge_failed:Pull Request is not mergeable"},
	}
	full := []struct {
		name string
		set  func(*Brake)
	}{
		{"slots", func(b *Brake) { b.InUse = 4 }},
		{"software", func(b *Brake) { b.InUse, b.SoftwareInUse = 2, 2 }},
	}
	for _, f := range full {
		for _, c := range causes {
			t.Run(f.name+"/"+c.name, func(t *testing.T) {
				in := loadInput(t, "settle-green-pr-done")
				c.mutate(&in)
				in.Repos[0].Controller.Profile = ProfileSoftware
				row5(&in).Profile = ProfileSoftware
				in.Brake = openBrake()
				f.set(in.Brake)
				if s, want := summary(Decide(in)), []string{fixClose12, c.start}; !reflect.DeepEqual(s, want) {
					t.Fatalf("summary = %q, want %q", s, want)
				}
			})
		}
	}
}

// The fix-pr start takes the slot of run 5 only where run 5 runs. On run 5's node it passes the
// full node; when the profile now places it on another node, run 5's slot frees nothing there,
// so the full node defers it. Making holdsSlot unconditional fails the second case.
func TestFixPRStartHoldsSlotOnlyOnItsNode(t *testing.T) {
	tests := []struct {
		name    string
		runNode string
		want    []string
	}{
		{"same node", "node-a", []string{fixClose12, "start:reason_pr_red:"}},
		{"other node", "", []string{"defer:slots: 1 of 1 in use on a:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := loadInput(t, "settle-green-pr-done")
			red(&in)
			in.Repos[0].Controller.Profile = "mac"
			row5(&in).Profile, row5(&in).Node = "mac", tt.runNode
			in.ProfileNodes = map[string]string{"mac": "node-a"}
			in.Brake = openBrake()
			in.Brake.Nodes = map[string]NodeSlots{"node-a": {Name: "a", Slots: 1, InUse: 1}}
			if s := summary(Decide(in)); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
		})
	}
}

// Run 5 is not a software run, so SoftwareInUse does not count it: a software fix-pr start in
// its place takes a new software slot, and the full software limit defers it. Dropping the
// profile condition of holdsSlot fails this test.
func TestFixPRStartCountsSoftwareWhenRunWasNot(t *testing.T) {
	in := loadInput(t, "settle-green-pr-done")
	red(&in)
	in.Repos[0].Controller.Profile = ProfileSoftware
	row5(&in).Profile = ""
	in.Brake = openBrake()
	in.Brake.InUse, in.Brake.SoftwareInUse = 3, 2
	if s, want := summary(Decide(in)), []string{"defer:software: 2 of 2 in use:"}; !reflect.DeepEqual(s, want) {
		t.Fatalf("summary = %q, want %q", s, want)
	}
}

// A fix-pr start the brake holds back in one tick starts in the tick the brake opens. Tick 1
// leaves run 5 untouched (only the defer, or nothing under a pause), so tick 2 reads the same
// open run and the same PR, and plans the close and the fix-pr start on the same key.
func TestFixPRStartsInTickAfterBrakeOpens(t *testing.T) {
	low := 4.0
	brakes := []struct {
		name   string
		closed func(*Brake)
		tick1  []string
	}{
		{"usage", func(b *Brake) { b.SessionPct = 90 }, []string{"defer:usage: session 90%, week 10%:"}},
		{"memory", func(b *Brake) { b.MemAvailableGiB = &low },
			[]string{"defer:memory: MemAvailable 4.0 GiB - 0 x 3 GiB < 8 GiB:"}},
		{"pause", func(b *Brake) { b.Paused = true }, []string{}},
	}
	causes := []struct {
		name   string
		mutate func(*Input)
		start  string
	}{
		{"red", red, "start:reason_pr_red:"},
		{"conflict", conflict, "start:reason_pr_conflict:"},
		{"roborev", roborev, "start:reason_pr_roborev:"},
		{"merge failed", mergeNote, "start:reason_merge_failed:Pull Request is not mergeable"},
	}
	for _, br := range brakes {
		for _, c := range causes {
			t.Run(br.name+"/"+c.name, func(t *testing.T) {
				in := loadInput(t, "settle-green-pr-done")
				c.mutate(&in)
				in.Brake = openBrake()
				br.closed(in.Brake)
				got := Decide(in)
				if s := summary(got); !reflect.DeepEqual(s, br.tick1) {
					t.Fatalf("tick 1 summary = %q, want %q", s, br.tick1)
				}
				for _, a := range got {
					if a.RunID == 5 {
						t.Fatalf("tick 1 touches run 5: %+v", a)
					}
				}

				in.Now = in.Now.Add(5 * time.Minute)
				in.Brake = openBrake()
				got = Decide(in)
				if s, want := summary(got), []string{fixClose12, c.start}; !reflect.DeepEqual(s, want) {
					t.Fatalf("tick 2 summary = %q, want %q", s, want)
				}
				start := got[1]
				if start.TriggerKey != "fixpr:12:abc123" || start.Command != "/lighthouse-fix-pr 12" ||
					start.Trigger != "fixpr" {
					t.Errorf("tick 2 start = %+v, want the fix-pr start of PR 12 on abc123", start)
				}
				if got[0].RunID != 5 {
					t.Errorf("tick 2 close = %+v, want run 5", got[0])
				}
			})
		}
	}
}
