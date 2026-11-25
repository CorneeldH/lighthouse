package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Dimension is what a version change is about.
type Dimension string

const (
	DimSuperpowers Dimension = "superpowers"
	DimModel       Dimension = "model"
	DimHarness     Dimension = "harness"
)

// VersionChange is one switch of a version inside one series, derived from setups.
type VersionChange struct {
	Dimension Dimension
	// Series is "" for superpowers, the harness name for harness and the lane for model
	// ("" = no lane).
	Series   string
	Old, New string
	// RunID or SessionID is the first run or session seen with New.
	RunID, SessionID sql.NullInt64
	At               time.Time // started_at of that run or session
	// Skip marks an older-than-seen version that is never proposed.
	Skip bool
}

// ErrNoSetups means the database predates the setups table.
var ErrNoSetups = errors.New("telemetry: setups table missing")

// versionSelect is one SELECT per dimension: series, value, started_at, setups.id, run id, session id.
// Rows of a run and of a session join setups through their own foreign key; both only count with
// assignment 'standard' and a non-NULL value. Model counts runs only (the dispatcher), per lane.
var versionSelects = map[Dimension]string{
	DimSuperpowers: `
SELECT '' AS series, s.superpowers_version AS val, r.started_at AS at, s.id AS sid, r.id AS rid, NULL AS ssid
  FROM setups s JOIN runs r ON s.run_id = r.id
  WHERE r.assignment = 'standard' AND s.superpowers_version IS NOT NULL
UNION ALL
SELECT '', s.superpowers_version, x.started_at, s.id, NULL, x.id
  FROM setups s JOIN sessions x ON s.session_id = x.id
  WHERE x.assignment = 'standard' AND s.superpowers_version IS NOT NULL
ORDER BY at, sid`,
	DimModel: `
SELECT coalesce(s.lane, ''), s.model, r.started_at AS at, s.id AS sid, r.id, NULL
  FROM setups s JOIN runs r ON s.run_id = r.id
  WHERE r.assignment = 'standard' AND s.model IS NOT NULL
ORDER BY at, sid`,
	DimHarness: `
SELECT coalesce(s.harness, ''), s.harness_version, r.started_at AS at, s.id AS sid, r.id, NULL
  FROM setups s JOIN runs r ON s.run_id = r.id
  WHERE r.assignment = 'standard' AND s.harness_version IS NOT NULL
UNION ALL
SELECT coalesce(s.harness, ''), s.harness_version, x.started_at, s.id, NULL, x.id
  FROM setups s JOIN sessions x ON s.session_id = x.id
  WHERE x.assignment = 'standard' AND s.harness_version IS NOT NULL
ORDER BY at, sid`,
}

// VersionChanges derives the version switches from setups, per dimension and series, in the order
// of started_at (then setups.id). A value seen before in its series is not a switch back to be
// reported again: only a value new to the series, differing from the last one, is a change.
// The result is sorted by At, then series. It only reads.
func VersionChanges(ctx context.Context, db *sql.DB) ([]VersionChange, error) {
	var out []VersionChange
	for _, dim := range []Dimension{DimSuperpowers, DimModel, DimHarness} {
		got, err := dimensionChanges(ctx, db, dim)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].Series < out[j].Series
	})
	return out, nil
}

func dimensionChanges(ctx context.Context, db *sql.DB, dim Dimension) ([]VersionChange, error) {
	rows, err := db.QueryContext(ctx, versionSelects[dim])
	if err != nil {
		if strings.Contains(err.Error(), "no such table: setups") {
			return nil, ErrNoSetups
		}
		return nil, fmt.Errorf("telemetry: version changes (%s): %w", dim, err)
	}
	defer func() { _ = rows.Close() }()

	type seen struct {
		last   string
		values map[string]bool
	}
	bySeries := map[string]*seen{}
	var out []VersionChange
	for rows.Next() {
		var series, val, at string
		var sid int64
		var runID, sessID sql.NullInt64
		if err := rows.Scan(&series, &val, &at, &sid, &runID, &sessID); err != nil {
			return nil, fmt.Errorf("telemetry: version changes (%s): %w", dim, err)
		}
		s := bySeries[series]
		if s == nil {
			bySeries[series] = &seen{last: val, values: map[string]bool{val: true}}
			continue
		}
		if val == s.last || s.values[val] {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, fmt.Errorf("telemetry: version changes (%s): started_at %q: %w", dim, at, err)
		}
		out = append(out, VersionChange{
			Dimension: dim, Series: series, Old: s.last, New: val,
			RunID: runID, SessionID: sessID, At: ts,
		})
		s.last = val
		s.values[val] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: version changes (%s): %w", dim, err)
	}
	return out, nil
}
