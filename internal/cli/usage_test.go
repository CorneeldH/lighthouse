package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

// cli-output change; the regression surface is the stdout/stderr line and the exit code.
func TestUsageRefreshExitsZeroWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"usage", "refresh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute = %v, want nil", err)
	}
	if !strings.HasPrefix(errOut.String(), "usage: failed: ") || strings.Count(errOut.String(), "\n") != 1 {
		t.Errorf("stderr = %q, want one line starting with %q", errOut.String(), "usage: failed: ")
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestUsageHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"usage", "refresh", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "--force") {
		t.Errorf("help lacks --force:\n%s", out.String())
	}
}

func TestReportPrintsOutcomeOnStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	report(&out, &errOut, func() (usage.Outcome, error) { return usage.Fresh, nil })
	if out.String() != "usage: fresh\n" || errOut.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

func TestReportPrintsFailureOnStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	report(&out, &errOut, func() (usage.Outcome, error) { return usage.Failed, errors.New("boom") })
	if out.Len() != 0 || errOut.String() != "usage: failed: boom\n" {
		t.Errorf("stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

func TestReportRecoversPanic(t *testing.T) {
	var out, errOut bytes.Buffer
	report(&out, &errOut, func() (usage.Outcome, error) { panic("kaboom") })
	if out.Len() != 0 || errOut.String() != "usage: failed: kaboom\n" {
		t.Errorf("stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

// cli-output change; the regression surface is stdout JSON, the stderr line and the exit error.
func TestPrintCodex(t *testing.T) {
	t.Run("measured", func(t *testing.T) {
		state := usage.CodexState{}
		var out, errOut bytes.Buffer
		err := printCodex(context.Background(), &out, &errOut, func(context.Context) (usage.CodexState, error) {
			return state, nil
		})
		want, mErr := json.MarshalIndent(state, "", "  ")
		if mErr != nil {
			t.Fatal(mErr)
		}
		if err != nil || out.String() != string(want)+"\n" || errOut.Len() != 0 {
			t.Errorf("err = %v, stdout = %q, stderr = %q", err, out.String(), errOut.String())
		}
	})
	t.Run("not measurable", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := printCodex(context.Background(), &out, &errOut, func(context.Context) (usage.CodexState, error) {
			return usage.CodexState{}, errors.New("codex not found")
		})
		if !errors.Is(err, errSilent) || out.Len() != 0 || errOut.String() != "not measurable: codex not found\n" {
			t.Errorf("err = %v, stdout = %q, stderr = %q", err, out.String(), errOut.String())
		}
	})
}

func TestUsageAlarmCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	run := func() (string, string, error) {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(""), &out, &errOut)
		root.SetArgs([]string{"usage", "alarm"})
		err := root.Execute()
		return out.String(), errOut.String(), err
	}

	out, errOut, err := run()
	if !errors.Is(err, errSilent) || out != "" || errOut != "" {
		t.Errorf("no file: got (%v, %q, %q), want errSilent and empty output", err, out, errOut)
	}

	paths, perr := usage.DefaultPaths()
	if perr != nil {
		t.Fatal(perr)
	}
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const line = "venster 92 Tue_14:30 2099-01-01T00:00:00Z\n"
	if err := os.WriteFile(paths.Alarm(), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err = run()
	if err != nil || out != line || errOut != "" {
		t.Errorf("valid: got (%v, %q, %q), want (nil, %q, empty)", err, out, errOut, line)
	}
}

// A controller run also prints the alarm copy next to the pause flag, so a blocking Stop gate in
// a container without the host cache steps aside like the usage gate does (#314). Any other
// session does not read the copy.
func TestUsageAlarmCommandReadsCopyInControllerRun(t *testing.T) {
	const line = "venster 92 Tue_14:30 2099-01-01T00:00:00Z\n"
	cases := []struct {
		name, run, wantOut string
		wantErr            error
	}{
		{"controller run", "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f", line, nil},
		{"no controller run", "", "", errSilent},
		{"run not uuid", "../x", "", errSilent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
			t.Setenv("LIGHTHOUSE_CONTROLLER_STATE", filepath.Join(tmp, "ctl"))
			t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", c.run)
			if err := os.MkdirAll(filepath.Join(tmp, "ctl"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tmp, "ctl", "usage-window.alarm"), []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			root := NewRoot(strings.NewReader(""), &out, &errOut)
			root.SetArgs([]string{"usage", "alarm"})
			err := root.Execute()
			if !errors.Is(err, c.wantErr) || out.String() != c.wantOut || errOut.Len() != 0 {
				t.Errorf("got (%v, %q, %q), want (%v, %q, empty)", err, out.String(), errOut.String(), c.wantErr, c.wantOut)
			}
		})
	}
}

func TestUsageAlarmCommandWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"usage", "alarm"})
	err := root.Execute()
	if !errors.Is(err, errSilent) || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "usage: ") || strings.Count(errOut.String(), "\n") != 1 {
		t.Errorf("got (%v, %q, %q), want errSilent and one usage: line on stderr", err, out.String(), errOut.String())
	}
}
