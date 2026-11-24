package cli

import (
	"bytes"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/agentaudit"
	"github.com/CorneeldH/lighthouse/internal/lintcheck"
)

// hooksCheckStubbed is true while a test has put its own hooksCheck in place; runInit then
// leaves it, and otherwise swaps in a no-op so no init test starts real smoketests.
var hooksCheckStubbed bool

func stubHooksCheck(t *testing.T, f func(string, io.Writer, io.Writer) int) {
	t.Helper()
	orig := hooksCheck
	hooksCheck, hooksCheckStubbed = f, true
	t.Cleanup(func() { hooksCheck, hooksCheckStubbed = orig, false })
}

// lintCheckStubbed is true while a test has put its own lintCheck in place; runInit then leaves
// it, and otherwise swaps in a no-op so no init test runs the real lint check.
var lintCheckStubbed bool

func stubLintCheck(t *testing.T, f func(string, []lintcheck.Lang, []string, string, io.Writer, io.Writer) int) {
	t.Helper()
	orig := lintCheck
	lintCheck, lintCheckStubbed = f, true
	t.Cleanup(func() { lintCheck, lintCheckStubbed = orig, false })
}

// runInit runs `lighthouse init <args>` in a new git repository with an isolated home.
func runInit(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	if !hooksCheckStubbed {
		stubHooksCheck(t, func(string, io.Writer, io.Writer) int { return 0 })
	}
	if !lintCheckStubbed {
		stubLintCheck(t, func(string, []lintcheck.Lang, []string, string, io.Writer, io.Writer) int { return 0 })
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(home, "host.toml"))
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	top.AddCommand(newInitCommand())
	top.SetOut(&out)
	top.SetErr(&errOut)
	top.SetIn(strings.NewReader(""))
	top.SetArgs(append([]string{"init"}, args...))
	code = execute(top, &errOut)
	return code, out.String(), errOut.String()
}

func stubAudit(t *testing.T, f func(agentaudit.Env, agentaudit.Options, io.Writer, io.Writer) int) {
	t.Helper()
	orig := auditSummary
	auditSummary = f
	t.Cleanup(func() { auditSummary = orig })
}

func TestAuditFailureKeepsInitExit(t *testing.T) {
	stubAudit(t, func(agentaudit.Env, agentaudit.Options, io.Writer, io.Writer) int { return 2 })
	code, _, errOut := runInit(t, "--yes")
	if code != 0 {
		t.Errorf("exit %d, want 0: a failed audit must not fail init\n%s", code, errOut)
	}
	if strings.Count(errOut, "agent-audit: COULD NOT CHECK") != 1 {
		t.Errorf("stderr lacks exactly one agent-audit: COULD NOT CHECK line:\n%s", errOut)
	}
}

func TestAuditOwnMessageNotDoubled(t *testing.T) {
	stubAudit(t, func(_ agentaudit.Env, _ agentaudit.Options, _, errOut io.Writer) int {
		return agentaudit.CouldNotCheck(errOut, io.ErrUnexpectedEOF)
	})
	code, _, errOut := runInit(t, "--yes")
	if code != 0 || strings.Count(errOut, "agent-audit: COULD NOT CHECK") != 1 || !strings.Contains(errOut, io.ErrUnexpectedEOF.Error()) {
		t.Errorf("exit %d, stderr %q; want 0 and the audit's own line once", code, errOut)
	}
}

func TestInitRunsAuditSummary(t *testing.T) {
	var calls int
	stubAudit(t, func(_ agentaudit.Env, _ agentaudit.Options, out, _ io.Writer) int {
		calls++
		_, _ = io.WriteString(out, "SUMMARY\n")
		return 0
	})
	code, out, errOut := runInit(t, "--yes")
	if code != 0 || calls != 1 || !strings.Contains(out, "SUMMARY") {
		t.Errorf("exit %d, %d calls, stdout %q, stderr %q", code, calls, out, errOut)
	}
}

func TestAuditFlagNeedsUpgrade(t *testing.T) {
	stubAudit(t, func(agentaudit.Env, agentaudit.Options, io.Writer, io.Writer) int {
		t.Error("audit ran")
		return 0
	})
	code, _, errOut := runInit(t, "--yes", "--audit")
	if code == 0 || !strings.Contains(errOut, "--audit needs --upgrade") {
		t.Errorf("exit %d, stderr %q; want a refusal naming --audit needs --upgrade", code, errOut)
	}
}

// Fails when the --level flag is not wired to initcmd.Options.Level: init then picks light.
func TestInitLevelFlagIsWired(t *testing.T) {
	stubAudit(t, func(agentaudit.Env, agentaudit.Options, io.Writer, io.Writer) int { return 0 })
	code, out, errOut := runInit(t, "--yes", "--level", "strict")
	if code != 0 || !strings.Contains(out, "Level: strict.") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, _, errOut = runInit(t, "--yes", "--level", "bogus")
	if code == 0 || !strings.Contains(errOut, `unknown level "bogus"`) {
		t.Errorf("exit %d, stderr %q; want the unknown level refused", code, errOut)
	}
}

// Classification: cli-output. Fails when a fully wired repo makes init print anything but
// `No changes.`: lint stdout (`<linter>: wired`) must stay out.
func TestInitPrintsNothingForWiredRepo(t *testing.T) {
	stubLintCheck(t, func(_ string, _ []lintcheck.Lang, _ []string, _ string, out, _ io.Writer) int {
		_, _ = io.WriteString(out, "ruff: wired\n")
		return 0
	})
	code, out, errOut := runInit(t, "--yes")
	if code != 0 || strings.Contains(out, "wired") || strings.Contains(errOut, "wired") || strings.Contains(errOut, "lint check") {
		t.Errorf("rc %d, out %q, err %q", code, out, errOut)
	}
}

// Classification: cli-output. Fails when findings are not shown under the heading on stderr.
func TestInitShowsMissingUnderHeading(t *testing.T) {
	stubLintCheck(t, func(_ string, _ []lintcheck.Lang, _ []string, _ string, out, _ io.Writer) int {
		_, _ = io.WriteString(out, "ci.yml:3: ruff — LINTMARK\n")
		return 1
	})
	code, out, errOut := runInit(t, "--yes")
	if code != 0 || strings.Contains(out, "LINTMARK") ||
		!strings.Contains(errOut, "lint check: some linters are not wired\nci.yml:3: ruff — LINTMARK") {
		t.Errorf("rc %d, out %q, err %q", code, out, errOut)
	}
}

// Classification: cli-output. Fails when exit 1 or 3 of the lint check changes the exit of init.
func TestInitDoesNotFailOnLintExit1And3(t *testing.T) {
	for _, rc := range []int{1, 3} {
		stubLintCheck(t, func(_ string, _ []lintcheck.Lang, _ []string, _ string, _, errOut io.Writer) int {
			_, _ = io.WriteString(errOut, "lint check: COULD NOT CHECK — x\n")
			return rc
		})
		if code, _, _ := runInit(t, "--yes"); code != 0 {
			t.Errorf("lint rc %d: init rc %d", rc, code)
		}
	}
}

// Classification: cli-output. Fails when init skips the lint check.
func TestInitRunsLintCheck(t *testing.T) {
	var calls int
	stubLintCheck(t, func(string, []lintcheck.Lang, []string, string, io.Writer, io.Writer) int { calls++; return 0 })
	if code, _, errOut := runInit(t, "--yes"); code != 0 || calls != 1 {
		t.Errorf("rc %d, %d calls, err %q", code, calls, errOut)
	}
}

// Classification: cli-output. Fails when exit 3 of the lint check shows the heading of exit 1:
// a check that could not run would then read as missing wiring.
func TestInitLintHeadingPerExit(t *testing.T) {
	for rc, heading := range map[int]string{1: "lint check: some linters are not wired\n", 3: "lint check: could not check\n"} {
		stubLintCheck(t, func(_ string, _ []lintcheck.Lang, _ []string, _ string, _, errOut io.Writer) int {
			_, _ = io.WriteString(errOut, "LINTMARK\n")
			return rc
		})
		code, _, errOut := runInit(t, "--yes")
		if code != 0 || !strings.Contains(errOut, heading+"LINTMARK") {
			t.Errorf("lint rc %d: init rc %d, stderr %q, want heading %q", rc, code, errOut, heading)
		}
	}
}
