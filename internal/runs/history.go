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
}

// History returns every run of repo, open and closed, ordered by id.
func History(ctx context.Context, db *sql.DB, repo string) ([]Row, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, issue, session_id, mode, trigger, trigger_key, attempt,
		command, prompt, branch, profile, started_at, ended_at IS NULL, coalesce(outcome, ''), coalesce(reason, '')
		FROM runs WHERE repo = ? ORDER BY id`, repo)
	if err != nil {
		return nil, fmt.Errorf("runs: history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Row
	for rows.Next() {
		var r Row
		var started, outcome string
		if err := rows.Scan(&r.ID, &r.Issue, &r.SessionID, &r.Mode, &r.Trigger, &r.TriggerKey, &r.Attempt,
			&r.Command, &r.Prompt, &r.Branch, &r.Profile, &started, &r.Open, &outcome, &r.Reason); err != nil {
			return nil, fmt.Errorf("runs: scan history row: %w", err)
		}
		r.Outcome = Outcome(outcome)
		r.StartedAt, err = time.Parse(time.RFC3339, started)
		if err != nil {
			return nil, fmt.Errorf("runs: run %d started_at %q: %w", r.ID, started, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: history: %w", err)
	}
	return out, nil
}
