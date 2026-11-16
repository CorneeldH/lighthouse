package report

import (
	"strings"
	"testing"
	"time"
)

func amsterdam(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	return loc
}

func TestWeekAcrossISOYear(t *testing.T) {
	loc := amsterdam(t)
	p := NewPeriod(KindWeek, time.Date(2024, 12, 31, 15, 0, 0, 0, loc))
	if p.Label != "2025-W01" {
		t.Errorf("label = %q, want 2025-W01", p.Label)
	}
	if want := time.Date(2024, 12, 30, 0, 0, 0, 0, loc); !p.Since.Equal(want) {
		t.Errorf("since = %v, want %v", p.Since, want)
	}
	if want := time.Date(2025, 1, 6, 0, 0, 0, 0, loc); !p.Until.Equal(want) {
		t.Errorf("until = %v, want %v", p.Until, want)
	}
}

func TestDayLabelAndBounds(t *testing.T) {
	loc := amsterdam(t)
	p := NewPeriod(KindDay, time.Date(2026, 11, 16, 13, 30, 0, 0, loc))
	if p.Label != "2026-11-16" {
		t.Errorf("label = %q", p.Label)
	}
	if want := time.Date(2026, 11, 16, 0, 0, 0, 0, loc); !p.Since.Equal(want) {
		t.Errorf("since = %v", p.Since)
	}
	if want := time.Date(2026, 11, 17, 0, 0, 0, 0, loc); !p.Until.Equal(want) {
		t.Errorf("until = %v", p.Until)
	}
}

func TestMonthLabelLeapFebruary(t *testing.T) {
	loc := amsterdam(t)
	p := NewPeriod(KindMonth, time.Date(2028, 2, 10, 9, 0, 0, 0, loc))
	if p.Label != "2028-02" {
		t.Errorf("label = %q", p.Label)
	}
	if want := time.Date(2028, 3, 1, 0, 0, 0, 0, loc); !p.Until.Equal(want) {
		t.Errorf("until = %v", p.Until)
	}
	if days := p.Until.Sub(p.Since) / (24 * time.Hour); days != 29 {
		t.Errorf("days = %d, want 29", days)
	}
}

func TestDSTDayLengths(t *testing.T) {
	loc := amsterdam(t)
	tests := []struct {
		name string
		at   time.Time
		want time.Duration
	}{
		{"spring forward", time.Date(2025, 3, 30, 12, 0, 0, 0, loc), 23 * time.Hour},
		{"fall back", time.Date(2025, 10, 26, 12, 0, 0, 0, loc), 25 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPeriod(KindDay, tt.at)
			if got := p.Until.Sub(p.Since); got != tt.want {
				t.Errorf("length = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWeekAcrossDST(t *testing.T) {
	loc := amsterdam(t)
	p := NewPeriod(KindWeek, time.Date(2025, 3, 30, 12, 0, 0, 0, loc))
	if want := time.Date(2025, 3, 24, 0, 0, 0, 0, loc); !p.Since.Equal(want) {
		t.Errorf("since = %v, want %v", p.Since, want)
	}
	if want := time.Date(2025, 3, 31, 0, 0, 0, 0, loc); !p.Until.Equal(want) {
		t.Errorf("until = %v, want %v", p.Until, want)
	}
	if got := p.Until.Sub(p.Since); got != 7*24*time.Hour-time.Hour {
		t.Errorf("length = %v, want 167h", got)
	}
	if p.Label != "2025-W13" {
		t.Errorf("label = %q", p.Label)
	}
}

func TestParseKind(t *testing.T) {
	for _, s := range []string{"day", "week", "month"} {
		k, err := ParseKind(s)
		if err != nil || string(k) != s {
			t.Errorf("ParseKind(%q) = %q, %v", s, k, err)
		}
	}
}

func TestParseKindRejectsOther(t *testing.T) {
	_, err := ParseKind("year")
	if err == nil || !strings.Contains(err.Error(), "day|week|month") {
		t.Errorf("err = %v, want mention of day|week|month", err)
	}
}
