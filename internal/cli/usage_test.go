package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
