package cli

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestUninstallCommandRegistered(t *testing.T) {
	root := NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	cmd, _, err := root.Find([]string{"uninstall"})
	if err != nil || cmd == nil || cmd.Name() != "uninstall" {
		t.Fatalf("root does not know uninstall: %v", err)
	}
	help := cmd.UsageString()
	for _, flag := range []string{"--yes", "--force"} {
		if !strings.Contains(help, flag) {
			t.Errorf("uninstall help lacks %s:\n%s", flag, help)
		}
	}
}

// Fails when the command ignores the exit code of initcmd.Uninstall or drops its message:
// a repo without an install must exit 1 with the reason on stderr.
func TestUninstallCommandNotInstalled(t *testing.T) {
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
	top.AddCommand(newUninstallCommand())
	top.SetOut(&out)
	top.SetErr(&errOut)
	top.SetIn(strings.NewReader(""))
	top.SetArgs([]string{"uninstall", "--yes"})
	code := execute(top, &errOut)
	if code != 1 || strings.TrimSpace(errOut.String()) == "" {
		t.Errorf("exit %d, stderr %q; want 1 and an error on stderr", code, errOut.String())
	}
}
