package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// triggerUsage is the trigger of a usage resume. It matches dispatch's
// constant for the same trigger; runs cannot import dispatch (import cycle).
const triggerUsage = "usage"

// HostRun is an open run with the repo it belongs to.
type HostRun struct {
	Repo string
	OpenRun
}

// OpenAll returns the open runs of every repo, ordered by id.
func OpenAll(ctx context.Context, db *sql.DB) ([]HostRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT repo, `+openColumns+`
		FROM runs WHERE ended_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("runs: open runs of all repos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []HostRun
	for rows.Next() {
		var repo string
		r, err := scanOpen(rows, &repo)
		if err != nil {
			return nil, err
		}
		out = append(out, HostRun{Repo: repo, OpenRun: r})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: open runs of all repos: %w", err)
	}
	return out, nil
}

// LastOfSession returns the repo and the last run (highest id) with session; ok is false when none.
func LastOfSession(ctx context.Context, db *sql.DB, session string) (repo string, row Row, ok bool, err error) {
	r := db.QueryRowContext(ctx, `SELECT repo, `+rowColumns+` FROM runs WHERE session_id = ? ORDER BY id DESC LIMIT 1`, session)
	row, err = scanRow(r, &repo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", Row{}, false, nil
	}
	if err != nil {
		return "", Row{}, false, err
	}
	return repo, row, true, nil
}

// Resumable reports whether last, the last run of a marker's session, is parked for the
// marker with usage key: closed as parked, or a first usage resume with key that failed to start.
func Resumable(last Row, key string) bool {
	if last.Open {
		return false
	}
	if last.Outcome == Parked {
		return true
	}
	return last.Trigger == triggerUsage && last.TriggerKey == key &&
		last.Outcome == StartFailed && last.Attempt == 1
}
