// Package runs keeps the run state of the dispatcher: which issue or schedule
// has an open run, and how often a trigger has been tried.
package runs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Outcome is how a run ended.
type Outcome string

// The outcomes a run can end with.
const (
	StartFailed Outcome = "start_failed"
	Handoff     Outcome = "handoff"
	Question    Outcome = "question"
	Parked      Outcome = "parked"
	Done        Outcome = "done"
	Returned    Outcome = "returned"
	// Failed and Retry end only a scheduled run: Failed is a real outcome that
	// settles the slot, Retry a temporary failure that leaves it open.
	Failed Outcome = "failed"
	Retry  Outcome = "retry"
)

// TriggerScheduled is the trigger of a scheduled run; its trigger key is
// "scheduled:<schedule>:<slot>".
const TriggerScheduled = "scheduled"

var (
	// ErrClaimed: the issue or schedule already has an open run.
	ErrClaimed = errors.New("runs: issue or schedule already has an open run")
	// ErrTriggerSpent: the trigger already started a run, or failed twice.
	ErrTriggerSpent = errors.New("runs: trigger already spent")
	// ErrNotOpen: the run is already closed, or does not exist.
	ErrNotOpen = errors.New("runs: run is not open")
)

// Claim describes the run to start. Mode is "start" or "resume". Exactly one
// of Issue and Schedule is set: Schedule with trigger scheduled, Issue with
// every other trigger.
type Claim struct {
	Repo       string
	Issue      int    // 0 for a scheduled run
	Schedule   string // "" for an issue run
	SessionID  string
	Mode       string
	Trigger    string // handoff, answer, usage, closecheck, manual, fixpr or scheduled
	TriggerKey string // unique per repo, e.g. "start:<issue>"
	Command    string
	Prompt     string
	Profile    string
	Node       string // "" is self
	Branch     string // written at claim time, so a run that closes before launch still has it
	StartSHA   string // scheduled run: the commit the run started from
	Verdict    string // scheduled run: the job's verdict as opaque JSON
	Now        time.Time
}

// Run identifies a claimed run.
type Run struct {
	ID      int64
	Attempt int
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Take claims (repo, issue) or (repo, schedule) in one immediate
// transaction. For an issue run a trigger that ran before is spent, except
// after a start_failed: that gets one more attempt. For a scheduled run every
// earlier run of the trigger key is an attempt: the key is spent after a done
// or failed, or after two attempts.
func Take(ctx context.Context, db *sql.DB, c Claim) (Run, error) {
	scheduled := c.Trigger == TriggerScheduled
	if scheduled != (c.Schedule != "") || scheduled == (c.Issue != 0) {
		return Run{}, fmt.Errorf("runs: claim needs a schedule with trigger %s and an issue otherwise; got issue %d, schedule %q, trigger %q",
			TriggerScheduled, c.Issue, c.Schedule, c.Trigger)
	}
	if scheduled {
		prefix := TriggerScheduled + ":" + c.Schedule + ":"
		if !strings.HasPrefix(c.TriggerKey, prefix) || len(c.TriggerKey) == len(prefix) {
			return Run{}, fmt.Errorf("runs: scheduled claim of %q needs trigger key %s<slot>; got %q", c.Schedule, prefix, c.TriggerKey)
		}
	}
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate: BEGIN IMMEDIATE
	if err != nil {
		return Run{}, fmt.Errorf("runs: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total, failed, settled int
	err = tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(outcome = 'start_failed'), 0),
		coalesce(sum(outcome IN ('done', 'failed')), 0)
		FROM runs WHERE repo = ? AND trigger_key = ?`, c.Repo, c.TriggerKey).Scan(&total, &failed, &settled)
	if err != nil {
		return Run{}, fmt.Errorf("runs: count trigger: %w", err)
	}
	if scheduled {
		// Every earlier run is an attempt; an open one is left to the
		// runs_open_schedule index, which reports ErrClaimed.
		failed = total
		if settled > 0 {
			return Run{}, ErrTriggerSpent
		}
	}
	if total != failed || failed >= 2 {
		return Run{}, ErrTriggerSpent
	}
	attempt := failed + 1

	var issue, schedule, trajectory any
	if scheduled {
		schedule = c.Schedule
	} else {
		issue = c.Issue
		// Inside the transaction: a refused claim (ErrClaimed) rolls the trajectory back too.
		var tid int64
		if _, err := tx.ExecContext(ctx, `INSERT INTO trajectories (repo, issue, created_at) VALUES (?, ?, ?)
			ON CONFLICT (repo, issue) DO NOTHING`, c.Repo, c.Issue, stamp(c.Now)); err != nil {
			return Run{}, fmt.Errorf("runs: insert trajectory: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM trajectories WHERE repo = ? AND issue = ?`,
			c.Repo, c.Issue).Scan(&tid); err != nil {
			return Run{}, fmt.Errorf("runs: select trajectory: %w", err)
		}
		trajectory = tid
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO runs
		(repo, issue, schedule, session_id, mode, trigger, trigger_key, attempt, command, prompt, profile, node, branch,
		start_sha, verdict, started_at, trajectory_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Repo, issue, schedule, c.SessionID, c.Mode, c.Trigger, c.TriggerKey, attempt,
		c.Command, c.Prompt, c.Profile, c.Node, c.Branch, c.StartSHA, c.Verdict, stamp(c.Now), trajectory)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return Run{}, ErrClaimed
		}
		return Run{}, fmt.Errorf("runs: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Run{}, fmt.Errorf("runs: row id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("runs: commit: %w", err)
	}
	return Run{ID: id, Attempt: attempt}, nil
}

// Release closes an open run with an outcome. A scheduled run ends only as done, failed or
// retry: failed and retry are only for a scheduled run, and start_failed, handoff, question,
// parked and returned only for an issue run. An outcome of the other kind is an error and the
// run stays open.
func Release(ctx context.Context, db *sql.DB, id int64, o Outcome, reason string, now time.Time) error {
	var kind, want string // kind narrows the UPDATE; want names the run kind the outcome is for
	switch o {
	case Done:
	case StartFailed, Handoff, Question, Parked, Returned:
		kind, want = " AND schedule IS NULL", "an issue run"
	case Failed, Retry:
		kind, want = " AND schedule IS NOT NULL", "a scheduled run"
	default:
		return fmt.Errorf("runs: unknown outcome %q", o)
	}
	res, err := db.ExecContext(ctx, `UPDATE runs SET outcome = ?, reason = ?, ended_at = ?
		WHERE id = ? AND ended_at IS NULL`+kind, string(o), reason, stamp(now), id)
	if err != nil {
		return fmt.Errorf("runs: release: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("runs: release rows: %w", err)
	}
	if n > 0 {
		return nil
	}
	if want != "" {
		var open bool
		err := db.QueryRowContext(ctx, `SELECT count(*) > 0 FROM runs WHERE id = ? AND ended_at IS NULL`, id).Scan(&open)
		if err != nil {
			return fmt.Errorf("runs: release run %d: %w", id, err)
		}
		if open {
			return fmt.Errorf("runs: outcome %q is only for %s; run %d is not one", o, want, id)
		}
	}
	return ErrNotOpen
}

// SetWorkplace records the workspace and branch of an open run.
func SetWorkplace(ctx context.Context, db *sql.DB, id int64, workspaceID, branch string) error {
	return setOpen(ctx, db, id, `UPDATE runs SET workspace_id = ?, branch = ?
		WHERE id = ? AND ended_at IS NULL`, workspaceID, branch, id)
}

// SetSession records the workplace session key of an open run.
func SetSession(ctx context.Context, db *sql.DB, id int64, key string) error {
	return setOpen(ctx, db, id, `UPDATE runs SET workplace_session = ?
		WHERE id = ? AND ended_at IS NULL`, key, id)
}

// setOpen runs an UPDATE that only matches an open row; no match is ErrNotOpen.
func setOpen(ctx context.Context, db *sql.DB, id int64, query string, args ...any) error {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("runs: update run %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("runs: update run %d rows: %w", id, err)
	}
	if n == 0 {
		return ErrNotOpen
	}
	return nil
}

// SetStartSHA records the commit an open scheduled run started from.
func SetStartSHA(ctx context.Context, db *sql.DB, id int64, sha string) error {
	return setOpen(ctx, db, id, `UPDATE runs SET start_sha = ? WHERE id = ? AND ended_at IS NULL`, sha, id)
}

// SetVerdict records the opaque verdict JSON of an open scheduled run.
func SetVerdict(ctx context.Context, db *sql.DB, id int64, verdict string) error {
	return setOpen(ctx, db, id, `UPDATE runs SET verdict = ? WHERE id = ? AND ended_at IS NULL`, verdict, id)
}

// SetPRAt records when the PR of an open scheduled run was opened.
func SetPRAt(ctx context.Context, db *sql.DB, id int64, at time.Time) error {
	return setOpen(ctx, db, id, `UPDATE runs SET pr_at = ? WHERE id = ? AND ended_at IS NULL`, stamp(at), id)
}

// NoteMergeFailed records on an open run the head sha whose merge GitHub
// refused, and GitHub's message. It returns ErrNotOpen for a closed or unknown run.
func NoteMergeFailed(ctx context.Context, db *sql.DB, id int64, sha, message string) error {
	return setOpen(ctx, db, id, `UPDATE runs SET merge_sha = ?, merge_error = ?
		WHERE id = ? AND ended_at IS NULL`, sha, message, id)
}

// SetPushed marks an open push row with the SHA the pusher pushed: its push phase is over. It
// returns ErrNotOpen for a closed or unknown run.
func SetPushed(ctx context.Context, db *sql.DB, id int64, sha string) error {
	return setOpen(ctx, db, id, `UPDATE runs SET pushed_sha = ?
		WHERE id = ? AND ended_at IS NULL`, sha, id)
}

// OpenRun is an open run with its workplace columns.
type OpenRun struct {
	ID               int64     `json:"id"`
	Issue            int       `json:"issue"`              // 0 for a scheduled run
	Schedule         string    `json:"schedule,omitempty"` // "" for an issue run
	SessionID        string    `json:"session_id"`
	TriggerKey       string    `json:"trigger_key"`
	Command          string    `json:"command,omitempty"`
	Profile          string    `json:"profile"`
	Node             string    `json:"node,omitempty"`
	Branch           string    `json:"branch"`
	WorkspaceID      string    `json:"workspace_id"`
	WorkplaceSession string    `json:"workplace_session"`
	StartedAt        time.Time `json:"started_at"`
	MergeSHA         string    `json:"merge_sha,omitempty"`
	MergeError       string    `json:"merge_error,omitempty"`
	PushedSHA        string    `json:"pushed_sha,omitempty"` // push row: the SHA the pusher pushed
	StartSHA         string    `json:"start_sha,omitempty"`
	Verdict          string    `json:"verdict,omitempty"` // opaque JSON of the scheduled job's verdict
	PRAt             time.Time `json:"pr_at,omitzero"`    // zero: no PR yet
}

// Open returns the open runs of repo, ordered by id.
func Open(ctx context.Context, db *sql.DB, repo string) ([]OpenRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+openColumns+`
		FROM runs WHERE repo = ? AND ended_at IS NULL ORDER BY id`, repo)
	if err != nil {
		return nil, fmt.Errorf("runs: open runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []OpenRun
	for rows.Next() {
		r, err := scanOpen(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: open runs: %w", err)
	}
	return out, nil
}

// openColumns are the runs columns scanOpen reads, in order.
const openColumns = `id, coalesce(issue, 0), coalesce(schedule, ''), session_id, trigger_key, command, profile, node, branch,
	workspace_id, workplace_session, started_at, merge_sha, merge_error, pushed_sha, start_sha, verdict, coalesce(pr_at, '')`

// scanOpen reads a row selected with openColumns, after any lead destinations
// for columns selected before them.
func scanOpen(rows interface{ Scan(...any) error }, lead ...any) (OpenRun, error) {
	var r OpenRun
	var started, prAt string
	dest := append(lead, &r.ID, &r.Issue, &r.Schedule, &r.SessionID, &r.TriggerKey, &r.Command, &r.Profile, &r.Node, &r.Branch,
		&r.WorkspaceID, &r.WorkplaceSession, &started, &r.MergeSHA, &r.MergeError, &r.PushedSHA, &r.StartSHA, &r.Verdict, &prAt)
	if err := rows.Scan(dest...); err != nil {
		return OpenRun{}, fmt.Errorf("runs: scan open run: %w", err)
	}
	var err error
	if r.StartedAt, r.PRAt, err = parseTimes(r.ID, started, prAt); err != nil {
		return OpenRun{}, err
	}
	return r, nil
}

// parseTimes parses the started_at and the optional pr_at ("" is none) of run id.
func parseTimes(id int64, started, prAt string) (time.Time, time.Time, error) {
	s, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("runs: run %d started_at %q: %w", id, started, err)
	}
	if prAt == "" {
		return s, time.Time{}, nil
	}
	p, err := time.Parse(time.RFC3339, prAt)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("runs: run %d pr_at %q: %w", id, prAt, err)
	}
	return s, p, nil
}

// SessionKeys maps the workplace session key of every run of the issue, open
// and closed, to that run's session_id: the forge sessions lighthouse started
// for it. It keys on the forge key, not on session_id, because a resume run
// gets a new row with the same session_id: a late agent of a resume whose
// start failed must not pass for the earlier run's session. The session_id
// lets the caller reject a key whose forge session turned out to be another
// agent's. A run without a key (never accepted, or returned as uncertain)
// adds nothing, so its late agent counts as foreign.
func SessionKeys(ctx context.Context, db *sql.DB, repo string, issue int) (map[string]string, error) {
	return sessionKeys(ctx, db, `SELECT workplace_session, session_id FROM runs
		WHERE repo = ? AND issue = ? AND workplace_session != ''`, repo, issue)
}

// ScheduleSessionKeys is SessionKeys for the runs of schedule in repo.
func ScheduleSessionKeys(ctx context.Context, db *sql.DB, repo, schedule string) (map[string]string, error) {
	return sessionKeys(ctx, db, `SELECT workplace_session, session_id FROM runs
		WHERE repo = ? AND schedule = ? AND workplace_session != ''`, repo, schedule)
}

func sessionKeys(ctx context.Context, db *sql.DB, query string, args ...any) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("runs: session keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var key, sessionID string
		if err := rows.Scan(&key, &sessionID); err != nil {
			return nil, fmt.Errorf("runs: scan session key: %w", err)
		}
		out[key] = sessionID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: session keys: %w", err)
	}
	return out, nil
}

// NewSessionID returns a random UUID v4, lower case.
func NewSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
