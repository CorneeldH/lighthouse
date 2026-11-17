// Package cron parses five-field cron expressions and finds the most recent
// scheduled slot at or before a moment, optionally thinned to every n-th week.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// lookback is how far back Last searches for a slot.
const lookback = 400 * 24 * 60 // minutes

// Expr is a parsed cron expression: minute hour day-of-month month day-of-week.
type Expr struct {
	minute, hour, dom, month, dow uint64 // bit sets
	domStar, dowStar              bool   // field started with "*"
}

// Filter thins slots to every Weeks-th week, counted from the week that
// contains Anchor (weeks start on Monday; the anchor week counts). The zero
// value applies no filter.
type Filter struct {
	Weeks  int
	Anchor time.Time
}

var monthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var dowNames = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
}

// Parse parses a five-field cron expression. It supports "*", "*/n", lists,
// ranges (with optional /n step) and month and weekday names.
func Parse(expr string) (Expr, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return Expr{}, fmt.Errorf("cron: want 5 fields, got %d in %q", len(f), expr)
	}
	var e Expr
	var err error
	if e.minute, err = parseField(f[0], 0, 59, nil); err != nil {
		return Expr{}, fmt.Errorf("cron: minute: %w", err)
	}
	if e.hour, err = parseField(f[1], 0, 23, nil); err != nil {
		return Expr{}, fmt.Errorf("cron: hour: %w", err)
	}
	if e.dom, err = parseField(f[2], 1, 31, nil); err != nil {
		return Expr{}, fmt.Errorf("cron: day of month: %w", err)
	}
	if e.month, err = parseField(f[3], 1, 12, monthNames); err != nil {
		return Expr{}, fmt.Errorf("cron: month: %w", err)
	}
	if e.dow, err = parseField(f[4], 0, 7, dowNames); err != nil {
		return Expr{}, fmt.Errorf("cron: day of week: %w", err)
	}
	if e.dow&(1<<7) != 0 { // 7 is Sunday too
		e.dow |= 1
	}
	e.domStar = strings.HasPrefix(f[2], "*")
	e.dowStar = strings.HasPrefix(f[4], "*")
	return e, nil
}

func parseField(s string, lo, hi int, names map[string]int) (uint64, error) {
	var set uint64
	for _, part := range strings.Split(s, ",") {
		rng, stepStr, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("bad step %q", stepStr)
			}
			step = n
		}
		var a, b int
		if rng == "*" {
			a, b = lo, hi
			if hi == 7 { // day of week: * means 0-6
				b = 6
			}
		} else {
			from, to, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = parseValue(from, names); err != nil {
				return 0, err
			}
			b = a
			if isRange {
				if hi == 7 && strings.EqualFold(to, "SUN") {
					b = 7 // a weekday range may end in Sunday: MON-SUN
				} else if b, err = parseValue(to, names); err != nil {
					return 0, err
				}
			} else if hasStep {
				b = hi
			}
		}
		if a < lo || b > hi || a > b {
			return 0, fmt.Errorf("value out of range %d-%d in %q", lo, hi, part)
		}
		for v := a; v <= b; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

func parseValue(s string, names map[string]int) (int, error) {
	if v, ok := names[strings.ToUpper(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("bad value %q", s)
	}
	return v, nil
}

// matches reports whether the wall-clock minute w (a UTC-labelled civil time)
// is a slot of e.
func (e Expr) matches(w time.Time) bool {
	if e.minute&(1<<uint(w.Minute())) == 0 || e.hour&(1<<uint(w.Hour())) == 0 ||
		e.month&(1<<uint(w.Month())) == 0 {
		return false
	}
	domOK := e.dom&(1<<uint(w.Day())) != 0
	dowOK := e.dow&(1<<uint(w.Weekday())) != 0
	if !e.domStar && !e.dowStar { // Vixie-cron: OR when both are restricted
		return domOK || dowOK
	}
	return domOK && dowOK
}

// civilDays is the number of days from the Unix epoch to the civil date of t.
func civilDays(t time.Time) int {
	y, m, d := t.Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// mondayIndex is the day number of the Monday of the week containing t.
func mondayIndex(t time.Time) int {
	days := civilDays(t)
	wd := (int(t.Weekday()) + 6) % 7 // Monday = 0
	return days - wd
}

func (f Filter) allows(w time.Time) bool {
	if f.Weeks <= 0 {
		return true
	}
	weeks := (mondayIndex(w) - mondayIndex(f.Anchor)) / 7 // both are Mondays: exact
	return ((weeks%f.Weeks)+f.Weeks)%f.Weeks == 0
}

// Last returns the latest slot at or before now that passes f, searching at
// most 400 days back. It walks back minute by minute in wall-clock time of
// now's location: a wall-clock minute that does not exist (spring DST gap) is
// skipped, a repeated one (autumn) counts once, at its first occurrence.
func (e Expr) Last(now time.Time, f Filter) (time.Time, bool) {
	loc := now.Location()
	w := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, time.UTC)
	if sameWall(now.Add(-time.Hour), w) {
		// now is in the second pass of a repeated hour: every wall minute of
		// that hour already had its first occurrence, so start at its end.
		w = w.Add(time.Duration(59-w.Minute()) * time.Minute)
	}
	for i := 0; i < lookback; i, w = i+1, w.Add(-time.Minute) {
		if !e.matches(w) || !f.allows(w) {
			continue
		}
		t := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, loc)
		if !sameWall(t, w) {
			continue // minute skipped by a DST gap
		}
		if prev := t.Add(-time.Hour); sameWall(prev, w) {
			t = prev // repeated hour: first occurrence
		}
		if t.After(now) {
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

func sameWall(t, w time.Time) bool {
	return t.Year() == w.Year() && t.Month() == w.Month() && t.Day() == w.Day() &&
		t.Hour() == w.Hour() && t.Minute() == w.Minute()
}

// Slot formats t as YYYYMMDDTHHMM in t's own location.
func Slot(t time.Time) string {
	return t.Format("20060102T1504")
}
