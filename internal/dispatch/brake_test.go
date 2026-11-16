package dispatch

import (
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
)

// A non-parked resume (answer, handoff) takes a new slot: its previous run is closed. Counting
// only starts as admitted fails this test.
func TestBrakeResumeCountsForSlots(t *testing.T) {
	gib := 32.0
	b := &Brake{
		UsageMeasured: true, HaltPctSession: 85, HaltPctWeek: 90,
		MemAvailableGiB: &gib, MinMemAvailableGB: 8, ReservePerRunGB: 3,
		Slots: 1, MaxSoftware: 5,
	}
	resume := planned{Action: Action{Kind: KindResume, Repo: "o/a", Issue: 1, Trigger: "answer"}}
	start := planned{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}}
	got := b.apply([]planned{resume, start})
	if got[0].Kind != KindResume {
		t.Fatalf("resume must pass: %+v", got[0].Action)
	}
	if got[1].Kind != KindDefer || got[1].Reason != "slots: 1 of 1 in use" {
		t.Errorf("start after a resume must defer on slots, got %+v", got[1].Action)
	}
}

// A parked (usage) resume skips slots and software and is not counted in them, but it counts
// for memory. Breaking the parked skip or the admitted exclusion fails this test.
func TestBrakeParkedResume(t *testing.T) {
	gib := 32.0
	b := &Brake{
		UsageMeasured: true, HaltPctSession: 85, HaltPctWeek: 90,
		MemAvailableGiB: &gib, MinMemAvailableGB: 8, ReservePerRunGB: 3,
		Slots: 1, MaxSoftware: 1, InUse: 1, SoftwareInUse: 1,
	}
	parked := planned{Action: Action{Kind: KindResume, Repo: "o/a", Issue: 1, Trigger: triggerUsage}, software: true, holdsSlot: true}
	fresh := planned{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, software: true}
	got := b.apply([]planned{parked, fresh})
	if len(got) != 2 || got[0].Kind != KindResume {
		t.Fatalf("parked resume must pass full slots: %+v", got)
	}
	if got[1].Kind != KindDefer || got[1].Reason != "slots: 1 of 1 in use" {
		t.Errorf("fresh start must defer on slots, got %+v", got[1].Action)
	}

	gib = 10.0
	got = b.apply([]planned{parked, parked})
	if got[1].Kind != KindDefer || got[1].Reason != "memory: MemAvailable 10.0 GiB - 1 x 3 GiB < 8 GiB" {
		t.Errorf("parked resume must count for memory, got %+v", got[1].Action)
	}
}

// A node whose markers could not be read this tick takes no start and no resume: its parked runs
// are not counted, so its occupancy is unknown. Another node with free slots still takes one.
func TestBrakeClosesNodeWithUnmeasuredMarkers(t *testing.T) {
	// lighthouse run (Manual) skips memory, slots and software, but not an unknown occupancy.
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual %t", manual), func(t *testing.T) { testBrakeClosesNodeWithUnmeasuredMarkers(t, manual) })
	}
}

func testBrakeClosesNodeWithUnmeasuredMarkers(t *testing.T, manual bool) {
	blind := "node-" + strings.ToLower(rand.Text()[:8])
	seen := "node-" + strings.ToLower(rand.Text()[:8])
	b := &Brake{
		UsageMeasured: true, HaltPctSession: 85, HaltPctWeek: 90, MaxSoftware: 5, Manual: manual,
		Nodes: map[string]NodeSlots{
			blind: {Name: "blind", Slots: 4, Unmeasured: true},
			seen:  {Name: "seen", Slots: 4},
		},
	}
	start := planned{Action: Action{Kind: KindStart, Repo: "o/a", Issue: 1}, node: blind}
	parked := planned{Action: Action{Kind: KindResume, Repo: "o/a", Issue: 2, Trigger: triggerUsage}, node: blind, holdsSlot: true}
	other := planned{Action: Action{Kind: KindStart, Repo: "o/a", Issue: 3}, node: seen}
	got := b.apply([]planned{start, parked, other})
	for i, want := range []string{"markers not readable", "markers not readable"} {
		if got[i].Kind != KindDefer || got[i].Reason != want {
			t.Errorf("action %d = %+v, want a defer with %q", i, got[i].Action, want)
		}
	}
	if got[2].Kind != KindStart {
		t.Errorf("start on the other node = %+v, want it to pass", got[2].Action)
	}
}
