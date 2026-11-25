package report

// This file lists the coverage gaps of spec §8: one line per gap, each a
// count with its reason. A line that is not measured has no count (never 0)
// and says why in its note. The trajectory lines count over d with the same
// rules Load and Components use; the session, review-run and ledger lines
// read db, because Load keeps no unlinked rows.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CoverageLine is one gap. Count nil: not measured, Note says why.
type CoverageLine struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
	Count  *int   `json:"count"`
	Note   string `json:"note"`
}

// CoverageReport is every gap. NotMigrated: the database lacks tables and
// Lines holds only the not_migrated line.
type CoverageReport struct {
	Lines       []CoverageLine `json:"lines"`
	NotMigrated bool           `json:"not_migrated"`
}

// Notes of the lines that are not measured.
const (
	NoteNotMeasured      = "not measured"
	NoteUntilExplore     = "not measured until Explore/exploit"
	ReasonNotMigrated    = "database not migrated"
	noteReviewRunsNoRepo = "review_runs stores no repo: counted over every repo"
)

// sessionUnlinkedReasons are the values of sessions.unlinked_reason.
var sessionUnlinkedReasons = []struct{ value, reason string }{
	{"main_only", "sessions without a trajectory: only on the main branch"},
	{"no_issue_branch", "sessions without a trajectory: no issue branch"},
	{"no_issue_number", "sessions without a trajectory: no issue number in the branch"},
}

// Coverage lists the gaps of d and db for repo ("" is every repo).
func Coverage(ctx context.Context, d *Dataset, db *sql.DB, repo string) (CoverageReport, error) {
	if len(d.Missing) > 0 {
		return CoverageReport{NotMigrated: true, Lines: []CoverageLine{{
			Key: "not_migrated", Reason: ReasonNotMigrated,
			Note: "missing tables: " + strings.Join(d.Missing, ", "),
		}}}, nil
	}
	var lines []CoverageLine
	add := func(key, reason string, n int) {
		lines = append(lines, CoverageLine{Key: key, Reason: reason, Count: &n})
	}
	q := querier{ctx: ctx, db: db}

	for _, r := range sessionUnlinkedReasons {
		n := q.count(`SELECT count(*) FROM sessions WHERE trajectory_id IS NULL AND unlinked_reason = ?
			AND (? = '' OR repo = ?)`, r.value, repo, repo)
		add("sessions_unlinked_"+r.value, r.reason, n)
	}
	n := q.count(`SELECT count(*) FROM review_runs WHERE trajectory_id IS NULL AND unlinked_reason = 'no_trajectory'`)
	add("review_runs_unlinked", "review records without a trajectory (no_trajectory)", n)
	if repo != "" {
		lines[len(lines)-1].Note = noteReviewRunsNoRepo
	}

	var in []*Trajectory
	for i := range d.Trajectories {
		if repo == "" || d.Trajectories[i].Repo == repo {
			in = append(in, &d.Trajectories[i])
		}
	}
	cohorts := d.Cohorts()
	for _, c := range []string{"plan_review", "spec_review"} {
		add("before_cohort_"+c, "plans before the linking of "+c+" records (cohort)", countOf(in, func(t *Trajectory) bool {
			return t.PlanTasks != nil && cohorts.Place(t, c) == PlaceBeforeCohort
		}))
	}
	counts := []struct {
		key, reason string
		match       func(*Trajectory) bool
	}{
		{"no_setup", "trajectories without a setup", func(t *Trajectory) bool { return !t.HasSetup }},
		{"no_usage", "trajectories without usage: no cost", func(t *Trajectory) bool { return !t.HasUsage }},
		{"unpriced_usage", "trajectories with unpriced usage (cost IS NULL): no cost",
			func(t *Trajectory) bool { return t.HasUsage && t.Cost == nil }},
		{"no_route", "trajectories without a route", func(t *Trajectory) bool { return t.Route == nil }},
		{"no_area", "trajectories without an area", func(t *Trajectory) bool { return t.Area == nil }},
		{"no_pr_size", "trajectories without a PR size", func(t *Trajectory) bool { return t.PRSize == "" }},
		{"running", "trajectories outside the outcome: recovery window still open",
			func(t *Trajectory) bool { return !t.Closed && len(t.Recoveries) > 0 }},
		{"no_recovery_measurement", "trajectories outside the outcome: no recovery measurement (never merged, closed without a PR, or measure-recovery did not run)",
			func(t *Trajectory) bool { return len(t.Recoveries) == 0 }},
		{"no_lane", "trajectories outside the lane comparison: no lane", func(t *Trajectory) bool {
			return t.Lane != "sdd" && t.Lane != "native" && t.Lane != "mixed"
		}},
		{"mixed_lane", "trajectories outside the lane comparison: mixed lane",
			func(t *Trajectory) bool { return t.Lane == "mixed" }},
		{"no_model", "trajectories outside the model comparison: no model", func(t *Trajectory) bool { return t.Model == "" }},
		{"no_harness", "trajectories outside the harness comparison: no harness",
			func(t *Trajectory) bool { return t.Harness == "" }},
		{"harness_mixed", "trajectories outside the harness comparison: more than one harness",
			func(t *Trajectory) bool { return t.Harness == "mixed" }},
	}
	for _, c := range counts {
		add(c.key, c.reason, countOf(in, c.match))
	}

	n = q.count(`SELECT count(*) FROM sessions WHERE (trajectory_id IS NULL OR ended_at IS NULL)
		AND (? = '' OR repo = ?)`, repo, repo)
	add("questions_unlinked_or_open", "sessions open or without a trajectory: collect records their questions only once closed and linked", n)

	ledger := q.count(`SELECT count(*) FROM findings f JOIN trajectories t ON t.id = f.trajectory_id
		WHERE f.component = 'task_review' AND (? = '' OR t.repo = ?)`, repo, repo)
	if q.err != nil {
		return CoverageReport{}, q.err
	}
	unmeasured := []CoverageLine{
		{Key: "task_review", Reason: "task_review: " + ReasonTaskReview,
			Note: fmt.Sprintf("%s; %d indexed ledger findings", NoteNotMeasured, ledger)},
		{Key: "final_review", Reason: "final_review: " + ReasonNoSource, Note: NoteNotMeasured},
		{Key: "pr_review", Reason: "pr_review: " + ReasonNoSource, Note: NoteNotMeasured},
		{Key: "test_gate", Reason: "test_gate: " + ReasonNoSource, Note: NoteNotMeasured},
		{Key: "review_costs", Reason: "review costs: not recorded", Note: NoteNotMeasured},
		{Key: "harness_codex", Reason: "Codex: no source", Note: NoteUntilExplore},
		{Key: "harness_opencode", Reason: "OpenCode: no source", Note: NoteUntilExplore},
	}
	return CoverageReport{Lines: append(lines, unmeasured...)}, nil
}

func countOf(ts []*Trajectory, match func(*Trajectory) bool) int {
	n := 0
	for _, t := range ts {
		if match(t) {
			n++
		}
	}
	return n
}

// querier runs count queries and keeps the first error.
type querier struct {
	ctx context.Context
	db  *sql.DB
	err error
}

func (q *querier) count(query string, args ...any) int {
	if q.err != nil {
		return 0
	}
	var n int
	if err := q.db.QueryRowContext(q.ctx, query, args...).Scan(&n); err != nil {
		q.err = fmt.Errorf("report: coverage: %w", err)
	}
	return n
}
