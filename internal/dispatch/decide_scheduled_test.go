package dispatch

import (
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
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

func kinds(as []Action) []Kind {
	out := make([]Kind, len(as))
	for i, a := range as {
		out[i] = a.Kind
	}
	return out
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
