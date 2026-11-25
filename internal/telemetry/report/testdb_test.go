package report

// The fixture builder of the report tests: a migrated lighthouse.db in a
// temporary directory, filled with plain INSERTs so each test states exactly
// the rows it reads.

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
)

// t0 is the base time of every fixture.
var t0 = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// at is t0 plus n minutes.
func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

func stampOf(t time.Time) string { return t.UTC().Format(time.RFC3339) }

type fixture struct {
	t   *testing.T
	db  *sql.DB
	seq int
}

// newFixture opens a migrated database and returns it with its builder.
func newFixture(t *testing.T) (*sql.DB, *fixture) {
	t.Helper()
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB, &fixture{t: t, db: s.DB}
}

func (f *fixture) exec(query string, args ...any) int64 {
	f.t.Helper()
	res, err := f.db.Exec(query, args...)
	if err != nil {
		f.t.Fatalf("exec %q: %v", query, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		f.t.Fatalf("last insert id: %v", err)
	}
	return id
}

// sourceID returns a fresh unique source id.
func (f *fixture) sourceID() string {
	f.seq++
	return "fx-" + time.Duration(f.seq).String()
}

type trajectoryOpts struct {
	planTasks   *int
	route, area *string
	createdAt   time.Time
}

type trajectoryOpt func(*trajectoryOpts)

func withPlan(n int) trajectoryOpt        { return func(o *trajectoryOpts) { o.planTasks = &n } }
func withRoute(r string) trajectoryOpt    { return func(o *trajectoryOpts) { o.route = &r } }
func withArea(a string) trajectoryOpt     { return func(o *trajectoryOpts) { o.area = &a } }
func createdAt(t time.Time) trajectoryOpt { return func(o *trajectoryOpts) { o.createdAt = t } }

// addTrajectory inserts a trajectory, created at t0 unless createdAt says otherwise.
func (f *fixture) addTrajectory(repo string, issue int, opts ...trajectoryOpt) int64 {
	f.t.Helper()
	o := trajectoryOpts{createdAt: t0}
	for _, opt := range opts {
		opt(&o)
	}
	return f.exec(`INSERT INTO trajectories (repo, issue, created_at, plan_tasks, route, area)
		VALUES (?, ?, ?, ?, ?, ?)`, repo, issue, stampOf(o.createdAt), o.planTasks, o.route, o.area)
}

// addRun inserts an ended run of trajectory tid from start to end.
func (f *fixture) addRun(tid int64, assignment string, start, end time.Time) int64 {
	f.t.Helper()
	var repo string
	var issue int
	if err := f.db.QueryRow(`SELECT repo, issue FROM trajectories WHERE id = ?`, tid).Scan(&repo, &issue); err != nil {
		f.t.Fatalf("read trajectory %d: %v", tid, err)
	}
	return f.exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt,
		started_at, ended_at, outcome, trajectory_id, assignment)
		VALUES (?, ?, ?, 'start', 'handoff', ?, 1, ?, ?, 'done', ?, ?)`,
		repo, issue, f.sourceID(), f.sourceID(), stampOf(start), stampOf(end), tid, assignment)
}

// addSession inserts an ended interactive session of trajectory tid.
func (f *fixture) addSession(tid int64, harness, assignment string, start, end time.Time) int64 {
	f.t.Helper()
	var repo string
	if err := f.db.QueryRow(`SELECT repo FROM trajectories WHERE id = ?`, tid).Scan(&repo); err != nil {
		f.t.Fatalf("read trajectory %d: %v", tid, err)
	}
	return f.exec(`INSERT INTO sessions (source, source_id, recorded_at, repo, harness, session_key,
		transcript_path, trajectory_id, link_basis, assignment, started_at, ended_at)
		VALUES ('transcript', ?, ?, ?, ?, ?, '/t.jsonl', ?, 'branch', ?, ?, ?)`,
		f.sourceID(), stampOf(t0), repo, harness, f.sourceID(), tid, assignment, stampOf(start), stampOf(end))
}

// owner is the run or the session a setup, usage row or event belongs to.
type owner struct{ run, session int64 }

func ofRun(id int64) owner     { return owner{run: id} }
func ofSession(id int64) owner { return owner{session: id} }

func (o owner) args() (any, any) {
	var run, session any
	if o.run != 0 {
		run = o.run
	}
	if o.session != 0 {
		session = o.session
	}
	return run, session
}

// addSetup records the setup of a run or session; empty strings are NULL.
func (f *fixture) addSetup(o owner, lane, model, harness string) {
	f.t.Helper()
	run, session := o.args()
	f.exec(`INSERT INTO setups (source, source_id, recorded_at, run_id, session_id, lane, model, harness)
		VALUES ('fx', ?, ?, ?, ?, ?, ?, ?)`, f.sourceID(), stampOf(t0), run, session,
		nullable(lane), nullable(model), nullable(harness))
}

// addUsage records the usage of one model; a nil cost is unpriced.
func (f *fixture) addUsage(o owner, model string, tokens int64, cost *float64, kind string) {
	f.t.Helper()
	f.addUsageAt(o, model, tokens, cost, kind, t0)
}

// addUsageAt is addUsage with the recorded_at of the row.
func (f *fixture) addUsageAt(o owner, model string, tokens int64, cost *float64, kind string, recorded time.Time) {
	f.t.Helper()
	run, session := o.args()
	var k any
	if cost != nil {
		k = kind
	}
	f.exec(`INSERT INTO usage (source, source_id, recorded_at, run_id, session_id, model, input_tokens, cost, cost_kind)
		VALUES ('fx', ?, ?, ?, ?, ?, ?, ?, ?)`, f.sourceID(), stampOf(recorded), run, session, model, tokens, cost, k)
}

// addReview records a linked review run of component at ranAt.
func (f *fixture) addReview(tid int64, component string, ranAt time.Time, findings int) (int64, string) {
	f.t.Helper()
	path := "/r/" + f.sourceID() + "/record.json"
	id := f.exec(`INSERT INTO review_runs (source, source_id, recorded_at, trajectory_id, component,
		record_path, record_sum, ran_at, outcome, finding_count)
		VALUES ('review-index', ?, ?, ?, ?, ?, 'sum', ?, 'converged', ?)`,
		f.sourceID(), stampOf(t0), tid, component, path, stampOf(ranAt), findings)
	return id, path
}

// addUnlinkedReview records a review run without a trajectory.
func (f *fixture) addUnlinkedReview(component string, ranAt time.Time) {
	f.t.Helper()
	f.exec(`INSERT INTO review_runs (source, source_id, recorded_at, unlinked_reason, component,
		record_path, record_sum, ran_at, finding_count)
		VALUES ('review-index', ?, ?, 'no_trajectory', ?, '/r/x/record.json', 'sum', ?, 0)`,
		f.sourceID(), stampOf(t0), component, stampOf(ranAt))
}

// addFinding records a finding of trajectory tid in the record at path.
func (f *fixture) addFinding(tid int64, component, key, severity, decision, path string) {
	f.t.Helper()
	f.exec(`INSERT INTO findings (source, source_id, recorded_at, trajectory_id, component, finding_key,
		severity, decision, record_path, record_sum)
		VALUES ('review-index', ?, ?, ?, ?, ?, ?, ?, ?, 'sum')`,
		f.sourceID(), stampOf(t0), tid, component, key, severity, nullable(decision), path)
}

// addRecovery records the recovery measurement of one PR with its items.
func (f *fixture) addRecovery(tid int64, pr int, status string, mergedAt time.Time, codeLines *int, items ...string) int64 {
	f.t.Helper()
	id := f.exec(`INSERT INTO recoveries (source, source_id, recorded_at, trajectory_id, pr, merged_at,
		window_end, status, code_lines) VALUES ('fx', ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.sourceID(), stampOf(t0), tid, pr, stampOf(mergedAt), stampOf(mergedAt.Add(14*24*time.Hour)), status, codeLines)
	for i, kind := range items {
		f.exec(`INSERT INTO recovery_items (recovery_id, kind, ref, at) VALUES (?, ?, ?, ?)`,
			id, kind, f.sourceID(), stampOf(mergedAt.Add(time.Duration(i+1)*time.Hour)))
	}
	return id
}

// addQuestion records a question of a run or session at the given time.
func (f *fixture) addQuestion(tid int64, o owner, when time.Time) {
	f.t.Helper()
	run, session := o.args()
	ref := f.sourceID()
	f.exec(`INSERT INTO trajectory_events (source, source_id, recorded_at, trajectory_id, run_id, session_id,
		kind, ref, at) VALUES ('fx', ?, ?, ?, ?, ?, 'question', ?, ?)`,
		ref, stampOf(t0), tid, run, session, ref, stampOf(when))
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
