package report

// This file loads every trajectory of lighthouse.db into one Dataset: one
// query per table, grouped in Go by trajectory id. Only the timeline is read
// per trajectory, through telemetry.LoadTimeline, and only for closed
// trajectories with a run or session. Load only reads.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// Dataset is everything the telemetry UI aggregates. Missing lists the tables
// that are absent (an unmigrated database); Trajectories is then empty.
type Dataset struct {
	Trajectories []Trajectory
	Now          time.Time
	Missing      []string
}

// Trajectory is one trajectory with what the UI measures of it. A nil pointer
// is not measured, never zero.
type Trajectory struct {
	ID        int64
	Repo      string
	Issue     int
	PlanTasks *int
	Route     *string
	Area      *string
	// PRSize is the class of the largest PR: PRBelow, PRAbove, or "" when
	// unknown.
	PRSize string
	// Lane is LaneOf the setups, Model the dominant model, Harness the one
	// harness of the setups and sessions ("mixed" for more than one),
	// Assignment AssignmentOf the runs and sessions.
	Lane, Model, Harness, Assignment string
	// Cost is the sum of usage.cost: nil without usage rows or with an
	// unpriced row. CostKind is "", estimate, quota, actual or mixed.
	Cost               *float64
	CostKind           string
	HasSetup, HasUsage bool
	Reviews            []Review
	Findings           []Finding
	Recoveries         []Recovery
	CreatedAt          time.Time
	// Closed: at least one recovery and none open; ClosedAt is the last merge.
	Closed    bool
	ClosedAt  *time.Time
	Questions QuestionCounts
	// Active and HumanWait come from the timeline, only for a closed
	// trajectory with at least one run or session.
	Active, HumanWait *time.Duration
	Runs, Sessions    int
}

// Review is one linked review_runs row.
type Review struct {
	Component    string
	RanAt        time.Time
	Outcome      string
	FindingCount int
}

// Finding is one findings row. ReviewRunID is the review_runs row of the same
// record, 0 when there is none (a ledger finding).
type Finding struct {
	Component, Key, Severity, Decision, RecordPath string
	ReviewRunID                                    int64
}

// RecoveryItem is one repair after a merge.
type RecoveryItem = telemetry.RecoveryItem

// Recovery is the recovery measurement of one PR. CodeLines nil: not measured.
type Recovery struct {
	PR        int
	Status    string
	MergedAt  time.Time
	Items     []RecoveryItem
	CodeLines *int
}

// QuestionCounts splits the questions of a trajectory by source: a run of the
// dispatcher or an interactive session.
type QuestionCounts struct {
	Dispatcher int `json:"dispatcher"`
	Transcript int `json:"transcript"`
}

// Cohorts holds, per repo and component, the ran_at of the first linked
// review_runs row of that component.
type Cohorts map[string]map[string]time.Time

// Placements of a trajectory for a review component.
const (
	PlaceWith         = "with"
	PlaceWithout      = "without"
	PlaceBeforeCohort = "before_cohort"
)

// Cohorts returns the cohort start of every repo and component.
func (d *Dataset) Cohorts() Cohorts {
	c := Cohorts{}
	for i := range d.Trajectories {
		t := &d.Trajectories[i]
		for _, r := range t.Reviews {
			if c[t.Repo] == nil {
				c[t.Repo] = map[string]time.Time{}
			}
			if start, ok := c[t.Repo][r.Component]; !ok || r.RanAt.Before(start) {
				c[t.Repo][r.Component] = r.RanAt
			}
		}
	}
	return c
}

// Place puts t on a side of the comparison of a review component: with when
// it has a review of that component; without when it has none and was
// created at or after the cohort start of its repo; otherwise before_cohort.
// The population itself (a plan) is the caller's.
func (c Cohorts) Place(t *Trajectory, component string) string {
	if t.HasReview(component) {
		return PlaceWith
	}
	start, ok := c[t.Repo][component]
	if !ok || t.CreatedAt.Before(start) {
		return PlaceBeforeCohort
	}
	return PlaceWithout
}

// HasReview reports whether t has a linked review of component.
func (t *Trajectory) HasReview(component string) bool {
	for _, r := range t.Reviews {
		if r.Component == component {
			return true
		}
	}
	return false
}

// tables are the tables Load reads.
var tables = []string{
	"trajectories", "runs", "sessions", "setups", "usage", "findings",
	"recoveries", "recovery_items", "trajectory_events", "review_runs",
}

// Load reads every trajectory at now. A missing table gives a Dataset with
// Missing set and no error.
func Load(ctx context.Context, db *sql.DB, now time.Time) (*Dataset, error) {
	d := &Dataset{Now: now}
	missing, err := missingTables(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		d.Missing = missing
		return d, nil
	}
	l := loader{ctx: ctx, db: db, byID: map[int64]*acc{}}
	steps := []func() error{
		l.trajectories, l.runs, l.sessions, l.setups, l.usage, l.reviews,
		l.findings, l.recoveries, l.questions,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	d.Trajectories = make([]Trajectory, 0, len(l.order))
	for _, a := range l.order {
		a.finish()
		if a.t.Closed && a.t.Runs+a.t.Sessions > 0 {
			tl, err := telemetry.LoadTimeline(ctx, db, a.t.ID, *a.t.ClosedAt, now)
			if err != nil {
				return nil, err
			}
			active := tl.ByCause[telemetry.CauseActive].Duration
			human := tl.ByCause[telemetry.CauseHuman].Duration
			a.t.Active, a.t.HumanWait = &active, &human
		}
		d.Trajectories = append(d.Trajectories, a.t)
	}
	return d, nil
}

func missingTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return nil, fmt.Errorf("report: list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("report: scan table: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report: list tables: %w", err)
	}
	var missing []string
	for _, name := range tables {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

// acc gathers the rows of one trajectory before finish folds them.
type acc struct {
	t           Trajectory
	lanes       []string
	assignments []string
	harnesses   map[string]bool
	setupModels map[string]bool
	byModel     map[string]ModelUse
	unpriced    bool
	costSum     float64
	costKinds   map[string]bool
}

func (a *acc) finish() {
	t := &a.t
	t.Lane = LaneOf(a.lanes)
	t.Assignment = AssignmentOf(a.assignments)
	switch len(a.harnesses) {
	case 0:
	case 1:
		for h := range a.harnesses {
			t.Harness = h
		}
	default:
		t.Harness = "mixed"
	}
	if len(a.byModel) > 0 {
		t.Model = DominantModel(a.byModel)
	} else if len(a.setupModels) == 1 {
		for m := range a.setupModels {
			t.Model = m
		}
	}
	if t.HasUsage && !a.unpriced {
		sum := a.costSum
		t.Cost = &sum
		if len(a.costKinds) == 1 {
			for k := range a.costKinds {
				t.CostKind = k
			}
		} else {
			t.CostKind = "mixed"
		}
	}
	t.PRSize = prSize(t.Recoveries)
	if len(t.Recoveries) > 0 {
		t.Closed = true
		var last time.Time
		for _, r := range t.Recoveries {
			if r.Status == string(telemetry.RecoveryOpen) {
				t.Closed = false
			}
			if r.MergedAt.After(last) {
				last = r.MergedAt
			}
		}
		if t.Closed {
			t.ClosedAt = &last
		}
	}
}

// prSize classes the largest PR. An unmeasured PR makes the class unknown,
// unless a measured PR is already above the threshold.
func prSize(recs []Recovery) string {
	largest, unknown := -1, false
	for _, r := range recs {
		if r.CodeLines == nil {
			unknown = true
			continue
		}
		largest = max(largest, *r.CodeLines)
	}
	if largest >= 0 && PRSizeClass(largest) == PRAbove {
		return PRAbove
	}
	if unknown || largest < 0 {
		return ""
	}
	return PRBelow
}

type loader struct {
	ctx   context.Context
	db    *sql.DB
	byID  map[int64]*acc
	order []*acc
}

// each runs query and calls scan per row.
func (l *loader) each(what, query string, scan func(*sql.Rows) error) error {
	rows, err := l.db.QueryContext(l.ctx, query)
	if err != nil {
		return fmt.Errorf("report: read %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("report: scan %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("report: read %s: %w", what, err)
	}
	return nil
}

// of returns the accumulator of trajectory id, nil for an unknown id.
func (l *loader) of(id sql.NullInt64) *acc {
	if !id.Valid {
		return nil
	}
	return l.byID[id.Int64]
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}

func (l *loader) trajectories() error {
	return l.each("trajectories", `SELECT id, repo, issue, plan_tasks, route, area, created_at
		FROM trajectories ORDER BY repo, issue, id`, func(rows *sql.Rows) error {
		var t Trajectory
		var plan sql.NullInt64
		var route, area sql.NullString
		var created string
		if err := rows.Scan(&t.ID, &t.Repo, &t.Issue, &plan, &route, &area, &created); err != nil {
			return err
		}
		if plan.Valid {
			n := int(plan.Int64)
			t.PlanTasks = &n
		}
		if route.Valid {
			t.Route = &route.String
		}
		if area.Valid {
			t.Area = &area.String
		}
		var err error
		if t.CreatedAt, err = parseTime(created); err != nil {
			return err
		}
		a := &acc{t: t, harnesses: map[string]bool{}, setupModels: map[string]bool{},
			byModel: map[string]ModelUse{}, costKinds: map[string]bool{}}
		l.byID[t.ID] = a
		l.order = append(l.order, a)
		return nil
	})
}

func (l *loader) runs() error {
	return l.each("runs", `SELECT trajectory_id, assignment FROM runs WHERE trajectory_id IS NOT NULL`,
		func(rows *sql.Rows) error {
			var tid sql.NullInt64
			var assignment string
			if err := rows.Scan(&tid, &assignment); err != nil {
				return err
			}
			if a := l.of(tid); a != nil {
				a.t.Runs++
				a.assignments = append(a.assignments, assignment)
			}
			return nil
		})
}

func (l *loader) sessions() error {
	return l.each("sessions", `SELECT trajectory_id, assignment, harness FROM sessions WHERE trajectory_id IS NOT NULL`,
		func(rows *sql.Rows) error {
			var tid sql.NullInt64
			var assignment, harness string
			if err := rows.Scan(&tid, &assignment, &harness); err != nil {
				return err
			}
			if a := l.of(tid); a != nil {
				a.t.Sessions++
				a.assignments = append(a.assignments, assignment)
				if harness != "" {
					a.harnesses[harness] = true
				}
			}
			return nil
		})
}

// ownerTrajectory is the trajectory of the run or session a row belongs to.
const ownerTrajectory = `COALESCE(r.trajectory_id, s.trajectory_id)`

func (l *loader) setups() error {
	return l.each("setups", `SELECT `+ownerTrajectory+`, x.lane, x.model, x.harness FROM setups x
		LEFT JOIN runs r ON r.id = x.run_id LEFT JOIN sessions s ON s.id = x.session_id`,
		func(rows *sql.Rows) error {
			var tid sql.NullInt64
			var lane, model, harness sql.NullString
			if err := rows.Scan(&tid, &lane, &model, &harness); err != nil {
				return err
			}
			a := l.of(tid)
			if a == nil {
				return nil
			}
			a.t.HasSetup = true
			a.lanes = append(a.lanes, lane.String)
			if model.String != "" {
				a.setupModels[model.String] = true
			}
			if harness.String != "" {
				a.harnesses[harness.String] = true
			}
			return nil
		})
}

func (l *loader) usage() error {
	return l.each("usage", `SELECT `+ownerTrajectory+`, x.model,
		x.input_tokens + x.output_tokens + x.cache_read_tokens + x.cache_write_tokens, x.cost, x.cost_kind
		FROM usage x LEFT JOIN runs r ON r.id = x.run_id LEFT JOIN sessions s ON s.id = x.session_id`,
		func(rows *sql.Rows) error {
			var tid sql.NullInt64
			var model string
			var tokens int64
			var cost sql.NullFloat64
			var kind sql.NullString
			if err := rows.Scan(&tid, &model, &tokens, &cost, &kind); err != nil {
				return err
			}
			a := l.of(tid)
			if a == nil {
				return nil
			}
			a.t.HasUsage = true
			u := a.byModel[model]
			u.Tokens += tokens
			if cost.Valid {
				a.costSum += cost.Float64
				a.costKinds[kind.String] = true
				c := cost.Float64
				if u.Cost != nil {
					c += *u.Cost
				}
				u.Cost = &c
			} else {
				a.unpriced = true
			}
			a.byModel[model] = u
			return nil
		})
}

func (l *loader) reviews() error {
	return l.each("review_runs", `SELECT trajectory_id, component, ran_at, outcome, finding_count
		FROM review_runs WHERE trajectory_id IS NOT NULL ORDER BY ran_at, id`, func(rows *sql.Rows) error {
		var tid sql.NullInt64
		var r Review
		var ranAt string
		var outcome sql.NullString
		if err := rows.Scan(&tid, &r.Component, &ranAt, &outcome, &r.FindingCount); err != nil {
			return err
		}
		a := l.of(tid)
		if a == nil {
			return nil
		}
		var err error
		if r.RanAt, err = parseTime(ranAt); err != nil {
			return err
		}
		r.Outcome = outcome.String
		a.t.Reviews = append(a.t.Reviews, r)
		return nil
	})
}

func (l *loader) findings() error {
	return l.each("findings", `SELECT f.trajectory_id, f.component, f.finding_key, f.severity, f.decision, f.record_path,
		(SELECT min(rr.id) FROM review_runs rr WHERE rr.trajectory_id = f.trajectory_id
			AND rr.component = f.component AND rr.record_path = f.record_path)
		FROM findings f ORDER BY f.id`, func(rows *sql.Rows) error {
		var tid, run sql.NullInt64
		var f Finding
		var decision sql.NullString
		if err := rows.Scan(&tid, &f.Component, &f.Key, &f.Severity, &decision, &f.RecordPath, &run); err != nil {
			return err
		}
		if a := l.of(tid); a != nil {
			f.Decision, f.ReviewRunID = decision.String, run.Int64
			a.t.Findings = append(a.t.Findings, f)
		}
		return nil
	})
}

func (l *loader) recoveries() error {
	type key struct {
		tid int64
		idx int
	}
	byRecovery := map[int64]key{}
	err := l.each("recoveries", `SELECT id, trajectory_id, pr, status, merged_at, code_lines
		FROM recoveries ORDER BY merged_at, pr`, func(rows *sql.Rows) error {
		var id int64
		var tid sql.NullInt64
		var r Recovery
		var merged string
		var lines sql.NullInt64
		if err := rows.Scan(&id, &tid, &r.PR, &r.Status, &merged, &lines); err != nil {
			return err
		}
		a := l.of(tid)
		if a == nil {
			return nil
		}
		var err error
		if r.MergedAt, err = parseTime(merged); err != nil {
			return err
		}
		if lines.Valid {
			n := int(lines.Int64)
			r.CodeLines = &n
		}
		byRecovery[id] = key{tid.Int64, len(a.t.Recoveries)}
		a.t.Recoveries = append(a.t.Recoveries, r)
		return nil
	})
	if err != nil {
		return err
	}
	return l.each("recovery_items", `SELECT recovery_id, kind, ref, at FROM recovery_items ORDER BY at, id`,
		func(rows *sql.Rows) error {
			var rid int64
			var it RecoveryItem
			var when string
			if err := rows.Scan(&rid, &it.Kind, &it.Ref, &when); err != nil {
				return err
			}
			k, ok := byRecovery[rid]
			if !ok {
				return nil
			}
			var err error
			if it.At, err = parseTime(when); err != nil {
				return err
			}
			rec := &l.byID[k.tid].t.Recoveries[k.idx]
			rec.Items = append(rec.Items, it)
			return nil
		})
}

func (l *loader) questions() error {
	return l.each("trajectory_events", `SELECT trajectory_id, run_id IS NOT NULL FROM trajectory_events
		WHERE kind = 'question'`, func(rows *sql.Rows) error {
		var tid sql.NullInt64
		var dispatcher bool
		if err := rows.Scan(&tid, &dispatcher); err != nil {
			return err
		}
		if a := l.of(tid); a != nil {
			if dispatcher {
				a.t.Questions.Dispatcher++
			} else {
				a.t.Questions.Transcript++
			}
		}
		return nil
	})
}
