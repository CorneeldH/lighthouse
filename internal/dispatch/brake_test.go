package dispatch

import "testing"

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
