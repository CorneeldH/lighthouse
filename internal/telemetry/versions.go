package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/pins"
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
`,
	DimModel: `
SELECT coalesce(s.lane, ''), s.model, r.started_at AS at, s.id AS sid, r.id, NULL
  FROM setups s JOIN runs r ON s.run_id = r.id
  WHERE r.assignment = 'standard' AND s.model IS NOT NULL
`,
	DimHarness: `
SELECT s.harness, s.harness_version, r.started_at AS at, s.id AS sid, r.id, NULL
  FROM setups s JOIN runs r ON s.run_id = r.id
  WHERE r.assignment = 'standard' AND s.harness_version IS NOT NULL AND s.harness IS NOT NULL
UNION ALL
SELECT s.harness, s.harness_version, x.started_at, s.id, NULL, x.id
  FROM setups s JOIN sessions x ON s.session_id = x.id
  WHERE x.assignment = 'standard' AND s.harness_version IS NOT NULL AND s.harness IS NOT NULL
`,
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

	type row struct {
		series, val string
		at          time.Time
		sid         int64
		runID, sess sql.NullInt64
	}
	var all []row
	for rows.Next() {
		var r row
		var at string
		if err := rows.Scan(&r.series, &r.val, &at, &r.sid, &r.runID, &r.sess); err != nil {
			return nil, fmt.Errorf("telemetry: version changes (%s): %w", dim, err)
		}
		ts, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, fmt.Errorf("telemetry: version changes (%s): started_at %q: %w", dim, at, err)
		}
		r.at = ts
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: version changes (%s): %w", dim, err)
	}
	// Sort on the parsed time: RFC3339Nano text does not sort chronologically (trailing zeros dropped).
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].at.Equal(all[j].at) {
			return all[i].at.Before(all[j].at)
		}
		return all[i].sid < all[j].sid
	})

	type seen struct {
		last   string
		prev   string // value of the previous row of the series
		values map[string]bool
		max    *pins.Version // highest parseable value seen so far
	}
	bySeries := map[string]*seen{}
	var out []VersionChange
	for _, r := range all {
		series, val := r.series, r.val
		s := bySeries[series]
		if s == nil {
			s = &seen{last: val, prev: val, values: map[string]bool{val: true}}
			if v, err := pins.ParseVersion(val); err == nil {
				s.max = &v
			}
			bySeries[series] = s
			continue
		}
		before := s.prev
		s.prev = val
		if val == s.last || s.values[val] {
			continue
		}
		skip := false
		if v, err := pins.ParseVersion(val); err == nil {
			if dim != DimModel && s.max != nil && v.Compare(*s.max) < 0 {
				skip = true
			}
			if s.max == nil || s.max.Compare(v) < 0 {
				s.max = &v
			}
		}
		out = append(out, VersionChange{
			Dimension: dim, Series: series, Old: before, New: val,
			RunID: r.runID, SessionID: r.sess, At: r.at, Skip: skip,
		})
		s.last = val
		s.values[val] = true
	}
	return out, nil
}

// VersionMarker is one version change placed on its ISO week for the charts.
type VersionMarker struct {
	Week time.Time // Monday 00:00 UTC of the ISO week of the change
	Kind Dimension
	Text string
}

// VersionMarkers returns a marker per version change, Skip changes included, in the order of
// VersionChanges. It only reads.
func VersionMarkers(ctx context.Context, db *sql.DB) ([]VersionMarker, error) {
	changes, err := VersionChanges(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make([]VersionMarker, 0, len(changes))
	for _, c := range changes {
		text := string(c.Dimension)
		if c.Series != "" {
			text += " " + c.Series
		}
		text += " " + c.Old + "→" + c.New
		out = append(out, VersionMarker{Week: isoWeekStart(c.At), Kind: c.Dimension, Text: text})
	}
	return out, nil
}

func isoWeekStart(t time.Time) time.Time {
	t = t.UTC()
	back := (int(t.Weekday()) + 6) % 7 // Monday = 0
	return time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, time.UTC)
}

// ComponentsFor names the components a change of the dimension can influence.
func ComponentsFor(d Dimension) []string {
	switch d {
	case DimSuperpowers:
		return []string{"task_review", "final_review"}
	case DimModel:
		return []string{"model"}
	case DimHarness:
		return []string{"harness"}
	}
	return nil
}

// LatestProposals keeps the last change per (dimension, series). A series whose last change is a
// Skip gives nothing, not an earlier change. The output order is the first appearance of each series.
func LatestProposals(changes []VersionChange) []VersionChange {
	type key struct {
		d Dimension
		s string
	}
	idx := map[key]int{}
	var last []VersionChange
	for _, c := range changes {
		k := key{c.Dimension, c.Series}
		if i, ok := idx[k]; ok {
			last[i] = c
			continue
		}
		idx[k] = len(last)
		last = append(last, c)
	}
	var out []VersionChange
	for _, c := range last {
		if !c.Skip && len(ComponentsFor(c.Dimension)) > 0 {
			out = append(out, c)
		}
	}
	return out
}
