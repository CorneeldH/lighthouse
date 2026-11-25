package report

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func coverage(t *testing.T, db *sql.DB, repo string) CoverageReport {
	t.Helper()
	r, err := Coverage(context.Background(), load(t, db), db, repo)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	return r
}

func lineOf(t *testing.T, r CoverageReport, key string) CoverageLine {
	t.Helper()
	for _, l := range r.Lines {
		if l.Key == key {
			return l
		}
	}
	t.Fatalf("no coverage line %q", key)
	return CoverageLine{}
}

func wantCount(t *testing.T, r CoverageReport, key string, n int) {
	t.Helper()
	l := lineOf(t, r, key)
	if l.Count == nil || *l.Count != n {
		got := "nil"
		if l.Count != nil {
			got = fmt.Sprint(*l.Count)
		}
		t.Errorf("%s count = %s; want %d", key, got, n)
	}
	if l.Reason == "" {
		t.Errorf("%s has no reason", key)
	}
}

// addUnlinkedSession inserts an ended session of repo without a trajectory.
func (f *fixture) addUnlinkedSession(repo, reason string) {
	f.t.Helper()
	f.exec(`INSERT INTO sessions (source, source_id, recorded_at, repo, harness, session_key,
		transcript_path, unlinked_reason, started_at, ended_at)
		VALUES ('transcript', ?, ?, ?, 'claude-code', ?, '/t.jsonl', ?, ?, ?)`,
		f.sourceID(), stampOf(t0), repo, f.sourceID(), reason, stampOf(t0), stampOf(at(5)))
}

// addOpenSession inserts a linked session of tid that has not ended.
func (f *fixture) addOpenSession(tid int64) {
	f.t.Helper()
	id := f.addSession(tid, "claude-code", "standard", t0, at(5))
	f.exec(`UPDATE sessions SET ended_at = NULL WHERE id = ?`, id)
}

func TestCoverageCountsUnlinkedSessionsPerReason(t *testing.T) {
	db, f := newFixture(t)
	f.addUnlinkedSession("o/a", "main_only")
	f.addUnlinkedSession("o/a", "main_only")
	f.addUnlinkedSession("o/a", "no_issue_number")
	f.addUnlinkedSession("o/b", "no_issue_branch")
	tid := f.addTrajectory("o/a", 1)
	f.addSession(tid, "claude-code", "standard", t0, at(5))

	all := coverage(t, db, "")
	wantCount(t, all, "sessions_unlinked_main_only", 2)
	wantCount(t, all, "sessions_unlinked_no_issue_branch", 1)
	wantCount(t, all, "sessions_unlinked_no_issue_number", 1)

	a := coverage(t, db, "o/a")
	wantCount(t, a, "sessions_unlinked_no_issue_branch", 0)
	wantCount(t, a, "sessions_unlinked_main_only", 2)
}

func TestCoverageUnlinkedReviewRuns(t *testing.T) {
	db, f := newFixture(t)
	f.addUnlinkedReview("plan_review", t0)
	f.addUnlinkedReview("spec_review", t0)
	tid := f.addTrajectory("o/a", 1, withPlan(3))
	f.addReview(tid, "plan_review", t0, 0)

	r := coverage(t, db, "")
	wantCount(t, r, "review_runs_unlinked", 2)
	// review_runs stores no repo: a repo filter cannot narrow the line, and
	// the note says so.
	a := coverage(t, db, "o/a")
	wantCount(t, a, "review_runs_unlinked", 2)
	if lineOf(t, a, "review_runs_unlinked").Note == "" {
		t.Error("review_runs_unlinked under a repo filter has no note")
	}
}

func TestCoverageBeforeCohortPerComponent(t *testing.T) {
	db, f := newFixture(t)
	reviewed := f.addTrajectory("o/a", 1, withPlan(3), createdAt(at(100)))
	f.addReview(reviewed, "plan_review", at(100), 0)
	f.addTrajectory("o/a", 2, withPlan(3), createdAt(at(10)))  // before both cohorts
	f.addTrajectory("o/a", 3, withPlan(3), createdAt(at(200))) // without plan review; spec has no cohort
	f.addTrajectory("o/a", 4, createdAt(at(10)))               // no plan: outside the population

	r := coverage(t, db, "")
	wantCount(t, r, "before_cohort_plan_review", 1)
	wantCount(t, r, "before_cohort_spec_review", 3)

	c := Components(load(t, db), Filter{})
	for _, comp := range []string{"plan_review", "spec_review"} {
		want := blockOf(t, c, comp).Outside.BeforeCohort
		if got := *lineOf(t, r, "before_cohort_"+comp).Count; got != want {
			t.Errorf("before_cohort_%s = %d; Components says %d", comp, got, want)
		}
	}
}

func TestCoverageUnpricedAndNoUsage(t *testing.T) {
	db, f := newFixture(t)
	cost := 1.5
	priced := f.addTrajectory("o/a", 1)
	run := f.addRun(priced, "standard", t0, at(5))
	f.addSetup(ofRun(run), "sdd", "m", "claude-code")
	f.addUsage(ofRun(run), "m", 10, &cost, "estimate")

	unpriced := f.addTrajectory("o/a", 2)
	run2 := f.addRun(unpriced, "standard", t0, at(5))
	f.addUsage(ofRun(run2), "m", 10, &cost, "estimate")
	f.addUsage(ofRun(run2), "other", 10, nil, "")

	f.addTrajectory("o/a", 3) // nothing at all

	r := coverage(t, db, "")
	wantCount(t, r, "no_setup", 2)
	wantCount(t, r, "no_usage", 1)
	wantCount(t, r, "unpriced_usage", 1)
}

func TestCoverageMissingTraitsPerKind(t *testing.T) {
	db, f := newFixture(t)
	big, small := 900, 10
	full := f.addTrajectory("o/a", 1, withRoute("2"), withArea("ui"))
	f.addRecovery(full, 10, "none", t0, &small)
	noArea := f.addTrajectory("o/a", 2, withRoute("1"))
	f.addRecovery(noArea, 11, "none", t0, nil)
	f.addRecovery(noArea, 12, "none", t0, &small) // one unmeasured PR: size unknown
	noRoute := f.addTrajectory("o/a", 3, withArea("ui"))
	f.addRecovery(noRoute, 13, "none", t0, nil)
	f.addRecovery(noRoute, 14, "none", t0, &big) // above the threshold: size known
	f.addTrajectory("o/b", 4)

	r := coverage(t, db, "")
	wantCount(t, r, "no_route", 2)
	wantCount(t, r, "no_area", 2)
	wantCount(t, r, "no_pr_size", 2)

	a := coverage(t, db, "o/a")
	wantCount(t, a, "no_route", 1)
	wantCount(t, a, "no_pr_size", 1)
}

func TestCoverageOutsideOutcomeReasons(t *testing.T) {
	db, f := newFixture(t)
	closed := f.addTrajectory("o/a", 1)
	f.addRecovery(closed, 10, "none", t0, nil)
	running := f.addTrajectory("o/a", 2)
	f.addRecovery(running, 11, "none", t0, nil)
	f.addRecovery(running, 12, "open", at(10), nil)
	f.addTrajectory("o/a", 3) // no recovery measurement

	sdd := f.addTrajectory("o/a", 4)
	r1 := f.addRun(sdd, "standard", t0, at(5))
	f.addSetup(ofRun(r1), "sdd", "m", "claude-code")
	mixed := f.addTrajectory("o/a", 5)
	r2 := f.addRun(mixed, "standard", t0, at(5))
	r3 := f.addRun(mixed, "standard", t0, at(5))
	f.addSetup(ofRun(r2), "sdd", "m", "claude-code")
	f.addSetup(ofRun(r3), "native", "m", "claude-code")

	r := coverage(t, db, "")
	wantCount(t, r, "running", 1)
	wantCount(t, r, "no_recovery_measurement", 3)
	wantCount(t, r, "no_lane", 3)
	wantCount(t, r, "mixed_lane", 1)

	lane := blockOf(t, Components(load(t, db), Filter{}), "lane").Outside
	if *lineOf(t, r, "no_lane").Count != lane.NoLane || *lineOf(t, r, "mixed_lane").Count != lane.MixedLane {
		t.Errorf("lane lines differ from the lane block outside %+v", lane)
	}
}

func TestCoverageQuestionsUnlinkedOrOpen(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/a", 1)
	f.addSession(tid, "claude-code", "standard", t0, at(5)) // closed and linked: collected
	f.addOpenSession(tid)
	f.addUnlinkedSession("o/a", "main_only")
	f.addUnlinkedSession("o/b", "main_only")

	wantCount(t, coverage(t, db, ""), "questions_unlinked_or_open", 3)
	wantCount(t, coverage(t, db, "o/a"), "questions_unlinked_or_open", 2)
}

func TestCoverageUnmeasuredLinesHaveNoCount(t *testing.T) {
	db, f := newFixture(t)
	f.addTrajectory("o/a", 1)
	r := coverage(t, db, "")
	for _, key := range []string{"task_review", "final_review", "pr_review", "test_gate",
		"review_costs", "harness_codex", "harness_opencode"} {
		l := lineOf(t, r, key)
		if l.Count != nil {
			t.Errorf("%s count = %d; want nil (not measured)", key, *l.Count)
		}
		if l.Note == "" || l.Reason == "" {
			t.Errorf("%s = %+v; want a reason and a note", key, l)
		}
	}
	for _, key := range []string{"harness_codex", "harness_opencode"} {
		if !strings.Contains(lineOf(t, r, key).Note, "Explore/exploit") {
			t.Errorf("%s note = %q; want not measured until Explore/exploit", key, lineOf(t, r, key).Note)
		}
	}
}

func TestCoverageNotMigrated(t *testing.T) {
	db, f := newFixture(t)
	f.addUnlinkedSession("o/a", "main_only")
	if _, err := db.Exec(`DROP TABLE review_runs`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	r := coverage(t, db, "")
	if !r.NotMigrated || len(r.Lines) != 1 {
		t.Fatalf("report = %+v; want NotMigrated with one line", r)
	}
	if l := r.Lines[0]; l.Key != "not_migrated" || l.Reason != "database not migrated" || l.Count != nil {
		t.Errorf("line = %+v; want not_migrated, database not migrated, no count", l)
	}
}

func TestCoverageTaskReviewShowsIndexedLedgerCount(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/a", 1)
	b := f.addTrajectory("o/b", 2)
	f.addFinding(a, "task_review", "k1", "minor", "", "/ledger.json")
	f.addFinding(a, "task_review", "k2", "major", "", "/ledger.json")
	f.addFinding(b, "task_review", "k3", "minor", "", "/ledger.json")
	f.addFinding(a, "plan_review", "k4", "minor", "", "/r.json")

	l := lineOf(t, coverage(t, db, ""), "task_review")
	if l.Count != nil || !strings.Contains(l.Note, "3 indexed ledger findings") {
		t.Errorf("task_review = %+v; want no count and 3 indexed ledger findings in the note", l)
	}
	l = lineOf(t, coverage(t, db, "o/a"), "task_review")
	if !strings.Contains(l.Note, "2 indexed ledger findings") {
		t.Errorf("task_review under o/a note = %q; want 2 indexed ledger findings", l.Note)
	}
}

func TestCoverageUngroupedModelAndHarness(t *testing.T) {
	db, f := newFixture(t)
	one := f.addTrajectory("o/a", 1)
	r1 := f.addRun(one, "standard", t0, at(5))
	f.addSetup(ofRun(r1), "", "m", "claude-code")
	two := f.addTrajectory("o/a", 2)
	f.addSession(two, "claude-code", "standard", t0, at(5))
	f.addSession(two, "codex", "standard", t0, at(5))
	f.addTrajectory("o/a", 3)

	r := coverage(t, db, "")
	wantCount(t, r, "no_model", 2)
	wantCount(t, r, "no_harness", 1)
	wantCount(t, r, "harness_mixed", 1)
}
