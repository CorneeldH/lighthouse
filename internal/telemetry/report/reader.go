package report

import (
	"context"
	"database/sql"
	"time"
)

// Reader serves the telemetry reports of the UI from db, loading a fresh
// Dataset at now() for every call so rows written since the last call show.
// An unmigrated database (Dataset.Missing) is never an error: the lists are
// empty, the detail is not found and Coverage reports it.
type Reader struct {
	db  *sql.DB
	now func() time.Time
}

// NewReader returns a Reader of db; db is meant to be a read-only live
// connection (store.OpenReadOnlyLive).
func NewReader(db *sql.DB, now func() time.Time) *Reader {
	return &Reader{db: db, now: now}
}

func (r *Reader) load(ctx context.Context) (*Dataset, error) {
	return Load(ctx, r.db, r.now())
}

// Components aggregates the dataset under f; no blocks when tables are missing.
func (r *Reader) Components(ctx context.Context, f Filter) (ComponentsReport, error) {
	d, err := r.load(ctx)
	if err != nil {
		return ComponentsReport{}, err
	}
	if len(d.Missing) > 0 {
		return ComponentsReport{Blocks: []Block{}, Options: options(d)}, nil
	}
	return Components(d, f), nil
}

// Timeline builds the weekly timeline under f.
func (r *Reader) Timeline(ctx context.Context, f Filter) (TimelineReport, error) {
	d, err := r.load(ctx)
	if err != nil {
		return TimelineReport{}, err
	}
	return Timeline(ctx, d, r.db, f)
}

// Trajectories lists the trajectories under f.
func (r *Reader) Trajectories(ctx context.Context, f Filter) ([]ListRow, error) {
	d, err := r.load(ctx)
	if err != nil {
		return nil, err
	}
	return Trajectories(d, f), nil
}

// Detail reads trajectory id; false when there is none or the database lacks
// a table LoadDetail reads.
func (r *Reader) Detail(ctx context.Context, id int64) (Detail, bool, error) {
	missing, err := missingTables(ctx, r.db)
	if err != nil {
		return Detail{}, false, err
	}
	if len(missing) > 0 {
		return Detail{}, false, nil
	}
	return LoadDetail(ctx, r.db, id, r.now())
}

// Coverage lists the gaps for repo ("" is every repo).
func (r *Reader) Coverage(ctx context.Context, repo string) (CoverageReport, error) {
	d, err := r.load(ctx)
	if err != nil {
		return CoverageReport{}, err
	}
	return Coverage(ctx, d, r.db, repo)
}
