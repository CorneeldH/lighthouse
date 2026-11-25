// Command e2efixture seeds a lighthouse.db for the Playwright tests of the
// Telemetry tab (web/e2e): it migrates the database at the given path and
// writes one closed trajectory with a run, its setup and usage, a plan
// review with one adopted finding, a question and a recovered PR.
//
// Usage: go run ./internal/telemetry/report/e2efixture <db>
package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
)

// The values the e2e tests read back.
const (
	Repo  = "test/telemetry"
	Issue = 7
	PR    = 70
)

// t0 is the creation time of the trajectory; every other row is relative to it.
var t0 = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func stamp(d time.Duration) string { return t0.Add(d).UTC().Format(time.RFC3339) }

const source = "e2efixture"

// seed writes the fixture rows into db in one transaction.
func seed(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var last int64
	exec := func(query string, args ...any) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("%s: %w", query, err)
		}
		last, err = res.LastInsertId()
		return err
	}

	if err := exec(`INSERT INTO trajectories (repo, issue, created_at, plan_tasks, route, area)
		VALUES (?, ?, ?, 3, '2', 'web')`, Repo, Issue, stamp(0)); err != nil {
		return err
	}
	tid := last
	if err := exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt,
		started_at, ended_at, outcome, trajectory_id, assignment)
		VALUES (?, ?, 'e2e-session', 'start', 'handoff', 'e2e-handoff', 1, ?, ?, 'done', ?, 'standard')`,
		Repo, Issue, stamp(0), stamp(time.Hour), tid); err != nil {
		return err
	}
	run := last
	if err := exec(`INSERT INTO setups (source, source_id, recorded_at, run_id, model, harness, lane)
		VALUES (?, 'setup-1', ?, ?, 'claude-opus-4-1', 'claude', 'sdd')`, source, stamp(0), run); err != nil {
		return err
	}
	if err := exec(`INSERT INTO usage (source, source_id, recorded_at, run_id, model,
		input_tokens, output_tokens, cost, cost_kind)
		VALUES (?, 'usage-1', ?, ?, 'claude-opus-4-1', 120000, 8000, 1.25, 'estimate')`,
		source, stamp(time.Hour), run); err != nil {
		return err
	}
	if err := exec(`INSERT INTO trajectory_events (source, source_id, recorded_at, trajectory_id, run_id, kind, ref, at)
		VALUES (?, 'question-1', ?, ?, ?, 'question', 'question-1', ?)`,
		source, stamp(15*time.Minute), tid, run, stamp(15*time.Minute)); err != nil {
		return err
	}
	// The source id of a review run is <kind>/<chain>/<attempt>/record.json, as review-index writes it.
	const record = "/records/plan/e2e/1/record.json"
	if err := exec(`INSERT INTO review_runs (source, source_id, recorded_at, trajectory_id, component,
		record_path, record_sum, ran_at, outcome, finding_count)
		VALUES ('plan-review-record', 'plan/e2e/1/record.json', ?, ?, 'plan_review', ?, 'e2e', ?, 'converged', 1)`,
		stamp(30*time.Minute), tid, record, stamp(30*time.Minute)); err != nil {
		return err
	}
	if err := exec(`INSERT INTO findings (source, source_id, recorded_at, trajectory_id, component,
		finding_key, severity, decision, record_path, record_sum)
		VALUES ('plan-review-record', 'plan/e2e/1/record.json#f1', ?, ?, 'plan_review', 'f1', 'major', 'adopted', ?, 'e2e')`,
		stamp(30*time.Minute), tid, record); err != nil {
		return err
	}
	merged := 3 * time.Hour
	if err := exec(`INSERT INTO recoveries (source, source_id, recorded_at, trajectory_id, pr, merged_at,
		window_end, status, code_lines) VALUES (?, 'recovery-1', ?, ?, ?, ?, ?, 'recovered', 120)`,
		source, stamp(merged+15*24*time.Hour), tid, PR, stamp(merged), stamp(merged+14*24*time.Hour)); err != nil {
		return err
	}
	if err := exec(`INSERT INTO recovery_items (recovery_id, kind, ref, at) VALUES (?, 'fix', 'abc1234', ?)`,
		last, stamp(merged+24*time.Hour)); err != nil {
		return err
	}
	return tx.Commit()
}

func run(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: e2efixture <db>")
		return 2
	}
	ctx := context.Background()
	s, err := store.Open(ctx, args[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	err = seed(ctx, s.DB)
	if cerr := s.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "seed %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }
