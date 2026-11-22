package cli

import (
	"bytes"
	"io"
	"path/filepath"
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

// Classification: cli-output. Fails when a failing check after init changes init's exit code or
// loses the hint line.
func TestCheckHooksFailureKeepsInitExit(t *testing.T) {
	stubHooksCheck(t, func(string, io.Writer, io.Writer) int { return 1 })
	code, _, errOut := runInit(t, "--yes")
	if code != 0 {
		t.Errorf("exit %d, want 0: a failed hook check must not fail init\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "lighthouse check-hooks") {
		t.Errorf("stderr lacks the check-hooks hint:\n%s", errOut)
	}
}
