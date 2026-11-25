package report

// This file loads the detail of one trajectory (spec §7): its runs, sessions,
// reviews with their findings, recoveries, events and the time per cause.
// Only references are read: no text of a transcript, a question or a review.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// UsageRow is the use of one model by a run or session. Cost nil: unpriced.
type UsageRow struct {
	Model    string   `json:"model"`
	Tokens   int64    `json:"tokens"`
	Cost     *float64 `json:"cost"`
	CostKind string   `json:"cost_kind"`
}

// RunRow is one dispatcher run. EndedAt nil: still running.
type RunRow struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at"`
	Outcome    string     `json:"outcome"`
	Assignment string     `json:"assignment"`
	Usage      []UsageRow `json:"usage"`
}

// SessionRow is one interactive session. EndedAt nil: not ended.
type SessionRow struct {
	ID         int64      `json:"id"`
	Harness    string     `json:"harness"`
	Branch     string     `json:"branch"`
	LinkBasis  string     `json:"link_basis"`
	Assignment string     `json:"assignment"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at"`
	Usage      []UsageRow `json:"usage"`
}

// FindingRow is one finding by reference only. Decision "" is undecided.
type FindingRow struct {
	Component  string `json:"component"`
	Key        string `json:"key"`
	Severity   string `json:"severity"`
	Decision   string `json:"decision"`
	RecordPath string `json:"record_path"`
}

// ReviewRow is one linked review_runs row with the findings of its record.
type ReviewRow struct {
	ID           int64        `json:"id"`
	Component    string       `json:"component"`
	RecordPath   string       `json:"record_path"`
	RanAt        time.Time    `json:"ran_at"`
	Outcome      string       `json:"outcome"`
	FindingCount int          `json:"finding_count"`
	Findings     []FindingRow `json:"findings"`
}

// RecoveryItemRow is one repair after a merge: fix, revert or bug_issue, with
// its reference (a commit SHA or an issue number).
type RecoveryItemRow struct {
	Kind string    `json:"kind"`
	Ref  string    `json:"ref"`
	At   time.Time `json:"at"`
}

// RecoveryRow is the recovery measurement of one PR.
type RecoveryRow struct {
	PR       int               `json:"pr"`
	Status   string            `json:"status"`
	MergedAt time.Time         `json:"merged_at"`
	Items    []RecoveryItemRow `json:"items"`
}

// EventRow is one trajectory event; RunID or SessionID is its owner.
type EventRow struct {
	Kind      string    `json:"kind"`
	RunID     int64     `json:"run_id,omitempty"`
	SessionID int64     `json:"session_id,omitempty"`
	Ref       string    `json:"ref"`
	At        time.Time `json:"at"`
}

// Detail is one trajectory with everything the detail page shows. Findings
// holds the findings without a review_runs row of their record (a ledger).
// ByCause is in seconds per cause; every cause is nil without runs and
// sessions.
type Detail struct {
	ID         int64               `json:"id"`
	Repo       string              `json:"repo"`
	Issue      int                 `json:"issue"`
	Runs       []RunRow            `json:"runs"`
	Sessions   []SessionRow        `json:"sessions"`
	Reviews    []ReviewRow         `json:"reviews"`
	Findings   []FindingRow        `json:"findings"`
	Recoveries []RecoveryRow       `json:"recoveries"`
	Events     []EventRow          `json:"events"`
	Questions  QuestionCounts      `json:"questions"`
	ByCause    map[string]*float64 `json:"by_cause"`
}

// causes are the keys of Detail.ByCause.
var causes = []telemetry.Cause{
	telemetry.CauseActive, telemetry.CauseHuman, telemetry.CauseUsage,
	telemetry.CauseCIReview, telemetry.CausePickup, telemetry.CauseUnexplained,
}

// LoadDetail reads trajectory id at now; false when there is none.
func LoadDetail(ctx context.Context, db *sql.DB, id int64, now time.Time) (Detail, bool, error) {
	d := Detail{ID: id, Runs: []RunRow{}, Sessions: []SessionRow{}, Reviews: []ReviewRow{},
		Findings: []FindingRow{}, Recoveries: []RecoveryRow{}, Events: []EventRow{}, ByCause: map[string]*float64{}}
	err := db.QueryRowContext(ctx, `SELECT repo, issue FROM trajectories WHERE id = ?`, id).Scan(&d.Repo, &d.Issue)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, false, nil
	}
	if err != nil {
		return Detail{}, false, fmt.Errorf("report: read trajectory %d: %w", id, err)
	}
	l := detailLoader{loader: loader{ctx: ctx, db: db}, id: id, d: &d}
	steps := []func() error{l.runs, l.sessions, l.usage, l.reviews, l.findings, l.recoveries, l.events}
	for _, step := range steps {
		if err := step(); err != nil {
			return Detail{}, false, err
		}
	}
	for _, c := range causes {
		d.ByCause[string(c)] = nil
	}
	if len(d.Runs)+len(d.Sessions) > 0 {
		tl, err := telemetry.LoadTimeline(ctx, db, id, closedAt(d.Recoveries), now)
		if err != nil {
			return Detail{}, false, err
		}
		for _, c := range causes {
			s := tl.ByCause[c].Duration.Seconds()
			d.ByCause[string(c)] = &s
		}
	}
	return d, true, nil
}

// closedAt is the last merge when every recovery window is closed, otherwise
// zero (not closed).
func closedAt(recs []RecoveryRow) time.Time {
	var last time.Time
	for _, r := range recs {
		if r.Status == string(telemetry.RecoveryOpen) {
			return time.Time{}
		}
		if r.MergedAt.After(last) {
			last = r.MergedAt
		}
	}
	return last
}

type detailLoader struct {
	loader
	id int64
	d  *Detail
}

func (l *detailLoader) query(what, query string, scan func(*sql.Rows) error) error {
	rows, err := l.db.QueryContext(l.ctx, query, l.id)
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

func parseOptTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (l *detailLoader) runs() error {
	return l.query("runs", `SELECT id, started_at, ended_at, outcome, assignment FROM runs
		WHERE trajectory_id = ? ORDER BY started_at, id`, func(rows *sql.Rows) error {
		r := RunRow{Usage: []UsageRow{}}
		var started string
		var ended, outcome sql.NullString
		if err := rows.Scan(&r.ID, &started, &ended, &outcome, &r.Assignment); err != nil {
			return err
		}
		var err error
		if r.StartedAt, err = parseTime(started); err != nil {
			return err
		}
		if r.EndedAt, err = parseOptTime(ended); err != nil {
			return err
		}
		r.Outcome = outcome.String
		l.d.Runs = append(l.d.Runs, r)
		return nil
	})
}

func (l *detailLoader) sessions() error {
	return l.query("sessions", `SELECT id, harness, branch, link_basis, assignment, started_at, ended_at
		FROM sessions WHERE trajectory_id = ? ORDER BY started_at, id`, func(rows *sql.Rows) error {
		s := SessionRow{Usage: []UsageRow{}}
		var basis, ended sql.NullString
		var started string
		if err := rows.Scan(&s.ID, &s.Harness, &s.Branch, &basis, &s.Assignment, &started, &ended); err != nil {
			return err
		}
		var err error
		if s.StartedAt, err = parseTime(started); err != nil {
			return err
		}
		if s.EndedAt, err = parseOptTime(ended); err != nil {
			return err
		}
		s.LinkBasis = basis.String
		l.d.Sessions = append(l.d.Sessions, s)
		return nil
	})
}

func (l *detailLoader) usage() error {
	runs, sessions := map[int64]*RunRow{}, map[int64]*SessionRow{}
	for i := range l.d.Runs {
		runs[l.d.Runs[i].ID] = &l.d.Runs[i]
	}
	for i := range l.d.Sessions {
		sessions[l.d.Sessions[i].ID] = &l.d.Sessions[i]
	}
	return l.query("usage", `SELECT x.run_id, x.session_id, x.model,
		x.input_tokens + x.output_tokens + x.cache_read_tokens + x.cache_write_tokens, x.cost, x.cost_kind
		FROM usage x LEFT JOIN runs r ON r.id = x.run_id LEFT JOIN sessions s ON s.id = x.session_id
		WHERE `+ownerTrajectory+` = ? ORDER BY x.model, x.id`, func(rows *sql.Rows) error {
		var run, session sql.NullInt64
		var u UsageRow
		var cost sql.NullFloat64
		var kind sql.NullString
		if err := rows.Scan(&run, &session, &u.Model, &u.Tokens, &cost, &kind); err != nil {
			return err
		}
		if cost.Valid {
			c := cost.Float64
			u.Cost = &c
		}
		u.CostKind = kind.String
		if r := runs[run.Int64]; run.Valid && r != nil {
			r.Usage = append(r.Usage, u)
		} else if s := sessions[session.Int64]; session.Valid && s != nil {
			s.Usage = append(s.Usage, u)
		}
		return nil
	})
}

func (l *detailLoader) reviews() error {
	return l.query("review_runs", `SELECT id, component, record_path, ran_at, outcome, finding_count
		FROM review_runs WHERE trajectory_id = ? ORDER BY ran_at, id`, func(rows *sql.Rows) error {
		r := ReviewRow{Findings: []FindingRow{}}
		var ranAt string
		var outcome sql.NullString
		if err := rows.Scan(&r.ID, &r.Component, &r.RecordPath, &ranAt, &outcome, &r.FindingCount); err != nil {
			return err
		}
		var err error
		if r.RanAt, err = parseTime(ranAt); err != nil {
			return err
		}
		r.Outcome = outcome.String
		l.d.Reviews = append(l.d.Reviews, r)
		return nil
	})
}

// findings puts each finding under the first review_runs row of its
// component and record; the rest go to Detail.Findings.
func (l *detailLoader) findings() error {
	byRun := map[int64]*ReviewRow{}
	for i := range l.d.Reviews {
		byRun[l.d.Reviews[i].ID] = &l.d.Reviews[i]
	}
	return l.query("findings", `SELECT f.component, f.finding_key, f.severity, f.decision, f.record_path,
		(SELECT min(rr.id) FROM review_runs rr WHERE rr.trajectory_id = f.trajectory_id
			AND rr.component = f.component AND rr.record_path = f.record_path)
		FROM findings f WHERE f.trajectory_id = ? ORDER BY f.id`, func(rows *sql.Rows) error {
		var f FindingRow
		var decision sql.NullString
		var run sql.NullInt64
		if err := rows.Scan(&f.Component, &f.Key, &f.Severity, &decision, &f.RecordPath, &run); err != nil {
			return err
		}
		f.Decision = decision.String
		if r := byRun[run.Int64]; run.Valid && r != nil {
			r.Findings = append(r.Findings, f)
		} else {
			l.d.Findings = append(l.d.Findings, f)
		}
		return nil
	})
}

func (l *detailLoader) recoveries() error {
	byID := map[int64]int{}
	err := l.query("recoveries", `SELECT id, pr, status, merged_at FROM recoveries
		WHERE trajectory_id = ? ORDER BY merged_at, pr`, func(rows *sql.Rows) error {
		var id int64
		r := RecoveryRow{Items: []RecoveryItemRow{}}
		var merged string
		if err := rows.Scan(&id, &r.PR, &r.Status, &merged); err != nil {
			return err
		}
		var err error
		if r.MergedAt, err = parseTime(merged); err != nil {
			return err
		}
		byID[id] = len(l.d.Recoveries)
		l.d.Recoveries = append(l.d.Recoveries, r)
		return nil
	})
	if err != nil {
		return err
	}
	return l.query("recovery_items", `SELECT i.recovery_id, i.kind, i.ref, i.at FROM recovery_items i
		JOIN recoveries r ON r.id = i.recovery_id WHERE r.trajectory_id = ? ORDER BY i.at, i.id`,
		func(rows *sql.Rows) error {
			var rid int64
			var it RecoveryItemRow
			var when string
			if err := rows.Scan(&rid, &it.Kind, &it.Ref, &when); err != nil {
				return err
			}
			idx, ok := byID[rid]
			if !ok {
				return nil
			}
			var err error
			if it.At, err = parseTime(when); err != nil {
				return err
			}
			l.d.Recoveries[idx].Items = append(l.d.Recoveries[idx].Items, it)
			return nil
		})
}

func (l *detailLoader) events() error {
	return l.query("trajectory_events", `SELECT kind, run_id, session_id, ref, at FROM trajectory_events
		WHERE trajectory_id = ? ORDER BY at, id`, func(rows *sql.Rows) error {
		var e EventRow
		var run, session sql.NullInt64
		var when string
		if err := rows.Scan(&e.Kind, &run, &session, &e.Ref, &when); err != nil {
			return err
		}
		var err error
		if e.At, err = parseTime(when); err != nil {
			return err
		}
		e.RunID, e.SessionID = run.Int64, session.Int64
		if e.Kind == string(telemetry.EventQuestion) {
			if run.Valid {
				l.d.Questions.Dispatcher++
			} else {
				l.d.Questions.Transcript++
			}
		}
		l.d.Events = append(l.d.Events, e)
		return nil
	})
}
