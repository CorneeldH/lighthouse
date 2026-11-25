package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
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
	setupID    int64 // 0 = automatic
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
	var setupID any
	if r.setupID != 0 {
		setupID = r.setupID
	}
	res, err := db.Exec(`INSERT INTO setups (id, source, source_id, recorded_at, run_id, session_id, model, harness,
		harness_version, superpowers_version, lane) VALUES (?, 't', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		setupID, seq, rec, runID, sessID, r.model, r.harness, r.harnessVer, r.superpower, r.lane)
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

func TestVersionChanges_OldIsPrecedingRowValue(t *testing.T) {
	db := openDB(t)
	for i, v := range []string{"A", "B", "A", "C"} {
		insertSetup(t, db, sp(i, v))
	}
	got := changes(t, db)
	if len(got) != 2 || got[1].Old != "A" || got[1].New != "C" {
		t.Fatalf("got %+v, want second change A->C", got)
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
	// Inserted "second" first, but with the higher setups id: the id decides, not insertion order.
	b := sp(0, "second")
	b.setupID = 20
	a := sp(0, "first")
	a.setupID = 10
	insertSetup(t, db, b)
	insertSetup(t, db, a)
	got := changes(t, db)
	if len(got) != 1 || got[0].Old != "first" || got[0].New != "second" {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_FractionalAndWholeSecondsSortChronologically(t *testing.T) {
	db := openDB(t)
	// As text "...:00.5Z" sorts before "...:00Z", chronologically it is the other way round.
	whole := setupRow{startedAt: t0, superpower: "old"}
	frac := setupRow{startedAt: t0.Add(500 * time.Millisecond), superpower: "new"}
	insertSetup(t, db, frac)
	insertSetup(t, db, whole)
	got := changes(t, db)
	if len(got) != 1 || got[0].Old != "old" || got[0].New != "new" {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionChanges_NullHarnessNotCounted(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, setupRow{startedAt: t0, harness: "claude", harnessVer: "2.1"})
	insertSetup(t, db, setupRow{startedAt: t0.Add(time.Minute), harness: nil, harnessVer: "9.9"})
	if got := changes(t, db); len(got) != 0 {
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

func TestVersionMarkers_WeekKindText(t *testing.T) {
	db := openDB(t)
	wed := time.Date(2026, 11, 25, 9, 30, 0, 0, time.UTC)
	insertSetup(t, db, setupRow{startedAt: wed.Add(-time.Hour), superpower: "7.0.1", model: "sonnet-a", lane: "native", harness: "claude", harnessVer: "2.1"})
	insertSetup(t, db, setupRow{startedAt: wed, superpower: "7.0.2", model: "sonnet-b", lane: "native", harness: "claude", harnessVer: "2.2"})
	got, err := VersionMarkers(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	want := map[Dimension]string{
		DimSuperpowers: "superpowers 7.0.1→7.0.2",
		DimHarness:     "harness claude 2.1→2.2",
		DimModel:       "model native sonnet-a→sonnet-b",
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for _, m := range got {
		if !m.Week.Equal(monday) || m.Text != want[m.Kind] {
			t.Errorf("marker %+v, want week %v text %q", m, monday, want[m.Kind])
		}
	}
}

func TestVersionMarkers_SundayBelongsToItsISOWeek(t *testing.T) {
	db := openDB(t)
	sun := time.Date(2026, 11, 29, 23, 0, 0, 0, time.UTC)
	insertSetup(t, db, setupRow{startedAt: sun.Add(-time.Hour), superpower: "1.0.0"})
	insertSetup(t, db, setupRow{startedAt: sun, superpower: "1.0.1"})
	got, err := VersionMarkers(t.Context(), db)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if want := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC); !got[0].Week.Equal(want) {
		t.Fatalf("week %v, want %v", got[0].Week, want)
	}
}

func TestVersionChanges_OlderNeverSeenIsSkip(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "7.0.2"))
	insertSetup(t, db, sp(1, "7.0.1"))
	got := changes(t, db)
	if len(got) != 1 || !got[0].Skip {
		t.Fatalf("older first seen: %+v", got)
	}

	db = openDB(t)
	insertSetup(t, db, sp(0, "7.0.2"))
	insertSetup(t, db, sp(1, "7.0.3"))
	if got := changes(t, db); len(got) != 1 || got[0].Skip {
		t.Fatalf("newer: %+v", got)
	}

	db = openDB(t)
	insertSetup(t, db, setupRow{startedAt: t0, model: "a"})
	insertSetup(t, db, setupRow{startedAt: t0.Add(time.Minute), model: "b"})
	if got := changes(t, db); len(got) != 1 || got[0].Skip {
		t.Fatalf("model: %+v", got)
	}

	db = openDB(t)
	insertSetup(t, db, sp(0, "vlatest"))
	insertSetup(t, db, sp(1, "nightly"))
	if got := changes(t, db); len(got) != 1 || got[0].Skip {
		t.Fatalf("non-semver: %+v", got)
	}
}

func TestLatestProposals(t *testing.T) {
	mk := func(old, nw string, skip bool) VersionChange {
		return VersionChange{Dimension: DimSuperpowers, Old: old, New: nw, Skip: skip}
	}
	got := LatestProposals([]VersionChange{mk("1", "2", false), mk("2", "3", false), mk("3", "4", false)})
	if len(got) != 1 || got[0].New != "4" {
		t.Fatalf("got %+v", got)
	}
	if got := LatestProposals([]VersionChange{mk("1", "2", false), mk("2", "1.5", true)}); len(got) != 0 {
		t.Fatalf("skipped last must give none: %+v", got)
	}
	if want := []string{"task_review", "final_review"}; !reflect.DeepEqual(ComponentsFor(DimSuperpowers), want) {
		t.Fatalf("components %v", ComponentsFor(DimSuperpowers))
	}
	if !reflect.DeepEqual(ComponentsFor(DimModel), []string{"model"}) || !reflect.DeepEqual(ComponentsFor(DimHarness), []string{"harness"}) {
		t.Fatal("model/harness components")
	}
}

func TestLatestProposals_PerSeries(t *testing.T) {
	got := LatestProposals([]VersionChange{
		{Dimension: DimHarness, Series: "claude", Old: "1", New: "2"},
		{Dimension: DimHarness, Series: "codex", Old: "1", New: "2"},
		{Dimension: DimModel, Series: "", Old: "a", New: "b"},
	})
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	got = LatestProposals([]VersionChange{
		{Dimension: DimHarness, Series: "claude", Old: "1", New: "2"},
		{Dimension: DimHarness, Series: "codex", Old: "1", New: "2"},
		{Dimension: DimHarness, Series: "claude", Old: "2", New: "3"},
		{Dimension: DimHarness, Series: "codex", Old: "2", New: "3", Skip: true},
	})
	if len(got) != 1 || got[0].Series != "claude" || got[0].New != "3" {
		t.Fatalf("want only claude 3 (codex last is skipped): %+v", got)
	}
}

func TestVersionMarkers_ReadOnlyConnection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "l.db")
	s, err := store.OpenShared(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	insertSetup(t, s.DB, sp(0, "7.0.1"))
	insertSetup(t, s.DB, sp(1, "7.0.2"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := store.OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	got, err := VersionMarkers(ctx, ro.DB)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestVersionChanges_PrereleaseOrderIsSemver(t *testing.T) {
	db := openDB(t)
	insertSetup(t, db, sp(0, "7.0.0-rc.1"))
	insertSetup(t, db, sp(1, "7.0.0-rc.2"))
	insertSetup(t, db, sp(2, "7.0.0-rc.10"))
	for _, c := range changes(t, db) {
		if c.Skip {
			t.Fatalf("rc.10 after rc.2 must not skip: %+v", c)
		}
	}

	db = openDB(t)
	insertSetup(t, db, sp(0, "7.0.0-rc.10"))
	insertSetup(t, db, sp(1, "7.0.0-rc.2"))
	got := changes(t, db)
	if len(got) != 1 || !got[0].Skip {
		t.Fatalf("rc.2 after rc.10 must skip: %+v", got)
	}
}
