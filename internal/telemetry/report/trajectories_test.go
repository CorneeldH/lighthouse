package report

import (
	"encoding/json"
	"slices"
	"testing"
)

func issues(rows []ListRow) []int {
	out := []int{}
	for _, r := range rows {
		out = append(out, r.Issue)
	}
	return out
}

func TestTrajectoriesFilterByComponent(t *testing.T) {
	db, f := newFixture(t)
	reviewed := f.addTrajectory("o/r", 1, withPlan(2))
	f.addReview(reviewed, "plan_review", at(0), 1)
	laned := f.addTrajectory("o/r", 2)
	r := f.addRun(laned, "standard", at(0), at(10))
	f.addSetup(ofRun(r), "sdd", "", "")
	f.addTrajectory("o/r", 3)
	f.addTrajectory("o/other", 4)

	d := load(t, db)
	if got := issues(Trajectories(d, Filter{})); !slices.Equal(got, []int{4, 1, 2, 3}) {
		t.Errorf("unfiltered = %v; want [4 1 2 3] (repo, issue)", got)
	}
	if got := issues(Trajectories(d, Filter{Component: "plan_review"})); !slices.Equal(got, []int{1}) {
		t.Errorf("plan_review = %v; want [1]", got)
	}
	if got := issues(Trajectories(d, Filter{Component: "lane"})); !slices.Equal(got, []int{2}) {
		t.Errorf("lane = %v; want [2]", got)
	}
	if got := issues(Trajectories(d, Filter{Component: "spec_review"})); len(got) != 0 {
		t.Errorf("spec_review = %v; want none", got)
	}
	if got := issues(Trajectories(d, Filter{Repo: "o/r", Component: "plan_review"})); !slices.Equal(got, []int{1}) {
		t.Errorf("repo and plan_review = %v; want [1]", got)
	}
	rows := Trajectories(d, Filter{Component: "plan_review"})
	if row := rows[0]; !slices.Equal(row.Components, []string{"plan_review"}) || row.Size != SizeSmall {
		t.Errorf("row = %+v; want components [plan_review], size small", row)
	}
	b, _ := json.Marshal(Trajectories(d, Filter{Component: "harness"}))
	if string(b) != "[]" {
		t.Errorf("empty list JSON = %s; want []", b)
	}
}

func TestTrajectoriesRecoveryStates(t *testing.T) {
	db, f := newFixture(t)
	f.addTrajectory("o/r", 1)
	open := f.addTrajectory("o/r", 2)
	f.addRecovery(open, 20, "recovered", at(0), nil, "fix")
	f.addRecovery(open, 21, "open", at(10), nil)
	rec := f.addTrajectory("o/r", 3)
	f.addRecovery(rec, 30, "none", at(0), nil)
	f.addRecovery(rec, 31, "recovered", at(10), nil, "revert")
	none := f.addTrajectory("o/r", 4)
	f.addRecovery(none, 40, "none", at(0), nil)
	r := f.addRun(none, "standard", at(0), at(10))
	f.addUsage(ofRun(r), "opus", 1, fp(2), "estimate")

	rows := Trajectories(load(t, db), Filter{})
	want := map[int]struct {
		recovery string
		closed   bool
	}{
		1: {RecoveryNoneMeasured, false},
		2: {RecoveryOpen, false},
		3: {RecoveryRecovered, true},
		4: {RecoveryNone, true},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v; want %d", rows, len(want))
	}
	for _, row := range rows {
		w := want[row.Issue]
		if row.Recovery != w.recovery || row.Closed != w.closed {
			t.Errorf("issue %d: Recovery = %q, Closed = %v; want %q, %v", row.Issue, row.Recovery, row.Closed, w.recovery, w.closed)
		}
		if row.Issue == 4 && (fmtF(row.Cost) != "2" || row.CostKind != "estimate") {
			t.Errorf("issue 4: Cost = %s, CostKind = %q; want 2, estimate", fmtF(row.Cost), row.CostKind)
		}
	}
}
