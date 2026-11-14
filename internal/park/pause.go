package park

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PauseFile is PAUSE in LIGHTHOUSE_CONTROLLER_STATE, else in lighthouse/controller under
// XDG_STATE_HOME, else under $HOME/.local/state. A variable that is not an absolute path is
// passed over.
func PauseFile(getenv func(string) string) (string, error) {
	if dir := getenv("LIGHTHOUSE_CONTROLLER_STATE"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "PAUSE"), nil
	}
	base := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("none of LIGHTHOUSE_CONTROLLER_STATE, XDG_STATE_HOME and HOME is an absolute path: no place for the pause flag")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "lighthouse", "controller", "PAUSE"), nil
}

var runShape = regexp.MustCompile(`^[0-9a-f-]+$`)

// ControllerRun reports whether LIGHTHOUSE_CONTROLLER_RUN marks this process as a controller run:
// set, and only the characters of a uuid.
func ControllerRun(getenv func(string) string) bool {
	return runShape.MatchString(getenv("LIGHTHOUSE_CONTROLLER_RUN"))
}

var flagChars = regexp.MustCompile(`^[0-9T:+Z-]+$`)

// ReadPause returns the end of an active pause. The flag counts only as a regular file, not a
// symlink, whose first line has only the characters 0-9T:+Z-, is RFC 3339 with an offset and
// lies after now. Anything else is not paused.
func ReadPause(path string, now time.Time) (until time.Time, active bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return time.Time{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	if !flagChars.MatchString(line) {
		return time.Time{}, false
	}
	until, err = time.Parse(time.RFC3339, line)
	if err != nil || !until.After(now) {
		return time.Time{}, false
	}
	return until, true
}

// WritePause writes the flag atomically (tmp + rename), in the zone of until itself.
func WritePause(path string, until time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(until.Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ClearPause removes the flag; removed is false when there was none.
func ClearPause(path string) (removed bool, err error) {
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// NightError is Until without a clock time during the night: the user must pick one.
type NightError struct{ Suggestions []string }

func (e *NightError) Error() string {
	return "at night --until is required; pick e.g. " + strings.Join(e.Suggestions, ", ")
}

// Until is the end of a pause started at now, in now's zone. Without clock ("HH:MM") it is the
// next start of night after now, and by night a *NightError. With clock it is the first moment after now
// at that wall-clock time, within 24 hours.
func Until(now time.Time, clock, night string) (time.Time, error) {
	loc := now.Location()
	if clock == "" {
		if inNight(night, now) {
			return time.Time{}, &NightError{Suggestions: nextHours(now, 3)}
		}
		start, end, _ := strings.Cut(night, "-")
		if start == end {
			return time.Time{}, fmt.Errorf("night %q is empty: pass --until", night)
		}
		t, err := time.Parse("15:04", start)
		if err != nil {
			return time.Time{}, fmt.Errorf("night %q is not HH:MM-HH:MM", night)
		}
		// The next start of night after now: today's, else tomorrow's. An empty window has no
		// start of night, so it fails above.
		for _, day := range []int{0, 1} {
			c := time.Date(now.Year(), now.Month(), now.Day()+day, t.Hour(), t.Minute(), 0, 0, loc)
			if c.After(now) {
				return c, nil
			}
		}
		return time.Time{}, fmt.Errorf("night %q gives no end after now", night)
	}

	t, err := parseClock(clock)
	if err != nil {
		return time.Time{}, err
	}
	var earliest time.Time
	for _, day := range []int{0, 1} {
		c := time.Date(now.Year(), now.Month(), now.Day()+day, t.Hour(), t.Minute(), 0, 0, loc)
		// time.Date picks one pass of the autumn overlap, so try the neighbours an hour away
		// too. A candidate with another wall-clock time is the spring gap's normalised
		// time, or the wrong hour: it does not exist.
		for _, cand := range []time.Time{c, c.Add(time.Hour), c.Add(-time.Hour)} {
			if cand.Hour() != t.Hour() || cand.Minute() != t.Minute() || !cand.After(now) {
				continue
			}
			if earliest.IsZero() || cand.Before(earliest) {
				earliest = cand
			}
		}
	}
	if earliest.IsZero() {
		return time.Time{}, fmt.Errorf("clock %s does not exist within 24 hours", clock)
	}
	if earliest.After(now.Add(24 * time.Hour)) {
		return time.Time{}, fmt.Errorf("clock %s is past 24 hours", clock)
	}
	return earliest, nil
}

func parseClock(clock string) (time.Time, error) {
	t, err := time.Parse("15:04", clock)
	if err != nil || len(clock) != 5 {
		return time.Time{}, fmt.Errorf("clock %q must be HH:MM", clock)
	}
	return t, nil
}

// nextHours is the next n whole hours after now, as HH:MM in now's zone.
func nextHours(now time.Time, n int) []string {
	// Not Truncate: it works on absolute time, and a zone with a half-hour offset needs the wall clock.
	base := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	out := make([]string, n)
	for i := range out {
		out[i] = base.Add(time.Duration(i+1) * time.Hour).Format("15:04")
	}
	return out
}

// inNight reports whether now falls in the window "HH:MM-HH:MM", read in now's zone. A window
// across midnight wraps; an unreadable or empty window is false. It copies dispatch.InNight,
// which this leaf package cannot import.
func inNight(night string, now time.Time) bool {
	from, to, ok := strings.Cut(night, "-")
	if !ok {
		return false
	}
	start, err1 := time.Parse("15:04", from)
	end, err2 := time.Parse("15:04", to)
	if err1 != nil || err2 != nil {
		return false
	}
	minutes := func(t time.Time) int { return t.Hour()*60 + t.Minute() }
	s, e, n := minutes(start), minutes(end), minutes(now)
	switch {
	case s == e:
		return false
	case s < e:
		return n >= s && n < e
	default:
		return n >= s || n < e
	}
}
