package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
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

// summary gives kind:reason:reason_arg per action, without the note_done of an open Done run.
func summary(got []Action) []string {
	out := []string{}
	for _, a := range got {
		if a.Kind == KindNoteDone {
			// The done note of an open Done run is TestDecideNoteDoneForOpenDoneRun's.
			continue
		}
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
		{"parked in To do", func(in *Input) { item7(in).Status = statusToDo }, []string{}},
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

const mergeKey12 = "merge:12:abc123"

// pushedPR gives PR #12 the SHA of a pusher comment of its branch and turns the pusher on.
func pushedPR(in *Input) {
	pr12(in).PushedSHA = pushSHA
	in.Repos[0].WorkflowPush, in.Repos[0].PushTimeout = true, 30*time.Minute
}

// wantMergePush checks that got holds exactly one KindPush: the merge pusher of PR #12 on
// abc123, with no worktree and no permit.
func wantMergePush(t *testing.T, got []Action) {
	t.Helper()
	var pushes []Action
	for _, a := range got {
		if a.Kind == KindPush {
			pushes = append(pushes, a)
		}
	}
	if len(pushes) != 1 {
		t.Fatalf("pushes = %+v, want one merge pusher", pushes)
	}
	a := pushes[0]
	if a.Trigger != triggerMerge || a.TriggerKey != mergeKey12 || a.PR != 12 || a.HeadSHA != "abc123" ||
		a.Branch != "issue-7-bouw-de-keten" || a.Command != cmdPush || a.Worktree != "" || a.PermitSHA != "" ||
		a.Attempt != 1 || a.RunID != 0 {
		t.Errorf("push = %+v, want the merge pusher of PR 12 on abc123", a)
	}
}

// A pushed push row that owns a green PR with a pushed SHA hands the merge to the pusher: its
// row closes as a handoff to the merge key, without touching a session, and the merge pusher
// starts. No KindMerge with the run token.
func TestDecideMergeStartsPusherForPushRow(t *testing.T) {
	in := pushRow(t, pushSHA, 2*time.Hour)
	pr12(&in).PushedSHA = pushSHA
	got := Decide(in)
	if s := summary(got); !reflect.DeepEqual(s, []string{"close::" + mergeKey12, "push::"}) {
		t.Fatalf("summary = %q, want the close of run 5 and the merge pusher", s)
	}
	if c := got[0]; c.RunID != 5 || c.Outcome != runs.Handoff || c.Stop || c.End {
		t.Errorf("close = %+v, want run 5 closed as handoff without a session action", c)
	}
	wantMergePush(t, got)
}

// On the build route the write-pr run owns the PR; the pusher's handoff of its branch still sets
// PushedSHA, so that Done run's green PR goes to the merge pusher too, and its claude ends.
func TestDecideMergeStartsPusherForBuildRoutePR(t *testing.T) {
	in := loadInput(t, "settle-green-pr-done")
	row5(&in).Command, row5(&in).TriggerKey = "/lighthouse-write-pr 7", "handoff:64"
	pushedPR(&in)
	got := Decide(in)
	if s := summary(got); !reflect.DeepEqual(s, []string{"close::" + mergeKey12, "push::"}) {
		t.Fatalf("summary = %q, want the close of run 5 and the merge pusher", s)
	}
	if c := got[0]; c.RunID != 5 || c.Outcome != runs.Handoff || !c.End || c.SessionID != row5(&in).SessionID {
		t.Errorf("close = %+v, want run 5 closed as handoff, its claude ended", c)
	}
	wantMergePush(t, got)
}

// Without a pushed SHA the green PR of a session run merges as before, with the run token, also
// with the pusher on.
func TestDecideMergeUsesRunTokenWithoutPushedSHA(t *testing.T) {
	in := loadInput(t, "settle-green-pr-done")
	in.Repos[0].WorkflowPush = true
	got := Decide(in)
	if s := summary(got); !reflect.DeepEqual(s, []string{"merge::"}) {
		t.Fatalf("summary = %q, want the merge with the run token", s)
	}
	if got[0].PR != 12 || got[0].HeadSHA != "abc123" {
		t.Errorf("merge = %+v", got[0])
	}
}

// A push row that recorded its push but whose push note never reached the issue (the post failed)
// gives its PR no pushed SHA from a comment. The row's own PushedSHA still routes the merge to the
// merge pusher: the run token cannot merge a workflow change, and a push row has no session to
// merge for.
func TestDecideMergeByPusherForPushRowWithoutNote(t *testing.T) {
	in := pushRow(t, pushSHA, 2*time.Hour)
	got := Decide(in)
	if s := summary(got); !reflect.DeepEqual(s, []string{"close::" + mergeKey12, "push::"}) {
		t.Fatalf("summary = %q, want the close of run 5 and the merge pusher", s)
	}
	if c := got[0]; c.RunID != 5 || c.Outcome != runs.Handoff || c.Stop || c.End {
		t.Errorf("close = %+v, want run 5 closed as handoff without a session action", c)
	}
	wantMergePush(t, got)
}

// End to end from the comments: a pusher comment of branch A gives a green PR on branch B no
// pushed SHA, so B merges with the run token; the same comment for B's branch starts the pusher.
func TestDecideMergeIgnoresPushedSHAOfOtherBranch(t *testing.T) {
	for _, tc := range []struct {
		branch string
		want   []string
	}{
		{"issue-7-ander", []string{"merge::"}},
		{"issue-7-bouw-de-keten", []string{"close::" + mergeKey12, "push::"}},
	} {
		t.Run(tc.branch, func(t *testing.T) {
			in := loadInput(t, "settle-green-pr-done")
			in.Repos[0].WorkflowPush = true
			note := protocol.RenderPushNote(tc.branch, strings.Repeat("0", 40)+".."+pushSHA, ".github/workflows/ci.yml", "nl")
			srv, c := client(t)
			srv.Roles = map[string]string{"owner": "admin"}
			srv.Comments = map[int][]map[string]any{7: {comment(70, "owner", note)}}
			pre := Pre{Repo: testRepo, Items: []Item{{Issue: 7, Title: "Bouw de keten", State: "OPEN", ItemID: "I7",
				Status: "Controller", PRs: item7(&in).PRs}}, OpenRuns: []runs.OpenRun{{ID: 5, Issue: 7}}}
			snap, err := Complete(context.Background(), c, pre, NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, in.Now)
			if err != nil {
				t.Fatal(err)
			}
			item7(&in).PRs = snap.Items[0].PRs
			if s := summary(Decide(in)); !reflect.DeepEqual(s, tc.want) {
				t.Fatalf("summary = %q, want %q", s, tc.want)
			}
		})
	}
}

// The merge pusher waits like a push: a closed usage brake defers it and run 5 stays open (its
// close is dropped), a pause holds the merge back altogether. A merge key a run already spent
// (the pusher refused this head) hands the issue back once; without the pusher the issue goes back.
func TestDecideMergeByPusherGates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{"usage closed defers and keeps the run", func(in *Input) {
			in.Brake = openBrake()
			in.Brake.SessionPct = 90
		}, []string{"defer:usage: session 90%, week 10%:"}},
		{"paused merges nothing", func(in *Input) {
			in.Brake = openBrake()
			in.Brake.Paused = true
		}, []string{}},
		{"full slots do not hold it", func(in *Input) {
			in.Brake = openBrake()
			in.Brake.InUse = 4
		}, []string{"close::" + mergeKey12, "push::"}},
		{"spent merge key goes back", func(in *Input) {
			addRows(in, runs.Row{ID: 1, Issue: 7, Outcome: runs.Question, Command: cmdPush, TriggerKey: mergeKey12,
				Branch: "issue-7-bouw-de-keten", StartedAt: in.Now.Add(-3 * time.Hour)})
		}, []string{"return:reason_merge_failed:" + mergeKey12}},
		{"pusher off goes back", func(in *Input) { in.Repos[0].WorkflowPush = false },
			[]string{"return:reason_pusher_off:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := loadInput(t, "settle-green-pr-done")
			pushedPR(&in)
			tt.mutate(&in)
			if s := summary(Decide(in)); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
		})
	}
}

// mergeRow is the merge pusher's row of PR #12 on abc123, started at age ago.
func mergeRow(t *testing.T, age time.Duration) Input {
	t.Helper()
	in := pushRow(t, "", age)
	for i := range in.Repos[0].History {
		if r := &in.Repos[0].History[i]; r.ID == 5 {
			r.TriggerKey = mergeKey12
		}
	}
	pushedPR(&in)
	return in
}

// The merge pusher's own row is a push row without a pushed SHA: the green PR does not start a
// second merge, the timeout hands it back, and a PR merged meanwhile closes it as done, also when
// the pusher died before it closed its row.
func TestMergeRowOutcome(t *testing.T) {
	tests := []struct {
		name   string
		age    time.Duration
		mutate func(*Input)
		want   []string
	}{
		{"green PR waits for the pusher", 5 * time.Minute, func(*Input) {}, []string{}},
		{"merged closes done", 5 * time.Minute, func(in *Input) {
			pr12(in).State, pr12(in).MergedAt = "MERGED", in.Now.Add(-time.Minute)
		}, []string{"close::"}},
		{"silent at the timeout", 31 * time.Minute, func(*Input) {}, []string{"return:reason_pusher_silent:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := mergeRow(t, tt.age)
			tt.mutate(&in)
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
			for _, a := range got {
				if a.Kind == KindClose && a.Outcome != runs.Done {
					t.Errorf("close outcome = %q, want done", a.Outcome)
				}
			}
		})
	}
}

// A merge pusher that failed to start gets one more start on the same key, PR and head; after
// a second failure the issue goes back once with the cause.
func TestMergePusherRetry(t *testing.T) {
	failed := func(id int64, at time.Time) runs.Row { return failedPush(id, mergeKey12, at) }
	// Run 5 closed as a handoff to the merge key, as apply leaves it.
	closed := func(in *Input) runs.Row {
		r := *row5(in)
		r.Open, r.Outcome, r.Reason = false, runs.Handoff, mergeKey12
		return r
	}
	t.Run("one failure starts again", func(t *testing.T) {
		in := loadInput(t, "settle-green-pr-done")
		pushedPR(&in)
		in.Repos[0].History = []runs.Row{closed(&in), failed(6, in.Now.Add(-time.Minute))}
		got := Decide(in)
		if len(got) != 1 || got[0].Kind != KindPush || got[0].Attempt != 2 || got[0].Trigger != triggerMerge ||
			got[0].TriggerKey != mergeKey12 || got[0].PR != 12 || got[0].HeadSHA != "abc123" ||
			got[0].Branch != "issue-7-bouw-de-keten" || got[0].Worktree != "" {
			t.Fatalf("got %+v, want the second start of the merge pusher", got)
		}
	})
	t.Run("two failures go back", func(t *testing.T) {
		in := loadInput(t, "settle-green-pr-done")
		pushedPR(&in)
		in.Repos[0].History = []runs.Row{closed(&in), failed(6, in.Now.Add(-2*time.Minute)), failed(7, in.Now.Add(-time.Minute))}
		want := []string{"return:reason_start_failed:exec: lighthouse: not found"}
		if s := summary(Decide(in)); !reflect.DeepEqual(s, want) {
			t.Fatalf("summary = %q, want %q", s, want)
		}
	})
}
