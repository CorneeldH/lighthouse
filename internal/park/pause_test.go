package park

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

const night = "21:00-08:00"

func ams(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func at(loc *time.Location, y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, loc)
}

func TestPauseFile(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{"controller state", map[string]string{"LIGHTHOUSE_CONTROLLER_STATE": "/c", "XDG_STATE_HOME": "/s", "HOME": "/h"}, "/c/PAUSE", false},
		{"xdg", map[string]string{"XDG_STATE_HOME": "/s", "HOME": "/h"}, "/s/lighthouse/controller/PAUSE", false},
		{"home", map[string]string{"HOME": "/h"}, "/h/.local/state/lighthouse/controller/PAUSE", false},
		{"none absolute", map[string]string{"XDG_STATE_HOME": "s", "HOME": "h"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PauseFile(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("PauseFile = %q, %v; want %q, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestWriteReadPause(t *testing.T) {
	loc := ams(t)
	path := filepath.Join(t.TempDir(), "sub", "PAUSE")
	until := at(loc, 2026, 11, 1, 21, 0)
	if err := WritePause(path, until); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "2026-11-01T21:00:00+01:00\n" {
		t.Errorf("file = %q", b)
	}
	got, active := ReadPause(path, at(loc, 2026, 11, 1, 20, 0))
	if !active || !got.Equal(until) {
		t.Errorf("ReadPause at 20:00 = %v, %v; want %v, true", got, active, until)
	}
	if _, active := ReadPause(path, at(loc, 2026, 11, 1, 21, 0)); active {
		t.Error("ReadPause at 21:00 must be inactive")
	}
}

func TestReadPauseInvalid(t *testing.T) {
	loc := ams(t)
	now := at(loc, 2026, 11, 1, 20, 0)
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid")
	if err := os.WriteFile(valid, []byte("2026-11-01T21:00:00+01:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, active := ReadPause(valid, now); !active {
		t.Fatal("control: valid flag must be active")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(valid, link); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"naive":    "2026-11-01T21:00:00\n",
		"space":    "2026-11-01 21:00:00+01:00\n",
		"expired":  "2026-11-01T19:00:00+01:00\n",
		"fraction": "2026-11-01T21:00:00.5+01:00\n",
		"empty":    "",
	}
	paths := map[string]string{"symlink": link, "missing": filepath.Join(dir, "none")}
	for name, body := range cases {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[name] = p
	}
	for name, p := range paths {
		if _, active := ReadPause(p, now); active {
			t.Errorf("%s: want inactive", name)
		}
	}
}

func TestClearPause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PAUSE")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := ClearPause(path)
	if !removed || err != nil {
		t.Fatalf("first = %v, %v; want true, nil", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
	removed, err = ClearPause(path)
	if removed || err != nil {
		t.Errorf("second = %v, %v; want false, nil", removed, err)
	}
}

func TestUntilDayDefault(t *testing.T) {
	loc := ams(t)
	got, err := Until(at(loc, 2026, 11, 1, 14, 0), "", night)
	if err != nil {
		t.Fatal(err)
	}
	if want := at(loc, 2026, 11, 1, 21, 0); !got.Equal(want) || got.Location() != loc {
		t.Errorf("Until = %v, want %v", got, want)
	}
}

func TestUntilNightRequiresClock(t *testing.T) {
	loc := ams(t)
	_, err := Until(at(loc, 2026, 11, 1, 23, 10), "", night)
	var ne *NightError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %v, want *NightError", err)
	}
	if want := []string{"00:00", "01:00", "02:00"}; !reflect.DeepEqual(ne.Suggestions, want) {
		t.Errorf("Suggestions = %v, want %v", ne.Suggestions, want)
	}
	if want := "at night --until is required; pick e.g. 00:00, 01:00, 02:00"; ne.Error() != want {
		t.Errorf("Error() = %q, want %q", ne.Error(), want)
	}
	if _, err := Until(at(loc, 2026, 11, 1, 7, 59), "", night); !errors.As(err, &ne) {
		t.Errorf("07:59: err = %v, want *NightError", err)
	}
}

func TestUntilClock(t *testing.T) {
	loc := ams(t)
	now := at(loc, 2026, 11, 1, 14, 0)
	tests := []struct {
		clock string
		want  time.Time
	}{
		{"16:30", at(loc, 2026, 11, 1, 16, 30)},
		{"09:00", at(loc, 2026, 11, 2, 9, 0)},
		{"14:00", at(loc, 2026, 11, 2, 14, 0)},
	}
	for _, tt := range tests {
		got, err := Until(now, tt.clock, night)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("Until(%s) = %v, %v; want %v", tt.clock, got, err, tt.want)
		}
	}
}

func TestUntilBeyond24Hours(t *testing.T) {
	loc := ams(t)
	_, err := Until(at(loc, 2026, 10, 25, 1, 30), "01:20", night)
	if err == nil || !strings.Contains(err.Error(), "past 24 hours") {
		t.Errorf("err = %v, want one containing %q", err, "past 24 hours")
	}
}

func TestUntilSpringGap(t *testing.T) {
	loc := ams(t)
	_, err := Until(at(loc, 2026, 3, 28, 3, 0), "02:30", night)
	if err == nil || !strings.Contains(err.Error(), "does not exist within 24 hours") {
		t.Errorf("err = %v, want one containing %q", err, "does not exist within 24 hours")
	}
}

func TestUntilBadClock(t *testing.T) {
	loc := ams(t)
	for _, clock := range []string{"7:00", "24:00", "12:60"} {
		_, err := Until(at(loc, 2026, 11, 1, 14, 0), clock, night)
		if err == nil || !strings.Contains(err.Error(), "HH:MM") {
			t.Errorf("Until(%q) err = %v, want one containing HH:MM", clock, err)
		}
	}
}

func TestUntilAutumnOverlap(t *testing.T) {
	loc := ams(t)
	// 02:45 CEST on the 25-hour day: 02:30 comes round again an hour later, as CET.
	now := time.Date(2026, 10, 25, 0, 45, 0, 0, time.UTC).In(loc)
	got, err := Until(now, "02:30", night)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Until = %v, want %v (02:30 CET)", got, want)
	}
}

func TestUntilAutumnOverlapFirstPass(t *testing.T) {
	loc := ams(t)
	// 02:15 CEST: the first 02:30 (CEST) is still ahead.
	now := time.Date(2026, 10, 25, 0, 15, 0, 0, time.UTC).In(loc)
	got, err := Until(now, "02:30", night)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Until = %v, want %v (02:30 CEST)", got, want)
	}
}
