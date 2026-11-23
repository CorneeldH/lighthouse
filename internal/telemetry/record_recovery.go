package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecoveryRecord is the measurement of one PR with the trajectory it belongs
// to. Origin has Source "recovery" and SourceID "<repo>#<pr>".
type RecoveryRecord struct {
	Origin
	Repo  string
	Issue int
	Recovery
	// FixPRRounds is the number of fix-PR runs before the merge. It is a
	// column of the recovery, not an item.
	FixPRRounds int
}

// RecordRecovery stores or refreshes the recovery of one PR. isNew is true for
// a first record. A row whose status is already recovered or none is left as
// it is; an open row gets its items replaced and its status, merge time and
// fix-PR rounds updated. An invalid record writes nothing, not even the
// trajectory.
func RecordRecovery(ctx context.Context, db *sql.DB, r RecoveryRecord) (id int64, isNew bool, err error) {
	if err := r.validate(); err != nil {
		return 0, false, err
	}
	if r.Repo == "" || r.Issue < 1 {
		return 0, false, fmt.Errorf("telemetry: recovery needs a repo and an issue above 0; got %q, %d", r.Repo, r.Issue)
	}
	return inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) {
		repo, err := resolveRepo(ctx, tx, r.Repo)
		if err != nil {
			return 0, false, err
		}
		tid, err := ensureTrajectory(ctx, tx, repo, r.Issue, r.RecordedAt)
		if err != nil {
			return 0, false, err
		}
		id, isNew, err := insertOrFind(ctx, tx, "recoveries", `INSERT INTO recoveries (source, source_id, recorded_at,
			trajectory_id, pr, merged_at, window_end, status, fixpr_rounds)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			[]any{r.Source, r.SourceID, stamp(r.RecordedAt), tid, r.PR, stamp(r.MergedAt),
				stamp(r.MergedAt.Add(RecoveryWindow)), string(r.Status), r.FixPRRounds},
			bySource(r.Origin), lookup{"trajectory_id = ? AND pr = ?", []any{tid, r.PR}})
		if err != nil {
			return 0, false, err
		}
		if !isNew {
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM recoveries WHERE id = ?`, id).Scan(&status); err != nil {
				return 0, false, fmt.Errorf("telemetry: read recovery %d: %w", id, err)
			}
			if RecoveryStatus(status) != RecoveryOpen {
				return id, false, nil
			}
			if _, err := tx.ExecContext(ctx, `UPDATE recoveries SET status = ?, merged_at = ?, window_end = ?,
				fixpr_rounds = ?, recorded_at = ? WHERE id = ?`,
				string(r.Status), stamp(r.MergedAt), stamp(r.MergedAt.Add(RecoveryWindow)), r.FixPRRounds,
				stamp(r.RecordedAt), id); err != nil {
				return 0, false, fmt.Errorf("telemetry: update recovery: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_items WHERE recovery_id = ?`, id); err != nil {
				return 0, false, fmt.Errorf("telemetry: clear recovery items: %w", err)
			}
		}
		for _, it := range r.Items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_items (recovery_id, kind, ref, at) VALUES (?, ?, ?, ?)`,
				id, it.Kind, it.Ref, stamp(it.At)); err != nil {
				return 0, false, fmt.Errorf("telemetry: insert recovery item %s %s: %w", it.Kind, it.Ref, err)
			}
		}
		return id, isNew, nil
	})
}

// CountFixPRRounds counts dispatcher runs of trigger fixpr for pr that started
// before mergedAt. A run whose start failed is no round: its retry is the
// round. pr is an integer, so the LIKE pattern holds no wildcard from input.
func CountFixPRRounds(ctx context.Context, db *sql.DB, repo string, pr int, mergedAt time.Time) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM runs
		WHERE trigger = 'fixpr' AND repo = ? COLLATE NOCASE AND trigger_key LIKE ? AND started_at < ?
		AND coalesce(outcome, '') != 'start_failed'`,
		repo, fmt.Sprintf("fixpr:%d:%%", pr), stamp(mergedAt)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("telemetry: count fixpr rounds of #%d: %w", pr, err)
	}
	return n, nil
}
