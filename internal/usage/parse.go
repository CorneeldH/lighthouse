// Package usage reads the Claude subscription usage windows from the CLI.
package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Window is one usage window as /usage reports it.
type Window struct {
	Pct   int
	Reset time.Time // zero: /usage named no reset (an unused session window)
}

// Windows holds the two windows the meter tracks.
type Windows struct{ Session, Week Window }

var (
	sessionRE = regexp.MustCompile(`(?m)Current session:\s*(\d+)%\s+used(?:\s*·\s*resets\s+(.+?)\s*\(|[ \t]*$)`)
	weekRE    = regexp.MustCompile(`Current week \(all models\):\s*(\d+)%\s+used\s*·\s*resets\s+(.+?)\s*\(`)
	resetRE   = regexp.MustCompile(`(?i)^([A-Za-z]{3})\s+(\d{1,2}),\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)$`)
)

var months = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// Parse reads the JSON envelope of `claude -p /usage --output-format json`.
// It is all or nothing: a missing window or an unreadable reset time is an
// error and the returned Windows is zero. Reset times carry no year; they are
// read in loc relative to now.
func Parse(envelope []byte, now time.Time, loc *time.Location) (Windows, error) {
	var env struct {
		Result  *string
		IsError bool `json:"is_error"`
	}
	if err := json.Unmarshal(envelope, &env); err != nil {
		return Windows{}, fmt.Errorf("usage envelope: %w", err)
	}
	if env.IsError {
		return Windows{}, errors.New("usage envelope reports is_error")
	}
	if env.Result == nil {
		return Windows{}, errors.New("usage envelope has no result text")
	}
	text := *env.Result

	m := sessionRE.FindStringSubmatch(text)
	if m == nil {
		return Windows{}, errors.New("no session window in /usage output")
	}
	session, err := window(m[1], m[2], now, loc)
	if err != nil {
		return Windows{}, fmt.Errorf("session window: %w", err)
	}

	m = weekRE.FindStringSubmatch(text)
	if m == nil {
		return Windows{}, errors.New("no week window in /usage output")
	}
	week, err := window(m[1], m[2], now, loc)
	if err != nil {
		return Windows{}, fmt.Errorf("week window: %w", err)
	}
	return Windows{Session: session, Week: week}, nil
}

// window builds a Window; an empty reset text leaves Reset zero.
func window(pct, reset string, now time.Time, loc *time.Location) (Window, error) {
	n, err := strconv.Atoi(pct)
	if err != nil {
		return Window{}, err
	}
	w := Window{Pct: n}
	if reset == "" {
		return w, nil
	}
	w.Reset, err = parseReset(reset, now, loc)
	return w, err
}

// parseReset turns "Sep 6, 2:30pm" into a UTC instant.
func parseReset(text string, now time.Time, loc *time.Location) (time.Time, error) {
	m := resetRE.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, fmt.Errorf("unreadable reset time %q", text)
	}
	month, ok := months[strings.ToLower(m[1])]
	if !ok {
		return time.Time{}, fmt.Errorf("unknown month in reset time %q", text)
	}
	day, _ := strconv.Atoi(m[2])
	hour, _ := strconv.Atoi(m[3])
	minute := 0
	if m[4] != "" {
		minute, _ = strconv.Atoi(m[4])
	}
	hour %= 12
	if strings.EqualFold(m[5], "pm") {
		hour += 12
	}

	year := now.In(loc).Year()
	t := time.Date(year, month, day, hour, minute, 0, 0, loc)
	if t.Month() != month || t.Day() != day {
		return time.Time{}, fmt.Errorf("reset time %q does not exist", text)
	}
	// No year in the text: a date far behind now is next year's. Where the day
	// does not exist next year (29 February), the current year stays.
	if t.Before(now.AddDate(0, 0, -31)) {
		next := time.Date(year+1, month, day, hour, minute, 0, 0, loc)
		if next.Month() == month && next.Day() == day {
			t = next
		}
	}
	return t.UTC(), nil
}
