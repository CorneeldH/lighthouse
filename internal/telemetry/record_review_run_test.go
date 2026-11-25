package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func reviewRun(sourceID string, issue int, sum string) ReviewRunRecord {
	return ReviewRunRecord{
		Origin:       Origin{Source: "review", SourceID: sourceID, RecordedAt: t0},
		Repo:         "o/r",
		Issue:        issue,
		Component:    "plan_review",
		RecordPath:   "docs/review.md",
		RecordSum:    sum,
		RanAt:        t0,
		Outcome:      "converged",
		FindingCount: 0,
	}
}

func TestRecordReviewRunLinked(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id, isNew, err := InTransactionRun(ctx, db, reviewRun("plan:a:1", 7, "s1"))
	if err != nil || !isNew || id == 0 {
		t.Fatalf("got %d, %v, %v; want new row", id, isNew, err)
	}
	if n := count(t, db, `SELECT count(*) FROM review_runs WHERE finding_count = 0 AND trajectory_id IS NOT NULL AND unlinked_reason IS NULL`); n != 1 {
		t.Errorf("linked review run rows = %d; want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE repo = 'o/r' AND issue = 7`); n != 1 {
		t.Errorf("trajectories = %d; want 1", n)
	}
}

func TestRecordReviewRunUnlinked(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, _, err := InTransactionRun(ctx, db, reviewRun("plan:a:1", 0, "s1")); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM review_runs WHERE trajectory_id IS NULL AND unlinked_reason = 'no_trajectory'`); n != 1 {
		t.Errorf("unlinked rows = %d; want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories`); n != 0 {
		t.Errorf("trajectories = %d; want 0", n)
	}
}

func TestRecordReviewRunUpdatesSameOrigin(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	first, _, err := InTransactionRun(ctx, db, reviewRun("plan:a:1", 0, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	r := reviewRun("plan:a:1", 7, "s2")
	r.RecordPath = "docs/other.md"
	r.RanAt = t0.Add(time.Hour)
	r.Outcome = "round_limit"
	r.FindingCount = 3
	r.RecordedAt = t0.Add(2 * time.Hour)
	id, isNew, err := InTransactionRun(ctx, db, r)
	if err != nil || isNew || id != first {
		t.Fatalf("got %d, %v, %v; want id %d, not new", id, isNew, err, first)
	}
	if n := count(t, db, `SELECT count(*) FROM review_runs`); n != 1 {
		t.Fatalf("rows = %d; want 1", n)
	}
	var path, sum, ran, outcome, rec string
	var findings int
	var traj sql.NullInt64
	if err := db.QueryRow(`SELECT record_path, record_sum, ran_at, outcome, finding_count, trajectory_id, recorded_at
		FROM review_runs WHERE id = ?`, id).Scan(&path, &sum, &ran, &outcome, &findings, &traj, &rec); err != nil {
		t.Fatal(err)
	}
	if path != "docs/other.md" || sum != "s2" || ran != stamp(r.RanAt) || outcome != "round_limit" ||
		findings != 3 || !traj.Valid || rec != stamp(r.RecordedAt) {
		t.Errorf("row not updated: %s %s %s %s %d %v %s", path, sum, ran, outcome, findings, traj, rec)
	}
	if got, ok, err := ReviewRunSum(ctx, db, "review", "plan:a:1"); err != nil || !ok || got != "s2" {
		t.Errorf("ReviewRunSum = %q, %v, %v; want s2", got, ok, err)
	}
	if _, ok, _ := ReviewRunSum(ctx, db, "review", "nope"); ok {
		t.Error("ReviewRunSum found a missing row")
	}
}

func TestDeleteChainRemovesBothTables(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	err := InTransaction(ctx, db, func(tx *Tx) error {
		for _, sid := range []string{"a_b:1", "a_b:2", "axb:1"} {
			if _, _, err := tx.RecordReviewRun(ctx, reviewRun(sid, 7, "s")); err != nil {
				return err
			}
			f := FindingRecord{Origin: Origin{Source: "review", SourceID: sid + ":f", RecordedAt: t0},
				Repo: "o/r", Issue: 7, Component: "plan_review", FindingKey: "k", Severity: "high",
				RecordPath: "p", RecordSum: "s"}
			if _, _, err := tx.RecordFinding(ctx, f); err != nil {
				return err
			}
		}
		findings, runs, err := tx.DeleteChain(ctx, "review", "a_b:", "a_b:2")
		if err != nil {
			return err
		}
		if findings != 1 || runs != 1 {
			t.Errorf("deleted %d findings, %d runs; want 1, 1", findings, runs)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"findings", "review_runs"} {
		if n := count(t, db, `SELECT count(*) FROM `+table); n != 2 {
			t.Errorf("%s has %d rows; want 2 (kept and other chain)", table, n)
		}
		if n := count(t, db, `SELECT count(*) FROM `+table+` WHERE source_id IN ('a_b:1', 'a_b:1:f')`); n != 0 {
			t.Errorf("%s still has the deleted chain", table)
		}
	}
	err = InTransaction(ctx, db, func(tx *Tx) error {
		f, r, err := tx.DeleteChain(ctx, "review", "a_b:", "")
		if f != 1 || r != 1 {
			t.Errorf("keep empty deleted %d, %d; want 1, 1", f, r)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTxRollsBackReviewRunAndFindings(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	boom := errors.New("boom")
	err := InTransaction(ctx, db, func(tx *Tx) error {
		if _, _, err := tx.RecordReviewRun(ctx, reviewRun("plan:a:1", 7, "s")); err != nil {
			return err
		}
		f := FindingRecord{Origin: Origin{Source: "review", SourceID: "plan:a:1:f", RecordedAt: t0},
			Repo: "o/r", Issue: 7, Component: "plan_review", FindingKey: "k", Severity: "high",
			RecordPath: "p", RecordSum: "s"}
		if _, _, err := tx.RecordFinding(ctx, f); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
	for _, table := range []string{"review_runs", "findings", "trajectories"} {
		if n := count(t, db, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after rollback; want 0", table, n)
		}
	}
}

// InTransactionRun records one review run in its own transaction.
func InTransactionRun(ctx context.Context, db *sql.DB, r ReviewRunRecord) (id int64, isNew bool, err error) {
	err = InTransaction(ctx, db, func(tx *Tx) error {
		id, isNew, err = tx.RecordReviewRun(ctx, r)
		return err
	})
	return id, isNew, err
}
