// Package runs keeps the run state of the dispatcher: which issue has an
// open run, and how often a trigger has been tried.
package runs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
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
)

var (
	// ErrClaimed: the issue already has an open run.
	ErrClaimed = errors.New("runs: issue already has an open run")
	// ErrTriggerSpent: the trigger already started a run, or failed twice.
	ErrTriggerSpent = errors.New("runs: trigger already spent")
	// ErrNotOpen: the run is already closed, or does not exist.
	ErrNotOpen = errors.New("runs: run is not open")
)

// Claim describes the run to start. Mode is "start" or "resume".
type Claim struct {
	Repo       string
	Issue      int
	SessionID  string
	Mode       string
	Trigger    string // handoff, answer, usage, closecheck or manual
	TriggerKey string // unique per repo, e.g. "start:<issue>"
	Command    string
	Prompt     string
	Profile    string
	Now        time.Time
}

// Run identifies a claimed run.
type Run struct {
	ID      int64
	Attempt int
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Take claims (repo, issue) in one immediate transaction. A trigger that
// ran before is spent, except after a start_failed: that gets one more
// attempt.
func Take(ctx context.Context, db *sql.DB, c Claim) (Run, error) {
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate: BEGIN IMMEDIATE
	if err != nil {
		return Run{}, fmt.Errorf("runs: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total, failed int
	err = tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(outcome = 'start_failed'), 0)
		FROM runs WHERE repo = ? AND trigger_key = ?`, c.Repo, c.TriggerKey).Scan(&total, &failed)
	if err != nil {
		return Run{}, fmt.Errorf("runs: count trigger: %w", err)
	}
	if total != failed || failed >= 2 {
		return Run{}, ErrTriggerSpent
	}
	attempt := failed + 1

	res, err := tx.ExecContext(ctx, `INSERT INTO runs
		(repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt, profile, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Repo, c.Issue, c.SessionID, c.Mode, c.Trigger, c.TriggerKey, attempt,
		c.Command, c.Prompt, c.Profile, stamp(c.Now))
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

// Release closes an open run with an outcome.
func Release(ctx context.Context, db *sql.DB, id int64, o Outcome, reason string, now time.Time) error {
	switch o {
	case StartFailed, Handoff, Question, Parked, Done, Returned:
	default:
		return fmt.Errorf("runs: unknown outcome %q", o)
	}
	res, err := db.ExecContext(ctx, `UPDATE runs SET outcome = ?, reason = ?, ended_at = ?
		WHERE id = ? AND ended_at IS NULL`, string(o), reason, stamp(now), id)
	if err != nil {
		return fmt.Errorf("runs: release: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("runs: release rows: %w", err)
	}
	if n == 0 {
		return ErrNotOpen
	}
	return nil
}

// OpenRun is an open run with its workplace columns.
type OpenRun struct {
	ID               int64     `json:"id"`
	Issue            int       `json:"issue"`
	SessionID        string    `json:"session_id"`
	TriggerKey       string    `json:"trigger_key"`
	Profile          string    `json:"profile"`
	Branch           string    `json:"branch"`
	WorkspaceID      string    `json:"workspace_id"`
	WorkplaceSession string    `json:"workplace_session"`
	StartedAt        time.Time `json:"started_at"`
}

// Open returns the open runs of repo, ordered by id.
func Open(ctx context.Context, db *sql.DB, repo string) ([]OpenRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, issue, session_id, trigger_key, profile, branch,
		workspace_id, workplace_session, started_at
		FROM runs WHERE repo = ? AND ended_at IS NULL ORDER BY id`, repo)
	if err != nil {
		return nil, fmt.Errorf("runs: open runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []OpenRun
	for rows.Next() {
		var r OpenRun
		var started string
		if err := rows.Scan(&r.ID, &r.Issue, &r.SessionID, &r.TriggerKey, &r.Profile, &r.Branch,
			&r.WorkspaceID, &r.WorkplaceSession, &started); err != nil {
			return nil, fmt.Errorf("runs: scan open run: %w", err)
		}
		r.StartedAt, err = time.Parse(time.RFC3339, started)
		if err != nil {
			return nil, fmt.Errorf("runs: run %d started_at %q: %w", r.ID, started, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: open runs: %w", err)
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
