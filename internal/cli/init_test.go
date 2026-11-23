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

// runInit runs `lighthouse init <args>` in a new git repository with an isolated home.
func runInit(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	if !hooksCheckStubbed {
		stubHooksCheck(t, func(string, io.Writer, io.Writer) int { return 0 })
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
