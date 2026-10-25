package usage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

// AlarmLine returns the alarm line for the first window at or over its
// threshold, the session before the week. ok is false when neither is.
// The line is "<kind> <pct> <reset_local> <reset_at>\n"; spaces in
// reset_local become "_" and an empty field becomes "?".
func AlarmLine(s State, c Config) (line string, ok bool) {
	switch {
	case s.Session.Pct >= c.HaltPctSession:
		return alarmLine("venster", s.Session), true
	case s.Week.Pct >= c.HaltPctWeek:
		return alarmLine("week", s.Week), true
	}
	return "", false
}

func alarmLine(kind string, w WindowState) string {
	local := strings.ReplaceAll(w.ResetLocal, " ", "_")
	if local == "" {
		local = "?"
	}
	at := w.ResetAt
	if at == "" {
		at = "?"
	}
	return fmt.Sprintf("%s %d %s %s\n", kind, w.Pct, local, at)
}

// WriteAlarm writes the alarm file atomically, or removes it when no window is
// over its threshold. Removing an absent file is no error.
func WriteAlarm(path string, s State, c Config) error {
	line, ok := AlarmLine(s, c)
	if !ok {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeAtomic(path, []byte(line))
}

// PruneAlarm removes an alarm file that has expired or cannot be read as an
// alarm: fewer than four fields, or a reset_at that is not a time. It reports
// whether it removed the file. An absent file is no error.
func PruneAlarm(path string, now time.Time) (removed bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if f := strings.Fields(string(b)); len(f) >= 4 {
		if reset, perr := time.Parse(time.RFC3339, f[3]); perr == nil && now.Before(reset) {
			return false, nil
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, nil
}
