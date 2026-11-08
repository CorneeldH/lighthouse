package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func stateWith(sess, week int) State {
	return State{
		Schema:  1,
		Session: WindowState{sess, "2026-09-06T12:30:00Z", "14:30"},
		Week:    WindowState{week, "2026-09-10T14:59:00Z", "do 16:59"},
	}
}

func TestAlarmLine(t *testing.T) {
	empty := stateWith(90, 10)
	empty.Session.ResetAt, empty.Session.ResetLocal = "", ""
	cases := []struct {
		name   string
		s      State
		want   string
		wantOK bool
	}{
		{"session at threshold", stateWith(85, 10), "venster 85 14:30 2026-09-06T12:30:00Z\n", true},
		{"week at threshold", stateWith(84, 95), "week 95 do_16:59 2026-09-10T14:59:00Z\n", true},
		{"session wins", stateWith(99, 99), "venster 99 14:30 2026-09-06T12:30:00Z\n", true},
		{"below both", stateWith(84, 94), "", false},
		{"empty reset", empty, "venster 90 ? ?\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := AlarmLine(c.s, DefaultConfig())
			if ok != c.wantOK || got != c.want {
				t.Errorf("got (%q, %v), want (%q, %v)", got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestWriteAlarm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "usage-window.alarm")

	if err := WriteAlarm(path, stateWith(90, 10), DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "venster 90 14:30 2026-09-06T12:30:00Z\n"; string(b) != want {
		t.Errorf("content = %q, want %q", b, want)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1 (no leftover temp file)", len(entries))
	}

	if err := WriteAlarm(path, stateWith(10, 10), DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("alarm still there after drop below thresholds: %v", err)
	}
	if err := WriteAlarm(path, stateWith(10, 10), DefaultConfig()); err != nil {
		t.Errorf("removing absent alarm: %v", err)
	}
}

func TestPruneAlarm(t *testing.T) {
	now := utc(t, "2026-09-06T13:00:00Z")
	cases := []struct {
		name, content string
		missing       bool
		want          bool
	}{
		{"expired", "venster 90 14:30 2026-09-06T12:30:00Z\n", false, true},
		{"future", "venster 90 15:00 2026-09-06T14:00:00Z\n", false, false},
		{"exactly now", "venster 90 15:00 2026-09-06T13:00:00Z\n", false, true},
		{"too few fields", "venster 90\n", false, true},
		{"unknown reset", "venster 90 ? ?\n", false, true},
		{"absent", "", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "usage-window.alarm")
			if !c.missing {
				if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := PruneAlarm(path, now)
			if err != nil {
				t.Fatal(err)
			}
			if removed != c.want {
				t.Errorf("removed = %v, want %v", removed, c.want)
			}
			_, statErr := os.Stat(path)
			if c.want && !os.IsNotExist(statErr) {
				t.Errorf("file still present: %v", statErr)
			}
			if !c.want && !c.missing && statErr != nil {
				t.Errorf("file gone: %v", statErr)
			}
		})
	}
}

const alarmFixturePath = "../../tests/pack/fixtures/usage_alarm/cases.json"

type alarmCase struct {
	Name   string   `json:"name"`
	Setup  string   `json:"setup"`
	Line   string   `json:"line"`
	Valid  bool     `json:"valid"`
	Fields []string `json:"fields"`
}

// TestReadAlarmFixtures runs the cases that tests/pack/test_usage_gate.py runs
// against the usage-gate hook. The fixture pct is the text as written ("007"); ReadAlarm
// returns the integer, so it is compared with strconv.Atoi of that text.
func TestReadAlarmFixtures(t *testing.T) {
	raw, err := os.ReadFile(alarmFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var cases []alarmCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no alarm fixtures")
	}
	now := time.Now().UTC()
	const stamp = "2006-01-02T15:04:05Z"
	future := now.Add(time.Hour).Format(stamp)
	past := now.Add(-time.Minute).Format(stamp)
	expand := func(s string) string {
		return strings.NewReplacer("{future}", future, "{past}", past).Replace(s)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Setup == "unreadable" && os.Geteuid() == 0 {
				t.Skip("root reads a mode-000 file")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "usage-window.alarm")
			body := []byte(expand(c.Line) + "\n")
			switch c.Setup {
			case "absent":
			case "symlink":
				real := filepath.Join(dir, "elsewhere.alarm")
				if err := os.WriteFile(real, body, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, path); err != nil {
					t.Fatal(err)
				}
			case "file", "unreadable":
				mode := os.FileMode(0o600)
				if c.Setup == "unreadable" {
					mode = 0
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatalf("unknown setup %q", c.Setup)
			}
			got, ok := ReadAlarm(path, now)
			if ok != c.Valid {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, c.Valid, got)
			}
			if !ok {
				if got != (Alarm{}) {
					t.Errorf("got %+v, want zero Alarm", got)
				}
				return
			}
			wantPct, err := strconv.Atoi(c.Fields[1])
			if err != nil {
				t.Fatal(err)
			}
			want := Alarm{Kind: c.Fields[0], Pct: wantPct, ResetLocal: c.Fields[2], ResetAt: expand(c.Fields[3])}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestReadAlarmPctOverflowIsNoAlarm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	line := "venster 99999999999999999999 14:30 2099-01-01T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadAlarm(path, time.Now()); ok {
		t.Errorf("got %+v, want no alarm", got)
	}
}

func TestReadAlarmOnlyFirstLineCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	line := "venster 92 14:30 2099-01-01T00:00:00Z\ngarbage\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadAlarm(path, time.Now())
	if !ok || got.Pct != 92 {
		t.Errorf("got (%+v, %v), want valid pct 92", got, ok)
	}
}
