// Package report builds the periodic report of a repository.
package report

import (
	"fmt"
	"time"
)

// Kind is the length of a report period.
type Kind string

const (
	KindDay   Kind = "day"
	KindWeek  Kind = "week"
	KindMonth Kind = "month"
)

// Period is the half-open interval [Since, Until) in the location of the
// moment it was built from.
type Period struct {
	Kind  Kind
	Since time.Time
	Until time.Time
	Label string
}

// ParseKind validates a period kind given on the command line.
func ParseKind(s string) (Kind, error) {
	switch k := Kind(s); k {
	case KindDay, KindWeek, KindMonth:
		return k, nil
	}
	return "", fmt.Errorf("unknown period %q: use day|week|month", s)
}

// NewPeriod returns the period of the given kind that contains at. Bounds are
// built with time.Date in at's location, so a day around a DST change is 23 or
// 25 hours long.
func NewPeriod(kind Kind, at time.Time) Period {
	loc := at.Location()
	y, m, d := at.Date()
	p := Period{Kind: kind}
	switch kind {
	case KindWeek:
		back := (int(at.Weekday()) + 6) % 7 // Monday is day 0
		p.Since = time.Date(y, m, d-back, 0, 0, 0, 0, loc)
		p.Until = time.Date(y, m, d-back+7, 0, 0, 0, 0, loc)
		iy, iw := at.ISOWeek()
		p.Label = fmt.Sprintf("%04d-W%02d", iy, iw)
	case KindMonth:
		p.Since = time.Date(y, m, 1, 0, 0, 0, 0, loc)
		p.Until = time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
		p.Label = p.Since.Format("2006-01")
	default:
		p.Kind = KindDay
		p.Since = time.Date(y, m, d, 0, 0, 0, 0, loc)
		p.Until = time.Date(y, m, d+1, 0, 0, 0, 0, loc)
		p.Label = p.Since.Format("2006-01-02")
	}
	return p
}
