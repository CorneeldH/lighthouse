package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/scriptcase"
)

const worktreeAuditFixtures = "../../tests/pack/fixtures/scripts/worktree-audit/cases.json"

// TestWorktreeAuditCases replays every case of cases.json, the fixture that worktree_audit.py ran
// against first, through the real command: real git against the setup repo, a replayed gh.
func TestWorktreeAuditCases(t *testing.T) {
	for _, c := range scriptcase.Load(t, worktreeAuditFixtures) {
		t.Run(c.Name, func(t *testing.T) {
			if c.Only != "" && c.Only != "go" {
				t.Skip("not a Go case")
			}
			// The Python runner's root is <tmp>/root: the main checkout's row shows that name.
			// git reports the resolved path, so resolve the temp dir too: on macOS t.TempDir()
			// lies under /var, a symlink to /private/var.
			tmp, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(tmp, "root")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			fixturesetup.Build(t, root, c.Setup)
			toml := filepath.Join(t.TempDir(), "lighthouse.toml")
			if err := os.WriteFile(toml, []byte(fixturesetup.Fill(t, c.Config, root)), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LIGHTHOUSE_TOML", toml)
			t.Setenv("HOME", t.TempDir())
			for k, v := range c.Env {
				t.Setenv(k, fixturesetup.Fill(t, v, root))
			}
			t.Chdir(root)
			env := defaultWorktreeAuditEnv()
			env.Run = scriptcase.Replay(t, fillCalls(t, c.Calls, root))
			var out, errOut bytes.Buffer
			top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
			top.AddCommand(newWorktreeAuditCommand(env))
			top.SetOut(&out)
			top.SetErr(&errOut)
			args := []string{"worktree-audit"}
			for _, a := range c.Argv {
				args = append(args, fixturesetup.Fill(t, a, root))
			}
			top.SetArgs(args)
			code := execute(top, &errOut)
			if code != c.Expect.Code {
				t.Errorf("exit code = %d, want %d; stdout %q stderr %q", code, c.Expect.Code, out.String(), errOut.String())
			}
			contains := len(c.Expect.StdoutContains)+len(c.Expect.StderrContains) > 0
			if want := fixturesetup.Fill(t, c.Expect.Stdout, root); !contains && out.String() != want {
				t.Errorf("stdout\n got %q\nwant %q", out.String(), want)
			}
			if want := fixturesetup.Fill(t, c.Expect.Stderr, root); !contains && errOut.String() != want {
				t.Errorf("stderr\n got %q\nwant %q", errOut.String(), want)
			}
			for _, want := range c.Expect.StdoutContains {
				if !strings.Contains(out.String(), want) {
					t.Errorf("stdout %q lacks %q", out.String(), want)
				}
			}
			for _, want := range c.Expect.StderrContains {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("stderr %q lacks %q", errOut.String(), want)
				}
			}
		})
	}
}
