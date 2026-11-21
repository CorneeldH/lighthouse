package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecoveryState is what the store holds of one measured PR.
type RecoveryState struct {
	PR       int
	Issue    int
	Status   RecoveryStatus
	MergedAt time.Time
}

// RecoveryStates lists the stored recoveries of repo (case-insensitive), by PR.
func RecoveryStates(ctx context.Context, db *sql.DB, repo string) ([]RecoveryState, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.pr, t.issue, r.status, r.merged_at
		FROM recoveries r JOIN trajectories t ON t.id = r.trajectory_id
		WHERE t.repo = ? COLLATE NOCASE ORDER BY r.pr`, repo)
	if err != nil {
		return nil, fmt.Errorf("telemetry: list recoveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RecoveryState
	for rows.Next() {
		var s RecoveryState
		var status, merged string
		if err := rows.Scan(&s.PR, &s.Issue, &status, &merged); err != nil {
			return nil, fmt.Errorf("telemetry: scan recovery: %w", err)
		}
		if s.MergedAt, err = time.Parse(time.RFC3339, merged); err != nil {
			return nil, fmt.Errorf("telemetry: recovery of #%d: merged_at %q: %w", s.PR, merged, err)
		}
		s.Status = RecoveryStatus(status)
		out = append(out, s)
	}
	return out, rows.Err()
}
