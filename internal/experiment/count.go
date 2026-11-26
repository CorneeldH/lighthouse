package experiment

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// VariantCount is the number of trajectories of experimentID assigned to a variant of component
// that have a review run of that component and whose first run started at or after since. A
// variant trajectory without a review run does not count: it has not yet used its variant.
func VariantCount(ctx context.Context, db *sql.DB, experimentID, component string, since time.Time) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM trajectories t
		WHERE t.experiment_id = ?
		  AND substr(t.assignment, 1, length(?)) = ?
		  AND EXISTS (SELECT 1 FROM review_runs r WHERE r.trajectory_id = t.id AND r.component = ?)
		  AND (SELECT min(started_at) FROM runs WHERE trajectory_id = t.id) >= ?`,
		experimentID, "variant:"+component+"=", "variant:"+component+"=", component,
		since.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("experiment: count variants of %s: %w", experimentID, err)
	}
	return n, nil
}
