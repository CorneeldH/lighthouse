package cron

import (
	"testing"
	"time"
)

func ams(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return loc
}

func TestLast(t *testing.T) {
	loc := ams(t)
	d := func(y int, m time.Month, day, h, min int) time.Time {
		return time.Date(y, m, day, h, min, 0, 0, loc)
	}
	anchor := d(2026, 10, 12, 0, 0)
	biweekly := Filter{Weeks: 2, Anchor: anchor}

	tests := []struct {
		name   string
		expr   string
		filter Filter
		now    time.Time
		want   time.Time
		wantOK bool
	}{
		{"monthly", "0 4 1 * *", Filter{}, d(2026, 11, 16, 12, 0), d(2026, 11, 1, 4, 0), true},
		{"biweekly counting week", "0 1 * * MON", biweekly, d(2026, 11, 16, 12, 0), d(2026, 11, 9, 1, 0), true},
		{"biweekly odd week skipped, sunday", "0 1 * * MON", biweekly, d(2026, 11, 22, 12, 0), d(2026, 11, 9, 1, 0), true},
		{"biweekly next counting week", "0 1 * * MON", biweekly, d(2026, 11, 23, 1, 0), d(2026, 11, 23, 1, 0), true},
		{"biweekly anchor week itself counts", "0 1 * * MON", biweekly, d(2026, 10, 19, 12, 0), d(2026, 10, 12, 1, 0), true},
		{"now exactly on slot", "30 2 * * *", Filter{}, d(2026, 11, 16, 2, 30), d(2026, 11, 16, 2, 30), true},
		{"seconds after slot truncated", "30 2 * * *", Filter{}, d(2026, 11, 16, 2, 30).Add(59 * time.Second), d(2026, 11, 16, 2, 30), true},
		{"minute before slot", "30 2 * * *", Filter{}, d(2026, 11, 16, 2, 29), d(2026, 11, 15, 2, 30), true},
		{"spring skipped slot noon", "30 2 * * *", Filter{}, d(2026, 3, 29, 12, 0), d(2026, 3, 28, 2, 30), true},
		{"spring skipped slot evening", "30 2 * * *", Filter{}, d(2026, 3, 29, 23, 0), d(2026, 3, 28, 2, 30), true},
		{"spring day after", "30 2 * * *", Filter{}, d(2026, 3, 30, 12, 0), d(2026, 3, 30, 2, 30), true},
		{"vixie OR matches dow only", "0 0 14 * FRI", Filter{}, d(2026, 11, 13, 12, 0), d(2026, 11, 13, 0, 0), true},
		{"vixie OR matches dom only", "0 0 14 * FRI", Filter{}, d(2026, 11, 15, 12, 0), d(2026, 11, 14, 0, 0), true},
		{"dow only restricts", "0 0 * * 1-5", Filter{}, d(2026, 11, 15, 12, 0), d(2026, 11, 13, 0, 0), true},
		{"sunday as 7", "0 0 * * 7", Filter{}, d(2026, 11, 16, 12, 0), d(2026, 11, 15, 0, 0), true},
		{"step", "*/15 * * * *", Filter{}, d(2026, 11, 16, 12, 44), d(2026, 11, 16, 12, 30), true},
		{"list", "0 0 1,15 * *", Filter{}, d(2026, 11, 16, 12, 0), d(2026, 11, 15, 0, 0), true},
		{"range with step", "0 8-18/5 * * *", Filter{}, d(2026, 11, 16, 17, 0), d(2026, 11, 16, 13, 0), true},
		{"month name", "0 0 1 JAN *", Filter{}, d(2026, 11, 16, 12, 0), d(2026, 1, 1, 0, 0), true},
		{"no slot within 400 days", "0 0 30 FEB *", Filter{}, d(2026, 11, 16, 12, 0), time.Time{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Parse(tc.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.expr, err)
			}
			got, ok := e.Last(tc.now, tc.filter)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %v)", ok, tc.wantOK, got)
			}
			if ok && !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLastAutumnDuplicateHour(t *testing.T) {
	loc := ams(t)
	e, err := Parse("30 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC) // 02:30 CEST
	second := first.Add(time.Hour)                          // 02:30 CET
	for _, now := range []time.Time{
		time.Date(2026, 10, 25, 12, 0, 0, 0, loc),
		second.In(loc),
	} {
		got, ok := e.Last(now, Filter{})
		if !ok || !got.Equal(first) {
			t.Fatalf("now %v: got %v ok=%v, want first occurrence %v", now, got, ok, first)
		}
	}
	// Between the two occurrences (first 02:30 already passed): still the first.
	got, _ := e.Last(first.Add(30*time.Minute).In(loc), Filter{})
	if !got.Equal(first) {
		t.Fatalf("got %v, want %v", got, first)
	}
	// Day after: previous slot is the first occurrence of 25 Oct, once.
	got, _ = e.Last(time.Date(2026, 10, 26, 1, 0, 0, 0, loc), Filter{})
	if !got.Equal(first) {
		t.Fatalf("got %v, want %v", got, first)
	}
}

func TestParseErrors(t *testing.T) {
	for _, expr := range []string{
		"0 4 1 *", "61 * * * *", "0 0 * * FUNDAG", "", "* * * * * *",
		"0 24 * * *", "0 0 0 * *", "0 0 * 13 *", "0 0 * * 8",
		"5-1 * * * *", "*/0 * * * *", "a * * * *", "1,,2 * * * *",
	} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q): want error", expr)
		}
	}
}

func TestSlot(t *testing.T) {
	loc := ams(t)
	got := Slot(time.Date(2026, 3, 9, 4, 5, 33, 0, loc))
	if got != "20260309T0405" {
		t.Fatalf("Slot = %q", got)
	}
	// Location of t is used, not UTC.
	if got := Slot(time.Date(2026, 7, 1, 0, 30, 0, 0, loc)); got != "20260701T0030" {
		t.Fatalf("Slot = %q", got)
	}
}
