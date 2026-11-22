package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

var mergedAt = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func recoveryRec(status RecoveryStatus, items ...RecoveryItem) RecoveryRecord {
	return RecoveryRecord{
		Origin: Origin{Source: "recovery", SourceID: "o/r#7", RecordedAt: t0},
		Repo:   "o/r", Issue: 3, FixPRRounds: 2,
		Recovery: Recovery{PR: 7, MergedAt: mergedAt, Status: status, Items: items},
	}
}

func item(kind, ref string, d time.Duration) RecoveryItem {
	return RecoveryItem{Kind: kind, Ref: ref, At: mergedAt.Add(d)}
}

type recoveryRow struct {
	status, mergedAt, windowEnd string
	rounds                      int
	items                       []string
}

func readRecovery(t *testing.T, db *sql.DB, id int64) recoveryRow {
	t.Helper()
	var r recoveryRow
	if err := db.QueryRow(`SELECT status, merged_at, window_end, fixpr_rounds FROM recoveries WHERE id = ?`, id).
		Scan(&r.status, &r.mergedAt, &r.windowEnd, &r.rounds); err != nil {
		t.Fatalf("read recovery: %v", err)
	}
	rows, err := db.Query(`SELECT kind || ':' || ref FROM recovery_items WHERE recovery_id = ? ORDER BY id`, id)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		r.items = append(r.items, s)
	}
	return r
}

func TestRecordRecoveryFirstCreatesTrajectoryRowAndItems(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	id, isNew, err := RecordRecovery(ctx, db, recoveryRec(RecoveryOpen, item("fix", "abc", time.Hour)))
	if err != nil || !isNew {
		t.Fatalf("RecordRecovery = %d, %v, %v; want new", id, isNew, err)
	}
	got := readRecovery(t, db, id)
	want := recoveryRow{status: "open", mergedAt: stamp(mergedAt), windowEnd: stamp(mergedAt.Add(RecoveryWindow)),
		rounds: 2, items: []string{"fix:abc"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("row = %+v, want %+v", got, want)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE repo = 'o/r' AND issue = 3`); n != 1 {
		t.Errorf("trajectories = %d, want 1", n)
	}
}

func TestRecordRecoveryOpenRowIsReplaced(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	id1, _, err := RecordRecovery(ctx, db, recoveryRec(RecoveryOpen, item("fix", "abc", time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	next := recoveryRec(RecoveryOpen, item("revert", "def", 2*time.Hour), item("bug_issue", "9", 3*time.Hour))
	next.FixPRRounds = 5
	id2, isNew, err := RecordRecovery(ctx, db, next)
	if err != nil || isNew || id2 != id1 {
		t.Fatalf("second = %d, %v, %v; want %d, not new", id2, isNew, err, id1)
	}
	got := readRecovery(t, db, id1)
	if fmt.Sprint(got.items) != "[revert:def bug_issue:9]" || got.rounds != 5 {
		t.Errorf("row = %+v", got)
	}
	if n := count(t, db, `SELECT count(*) FROM recoveries`); n != 1 {
		t.Errorf("recoveries = %d, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM recovery_items`); n != 2 {
		t.Errorf("items = %d, want 2", n)
	}
}

func TestRecordRecoveryOpenBecomesClosed(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	id, _, err := RecordRecovery(ctx, db, recoveryRec(RecoveryOpen))
	if err != nil {
		t.Fatal(err)
	}
	if _, isNew, err := RecordRecovery(ctx, db, recoveryRec(RecoveryRecovered, item("fix", "abc", time.Hour))); err != nil || isNew {
		t.Fatalf("close = %v, %v", isNew, err)
	}
	if got := readRecovery(t, db, id); got.status != "recovered" || len(got.items) != 1 {
		t.Errorf("row = %+v", got)
	}
}

func TestRecordRecoveryClosedRowIsLeftAlone(t *testing.T) {
	for _, closed := range []RecoveryStatus{RecoveryRecovered, RecoveryNone} {
		t.Run(string(closed), func(t *testing.T) {
			db := openDB(t)
			ctx := context.Background()
			first := recoveryRec(closed)
			if closed == RecoveryRecovered {
				first = recoveryRec(closed, item("fix", "abc", time.Hour))
			}
			id, _, err := RecordRecovery(ctx, db, first)
			if err != nil {
				t.Fatal(err)
			}
			before := readRecovery(t, db, id)
			again := recoveryRec(RecoveryOpen, item("revert", "zzz", time.Hour))
			again.FixPRRounds = 9
			id2, isNew, err := RecordRecovery(ctx, db, again)
			if err != nil || isNew || id2 != id {
				t.Fatalf("again = %d, %v, %v", id2, isNew, err)
			}
			if after := readRecovery(t, db, id); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Errorf("closed row changed: %+v -> %+v", before, after)
			}
		})
	}
}

func TestRecordRecoveryRoundsAreNotItems(t *testing.T) {
	db := openDB(t)
	id, _, err := RecordRecovery(context.Background(), db, recoveryRec(RecoveryOpen))
	if err != nil {
		t.Fatal(err)
	}
	if got := readRecovery(t, db, id); got.rounds != 2 || len(got.items) != 0 {
		t.Errorf("row = %+v, want 2 rounds and no items", got)
	}
}

func TestRecordRecoveryInvalidInputLeavesNothing(t *testing.T) {
	bad := map[string]func(*RecoveryRecord){
		"no source":  func(r *RecoveryRecord) { r.Source = "" },
		"zero issue": func(r *RecoveryRecord) { r.Issue = 0 },
		"no repo":    func(r *RecoveryRecord) { r.Repo = "" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			r := recoveryRec(RecoveryOpen)
			mutate(&r)
			if _, _, err := RecordRecovery(context.Background(), db, r); err == nil {
				t.Fatal("want an error")
			}
			for _, tbl := range []string{"trajectories", "recoveries", "recovery_items"} {
				if n := count(t, db, `SELECT count(*) FROM `+tbl); n != 0 {
					t.Errorf("%s = %d, want 0", tbl, n)
				}
			}
		})
	}
}

func TestRecordRecoveryRollsBackTrajectoryOnItemFailure(t *testing.T) {
	db := openDB(t)
	r := recoveryRec(RecoveryOpen, item("fix", "abc", time.Hour), item("fix", "abc", 2*time.Hour))
	if _, _, err := RecordRecovery(context.Background(), db, r); err == nil {
		t.Fatal("duplicate item: want an error")
	}
	for _, tbl := range []string{"trajectories", "recoveries", "recovery_items"} {
		if n := count(t, db, `SELECT count(*) FROM `+tbl); n != 0 {
			t.Errorf("%s = %d, want 0", tbl, n)
		}
	}
}

func insertFixPRRun(t *testing.T, db *sql.DB, repo, key string, started time.Time, outcome string) {
	t.Helper()
	var out, ended any
	if outcome != "" {
		out, ended = outcome, stamp(started.Add(time.Minute))
	}
	if _, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt,
		started_at, ended_at, outcome) VALUES (?, ?, ?, 'start', 'fixpr', ?, 1, ?, ?, ?)`,
		repo, 3+int(started.Unix()%1000), fmt.Sprintf("s-%d-%s", started.UnixNano(), key), key, stamp(started), ended, out); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func TestCountFixPRRounds(t *testing.T) {
	db := openDB(t)
	before := mergedAt.Add(-time.Hour)
	insertFixPRRun(t, db, "o/r", "fixpr:7:aaa", before.Add(-3*time.Hour), "done")
	insertFixPRRun(t, db, "o/r", "fixpr:7:bbb", before.Add(-2*time.Hour), "returned")
	insertFixPRRun(t, db, "o/r", "fixpr:7:ccc", before.Add(-time.Hour), "")                  // still open: counts
	insertFixPRRun(t, db, "o/r", "fixpr:7:ccc", before.Add(-30*time.Minute), "start_failed") // failed start: not a round
	insertFixPRRun(t, db, "o/r", "fixpr:7:ddd", mergedAt.Add(time.Hour), "done")             // after the merge
	insertFixPRRun(t, db, "o/r", "fixpr:77:aaa", before, "done")                             // other PR, prefix-alike
	insertFixPRRun(t, db, "o/r", "fixpr:8:aaa", before, "done")                              // other PR
	insertFixPRRun(t, db, "x/y", "fixpr:7:aaa", before, "done")                              // other repo
	got, err := CountFixPRRounds(context.Background(), db, "o/r", 7, mergedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("CountFixPRRounds = %d, want 3", got)
	}
}

func TestCountFixPRRoundsIgnoresOtherTriggers(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt, started_at)
		VALUES ('o/r', 3, 's1', 'start', 'handoff', 'fixpr:7:aaa', 1, ?)`, stamp(mergedAt.Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got, err := CountFixPRRounds(context.Background(), db, "o/r", 7, mergedAt); err != nil || got != 0 {
		t.Errorf("= %d, %v; want 0", got, err)
	}
}
