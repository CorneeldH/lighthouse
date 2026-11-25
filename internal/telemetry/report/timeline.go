package report

// This file builds the weekly timeline of spec §6: per ISO week (Monday,
// UTC) the priced cost by usage.recorded_at, the human questions by their
// time, and the trajectories that closed that week with how many recovered.
// A week before the first row of a series is nil; after it, an empty week is
// 0. The filter applies through the trajectories of the Dataset.

import (
	"context"
	"database/sql"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// Week is one ISO week of the timeline. Start is its Monday (UTC) as
// "2006-01-02". A nil field is a week before the first row of its series.
type Week struct {
	Start     string   `json:"start"`
	Cost      *float64 `json:"cost"`
	Questions *int     `json:"questions"`
	Closed    *int     `json:"closed"`
	Recovered *int     `json:"recovered"`
}

// Marker is an event drawn on the timeline as a vertical line; #380 fills
// them.
type Marker struct {
	Week string `json:"week"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// TimelineReport is the weekly timeline. Weeks and Markers are never nil.
type TimelineReport struct {
	Weeks   []Week   `json:"weeks"`
	Markers []Marker `json:"markers"`
}

const weekLayout = "2006-01-02"

// weekStart returns the Monday 00:00 UTC of the week of t.
func weekStart(t time.Time) time.Time {
	u := t.UTC()
	day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// series sums values per week and knows its first week.
type series struct {
	sum   map[time.Time]float64
	first time.Time
	seen  bool
}

func newSeries() *series { return &series{sum: map[time.Time]float64{}} }

func (s *series) add(at time.Time, v float64) {
	w := weekStart(at)
	s.sum[w] += v
	if !s.seen || w.Before(s.first) {
		s.first, s.seen = w, true
	}
}

// float returns the value of week w: nil before the first week.
func (s *series) float(w time.Time) *float64 {
	if !s.seen || w.Before(s.first) {
		return nil
	}
	v := s.sum[w]
	return &v
}

func (s *series) int(w time.Time) *int {
	v := s.float(w)
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// Timeline builds the weekly timeline of d under f, reading the priced usage
// and the questions from db. The weeks run from the first week of any series
// to the week of d.Now (or the last week with data, if later).
func Timeline(d *Dataset, db *sql.DB, f Filter) (TimelineReport, error) {
	r := TimelineReport{Weeks: []Week{}, Markers: []Marker{}}
	if len(d.Missing) > 0 {
		return r, nil
	}
	in := map[int64]bool{}
	cost, questions, closed := newSeries(), newSeries(), newSeries()
	recovered := map[time.Time]int{}
	for i := range d.Trajectories {
		t := &d.Trajectories[i]
		if !f.Match(t) {
			continue
		}
		in[t.ID] = true
		if t.Closed && t.ClosedAt != nil {
			closed.add(*t.ClosedAt, 1)
			if hasRecovered(t) {
				recovered[weekStart(*t.ClosedAt)]++
			}
		}
	}
	l := loader{ctx: context.Background(), db: db}
	err := l.each("usage", `SELECT `+ownerTrajectory+`, x.recorded_at, x.cost FROM usage x
		LEFT JOIN runs r ON r.id = x.run_id LEFT JOIN sessions s ON s.id = x.session_id
		WHERE x.cost IS NOT NULL`, func(rows *sql.Rows) error {
		var tid sql.NullInt64
		var recorded string
		var c float64
		if err := rows.Scan(&tid, &recorded, &c); err != nil {
			return err
		}
		if !tid.Valid || !in[tid.Int64] {
			return nil
		}
		when, err := parseTime(recorded)
		if err != nil {
			return err
		}
		cost.add(when, c)
		return nil
	})
	if err != nil {
		return TimelineReport{}, err
	}
	err = l.each("trajectory_events", `SELECT trajectory_id, at FROM trajectory_events WHERE kind = 'question'`,
		func(rows *sql.Rows) error {
			var tid int64
			var stamp string
			if err := rows.Scan(&tid, &stamp); err != nil {
				return err
			}
			if !in[tid] {
				return nil
			}
			when, err := parseTime(stamp)
			if err != nil {
				return err
			}
			questions.add(when, 1)
			return nil
		})
	if err != nil {
		return TimelineReport{}, err
	}

	var from, to time.Time
	found := false
	for _, s := range []*series{cost, questions, closed} {
		for w := range s.sum {
			if !found || w.Before(from) {
				from = w
			}
			if !found || w.After(to) {
				to = w
			}
			found = true
		}
	}
	if !found {
		return r, nil
	}
	if now := weekStart(d.Now); now.After(to) {
		to = now
	}
	for w := from; !w.After(to); w = w.AddDate(0, 0, 7) {
		week := Week{Start: w.Format(weekLayout), Cost: cost.float(w), Questions: questions.int(w), Closed: closed.int(w)}
		if week.Closed != nil {
			n := recovered[w]
			week.Recovered = &n
		}
		r.Weeks = append(r.Weeks, week)
	}
	return r, nil
}

// hasRecovered reports whether a PR of t has status recovered.
func hasRecovered(t *Trajectory) bool {
	for _, rec := range t.Recoveries {
		if rec.Status == string(telemetry.RecoveryRecovered) {
			return true
		}
	}
	return false
}
