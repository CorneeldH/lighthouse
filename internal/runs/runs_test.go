package runs

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB
}

func claim(repo string, issue int, key string) Claim {
	return Claim{
		Repo: repo, Issue: issue, SessionID: NewSessionID(), Mode: "start",
		Trigger: "handoff", TriggerKey: key, Now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
	}
}

func TestTakeAndRelease(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if r.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", r.Attempt)
	}
	if _, err := Take(ctx, db, claim("owner/a", 7, "answer:7")); !errors.Is(err, ErrClaimed) {
		t.Fatalf("second Take error = %v, want ErrClaimed", err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := Take(ctx, db, claim("owner/a", 7, "answer:7")); err != nil {
		t.Fatalf("Take after Release: %v", err)
	}
}

func TestStartFailedGivesSecondAttempt(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := Release(ctx, db, r.ID, StartFailed, "no workspace", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	r2, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatalf("second Take: %v", err)
	}
	if r2.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", r2.Attempt)
	}
	var outcome, endedAt string
	if err := db.QueryRow(`SELECT outcome, coalesce(ended_at, '') FROM runs WHERE id = ?`, r.ID).Scan(&outcome, &endedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if outcome != "start_failed" || endedAt == "" {
		t.Errorf("first row outcome = %q, ended_at = %q; want start_failed and a time", outcome, endedAt)
	}
}

func TestSecondStartFailedSpendsTrigger(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for range 2 {
		r, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
		if err != nil {
			t.Fatalf("Take: %v", err)
		}
		if err := Release(ctx, db, r.ID, StartFailed, "x", time.Now()); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}
	if _, err := Take(ctx, db, claim("owner/a", 7, "start:7")); !errors.Is(err, ErrTriggerSpent) {
		t.Fatalf("Take error = %v, want ErrTriggerSpent", err)
	}
}

func TestOtherOutcomeSpendsTrigger(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := Release(ctx, db, r.ID, Returned, "", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := Take(ctx, db, claim("owner/a", 7, "start:7")); !errors.Is(err, ErrTriggerSpent) {
		t.Fatalf("Take error = %v, want ErrTriggerSpent", err)
	}
}

func TestOpenRowSpendsTrigger(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := Take(ctx, db, claim("owner/a", 7, "start:7")); err != nil {
		t.Fatalf("Take: %v", err)
	}
	// Same key while the row is open: spent, not merely claimed.
	if _, err := Take(ctx, db, claim("owner/a", 7, "start:7")); !errors.Is(err, ErrTriggerSpent) {
		t.Fatalf("Take error = %v, want ErrTriggerSpent", err)
	}
}

func TestTriggerKeyPerRepo(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	r2, err := Take(ctx, db, claim("owner/b", 7, "start:7"))
	if err != nil {
		t.Fatalf("Take other repo: %v", err)
	}
	if r2.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", r2.Attempt)
	}
}

func TestReleaseTwice(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := Release(ctx, db, r.ID, Outcome("bogus"), "", time.Now()); err == nil {
		t.Error("Release with unknown outcome succeeded")
	}
	var open int
	if err := db.QueryRow(`SELECT count(*) FROM runs WHERE id = ? AND ended_at IS NULL`, r.ID).Scan(&open); err != nil {
		t.Fatalf("query: %v", err)
	}
	if open != 1 {
		t.Fatalf("bogus Release changed the row")
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("second Release error = %v, want ErrNotOpen", err)
	}
}

func TestNewSessionID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := NewSessionID(), NewSessionID()
	if a == b {
		t.Errorf("two ids equal: %s", a)
	}
	for _, id := range []string{a, b} {
		if !re.MatchString(id) {
			t.Errorf("id %q does not match UUID v4", id)
		}
	}
}

func TestOpenListsOnlyOpenRunsOfRepo(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	nine, err := Take(ctx, db, claim("o/r", 9, "start:9"))
	if err != nil {
		t.Fatalf("Take 9: %v", err)
	}
	three := claim("o/r", 3, "start:3")
	if _, err := Take(ctx, db, three); err != nil {
		t.Fatalf("Take 3: %v", err)
	}
	if _, err := Take(ctx, db, claim("o/x", 5, "start:5")); err != nil {
		t.Fatalf("Take 5: %v", err)
	}
	if err := Release(ctx, db, nine.ID, Done, "", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	open, err := Open(ctx, db, "o/r")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 1 || open[0].Issue != 3 {
		t.Fatalf("Open = %+v, want only issue 3", open)
	}
	got := open[0]
	if !got.StartedAt.Equal(three.Now) || got.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt = %v, want %v in UTC", got.StartedAt, three.Now)
	}
	if got.TriggerKey != "start:3" || got.SessionID != three.SessionID {
		t.Errorf("TriggerKey, SessionID = %q, %q; want %q, %q", got.TriggerKey, got.SessionID, "start:3", three.SessionID)
	}
}
