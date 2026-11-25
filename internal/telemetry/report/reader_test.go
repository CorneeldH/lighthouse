package report

import (
	"context"
	"testing"
	"time"
)

func TestReaderLoadsAFreshDatasetPerCall(t *testing.T) {
	db, f := newFixture(t)
	r := NewReader(db, func() time.Time { return at(0) })
	ctx := context.Background()
	f.addTrajectory("o/r", 1)
	rows, err := r.Trajectories(ctx, Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("first Trajectories = %v, %v; want one row", rows, err)
	}
	id := f.addTrajectory("o/r", 2)
	rows, err = r.Trajectories(ctx, Filter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("second Trajectories = %v, %v; want the row written after the first call", rows, err)
	}
	if _, ok, err := r.Detail(ctx, id); err != nil || !ok {
		t.Errorf("Detail(%d) = %v, %v; want found", id, ok, err)
	}
}

func TestReaderMissingTablesGiveEmptyReports(t *testing.T) {
	db, f := newFixture(t)
	id := f.addTrajectory("o/r", 1)
	if _, err := db.Exec(`DROP TABLE review_runs`); err != nil {
		t.Fatal(err)
	}
	r := NewReader(db, func() time.Time { return at(0) })
	ctx := context.Background()

	c, err := r.Components(ctx, Filter{})
	if err != nil || c.Blocks == nil || len(c.Blocks) != 0 || c.Options.Repos == nil {
		t.Errorf("Components = %+v, %v; want no blocks as a non-nil empty slice", c, err)
	}
	tl, err := r.Timeline(ctx, Filter{})
	if err != nil || tl.Weeks == nil || len(tl.Weeks) != 0 {
		t.Errorf("Timeline = %+v, %v; want no weeks", tl, err)
	}
	rows, err := r.Trajectories(ctx, Filter{})
	if err != nil || rows == nil || len(rows) != 0 {
		t.Errorf("Trajectories = %v, %v; want a non-nil empty slice", rows, err)
	}
	if _, ok, err := r.Detail(ctx, id); err != nil || ok {
		t.Errorf("Detail = %v, %v; want not found and no error", ok, err)
	}
	cov, err := r.Coverage(ctx, "")
	if err != nil || !cov.NotMigrated {
		t.Errorf("Coverage = %+v, %v; want not migrated", cov, err)
	}
}
