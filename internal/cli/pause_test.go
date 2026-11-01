package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

// Classification: these tests fail when pause computes the wrong end, writes or removes the
// wrong flag, or words a refusal differently; they observe only the flag file, stdout and error.

func pauseAt(t *testing.T, y int, mo time.Month, d, h, m int) (pauseEnv, string) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(y, mo, d, h, m, 0, 0, loc)
	state := t.TempDir()
	env := pauseEnv{
		Getenv: func(k string) string {
			if k == "LIGHTHOUSE_CONTROLLER_STATE" {
				return state
			}
			return ""
		},
		Now: func() time.Time { return now },
	}
	return env, filepath.Join(state, "PAUSE")
}

func TestPauseWritesDefault(t *testing.T) {
	env, flag := pauseAt(t, 2026, 11, 1, 14, 0)
	var out bytes.Buffer
	if err := pause(env, "", false, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "lighthouse: paused until 2026-11-01T21:00:00+01:00\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	b, err := os.ReadFile(flag)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "2026-11-01T21:00:00+01:00\n"; got != want {
		t.Errorf("flag = %q, want %q", got, want)
	}
}

func TestPauseNightWithoutUntilRefuses(t *testing.T) {
	env, flag := pauseAt(t, 2026, 11, 1, 23, 10)
	err := pause(env, "", false, &bytes.Buffer{})
	if want := "at night --until is required; pick e.g. 00:00, 01:00, 02:00"; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if _, statErr := os.Stat(flag); statErr == nil {
		t.Error("flag written despite refusal")
	}
}

func TestPauseBeyond24HoursRefuses(t *testing.T) {
	env, flag := pauseAt(t, 2026, 10, 25, 1, 30)
	err := pause(env, "01:20", false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "past 24 hours") {
		t.Fatalf("error = %v, want one containing %q", err, "past 24 hours")
	}
	if _, statErr := os.Stat(flag); statErr == nil {
		t.Error("flag written despite refusal")
	}
}

func TestPauseUsesHostNight(t *testing.T) {
	env, flag := pauseAt(t, 2026, 11, 1, 21, 30)
	env.HostToml = filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(env.HostToml, []byte("[dispatch]\nnight = \"22:00-06:00\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pause(env, "", false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(flag)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "2026-11-01T22:00:00+01:00\n"; got != want {
		t.Errorf("flag = %q, want %q", got, want)
	}
}

func TestPauseOff(t *testing.T) {
	env, flag := pauseAt(t, 2026, 11, 1, 14, 0)
	if err := os.WriteFile(flag, []byte("2026-11-01T21:00:00+01:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := pause(env, "", true, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "lighthouse: pause off\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(flag); err == nil {
		t.Error("flag still there")
	}

	out.Reset()
	if err := pause(env, "", true, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "lighthouse: no pause\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestPauseOffWithUntilRefuses(t *testing.T) {
	env, _ := pauseAt(t, 2026, 11, 1, 14, 0)
	err := pause(env, "10:00", true, &bytes.Buffer{})
	if want := "pause off takes no --until"; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestPauseCommandRejectsOtherArgument(t *testing.T) {
	cmd := newPauseCommand()
	cmd.SetArgs([]string{"on"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("argument other than off accepted")
	}
}
