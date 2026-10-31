package dispatch

import (
	"slices"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/runs"
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
