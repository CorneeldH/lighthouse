package telemetry

// One transaction around several writers, so a transcript lands completely
// or not at all.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Tx is one open transaction; its methods do what the functions of the same
// name do, on the same SQL, but commit nothing themselves.
type Tx struct {
	tx *sql.Tx
}

// InTransaction runs fn in one transaction and commits only when fn returns
// nil; any error from fn rolls everything back.
func InTransaction(ctx context.Context, db *sql.DB, fn func(*Tx) error) error {
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate: BEGIN IMMEDIATE
	if err != nil {
		return fmt.Errorf("telemetry: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&Tx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("telemetry: commit: %w", err)
	}
	return nil
}

// RecordSession is RecordSession inside the transaction.
func (t *Tx) RecordSession(ctx context.Context, s SessionRecord) (int64, bool, error) {
	return recordSessionTx(ctx, t.tx, s)
}

// RecordSetup is RecordSetup inside the transaction.
func (t *Tx) RecordSetup(ctx context.Context, s SetupRecord) (int64, bool, error) {
	return recordSetupTx(ctx, t.tx, s)
}

// RecordUsage is RecordUsage inside the transaction.
func (t *Tx) RecordUsage(ctx context.Context, u UsageRecord) (int64, bool, error) {
	return recordUsageTx(ctx, t.tx, u)
}

// RecordEvent is RecordEvent inside the transaction.
func (t *Tx) RecordEvent(ctx context.Context, e EventRecord) (int64, bool, error) {
	return recordEventTx(ctx, t.tx, e)
}

// SetPlan is SetPlan inside the transaction.
func (t *Tx) SetPlan(ctx context.Context, repo string, issue int, tasks *int, path string) error {
	return setPlanTx(ctx, t.tx, repo, issue, tasks, path)
}

// SetPlan sets plan_tasks and plan_path of an existing trajectory; a nil
// tasks and an empty path are stored as NULL. A missing trajectory is an
// error.
func SetPlan(ctx context.Context, db *sql.DB, repo string, issue int, tasks *int, path string) error {
	return InTransaction(ctx, db, func(t *Tx) error { return t.SetPlan(ctx, repo, issue, tasks, path) })
}

func setPlanTx(ctx context.Context, tx *sql.Tx, repo string, issue int, tasks *int, path string) error {
	repo, err := resolveRepo(ctx, tx, repo)
	if err != nil {
		return err
	}
	id, ok, err := findTrajectory(ctx, tx, repo, issue)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("telemetry: no trajectory " + repo + " for the plan")
	}
	var n any
	if tasks != nil {
		n = *tasks
	}
	if _, err := tx.ExecContext(ctx, `UPDATE trajectories SET plan_tasks = ?, plan_path = ? WHERE id = ?`,
		n, nullString(path), id); err != nil {
		return fmt.Errorf("telemetry: set plan: %w", err)
	}
	return nil
}

// RecordFinding is RecordFinding inside the transaction.
func (t *Tx) RecordFinding(ctx context.Context, f FindingRecord) (int64, bool, error) {
	return recordFindingTx(ctx, t.tx, f)
}

// RecordReviewRun records a review run. Issue 0 stores no trajectory and the
// reason no_trajectory. A second offering of the same origin updates record
// path, record sum, ran_at, outcome, finding count, trajectory and
// recorded_at, and reports not new.
func (t *Tx) RecordReviewRun(ctx context.Context, r ReviewRunRecord) (int64, bool, error) {
	return recordReviewRunTx(ctx, t.tx, r)
}

// DeleteChain deletes the findings and review runs of source whose source_id
// starts with prefix and not with keep (all of them under prefix when keep is
// empty), and returns how many of each. It compares with substr, never LIKE:
// "_" and "%" in a chain name are no wildcards.
func (t *Tx) DeleteChain(ctx context.Context, source, prefix, keep string) (findings, runs int64, err error) {
	for i, table := range []string{"findings", "review_runs"} {
		query := `DELETE FROM ` + table + ` WHERE source = ? AND substr(source_id, 1, length(?)) = ?`
		args := []any{source, prefix, prefix}
		if keep != "" {
			query += ` AND substr(source_id, 1, length(?)) != ?`
			args = append(args, keep, keep)
		}
		res, err := t.tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, 0, fmt.Errorf("telemetry: delete %s: %w", table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, fmt.Errorf("telemetry: delete %s: %w", table, err)
		}
		if i == 0 {
			findings = n
		} else {
			runs = n
		}
	}
	return findings, runs, nil
}
