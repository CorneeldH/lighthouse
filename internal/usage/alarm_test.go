package usage

import (
	"os"
	"path/filepath"
	"testing"
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
