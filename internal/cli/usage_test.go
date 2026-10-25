package cli

import (
	"bytes"
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
