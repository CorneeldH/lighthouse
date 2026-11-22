package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Classification: cli-output. Fails when check-hooks outside a repo stops answering exit 2 with
// the COULD NOT CHECK line, or when it is not registered on the root command.
func TestCheckHooksNoRepoRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"check-hooks"})
	if code := execute(root, &errOut); code != 2 {
		t.Errorf("exit %d, want 2; stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "check-hooks: COULD NOT CHECK") {
		t.Errorf("stderr %q lacks check-hooks: COULD NOT CHECK", errOut.String())
	}
}

// Classification: cli-output. Fails when the command is no longer listed in --help.
func TestCheckHooksInHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"--help"})
	execute(root, &errOut)
	if !strings.Contains(out.String()+errOut.String(), "check-hooks") {
		t.Errorf("--help lacks check-hooks:\n%s%s", out.String(), errOut.String())
	}
}

// Classification: cli-output. Fails when a failing check after init changes init's exit code, or
// when the hint line does not say whether hooks fail (1) or the check could not run (2).
func TestCheckHooksFailureKeepsInitExit(t *testing.T) {
	const (
		failing  = "check-hooks: some hooks do not run; see `lighthouse check-hooks` for details"
		couldNot = "check-hooks: could not check the hooks; see `lighthouse check-hooks` for details"
	)
	cases := []struct {
		code      int
		want, not string
	}{
		{1, failing, couldNot},
		{2, couldNot, failing},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.code), func(t *testing.T) {
			stubHooksCheck(t, func(string, io.Writer, io.Writer) int { return c.code })
			code, _, errOut := runInit(t, "--yes")
			if code != 0 {
				t.Errorf("exit %d, want 0: a failed hook check must not fail init\n%s", code, errOut)
			}
			if !strings.Contains(errOut, c.want) || strings.Contains(errOut, c.not) {
				t.Errorf("stderr wants %q and not %q:\n%s", c.want, c.not, errOut)
			}
		})
	}
}

// runCheckHooksStub runs check-hooks with a stubbed hooksCheck inside a fake repo root.
func runCheckHooksStub(t *testing.T, code int, marker string) (exit int, stdout, stderr string) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_REPO_ROOT", t.TempDir())
	stubHooksCheck(t, func(_ string, out, errOut io.Writer) int {
		_, _ = io.WriteString(out, marker)
		return code
	})
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"check-hooks"})
	exit = execute(root, &errOut)
	return exit, out.String(), errOut.String()
}

// Classification: cli-output. Fails when the command sends the ok/FAIL lines to stderr instead of
// stdout, or adds anything to stderr on a clean run.
func TestCheckHooksReportGoesToStdout(t *testing.T) {
	exit, out, errOut := runCheckHooksStub(t, 0, "ok hook x\n")
	if exit != 0 || out != "ok hook x\n" || errOut != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0, the marker on stdout, empty stderr", exit, out, errOut)
	}
}

// Classification: cli-output. Fails when a finding (1) or COULD NOT CHECK (2) of the check is
// not the exit code of the command.
func TestCheckHooksExitCodePropagates(t *testing.T) {
	for _, want := range []int{1, 2} {
		if exit, _, errOut := runCheckHooksStub(t, want, "x\n"); exit != want {
			t.Errorf("exit %d, want %d; stderr %q", exit, want, errOut)
		}
	}
}
