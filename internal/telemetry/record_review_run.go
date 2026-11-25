package telemetry

// The writer for review_runs: one row per review run of a plan or spec, kept
// even when it has no findings. Only paths, sums, names and counts are stored.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ReviewRunRecord is one review run of a plan or spec. Issue 0 means the run
// belongs to no trajectory. The record itself stays a file: only its path and
// sum are kept.
type ReviewRunRecord struct {
	Origin
	Repo         string
	Issue        int
	Component    string // plan_review or spec_review
	RecordPath   string
	RecordSum    string
	RanAt        time.Time
	Outcome      string
	FindingCount int
}

func (r ReviewRunRecord) validate() error {
	if err := r.Origin.validate(); err != nil {
		return err
	}
	if r.Repo == "" {
		return errors.New("telemetry: review run needs a repo")
	}
	if r.Issue < 0 {
		return fmt.Errorf("telemetry: review run needs an issue of 0 or above; got %d", r.Issue)
	}
	if r.Component != "plan_review" && r.Component != "spec_review" {
		return fmt.Errorf("telemetry: review run needs component plan_review or spec_review; got %q", r.Component)
	}
	if r.RanAt.IsZero() {
		return errors.New("telemetry: review run needs a ran-at time")
	}
	return nil
}

// RecordReviewRun records a review run. Issue 0 stores no trajectory and the
// reason no_trajectory. A second offering of the same origin updates record
// path, record sum, ran_at, outcome, finding count, trajectory and
// recorded_at, and reports not new.
func recordReviewRunTx(ctx context.Context, tx *sql.Tx, r ReviewRunRecord) (int64, bool, error) {
	if err := r.validate(); err != nil {
		return 0, false, err
	}
	var trajectory, reason any
	if r.Issue > 0 {
		repo, err := resolveRepo(ctx, tx, r.Repo)
		if err != nil {
			return 0, false, err
		}
		tid, err := ensureTrajectory(ctx, tx, repo, r.Issue, r.RecordedAt)
		if err != nil {
			return 0, false, err
		}
		trajectory = tid
	} else {
		reason = "no_trajectory"
	}
	id, isNew, err := insertOrFind(ctx, tx, "review_runs", `INSERT INTO review_runs (source, source_id, recorded_at,
		trajectory_id, unlinked_reason, component, record_path, record_sum, ran_at, outcome, finding_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		[]any{r.Source, r.SourceID, stamp(r.RecordedAt), trajectory, reason, r.Component, r.RecordPath,
			r.RecordSum, stamp(r.RanAt), nullString(r.Outcome), r.FindingCount},
		bySource(r.Origin))
	if err != nil || isNew {
		return id, isNew, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_runs SET record_path = ?, record_sum = ?, ran_at = ?,
		outcome = ?, finding_count = ?, trajectory_id = ?, unlinked_reason = ?, recorded_at = ? WHERE id = ?`,
		r.RecordPath, r.RecordSum, stamp(r.RanAt), nullString(r.Outcome), r.FindingCount, trajectory, reason,
		stamp(r.RecordedAt), id); err != nil {
		return 0, false, fmt.Errorf("telemetry: update review run: %w", err)
	}
	return id, false, nil
}

// ReviewRunSum returns the record_sum stored for the review run with this origin.
func ReviewRunSum(ctx context.Context, db *sql.DB, source, sourceID string) (string, bool, error) {
	var sum string
	err := db.QueryRowContext(ctx, `SELECT record_sum FROM review_runs WHERE source = ? AND source_id = ?`,
		source, sourceID).Scan(&sum)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("telemetry: read review run sum: %w", err)
	}
	return sum, true, nil
}
