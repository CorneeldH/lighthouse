package report

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"
)

func load(t *testing.T, db *sql.DB) *Dataset {
	t.Helper()
	d, err := Load(context.Background(), db, at(100000))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

func byIssue(t *testing.T, d *Dataset, issue int) *Trajectory {
	t.Helper()
	for i := range d.Trajectories {
		if d.Trajectories[i].Issue == issue {
			return &d.Trajectories[i]
		}
	}
	t.Fatalf("no trajectory for issue %d", issue)
	return nil
}

func TestLoadCostIsNilForUnpricedRow(t *testing.T) {
	db, f := newFixture(t)
	unpriced := f.addTrajectory("o/r", 1)
	r := f.addRun(unpriced, "standard", at(0), at(10))
	f.addUsage(ofRun(r), "opus", 100, fp(2.5), "estimate")
	s := f.addSession(unpriced, "claude-code", "standard", at(0), at(10))
	f.addUsage(ofSession(s), "sonnet", 100, nil, "")

	priced := f.addTrajectory("o/r", 2)
	r2 := f.addRun(priced, "standard", at(0), at(10))
	f.addUsage(ofRun(r2), "opus", 100, fp(1.5), "estimate")
	s2 := f.addSession(priced, "claude-code", "standard", at(0), at(10))
	f.addUsage(ofSession(s2), "sonnet", 100, fp(1), "actual")

	f.addTrajectory("o/r", 3) // no usage at all

	d := load(t, db)
	if got := byIssue(t, d, 1); got.Cost != nil || !got.HasUsage {
		t.Errorf("unpriced: Cost = %v, HasUsage = %v; want nil, true", got.Cost, got.HasUsage)
	}
	got := byIssue(t, d, 2)
	if got.Cost == nil || *got.Cost != 2.5 || got.CostKind != "mixed" {
		t.Errorf("priced: Cost = %v, CostKind = %q; want 2.5, mixed", got.Cost, got.CostKind)
	}
	if got := byIssue(t, d, 3); got.Cost != nil || got.HasUsage || got.CostKind != "" {
		t.Errorf("no usage: Cost = %v, HasUsage = %v, CostKind = %q; want nil, false, empty", got.Cost, got.HasUsage, got.CostKind)
	}
}

func TestLoadClosedNeedsRecoveryAndNoOpenRow(t *testing.T) {
	db, f := newFixture(t)
	f.addTrajectory("o/r", 1) // no recovery
	open := f.addTrajectory("o/r", 2)
	f.addRecovery(open, 10, "none", at(0), nil)
	f.addRecovery(open, 11, "open", at(10), nil)
	done := f.addTrajectory("o/r", 3)
	f.addRecovery(done, 12, "recovered", at(0), nil, "fix")
	f.addRecovery(done, 13, "none", at(10), nil)

	d := load(t, db)
	for issue, want := range map[int]bool{1: false, 2: false, 3: true} {
		got := byIssue(t, d, issue)
		if got.Closed != want || (got.ClosedAt != nil) != want {
			t.Errorf("issue %d: Closed = %v, ClosedAt = %v; want closed %v", issue, got.Closed, got.ClosedAt, want)
		}
	}
	rec := byIssue(t, d, 3).Recoveries
	if len(rec) != 2 || rec[0].PR != 12 || rec[0].Status != "recovered" || len(rec[0].Items) != 1 || rec[0].Items[0].Kind != "fix" {
		t.Errorf("Recoveries = %+v; want PR 12 recovered with one fix, then PR 13", rec)
	}
}

func TestLoadClosedAtIsLastMerge(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	f.addRecovery(tid, 10, "none", at(500), nil)
	f.addRecovery(tid, 11, "none", at(20), nil)

	got := byIssue(t, load(t, db), 1)
	if got.ClosedAt == nil || !got.ClosedAt.Equal(at(500)) {
		t.Errorf("ClosedAt = %v; want %v", got.ClosedAt, at(500))
	}
}

func TestLoadLaneModelAssignment(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "variant:lane=sdd", at(0), at(10))
	f.addSetup(ofRun(r), "sdd", "opus", "claude-code")
	f.addUsage(ofRun(r), "sonnet", 900, fp(1), "estimate")
	s := f.addSession(tid, "claude-code", "standard", at(20), at(30))
	f.addSetup(ofSession(s), "native", "opus", "claude-code")
	f.addUsage(ofSession(s), "opus", 10, fp(3), "estimate")

	same := f.addTrajectory("o/r", 2)
	r2 := f.addRun(same, "variant:lane=sdd", at(0), at(10))
	f.addSetup(ofRun(r2), "sdd", "", "")
	f.addRun(same, "variant:lane=sdd", at(20), at(30))

	d := load(t, db)
	got := byIssue(t, d, 1)
	if got.Lane != "mixed" || got.Model != "opus" || got.Harness != "claude-code" || got.Assignment != "mixed" {
		t.Errorf("issue 1: lane %q, model %q, harness %q, assignment %q; want mixed, opus, claude-code, mixed",
			got.Lane, got.Model, got.Harness, got.Assignment)
	}
	if !got.HasSetup || got.Runs != 1 || got.Sessions != 1 {
		t.Errorf("issue 1: HasSetup %v, Runs %d, Sessions %d; want true, 1, 1", got.HasSetup, got.Runs, got.Sessions)
	}
	got = byIssue(t, d, 2)
	if got.Lane != "sdd" || got.Model != "" || got.Harness != "" || got.Assignment != "variant:lane=sdd" || got.Runs != 2 {
		t.Errorf("issue 2: lane %q, model %q, harness %q, assignment %q, runs %d; want sdd, empty, empty, variant:lane=sdd, 2",
			got.Lane, got.Model, got.Harness, got.Assignment, got.Runs)
	}
}

func TestLoadModelFallsBackToSetupWithoutUsage(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "standard", at(0), at(10))
	f.addSetup(ofRun(r), "", "opus", "")
	if got := byIssue(t, load(t, db), 1); got.Model != "opus" {
		t.Errorf("Model = %q; want opus from the setup", got.Model)
	}
}

func TestLoadPRSizeUsesLargestPR(t *testing.T) {
	db, f := newFixture(t)
	above := f.addTrajectory("o/r", 1)
	f.addRecovery(above, 10, "none", at(0), intp(20))
	f.addRecovery(above, 11, "none", at(1), intp(400))
	below := f.addTrajectory("o/r", 2)
	f.addRecovery(below, 12, "none", at(0), intp(20))
	f.addRecovery(below, 13, "none", at(1), intp(99))
	unknown := f.addTrajectory("o/r", 3)
	f.addRecovery(unknown, 14, "none", at(0), intp(20))
	f.addRecovery(unknown, 15, "none", at(1), nil)
	knownAbove := f.addTrajectory("o/r", 4)
	f.addRecovery(knownAbove, 16, "none", at(0), intp(150))
	f.addRecovery(knownAbove, 17, "none", at(1), nil)
	f.addTrajectory("o/r", 5)

	d := load(t, db)
	for issue, want := range map[int]string{1: PRAbove, 2: PRBelow, 3: "", 4: PRAbove, 5: ""} {
		if got := byIssue(t, d, issue).PRSize; got != want {
			t.Errorf("issue %d: PRSize = %q; want %q", issue, got, want)
		}
	}
	if cl := byIssue(t, d, 1).Recoveries[1].CodeLines; cl == nil || *cl != 400 {
		t.Errorf("CodeLines = %v; want 400", cl)
	}
}

func TestLoadQuestionsSplit(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "standard", at(0), at(10))
	s := f.addSession(tid, "claude-code", "standard", at(20), at(30))
	f.addQuestion(tid, ofRun(r), at(5))
	f.addQuestion(tid, ofRun(r), at(6))
	f.addQuestion(tid, ofSession(s), at(25))

	got := byIssue(t, load(t, db), 1).Questions
	if got != (QuestionCounts{Dispatcher: 2, Transcript: 1}) {
		t.Errorf("Questions = %+v; want 2 dispatcher, 1 transcript", got)
	}
}

func TestLoadMissingTable(t *testing.T) {
	db, f := newFixture(t)
	f.addTrajectory("o/r", 1)
	if _, err := db.Exec(`DROP TABLE review_runs`); err != nil {
		t.Fatal(err)
	}
	d, err := Load(context.Background(), db, at(0))
	if err != nil {
		t.Fatalf("Load: %v; want no error", err)
	}
	if !slices.Contains(d.Missing, "review_runs") || len(d.Trajectories) != 0 {
		t.Errorf("Missing = %v, %d trajectories; want review_runs and none", d.Missing, len(d.Trajectories))
	}
}

func TestLoadReviewsAndFindings(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1, withPlan(5), withRoute("4b"), withArea("ui"), createdAt(at(-60)))
	empty, _ := f.addReview(tid, "plan_review", at(10), 0)
	rr, path := f.addReview(tid, "spec_review", at(5), 1)
	f.addFinding(tid, "spec_review", "f1", "high", "adopted", path)
	f.addFinding(tid, "task_review", "l1", "low", "", "/ledger.json")
	f.addUnlinkedReview("plan_review", at(0))

	got := byIssue(t, load(t, db), 1)
	if got.PlanTasks == nil || *got.PlanTasks != 5 || got.Route == nil || *got.Route != "4b" ||
		got.Area == nil || *got.Area != "ui" || !got.CreatedAt.Equal(at(-60)) || got.Repo != "o/r" || got.ID != tid {
		t.Errorf("traits = %+v; want plan 5, route 4b, area ui, created %v", got, at(-60))
	}
	want := []Review{
		{Component: "spec_review", RanAt: at(5), Outcome: "converged", FindingCount: 1},
		{Component: "plan_review", RanAt: at(10), Outcome: "converged", FindingCount: 0},
	}
	if !slices.Equal(got.Reviews, want) {
		t.Errorf("Reviews = %+v; want %+v (the empty review %d included)", got.Reviews, want, empty)
	}
	wantF := []Finding{
		{Component: "spec_review", Key: "f1", Severity: "high", Decision: "adopted", RecordPath: path, ReviewRunID: rr},
		{Component: "task_review", Key: "l1", Severity: "low", RecordPath: "/ledger.json"},
	}
	if !slices.Equal(got.Findings, wantF) {
		t.Errorf("Findings = %+v; want %+v", got.Findings, wantF)
	}
}

func TestCohortsPerRepoPerComponent(t *testing.T) {
	db, f := newFixture(t)
	a1 := f.addTrajectory("o/a", 1, createdAt(at(0)))
	f.addReview(a1, "plan_review", at(300), 0)
	a2 := f.addTrajectory("o/a", 2, createdAt(at(0)))
	f.addReview(a2, "plan_review", at(100), 2)
	f.addReview(a2, "spec_review", at(50), 0)
	b1 := f.addTrajectory("o/b", 1, createdAt(at(0)))
	f.addReview(b1, "plan_review", at(700), 0)
	f.addTrajectory("o/a", 3, createdAt(at(99)))  // before the plan_review cohort of o/a
	f.addTrajectory("o/a", 4, createdAt(at(100))) // at the cohort start: inside
	f.addTrajectory("o/b", 2, createdAt(at(800)))
	f.addUnlinkedReview("plan_review", at(-1000)) // unlinked rows start no cohort

	d := load(t, db)
	c := d.Cohorts()
	want := Cohorts{
		"o/a": {"plan_review": at(100), "spec_review": at(50)},
		"o/b": {"plan_review": at(700)},
	}
	if len(c) != len(want) {
		t.Fatalf("Cohorts = %v; want %v", c, want)
	}
	for repo, comps := range want {
		for comp, start := range comps {
			if got, ok := c[repo][comp]; !ok || !got.Equal(start) {
				t.Errorf("Cohorts[%s][%s] = %v, %v; want %v", repo, comp, got, ok, start)
			}
		}
		if len(c[repo]) != len(comps) {
			t.Errorf("Cohorts[%s] = %v; want %v", repo, c[repo], comps)
		}
	}
	tests := []struct {
		repo      string
		issue     int
		component string
		want      string
	}{
		{"o/a", 1, "plan_review", PlaceWith},
		{"o/a", 1, "spec_review", PlaceBeforeCohort}, // created at 0, spec cohort starts at 50
		{"o/a", 3, "plan_review", PlaceBeforeCohort},
		{"o/a", 4, "plan_review", PlaceWithout},
		{"o/b", 2, "plan_review", PlaceWithout},
		{"o/b", 2, "spec_review", PlaceBeforeCohort}, // o/b has no spec_review cohort
	}
	for _, tt := range tests {
		var tr *Trajectory
		for i := range d.Trajectories {
			if d.Trajectories[i].Repo == tt.repo && d.Trajectories[i].Issue == tt.issue {
				tr = &d.Trajectories[i]
			}
		}
		if got := c.Place(tr, tt.component); got != tt.want {
			t.Errorf("Place(%s#%d, %s) = %q; want %q", tt.repo, tt.issue, tt.component, got, tt.want)
		}
	}
}

func TestLoadNoRunsNoSessionsGivesNilDuration(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	f.addRecovery(tid, 10, "none", at(100), nil)

	got := byIssue(t, load(t, db), 1)
	if !got.Closed || got.Active != nil || got.HumanWait != nil {
		t.Errorf("Closed %v, Active %v, HumanWait %v; want closed, nil, nil", got.Closed, got.Active, got.HumanWait)
	}
}

func TestLoadDurationsFromTimeline(t *testing.T) {
	db, f := newFixture(t)
	closed := f.addTrajectory("o/r", 1)
	f.addRun(closed, "standard", at(0), at(30))
	f.addRecovery(closed, 10, "none", at(60), nil)
	running := f.addTrajectory("o/r", 2)
	f.addRun(running, "standard", at(0), at(30))
	f.addRecovery(running, 11, "open", at(60), nil)

	d := load(t, db)
	got := byIssue(t, d, 1)
	if got.Active == nil || *got.Active != 30*time.Minute {
		t.Errorf("Active = %v; want 30m", got.Active)
	}
	if got.HumanWait == nil || *got.HumanWait != 0 {
		t.Errorf("HumanWait = %v; want 0 for a cause without time", got.HumanWait)
	}
	if got := byIssue(t, d, 2); got.Active != nil || got.HumanWait != nil {
		t.Errorf("running: Active %v, HumanWait %v; want nil, nil", got.Active, got.HumanWait)
	}
	if !d.Now.Equal(at(100000)) {
		t.Errorf("Now = %v; want %v", d.Now, at(100000))
	}
}
