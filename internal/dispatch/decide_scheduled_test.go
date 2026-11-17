package dispatch

import (
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: pure logic. These tests fail if Decide stops turning a ScheduledDue into one
// scheduled start, orders it after issue starts, counts it wrong in the brake (slot, software,
// node), loses its schedule on a defer, or picks the wrong attempt and branch.

var scheduledNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

const scheduledSlot = "20261031T0400"

// scheduledInput is o/r with issue 7 (P0) in Controller, which starts /lighthouse-start-issue 7,
// and schedule codebase-map due at scheduledSlot, under brake b.
func scheduledInput(b *Brake, history ...runs.Row) Input {
	return Input{Now: scheduledNow, Brake: b, Repos: []RepoInput{{
		Snapshot: Snapshot{Version: SnapshotVersion, Repo: "o/r", GatheredAt: scheduledNow, Items: []Item{{
			Issue: 7, Title: "Bouw", State: "OPEN", ItemID: "I7", Status: statusController, Priority: "P0",
			BlockedBy: nil, PRs: nil, Comments: nil,
		}}},
		Controller: repoconfig.Controller{Profile: ProfileSoftware, SpecReview: true, PlanReview: true},
		Language:   "nl", DefaultBranch: "main",
		Installed: map[string]bool{"lighthouse-start-issue": true},
		History:   history,
		Schedules: []ScheduledDue{{
			Schedule: "codebase-map", Profile: ProfileSoftware, Slot: scheduledSlot,
			Verdict: scheduled.Verdict{Mode: "full", Score: 3},
			Plan: scheduled.LaunchPlan{
				Command: "/gsd-map-codebase", Instructions: "no questions", Tools: []string{"Read", "Write"},
				MaxRuntime: 90 * time.Minute,
			},
		}},
	}}}
}

// openBrakeWith is an open brake with slots and maxSoftware.
func openBrakeWith(slots, maxSoftware int) *Brake {
	gib := 32.0
	return &Brake{UsageMeasured: true, HaltPctSession: 85, HaltPctWeek: 90, MemAvailableGiB: &gib,
		MinMemAvailableGB: 8, ReservePerRunGB: 3, Slots: slots, MaxSoftware: maxSoftware}
}

func TestDecideScheduledStartBeforeIssueStart(t *testing.T) {
	got := Decide(scheduledInput(openBrakeWith(4, 4)))
	if want := []Kind{KindScheduledStart, KindStart}; !reflect.DeepEqual(kinds(got), want) {
		t.Fatalf("kinds = %v, want %v (the scheduled start goes before the P0 issue start)", kinds(got), want)
	}
	record := scheduled.Record{Verdict: scheduled.Verdict{Mode: "full", Score: 3}, MaxRuntime: 90 * time.Minute}.Encode()
	want := Action{
		Kind: KindScheduledStart, Repo: "o/r", Schedule: "codebase-map", Slot: scheduledSlot,
		Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:codebase-map:" + scheduledSlot,
		Attempt: 1, Profile: ProfileSoftware, Command: "/gsd-map-codebase", Prompt: "/gsd-map-codebase",
		Branch: "scheduled/codebase-map-" + scheduledSlot + "-1", Language: "nl",
		Tools: []string{"Read", "Write"}, Instructions: "no questions", Verdict: record,
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("scheduled start = %+v\nwant %+v", got[0], want)
	}
}

// One slot: the scheduled start takes it and the issue start defers. Ordering the scheduled
// start after the issue start, or not counting it as admitted, fails this test.
func TestDecideScheduledStartTakesASlot(t *testing.T) {
	got := Decide(scheduledInput(openBrakeWith(1, 4)))
	if want := []Kind{KindDefer, KindScheduledStart}; !reflect.DeepEqual(kinds(got), want) {
		t.Fatalf("kinds = %v, want %v", kinds(got), want)
	}
	if d := got[0]; d.Issue != 7 || d.Reason != "slots: 1 of 1 in use" {
		t.Errorf("defer = %+v, want issue 7 on slots: 1 of 1 in use", d)
	}
}

// A software scheduled run counts against max_software: the issue start (software too) defers.
func TestDecideScheduledStartCountsAsSoftware(t *testing.T) {
	got := Decide(scheduledInput(openBrakeWith(4, 1)))
	if want := []Kind{KindDefer, KindScheduledStart}; !reflect.DeepEqual(kinds(got), want) {
		t.Fatalf("kinds = %v, want %v", kinds(got), want)
	}
	if d := got[0]; d.Issue != 7 || d.Reason != "software: 1 of 1 in use" {
		t.Errorf("defer = %+v, want issue 7 on software: 1 of 1 in use", d)
	}
	// A writing schedule leaves the software limit to the issue start.
	in := scheduledInput(openBrakeWith(4, 1))
	in.Repos[0].Schedules[0].Profile = "writing"
	if got := Decide(in); !reflect.DeepEqual(kinds(got), []Kind{KindScheduledStart, KindStart}) {
		t.Errorf("writing schedule: kinds = %v, want both to start", kinds(got))
	}
}

// A scheduled run always goes to this host: a profile on a node changes neither its node nor
// which slots it takes.
func TestDecideScheduledStartStaysOnThisHost(t *testing.T) {
	in := scheduledInput(openBrakeWith(4, 4))
	in.ProfileNodes = map[string]string{ProfileSoftware: "node-mac"}
	in.Brake.InUse = 4
	in.Brake.Nodes = map[string]NodeSlots{"node-mac": {Name: "mac", Slots: 4}}
	got := Decide(in)
	var sched *Action
	for i := range got {
		if got[i].Schedule == "codebase-map" {
			sched = &got[i]
		}
	}
	if sched == nil || sched.Kind != KindDefer || sched.Reason != "slots: 4 of 4 in use" || sched.Node != "" {
		t.Errorf("scheduled action = %+v, want a defer on this host's full slots", sched)
	}
}

// Every closed brake holds the scheduled start back as a defer that keeps its schedule and slot,
// and claims nothing; a pause drops it.
func TestDecideScheduledStartUnderClosedBrake(t *testing.T) {
	tests := []struct {
		name   string
		brake  func(*Brake)
		reason string
	}{
		{"usage", func(b *Brake) { b.SessionPct = 90 }, "usage: session 90%, week 0%"},
		{"memory", func(b *Brake) { g := 4.0; b.MemAvailableGiB = &g }, "memory: MemAvailable 4.0 GiB - 0 x 3 GiB < 8 GiB"},
		{"slots", func(b *Brake) { b.InUse = 4 }, "slots: 4 of 4 in use"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := scheduledInput(openBrakeWith(4, 4))
			in.Repos[0].Snapshot.Items = nil
			tc.brake(in.Brake)
			got := Decide(in)
			want := []Action{{Kind: KindDefer, Repo: "o/r", Schedule: "codebase-map", Slot: scheduledSlot,
				Language: "nl", Reason: tc.reason}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Decide = %+v\nwant %+v", got, want)
			}
		})
	}
	in := scheduledInput(openBrakeWith(4, 4))
	in.Repos[0].Snapshot.Items = nil
	in.Brake.Paused = true
	if got := Decide(in); len(got) != 0 {
		t.Errorf("paused: Decide = %+v, want nothing", got)
	}
}

// The attempt and branch follow the runs of the slot's trigger key: after one retry the start is
// attempt 2 on its own branch. Runs of other slots or schedules do not count.
func TestDecideScheduledSecondAttempt(t *testing.T) {
	key := "scheduled:codebase-map:" + scheduledSlot
	retry := runs.Row{ID: 1, Schedule: "codebase-map", Trigger: runs.TriggerScheduled, TriggerKey: key, Attempt: 1, Outcome: runs.Retry}
	other := runs.Row{ID: 2, Schedule: "codebase-map", Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:codebase-map:20261030T0400", Attempt: 1, Outcome: runs.Retry}
	otherSchedule := runs.Row{ID: 3, Schedule: "reflect", Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:reflect:" + scheduledSlot, Attempt: 1, Outcome: runs.Retry}
	got := Decide(scheduledInput(nil, retry, other, otherSchedule))
	if len(got) == 0 || got[0].Kind != KindScheduledStart {
		t.Fatalf("Decide = %+v, want a scheduled start first", got)
	}
	if a := got[0]; a.Attempt != 2 || a.Branch != "scheduled/codebase-map-"+scheduledSlot+"-2" {
		t.Errorf("start = attempt %d on %q, want attempt 2 on scheduled/codebase-map-%s-2", a.Attempt, a.Branch, scheduledSlot)
	}
}

// Decide does not trust its caller to have run the planner: an open run of the schedule, or a
// slot with two attempts, gives no start.
func TestDecideScheduledNoStartWhenOpenOrSpent(t *testing.T) {
	key := "scheduled:codebase-map:" + scheduledSlot
	open := runs.Row{ID: 1, Schedule: "codebase-map", Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:codebase-map:20261030T0400", Open: true}
	retry := runs.Row{ID: 2, Schedule: "codebase-map", Trigger: runs.TriggerScheduled, TriggerKey: key, Outcome: runs.Retry}
	for name, history := range map[string][]runs.Row{
		"open run of an earlier slot": {open},
		"two attempts":                {retry, retry},
	} {
		in := scheduledInput(nil, history...)
		in.Repos[0].Snapshot.Items = nil
		if got := Decide(in); len(got) != 0 {
			t.Errorf("%s: Decide = %+v, want nothing", name, got)
		}
	}
}

// The end of an open scheduled run (spec §4). Classification: pure logic. Each case is one row of
// the spec's table; it fails if Decide gives another action, outcome, reason or release for it.

const (
	finRunID   = 5
	finSession = "33333333-3333-4333-8333-333333333333"
	finBranch  = "scheduled/codebase-map-" + scheduledSlot + "-1"
	finSHA     = "fedcba9876543210fedcba9876543210fedcba98"
)

// finishInput is o/r with one open scheduled run (attempt 1, branch finBranch) that started
// age ago with a record of maxRuntime, working, with a recent transcript event; host max_wait 10m
// and max_runtime 8h, under an open brake. No schedule is due and no item is on the board.
func finishInput(age, maxRuntime time.Duration) Input {
	started := scheduledNow.Add(-age)
	rec := scheduled.Record{Verdict: scheduled.Verdict{Mode: "full"}, MaxRuntime: maxRuntime, GitState: "state"}.Encode()
	row := runs.Row{ID: finRunID, Schedule: "codebase-map", SessionID: finSession, Mode: "start",
		Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:codebase-map:" + scheduledSlot, Attempt: 1,
		Branch: finBranch, Profile: ProfileSoftware, StartedAt: started, Open: true, StartSHA: finSHA, Verdict: rec}
	open := runs.OpenRun{ID: finRunID, Schedule: "codebase-map", SessionID: finSession, TriggerKey: row.TriggerKey,
		Profile: ProfileSoftware, Branch: finBranch, WorkspaceID: "ws-5", WorkplaceSession: "key-5", StartedAt: started,
		StartSHA: finSHA, Verdict: rec}
	return Input{Now: scheduledNow, Brake: openBrakeWith(4, 4), MaxWait: Duration(10 * time.Minute),
		MaxRuntime: Duration(8 * time.Hour), Repos: []RepoInput{{
			Snapshot: Snapshot{Version: SnapshotVersion, Repo: "o/r", GatheredAt: scheduledNow,
				OpenRuns: []runs.OpenRun{open}},
			Language: "nl", DefaultBranch: "main",
			History: []runs.Row{row},
			Stands:  map[int64]workplace.Stand{finRunID: workplace.Working},
			Runs: map[int64]RunState{finRunID: {Transcript: &workplace.Transcript{
				LastEvent: scheduledNow.Add(-time.Minute), AssistantTurn: true}}},
		}}}
}

// finishRepo is the one repo of in, to change.
func finishRepo(in *Input) *RepoInput { return &in.Repos[0] }

// done makes the run's session Done.
func done(in *Input) { finishRepo(in).Stands[finRunID] = workplace.Done }

// inPRPhase sets pr_at on the run, at, records pr's head as the commit the check pushed, and
// gives its branch pr.
func inPRPhase(in *Input, at time.Time, pr forge.PR) {
	r := finishRepo(in)
	r.History[0].PRAt = at
	r.Snapshot.OpenRuns[0].PRAt = at
	r.Snapshot.ScheduledPRs = map[string][]forge.PR{finBranch: {pr}}
	setPushed(in, pr.HeadSHA)
}

// setPushed records sha as the commit the run's check pushed.
func setPushed(in *Input, sha string) {
	r := finishRepo(in)
	rec, err := scheduled.DecodeRecord(r.History[0].Verdict)
	if err != nil {
		panic(err)
	}
	rec.Pushed = sha
	r.History[0].Verdict = rec.Encode()
	r.Snapshot.OpenRuns[0].Verdict = rec.Encode()
}

// ownPR is the run's own PR #41 on finBranch.
func ownPR(state, mergeable, checks string) forge.PR {
	return forge.PR{Number: 41, State: state, Mergeable: mergeable, Checks: checks, HeadRef: finBranch, BaseRef: "main",
		HeadSHA: "abc1234abc1234abc1234abc1234abc1234abc12", URL: "https://example.com/o/r/pull/41"}
}

// closeAction is the close of the run with outcome and reason; the branch goes too.
func closeAction(o runs.Outcome, reason string, pr int, end, discard bool) Action {
	return Action{Kind: KindScheduledClose, Repo: "o/r", RunID: finRunID, Schedule: "codebase-map",
		Slot: scheduledSlot, Branch: finBranch, Language: "nl", Outcome: o, ReasonArg: reason, PR: pr,
		End: end, Discard: discard}
}

func TestDecideScheduledFinishTable(t *testing.T) {
	green := ownPR("OPEN", "MERGEABLE", "SUCCESS")
	merged := ownPR("MERGED", "", "SUCCESS")
	merged.MergedAt = scheduledNow.Add(-time.Minute)
	closed := ownPR("CLOSED", "", "SUCCESS")
	closed.ClosedAt = scheduledNow.Add(-time.Minute)
	finish := Action{Kind: KindScheduledFinish, Repo: "o/r", RunID: finRunID, Schedule: "codebase-map",
		Slot: scheduledSlot, Branch: finBranch, Language: "nl"}
	marker := park.Marker{SessionID: finSession, CreatedAt: scheduledNow.Add(-5 * time.Minute).Format(time.RFC3339),
		ResetAt: scheduledNow.Add(time.Hour).Format(time.RFC3339), Controller: true}

	tests := []struct {
		name  string
		age   time.Duration // since the start
		max   time.Duration // the record's max_runtime
		setup func(in *Input)
		want  []Action
	}{
		{name: "working agent: nothing", age: 30 * time.Minute, max: 90 * time.Minute, setup: func(*Input) {}},

		// Row 1: session done, no PR yet.
		{name: "done without a PR: finish", age: 30 * time.Minute, max: 90 * time.Minute, setup: done,
			want: []Action{finish}},
		{name: "done with its own open PR and no pr_at: finish on that PR", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				done(in)
				finishRepo(in).Snapshot.ScheduledPRs = map[string][]forge.PR{finBranch: {ownPR("OPEN", "UNKNOWN", "PENDING")}}
			},
			want: []Action{func() Action { a := finish; a.PR, a.HeadSHA = 41, ownPR("", "", "").HeadSHA; return a }()}},

		// Row 2: own PR green, brake open.
		{name: "green PR, brake open: merge on its head", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) { inPRPhase(in, scheduledNow.Add(-10*time.Minute), green) },
			want: []Action{{Kind: KindScheduledMerge, Repo: "o/r", RunID: finRunID, Schedule: "codebase-map",
				Slot: scheduledSlot, Branch: finBranch, Language: "nl", PR: 41, HeadSHA: green.HeadSHA, PRURL: green.URL}}},
		{name: "green PR, closed usage brake: merge all the same", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), green)
				in.Brake.SessionPct = 99
			},
			want: []Action{{Kind: KindScheduledMerge, Repo: "o/r", RunID: finRunID, Schedule: "codebase-map",
				Slot: scheduledSlot, Branch: finBranch, Language: "nl", PR: 41, HeadSHA: green.HeadSHA, PRURL: green.URL}}},
		// Finding F: the merge is bound to the commit the check pushed.
		{name: "green PR whose head moved off the pushed commit: close, failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), green)
				setPushed(in, finSHA)
			},
			want: []Action{closeAction(runs.Failed, "branch changed after push", 41, false, false)}},
		{name: "pending PR whose head moved off the pushed commit: close, failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), ownPR("OPEN", "MERGEABLE", "PENDING"))
				setPushed(in, finSHA)
			},
			want: []Action{closeAction(runs.Failed, "branch changed after push", 41, false, false)}},
		{name: "green PR without a recorded pushed commit: close, failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), green)
				setPushed(in, "")
			},
			want: []Action{closeAction(runs.Failed, "branch changed after push", 41, false, false)}},
		{name: "PR without a head and no recorded pushed commit: close, failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				pr := green
				pr.HeadSHA = ""
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), pr)
			},
			want: []Action{closeAction(runs.Failed, "branch changed after push", 41, false, false)}},
		{name: "green PR, paused: wait", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), green)
				in.Brake.Paused = true
			}},
		{name: "green PR with an open roborev review: wait", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				pr := green
				pr.Roborev = forge.RoborevOpen
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), pr)
			}},
		{name: "draft PR: wait", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				pr := green
				pr.Draft = true
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), pr)
			}},

		// Row 5: own PR red or with a conflict.
		{name: "red PR: close, failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), ownPR("OPEN", "MERGEABLE", "FAILURE"))
			},
			want: []Action{closeAction(runs.Failed, "PR #41 red", 41, false, false)}},
		{name: "conflicting PR: close, failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), ownPR("OPEN", "CONFLICTING", "SUCCESS"))
			},
			want: []Action{closeAction(runs.Failed, "PR #41 has a merge conflict", 41, false, false)}},

		// Row 6: own PR closed without a merge.
		{name: "PR closed without a merge: failed", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) { inPRPhase(in, scheduledNow.Add(-10*time.Minute), closed) },
			want:  []Action{closeAction(runs.Failed, "PR #41 closed without a merge", 0, false, false)}},
		// A merged PR (a merge whose close failed): done, branch removed.
		{name: "PR merged: done", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) { inPRPhase(in, scheduledNow.Add(-10*time.Minute), merged) },
			want:  []Action{closeAction(runs.Done, "", 0, false, false)}},

		// pr_timeout: 60 minutes from pr_at, a pause or a closed brake counting (extra a).
		{name: "pending PR past pr_timeout: close, failed", age: 2 * time.Hour, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-61*time.Minute), ownPR("OPEN", "MERGEABLE", "PENDING"))
			},
			want: []Action{closeAction(runs.Failed, "PR #41 not merged within pr_timeout 1h0m0s", 41, false, false)}},
		{name: "(a) green PR past pr_timeout while paused: close, failed", age: 2 * time.Hour, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-61*time.Minute), green)
				in.Brake.Paused = true
			},
			want: []Action{closeAction(runs.Failed, "PR #41 not merged within pr_timeout 1h0m0s", 41, false, false)}},
		{name: "(a) pending PR past pr_timeout under a closed usage brake: close, failed", age: 2 * time.Hour, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-61*time.Minute), ownPR("OPEN", "MERGEABLE", "PENDING"))
				in.Brake.SessionPct = 99
			},
			want: []Action{closeAction(runs.Failed, "PR #41 not merged within pr_timeout 1h0m0s", 41, false, false)}},
		{name: "no PR on the branch past pr_timeout: failed", age: 2 * time.Hour, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-61*time.Minute), forge.PR{Number: 50, State: "OPEN", HeadRef: "other"})
			},
			want: []Action{closeAction(runs.Failed, "no PR merged within pr_timeout 1h0m0s", 0, false, false)}},
		// (b) In the PR phase max_runtime no longer counts, the host's neither.
		{name: "(b) pr_at set, past max_runtime, PR pending: wait", age: 3 * time.Hour, max: 90 * time.Minute,
			setup: func(in *Input) {
				inPRPhase(in, scheduledNow.Add(-20*time.Minute), ownPR("OPEN", "MERGEABLE", "PENDING"))
				in.MaxRuntime = Duration(time.Hour)
			}},

		// Row 7: rules 9, 10, 15-19 and the usage park: retry, the agent ended first.
		{name: "parked by the usage guard: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) { finishRepo(in).Markers = []park.Marker{marker} },
			want:  []Action{closeAction(runs.Retry, "parked by the usage guard", 0, true, false)}},
		{name: "workplace gone: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) { finishRepo(in).Runs[finRunID] = RunState{Gone: true} },
			want:  []Action{closeAction(runs.Retry, "workplace gone", 0, false, false)}},
		{name: "rule 9 unknown command: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				finishRepo(in).Runs[finRunID] = RunState{Transcript: &workplace.Transcript{LastEvent: scheduledNow, UnknownCommand: "/gsd-map"}}
			},
			want: []Action{closeAction(runs.Retry, "unknown command `/gsd-map`", 0, true, false)}},
		{name: "rule 10 done without an assistant turn: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				done(in)
				finishRepo(in).Runs[finRunID] = RunState{Transcript: &workplace.Transcript{LastEvent: scheduledNow}}
			},
			want: []Action{closeAction(runs.Retry, "session did nothing", 0, true, false)}},
		{name: "rule 15 start uncertain: forget the launch, end the agent, retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				finishRepo(in).Snapshot.OpenRuns[0].WorkplaceSession = ""
				delete(finishRepo(in).Stands, finRunID)
				finishRepo(in).Runs[finRunID] = RunState{}
			},
			want: []Action{closeAction(runs.Retry, "start uncertain: no session found", 0, true, true)}},
		{name: "rule 16 permission prompt: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				st := finishRepo(in).Runs[finRunID]
				st.Permission = &Permission{At: scheduledNow.Add(-11 * time.Minute), Tool: "Bash", Path: "/home/x/secret"}
				st.Transcript.LastEvent = scheduledNow.Add(-11 * time.Minute)
				finishRepo(in).Runs[finRunID] = st
			},
			want: []Action{closeAction(runs.Retry, "waiting on a permission for `Bash`", 0, true, false)}},
		{name: "rule 17 silent: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				finishRepo(in).Runs[finRunID] = RunState{Transcript: &workplace.Transcript{LastEvent: scheduledNow.Add(-11 * time.Minute), AssistantTurn: true}}
			},
			want: []Action{closeAction(runs.Retry, "no activity in max_wait", 0, true, false)}},
		{name: "rule 18 silent by the forge stand: retry", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				finishRepo(in).Stands[finRunID] = workplace.Waiting
				finishRepo(in).Runs[finRunID] = RunState{SessionAt: scheduledNow.Add(-11 * time.Minute)}
			},
			want: []Action{closeAction(runs.Retry, "no activity in max_wait (forge stand)", 0, true, false)}},
		{name: "rule 19 (b) max_runtime of the record: retry, not failed", age: 91 * time.Minute, max: 90 * time.Minute,
			setup: func(*Input) {},
			want:  []Action{closeAction(runs.Retry, "timeout (max_runtime 1h30m0s)", 0, true, false)}},
		{name: "rule 19 on the last attempt: failed", age: 91 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) { finishRepo(in).History[0].Attempt = 2 },
			want:  []Action{closeAction(runs.Failed, "timeout (max_runtime 1h30m0s)", 0, true, false)}},
		{name: "rule 19 also over a done session: max_runtime holds up to the check", age: 91 * time.Minute, max: 90 * time.Minute,
			setup: done,
			want:  []Action{closeAction(runs.Retry, "timeout (max_runtime 1h30m0s)", 0, true, false)}},
		{name: "(b) a full verdict's 120m, not the host's 60m: wait", age: 90 * time.Minute, max: 120 * time.Minute,
			setup: func(in *Input) { in.MaxRuntime = Duration(time.Hour) }},
		{name: "(b) a fast verdict's 45m on a host of 120m: retry", age: 50 * time.Minute, max: 45 * time.Minute,
			setup: func(in *Input) { in.MaxRuntime = Duration(2 * time.Hour) },
			want:  []Action{closeAction(runs.Retry, "timeout (max_runtime 45m0s)", 0, true, false)}},
		{name: "a record without max_runtime: the host's", age: 61 * time.Minute, max: 0,
			setup: func(in *Input) { in.MaxRuntime = Duration(time.Hour) },
			want:  []Action{closeAction(runs.Retry, "timeout (max_runtime 1h0m0s)", 0, true, false)}},
		{name: "a retry closes the run's own open PR", age: 30 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				finishRepo(in).Runs[finRunID] = RunState{Transcript: &workplace.Transcript{LastEvent: scheduledNow.Add(-11 * time.Minute), AssistantTurn: true}}
				finishRepo(in).Snapshot.ScheduledPRs = map[string][]forge.PR{finBranch: {ownPR("OPEN", "MERGEABLE", "PENDING")}}
			},
			want: []Action{closeAction(runs.Retry, "no activity in max_wait", 41, true, false)}},

		// Finding 1: a claim a dead tick left without a workplace.
		{name: "claim without a workplace past max_wait: retry", age: 11 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				r := finishRepo(in)
				r.Snapshot.OpenRuns[0].WorkspaceID, r.Snapshot.OpenRuns[0].WorkplaceSession = "", ""
				delete(r.Stands, finRunID)
				delete(r.Runs, finRunID)
			},
			want: []Action{closeAction(runs.Retry, "claimed without a workplace", 0, false, false)}},
		{name: "claim without a workplace within max_wait: wait", age: 5 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				r := finishRepo(in)
				r.Snapshot.OpenRuns[0].WorkspaceID, r.Snapshot.OpenRuns[0].WorkplaceSession = "", ""
				delete(r.Stands, finRunID)
				delete(r.Runs, finRunID)
			}},
		{name: "a row the snapshot's open runs lack: wait", age: 11 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				r := finishRepo(in)
				r.Snapshot.OpenRuns = nil
				delete(r.Stands, finRunID)
				delete(r.Runs, finRunID)
			}},
		{name: "a PR into another base than the default branch is not the run's", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				pr := green
				pr.BaseRef = "release"
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), pr)
			}},
		{name: "a PR of another branch or a fork is not the run's", age: 40 * time.Minute, max: 90 * time.Minute,
			setup: func(in *Input) {
				fork := green
				fork.CrossRepo = true
				inPRPhase(in, scheduledNow.Add(-10*time.Minute), fork)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := finishInput(tc.age, tc.max)
			tc.setup(&in)
			got := Decide(in)
			if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("Decide = %+v\nwant %+v", got, tc.want)
			}
			// (c) A scheduled run has no issue: nothing that posts.
			for _, a := range got {
				if a.Kind == KindReturn || a.Kind == KindDone || a.Kind == KindRocket || a.Kind == KindMerge {
					t.Errorf("action %s for a scheduled run would post on issue %d", a.Kind, a.Issue)
				}
			}
		})
	}
}

// The end of a scheduled run takes no slot: a closing or finishing run goes through any brake,
// a pause included, and is no start the brake counts.
func TestDecideScheduledFinishPassesTheBrake(t *testing.T) {
	in := finishInput(30*time.Minute, 90*time.Minute)
	done(&in)
	in.Brake = &Brake{Paused: true, Slots: 0}
	if got := Decide(in); len(got) != 1 || got[0].Kind != KindScheduledFinish {
		t.Errorf("Decide = %+v, want the finish under a pause and without slots", got)
	}
}
