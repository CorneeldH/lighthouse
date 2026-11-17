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
	Issue      int    // 0 for a scheduled run
	Schedule   string // "" for an issue run
	SessionID  string
	Mode       string
	Trigger    string
	TriggerKey string
	Attempt    int
	Command    string
	Prompt     string
	Branch     string
	Profile    string
	Node       string // "" is self
	StartedAt  time.Time
	Open       bool
	Outcome    Outcome // "" while the run is open
	Reason     string
	MergeSHA   string // head sha of the last refused merge, "" if none
	MergeError string // GitHub's message for that refusal
	StartSHA   string
	Verdict    string    // opaque JSON of the scheduled job's verdict
	PRAt       time.Time // zero: no PR yet
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
const rowColumns = `id, coalesce(issue, 0), coalesce(schedule, ''), session_id, mode, trigger, trigger_key, attempt,
	command, prompt, branch, profile, node, started_at, ended_at IS NULL, coalesce(outcome, ''), coalesce(reason, ''),
	merge_sha, merge_error, start_sha, verdict, coalesce(pr_at, '')`

// scanRow reads a row selected with rowColumns, after any lead destinations
// for columns selected before them.
func scanRow(rows interface{ Scan(...any) error }, lead ...any) (Row, error) {
	var r Row
	var started, outcome, prAt string
	dest := append(lead, &r.ID, &r.Issue, &r.Schedule, &r.SessionID, &r.Mode, &r.Trigger, &r.TriggerKey, &r.Attempt,
		&r.Command, &r.Prompt, &r.Branch, &r.Profile, &r.Node, &started, &r.Open, &outcome, &r.Reason, &r.MergeSHA, &r.MergeError,
		&r.StartSHA, &r.Verdict, &prAt)
	if err := rows.Scan(dest...); err != nil {
		return Row{}, fmt.Errorf("runs: scan history row: %w", err)
	}
	r.Outcome = Outcome(outcome)
	var err error
	if r.StartedAt, r.PRAt, err = parseTimes(r.ID, started, prAt); err != nil {
		return Row{}, err
	}
	return r, nil
}
