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

// A run that closes in this tick gives its slot to the starts of this tick (#359): the host slot,
// the software slot of a software run, the slot of its node. A close without frees, on another
// node, on a node outside Nodes, or of a run that is not software frees nothing for the start.
// Testing the starts against b instead of the freed copy fails host, software and node.
func TestBrakeFreedSlotAdmitsStart(t *testing.T) {
	nodeA := NodeSlots{Name: "a", Slots: 1, InUse: 1}
	cases := []struct {
		name   string
		brake  func(*Brake)
		close  func(*planned)
		starts []planned
		want   []Kind
		reason string // the defer text of the last start, when it defers
	}{
		{"host", func(b *Brake) { b.InUse = 4 }, func(*planned) {},
			nil, []Kind{KindStart}, ""},
		{"software", func(b *Brake) { b.InUse, b.SoftwareInUse = 3, 2 }, func(p *planned) { p.software = true },
			[]planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, software: true}}, []Kind{KindStart}, ""},
		{"node", func(b *Brake) { b.Nodes = map[string]NodeSlots{"node-a": nodeA} }, func(p *planned) { p.node = "node-a" },
			[]planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, node: "node-a"}}, []Kind{KindStart}, ""},
		{"no frees", func(b *Brake) { b.InUse = 4 }, func(p *planned) { p.frees = false },
			nil, []Kind{KindDefer}, "slots: 4 of 4 in use"},
		{"other node", func(b *Brake) {
			b.Nodes = map[string]NodeSlots{"node-a": nodeA, "node-b": {Name: "b", Slots: 1, InUse: 1}}
		}, func(p *planned) { p.node = "node-b" },
			[]planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, node: "node-a"}}, []Kind{KindDefer}, "slots: 1 of 1 in use on a"},
		{"node outside Nodes", func(b *Brake) { b.Nodes = map[string]NodeSlots{"node-a": nodeA} }, func(p *planned) { p.node = "node-x" },
			[]planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, node: "node-a"}}, []Kind{KindDefer}, "slots: 1 of 1 in use on a"},
		{"no software freed", func(b *Brake) { b.InUse, b.SoftwareInUse = 3, 2 }, func(p *planned) { p.software = false },
			[]planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, software: true}}, []Kind{KindDefer}, "software: 2 of 2 in use"},
		{"one slot, two starts", func(b *Brake) { b.InUse = 4 }, func(*planned) {},
			[]planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}}, {Action: Action{Kind: KindStart, Repo: "o/c", Issue: 3}}},
			[]Kind{KindStart, KindDefer}, "slots: 4 of 4 in use"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := openBrake()
			c.brake(b)
			closing := planned{Action: Action{Kind: KindClose, Repo: "o/a", Issue: 1, RunID: 9}, frees: true}
			c.close(&closing)
			starts := c.starts
			if starts == nil {
				starts = []planned{{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}}}
			}
			got := b.apply(append([]planned{closing}, starts...))
			if len(got) != 1+len(c.want) || got[0].Kind != KindClose {
				t.Fatalf("got %+v", got)
			}
			for i, k := range c.want {
				if got[1+i].Kind != k {
					t.Errorf("action %d: got %+v, want %s", 1+i, got[1+i].Action, k)
				}
			}
			if last := got[len(got)-1]; c.reason != "" && last.Reason != c.reason {
				t.Errorf("defer reason %q, want %q", last.Reason, c.reason)
			}
		})
	}
}

// A close that frees a slot the brake never counted (in_use 0) does not push a counter below
// zero: otherwise five starts would pass four slots. Removing the clamp in freed fails this test.
func TestBrakeFreedNeverBelowZero(t *testing.T) {
	b := openBrake()
	ps := []planned{{Action: Action{Kind: KindClose, Repo: "o/a", Issue: 1, RunID: 9}, frees: true, software: true}}
	for i := 2; i <= 6; i++ {
		ps = append(ps, planned{Action: Action{Kind: KindStart, Repo: "o/b", Issue: i}})
	}
	got := b.apply(ps)
	for i := 1; i <= 4; i++ {
		if got[i].Kind != KindStart {
			t.Errorf("start %d must pass: %+v", i, got[i].Action)
		}
	}
	if got[5].Kind != KindDefer || got[5].Reason != "slots: 4 of 4 in use" {
		t.Errorf("fifth start must defer on slots, got %+v", got[5].Action)
	}
}

// freed works on a copy: b stays the measurement of this tick, its Nodes map included. Copying
// the Brake without its own Nodes map fails this test.
func TestBrakeFreedKeepsMeasurement(t *testing.T) {
	b := openBrake()
	b.InUse = 2
	b.Nodes = map[string]NodeSlots{"node-a": {Name: "a", Slots: 1, InUse: 1}}
	b.apply([]planned{
		{Action: Action{Kind: KindClose, Repo: "o/a", Issue: 1, RunID: 8}, frees: true, node: "node-a"},
		{Action: Action{Kind: KindClose, Repo: "o/b", Issue: 2, RunID: 9}, frees: true},
	})
	if b.InUse != 2 || b.Nodes["node-a"].InUse != 1 {
		t.Errorf("apply changed the measurement: InUse %d, node-a %d", b.InUse, b.Nodes["node-a"].InUse)
	}
}
