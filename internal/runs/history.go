package runs

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Row is one run as the history shows it, open or closed.
type Row struct {
	ID         int64
	Issue      int
	SessionID  string
	Mode       string
	Trigger    string
	TriggerKey string
	Attempt    int
	Command    string
	Prompt     string
	Branch     string
	Profile    string
	StartedAt  time.Time
	Open       bool
	Outcome    Outcome // "" while the run is open
	Reason     string
	MergeSHA   string // head sha of the last refused merge, "" if none
	MergeError string // GitHub's message for that refusal
}

// History returns every run of repo, open and closed, ordered by id.
func History(ctx context.Context, db *sql.DB, repo string) ([]Row, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+rowColumns+` FROM runs WHERE repo = ? ORDER BY id`, repo)
	if err != nil {
		return nil, fmt.Errorf("runs: history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Row
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: history: %w", err)
	}
	return out, nil
}

// rowColumns are the runs columns scanRow reads, in order.
const rowColumns = `id, issue, session_id, mode, trigger, trigger_key, attempt,
	command, prompt, branch, profile, started_at, ended_at IS NULL, coalesce(outcome, ''), coalesce(reason, ''),
	merge_sha, merge_error`

// scanRow reads a row selected with rowColumns, after any lead destinations
// for columns selected before them.
func scanRow(rows interface{ Scan(...any) error }, lead ...any) (Row, error) {
	var r Row
	var started, outcome string
	dest := append(lead, &r.ID, &r.Issue, &r.SessionID, &r.Mode, &r.Trigger, &r.TriggerKey, &r.Attempt,
		&r.Command, &r.Prompt, &r.Branch, &r.Profile, &started, &r.Open, &outcome, &r.Reason, &r.MergeSHA, &r.MergeError)
	if err := rows.Scan(dest...); err != nil {
		return Row{}, fmt.Errorf("runs: scan history row: %w", err)
	}
	r.Outcome = Outcome(outcome)
	var err error
	r.StartedAt, err = time.Parse(time.RFC3339, started)
	if err != nil {
		return Row{}, fmt.Errorf("runs: run %d started_at %q: %w", r.ID, started, err)
	}
	return r, nil
}
