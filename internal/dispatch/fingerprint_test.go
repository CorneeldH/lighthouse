package dispatch

import (
	"slices"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func onHoldPre(b forge.Blocker) Pre {
	return Pre{Items: []Item{{Issue: 1, Status: "On hold", BlockedBy: []forge.Blocker{b}}}}
}

func TestFingerprintChangesOnMergedBlocker(t *testing.T) {
	open := forge.Blocker{Repo: "o/r", Issue: 5, State: "OPEN"}
	base := Fingerprint(onHoldPre(open))

	merged := open
	merged.MergedPR = true
	if Fingerprint(onHoldPre(merged)) == base {
		t.Error("fingerprint unchanged after blocker got a merged PR")
	}
	closed := open
	closed.State = "CLOSED"
	if Fingerprint(onHoldPre(closed)) == base {
		t.Error("fingerprint unchanged after blocker closed")
	}
}

func TestFingerprintChangesOnGreenCheck(t *testing.T) {
	mk := func(checks string, withRun bool) Pre {
		p := Pre{Items: []Item{{Issue: 1, Status: "Controller", PRs: []forge.PR{{Number: 9, State: "OPEN", Checks: checks}}}}}
		if withRun {
			p.OpenRuns = []runs.OpenRun{{ID: 1, Issue: 1, StartedAt: time.Unix(100, 0)}}
		}
		return p
	}
	if Fingerprint(mk("PENDING", true)) == Fingerprint(mk("SUCCESS", true)) {
		t.Error("item with open run: checks change not seen")
	}
	if Fingerprint(mk("PENDING", false)) != Fingerprint(mk("SUCCESS", false)) {
		t.Error("item without open run: checks change should not count")
	}
}

func TestFingerprintIgnoresOrder(t *testing.T) {
	t0 := time.Unix(100, 0)
	pre := Pre{
		Items: []Item{
			{Issue: 1, Status: "On hold", BlockedBy: []forge.Blocker{{Repo: "o/r", Issue: 5}, {Repo: "o/r", Issue: 6}}},
			{Issue: 2, Status: "Controller", PRs: []forge.PR{{Number: 7}, {Number: 8}}},
		},
		OpenRuns: []runs.OpenRun{{ID: 1, Issue: 2, StartedAt: t0}, {ID: 2, Issue: 1, StartedAt: t0}},
	}
	rev := Pre{
		Items: []Item{
			{Issue: 2, Status: "Controller", PRs: []forge.PR{{Number: 8}, {Number: 7}}},
			{Issue: 1, Status: "On hold", BlockedBy: []forge.Blocker{{Repo: "o/r", Issue: 6}, {Repo: "o/r", Issue: 5}}},
		},
		OpenRuns: slices.Clone(pre.OpenRuns),
	}
	slices.Reverse(rev.OpenRuns)
	if Fingerprint(pre) != Fingerprint(rev) {
		t.Error("fingerprint depends on order")
	}
}

func TestFingerprintEmptyAndNilAgree(t *testing.T) {
	a := Pre{Items: []Item{{Issue: 1, Status: "Controller"}}}
	b := Pre{Items: []Item{{Issue: 1, Status: "Controller", BlockedBy: []forge.Blocker{}, PRs: []forge.PR{}}}, OpenRuns: []runs.OpenRun{}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("nil and empty lists hash differently")
	}
}

func TestFingerprintIgnoresComments(t *testing.T) {
	a := Pre{Items: []Item{{Issue: 1, Status: "Controller"}}}
	b := Pre{Items: []Item{{Issue: 1, Status: "Controller", Comments: []Comment{{ID: 1, Body: "x"}}}}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("comments changed the fingerprint")
	}
}

func TestFingerprintIgnoresBlockersOutsideOnHold(t *testing.T) {
	mk := func(merged bool) Pre {
		return Pre{Items: []Item{{Issue: 1, Status: "Controller", BlockedBy: []forge.Blocker{{Repo: "o/r", Issue: 5, MergedPR: merged}}}}}
	}
	if Fingerprint(mk(false)) != Fingerprint(mk(true)) {
		t.Error("blocker of a Controller item changed the fingerprint")
	}
}

func TestFingerprintEveryInputCounts(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	base := func() Pre {
		return Pre{
			Items: []Item{{
				Issue: 1, Status: "Controller", Priority: "P1", ItemUpdatedAt: t0, IssueUpdatedAt: t0,
				PRs: []forge.PR{{Number: 9, State: "OPEN", Checks: "PENDING"}},
			}},
			OpenRuns: []runs.OpenRun{{ID: 1, Issue: 1, StartedAt: t0}},
		}
	}
	tests := []struct {
		name   string
		change func(*Pre)
	}{
		{"status", func(p *Pre) { p.Items[0].Status = "On hold" }},
		{"priority", func(p *Pre) { p.Items[0].Priority = "P2" }},
		{"item_updated_at", func(p *Pre) { p.Items[0].ItemUpdatedAt = t0.Add(time.Second) }},
		{"issue_updated_at", func(p *Pre) { p.Items[0].IssueUpdatedAt = t0.Add(time.Second) }},
		{"extra item", func(p *Pre) { p.Items = append(p.Items, Item{Issue: 2, Status: "Controller"}) }},
		{"extra open run", func(p *Pre) { p.OpenRuns = append(p.OpenRuns, runs.OpenRun{ID: 2, Issue: 3, StartedAt: t0}) }},
		{"removed open run", func(p *Pre) { p.OpenRuns = nil }},
		{"open run id", func(p *Pre) { p.OpenRuns[0].ID = 2 }},
		{"open run started_at", func(p *Pre) { p.OpenRuns[0].StartedAt = t0.Add(time.Second) }},
		{"open run issue", func(p *Pre) { p.OpenRuns[0].Issue = 2 }},
		{"pr state", func(p *Pre) { p.Items[0].PRs[0].State = "MERGED" }},
		{"pr draft", func(p *Pre) { p.Items[0].PRs[0].Draft = true }},
		{"pr mergeable", func(p *Pre) { p.Items[0].PRs[0].Mergeable = "CONFLICTING" }},
		{"pr head_sha", func(p *Pre) { p.Items[0].PRs[0].HeadSHA = "abc" }},
	}
	want := Fingerprint(base())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base()
			tc.change(&p)
			if Fingerprint(p) == want {
				t.Errorf("changing %s left the fingerprint unchanged", tc.name)
			}
		})
	}
}

func TestFingerprintStands(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	mk := func(stands map[int64]workplace.Stand) Pre {
		return Pre{
			Items:    []Item{{Issue: 1, Status: "Controller"}},
			OpenRuns: []runs.OpenRun{{ID: 1, Issue: 1, StartedAt: t0}},
			Stands:   stands,
		}
	}
	working := Fingerprint(mk(map[int64]workplace.Stand{1: workplace.Working}))
	for name, stands := range map[string]map[int64]workplace.Stand{
		"done":           {1: workplace.Done},
		"waiting":        {1: workplace.Waiting},
		"not measurable": nil,
	} {
		if Fingerprint(mk(stands)) == working {
			t.Errorf("stand %s hashes like working", name)
		}
	}
	if Fingerprint(mk(map[int64]workplace.Stand{1: workplace.Working, 9: workplace.Done})) != working {
		t.Error("the stand of a run that is not open changed the fingerprint")
	}
}

// Activity and a permission note do not change the fingerprint: they change every tick, and
// the boundaries make the tick look at them in time. A parking does, through the markers.
func TestFingerprintIgnoresActivity(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	mk := func(states map[int64]RunState) Pre {
		return Pre{
			Items:    []Item{{Issue: 1, Status: "Controller"}},
			OpenRuns: []runs.OpenRun{{ID: 1, Issue: 1, StartedAt: t0}},
			Runs:     states,
		}
	}
	base := Fingerprint(mk(map[int64]RunState{1: {}}))
	for name, state := range map[string]RunState{
		"not measured": {},
		"last event":   {Transcript: &workplace.Transcript{LastEvent: t0.Add(time.Minute)}},
		"permission":   {Permission: &Permission{At: t0.Add(time.Minute), Tool: "Write"}},
		"session at":   {SessionAt: t0.Add(time.Minute)},
		"gone":         {Gone: true},
	} {
		states := map[int64]RunState{1: state}
		if name == "not measured" {
			states = nil
		}
		if Fingerprint(mk(states)) != base {
			t.Errorf("%s changed the fingerprint", name)
		}
	}
}

// The markers and the pause flag decide whether a tick has work: a marker whose reset_at
// passed hands its run back, and a pause that ends opens the starts again. Order is no change.
func TestFingerprintMarkersAndPause(t *testing.T) {
	mk := func(session, created, reset string) park.Marker {
		return park.Marker{SessionID: session, Controller: true, CreatedAt: created, ResetAt: reset}
	}
	a := mk("a", "2026-10-31T10:00:00Z", "2026-10-31T15:00:00Z")
	b := mk("b", "2026-10-31T10:00:00Z", "2026-10-31T15:00:00Z")
	until := time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC)
	base := Fingerprint(Pre{Markers: []park.Marker{a, b}, PauseUntil: until})

	if got := Fingerprint(Pre{Markers: []park.Marker{b, a}, PauseUntil: until}); got != base {
		t.Error("marker order changed the fingerprint")
	}
	for name, pre := range map[string]Pre{
		"extra marker":     {Markers: []park.Marker{a, b, mk("c", "2026-10-31T10:00:00Z", "2026-10-31T15:00:00Z")}, PauseUntil: until},
		"other reset_at":   {Markers: []park.Marker{a, mk("b", "2026-10-31T10:00:00Z", "2026-10-31T16:00:00Z")}, PauseUntil: until},
		"other created_at": {Markers: []park.Marker{a, mk("b", "2026-10-31T11:00:00Z", "2026-10-31T15:00:00Z")}, PauseUntil: until},
		"other pause":      {Markers: []park.Marker{a, b}, PauseUntil: until.Add(time.Hour)},
		"no pause":         {Markers: []park.Marker{a, b}},
	} {
		if Fingerprint(pre) == base {
			t.Errorf("%s: fingerprint unchanged", name)
		}
	}
}
