package usage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
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

// CurrentAlarm reads the alarm file, and in a controller run then the copy next to the pause
// flag: in a container that mounts the controller state but not the host cache, the copy is
// the only alarm the run sees.
func (p Paths) CurrentAlarm(now time.Time, controllerRun bool) (Alarm, bool) {
	if a, ok := ReadAlarm(p.Alarm(), now); ok {
		return a, true
	}
	if !controllerRun || p.Mirror == "" {
		return Alarm{}, false
	}
	return ReadAlarm(p.Mirror, now)
}

// Alarm is a valid alarm file: the window that is over its threshold.
// ResetLocal has real spaces, not the "_" of the file.
type Alarm struct {
	Kind       string
	Pct        int
	ResetLocal string
	ResetAt    string
}

var resetAtPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

// ReadAlarm reads the alarm file with the rules of the former shell alarm helper: a
// regular file (no symlink), only the first line, four fields split on space
// and tab only (a "\r" stays in its field and makes it invalid), kind "venster"
// or "week", pct digits that fit an int, and a reset_at in the strict UTC form
// that lies after now. Any other case, including a read error, is no alarm.
func ReadAlarm(path string, now time.Time) (Alarm, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return Alarm{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Alarm{}, false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
	if len(f) != 4 || (f[0] != "venster" && f[0] != "week") {
		return Alarm{}, false
	}
	for i := 0; i < len(f[1]); i++ {
		if f[1][i] < '0' || f[1][i] > '9' {
			return Alarm{}, false
		}
	}
	pct, err := strconv.Atoi(f[1])
	if err != nil || !resetAtPattern.MatchString(f[3]) {
		return Alarm{}, false
	}
	reset, err := time.Parse(time.RFC3339, f[3])
	if err != nil || !now.Before(reset) {
		return Alarm{}, false
	}
	return Alarm{Kind: f[0], Pct: pct, ResetLocal: strings.ReplaceAll(f[2], "_", " "), ResetAt: f[3]}, true
}
