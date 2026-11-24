package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestInTransactionRollsBack(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	boom := errors.New("boom")
	err := InTransaction(ctx, db, func(tx *Tx) error {
		sid, _, err := tx.RecordSession(ctx, session("s1", Line{Branch: "issue-5-x"}))
		if err != nil {
			return err
		}
		o := Origin{Source: "transcript", SourceID: "transcript:s1", RecordedAt: t0}
		if _, _, err := tx.RecordSetup(ctx, SetupRecord{Origin: o, SessionID: sid, Model: ptr("opus")}); err != nil {
			return err
		}
		if _, _, err := tx.RecordUsage(ctx, UsageRecord{Origin: o, SessionID: sid, Model: "opus", InputTokens: 1}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
	for _, table := range []string{"sessions", "setups", "usage", "trajectories"} {
		if n := count(t, db, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after rollback; want 0", table, n)
		}
	}
}

func TestInTransactionCommits(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	err := InTransaction(ctx, db, func(tx *Tx) error {
		_, _, err := tx.RecordSession(ctx, session("s1"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM sessions`); n != 1 {
		t.Errorf("%d sessions; want 1", n)
	}
}

func TestSetPlanNullKeepsNull(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	takeRun(t, db, "o/r", 5, "start:5", "start")
	if _, _, err := RecordSession(ctx, db, session("s1", Line{Branch: "issue-5-x"})); err != nil {
		t.Fatal(err)
	}
	read := func() (sql.NullInt64, sql.NullString) {
		var n sql.NullInt64
		var p sql.NullString
		if err := db.QueryRow(`SELECT plan_tasks, plan_path FROM trajectories WHERE repo = 'o/r' AND issue = 5`).Scan(&n, &p); err != nil {
			t.Fatal(err)
		}
		return n, p
	}
	if err := SetPlan(ctx, db, "o/r", 5, nil, ""); err != nil {
		t.Fatal(err)
	}
	if n, p := read(); n.Valid || p.Valid {
		t.Errorf("plan = %v, %v; want NULL, NULL", n, p)
	}
	tasks := 7
	if err := SetPlan(ctx, db, "o/r", 5, &tasks, "docs/plan.md"); err != nil {
		t.Fatal(err)
	}
	if n, p := read(); n.Int64 != 7 || p.String != "docs/plan.md" {
		t.Errorf("plan = %v, %v; want 7, docs/plan.md", n, p)
	}
	if err := SetPlan(ctx, db, "o/r", 99, &tasks, "x"); err == nil {
		t.Error("missing trajectory: no error")
	}
}
