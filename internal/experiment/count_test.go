package experiment

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

var countStart = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func openCountDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB
}

// variantTrajectory claims a first run of issue with assignment at started.
func variantTrajectory(t *testing.T, db *sql.DB, issue int, assignment, expID string, started time.Time) {
	t.Helper()
	_, err := runs.Take(context.Background(), db, runs.Claim{
		Repo: "o/r", Issue: issue, SessionID: runs.NewSessionID(), Mode: "start", Trigger: "handoff",
		TriggerKey: "start:" + string(rune('a'+issue)), Now: started, Assignment: assignment, ExperimentID: expID,
	})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
}

func addReviewRun(t *testing.T, db *sql.DB, issue int, component string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO review_runs (source, source_id, recorded_at, trajectory_id, component,
		record_path, record_sum, ran_at, finding_count)
		SELECT 'test', ?, '2026-11-02T00:00:00Z', id, ?, 'p', 's', '2026-11-02T00:00:00Z', 0
		FROM trajectories WHERE repo = 'o/r' AND issue = ?`, component+string(rune('a'+issue)), component, issue)
	if err != nil {
		t.Fatalf("insert review run: %v", err)
	}
}

func count(t *testing.T, db *sql.DB, since time.Time) int {
	t.Helper()
	n, err := VariantCount(context.Background(), db, "e1", "plan_review", since)
	if err != nil {
		t.Fatalf("VariantCount: %v", err)
	}
	return n
}

func TestVariantCountOnlyWithReviewRun(t *testing.T) {
	db := openCountDB(t)
	at := countStart.Add(time.Hour)
	variantTrajectory(t, db, 1, "variant:plan_review=codex", "e1", at)
	variantTrajectory(t, db, 2, "variant:plan_review=codex", "e1", at)
	variantTrajectory(t, db, 3, "standard", "", at)
	addReviewRun(t, db, 1, "plan_review") // zero findings still counts
	addReviewRun(t, db, 3, "plan_review") // standard does not count
	if got := count(t, db, countStart); got != 1 {
		t.Errorf("count = %d, want 1 (only the variant trajectory with a review run)", got)
	}
	addReviewRun(t, db, 2, "spec_review") // a review run of another component does not count
	if got := count(t, db, countStart); got != 1 {
		t.Errorf("count = %d, want 1 after a run of another component", got)
	}
}

func TestVariantCountBeforeStart(t *testing.T) {
	db := openCountDB(t)
	variantTrajectory(t, db, 1, "variant:plan_review=codex", "e1", countStart.Add(-time.Hour))
	variantTrajectory(t, db, 2, "variant:plan_review=codex", "e1", countStart)
	addReviewRun(t, db, 1, "plan_review")
	addReviewRun(t, db, 2, "plan_review")
	if got := count(t, db, countStart); got != 1 {
		t.Errorf("count = %d, want 1 (the run before the start does not count)", got)
	}
}
