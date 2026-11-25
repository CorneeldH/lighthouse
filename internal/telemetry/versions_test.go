package telemetry

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// setupRow is one setups row plus the run or session it hangs on.
type setupRow struct {
	session    bool // false: a run row
	startedAt  time.Time
	recordedAt time.Time // zero: same as startedAt
	assignment string    // "" = standard
	lane       any       // nil or string, runs only
	model      any
	harness    any
	harnessVer any
	superpower any
}

var seq int

// insertSetup inserts the run or session and its setups row, and returns the setups id.
func insertSetup(t *testing.T, db *sql.DB, r setupRow) int64 {
	t.Helper()
	seq++
	if r.assignment == "" {
		r.assignment = "standard"
	}
	if r.recordedAt.IsZero() {
		r.recordedAt = r.startedAt
	}
	started := r.startedAt.UTC().Format(time.RFC3339Nano)
	rec := r.recordedAt.UTC().Format(time.RFC3339Nano)
	var runID, sessID any
	if r.session {
		res, err := db.Exec(`INSERT INTO sessions (source, source_id, recorded_at, repo, harness, session_key,
			transcript_path, unlinked_reason, assignment, started_at) VALUES ('t', ?, ?, 'o/r', 'claude-code', ?, '/t', 'no_issue_branch', ?, ?)`,
			seq, rec, seq, r.assignment, started)
		if err != nil {
			t.Fatalf("insert session: %v", err)
		}
		sessID, _ = res.LastInsertId()
	} else {
		res, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt,
			assignment, started_at) VALUES ('o/r', ?, ?, 'start', 'handoff', ?, 1, ?, ?)`,
			seq, seq, seq, r.assignment, started)
		if err != nil {
			t.Fatalf("insert run: %v", err)
		}
		runID, _ = res.LastInsertId()
	}
	res, err := db.Exec(`INSERT INTO setups (source, source_id, recorded_at, run_id, session_id, model, harness,
		harness_version, superpowers_version, lane) VALUES ('t', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, rec, runID, sessID, r.model, r.harness, r.harnessVer, r.superpower, r.lane)
	if err != nil {
		t.Fatalf("insert setup: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func sp(m int, v any) setupRow {
	return setupRow{startedAt: t0.Add(time.Duration(m) * time.Minute), superpower: v}
}

func changes(t *testing.T, db *sql.DB) []VersionChange {
	t.Helper()
	got, err := VersionChanges(t.Context(), db)
	if err != nil {
		t.Fatalf("VersionChanges: %v", err)
	}
	return got
}

func TestVersionChanges_TwoRunsOneChange(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "7.0.1"))
	insertSetup(t, db, sp(1, "7.0.2"))
	got := changes(t, db)
	if len(got) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.Dimension != DimSuperpowers || c.Old != "7.0.1" || c.New != "7.0.2" || c.Series != "" {
		t.Errorf("change = %+v", c)
	}
	if !c.RunID.Valid || c.SessionID.Valid {
		t.Errorf("RunID/SessionID = %v/%v, want the run", c.RunID, c.SessionID)
	}
	if !c.At.Equal(t0.Add(time.Minute)) {
		t.Errorf("At = %v", c.At)
	}
	var want int64
	if err := db.QueryRow(`SELECT id FROM runs WHERE started_at = ?`, c.At.UTC().Format(time.RFC3339Nano)).Scan(&want); err != nil {
		t.Fatal(err)
	}
	if c.RunID.Int64 != want {
		t.Errorf("RunID = %d, want %d", c.RunID.Int64, want)
	}
}

func TestVersionChanges_SessionRowCarriesSessionID(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "1"))
	r := sp(1, "2")
	r.session = true
	insertSetup(t, db, r)
	got := changes(t, db)
	if len(got) != 1 || !got[0].SessionID.Valid || got[0].RunID.Valid {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_ABABIsOneChange(t *testing.T) {
	db := openDB(t)
	for i, v := range []string{"A", "B", "A", "B"} {
		insertSetup(t, db, sp(i, v))
	}
	got := changes(t, db)
	if len(got) != 1 || got[0].Old != "A" || got[0].New != "B" {
		t.Fatalf("got %+v, want one A->B", got)
	}
}

func TestVersionChanges_FirstValueNoChange(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "A"))
	if got := changes(t, db); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_NullSkipped(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "A"))
	insertSetup(t, db, sp(1, nil))
	insertSetup(t, db, sp(2, "A"))
	if got := changes(t, db); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_OrdersByStartedAtNotRecordedAt(t *testing.T) {
	db := openDB(t)
	a := sp(0, "old")
	a.recordedAt = t0.Add(10 * time.Minute)
	b := sp(1, "new")
	b.recordedAt = t0
	insertSetup(t, db, b)
	insertSetup(t, db, a)
	got := changes(t, db)
	if len(got) != 1 || got[0].Old != "old" || got[0].New != "new" {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_EqualStartedAtOrdersBySetupsID(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "first"))
	insertSetup(t, db, sp(0, "second"))
	got := changes(t, db)
	if len(got) != 1 || got[0].Old != "first" || got[0].New != "second" {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_ModelPerLaneDispatcherOnly(t *testing.T) {
	db := openDB(t)
	m := func(min int, lane, model any) setupRow {
		return setupRow{startedAt: t0.Add(time.Duration(min) * time.Minute), lane: lane, model: model}
	}
	insertSetup(t, db, m(0, "sdd", nil))
	insertSetup(t, db, m(1, "native", "sonnet"))
	insertSetup(t, db, m(2, "sdd", "opus"))
	insertSetup(t, db, m(3, "native", "sonnet"))
	s := m(4, nil, "other-model")
	s.session = true
	insertSetup(t, db, s)
	if got := changes(t, db); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
	insertSetup(t, db, m(5, "native", "sonnet-b"))
	got := changes(t, db)
	if len(got) != 1 || got[0].Dimension != DimModel || got[0].Series != "native" ||
		got[0].Old != "sonnet" || got[0].New != "sonnet-b" {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_HarnessPerHarness(t *testing.T) {
	db := openDB(t)
	h := func(min int, name, ver string) setupRow {
		return setupRow{startedAt: t0.Add(time.Duration(min) * time.Minute), harness: name, harnessVer: ver}
	}
	insertSetup(t, db, h(0, "claude", "2.1"))
	insertSetup(t, db, h(1, "codex", "0.6"))
	insertSetup(t, db, h(2, "claude", "2.1"))
	if got := changes(t, db); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	insertSetup(t, db, h(3, "claude", "2.2"))
	got := changes(t, db)
	if len(got) != 1 || got[0].Dimension != DimHarness || got[0].Series != "claude" ||
		got[0].Old != "2.1" || got[0].New != "2.2" {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_OnlyStandardAssignment(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "A"))
	x := sp(1, "B")
	x.assignment = "shadow:x"
	insertSetup(t, db, x)
	y := sp(2, "C")
	y.session = true
	y.assignment = "variant:a=b"
	insertSetup(t, db, y)
	if got := changes(t, db); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_NoSetupsTable(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`DROP TABLE setups`); err != nil {
		t.Fatal(err)
	}
	_, err := VersionChanges(t.Context(), db)
	if !errors.Is(err, ErrNoSetups) {
		t.Fatalf("err = %v, want ErrNoSetups", err)
	}
}
