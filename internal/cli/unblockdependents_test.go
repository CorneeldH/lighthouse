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

const unblockFixtures = "../../tests/pack/fixtures/scripts/unblock-dependents/cases.json"

// TestUnblockCases replays every case of cases.json, the one fixture that the Python script ran
// against first, through the real command: flags, config, summary file and exit code included.
func TestUnblockCases(t *testing.T) {
	for _, c := range scriptcase.Load(t, unblockFixtures) {
		t.Run(c.Name, func(t *testing.T) {
			if c.Only != "" && c.Only != "go" {
				t.Skip("not a Go case")
			}
			root := t.TempDir()
			fixturesetup.Build(t, root, c.Setup)
			toml := filepath.Join(t.TempDir(), "lighthouse.toml")
			if err := os.WriteFile(toml, []byte(fixturesetup.Fill(t, c.Config, root)), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LIGHTHOUSE_TOML", toml)
			t.Setenv("GITHUB_STEP_SUMMARY", "")
			for k, v := range c.Env {
				t.Setenv(k, fixturesetup.Fill(t, v, root))
			}
			var out, errOut bytes.Buffer
			top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
			top.AddCommand(newUnblockCommand(scriptcase.Replay(t, c.Calls)))
			top.SetOut(&out)
			top.SetErr(&errOut)
			args := []string{"unblock-dependents"}
			for _, a := range c.Argv {
				args = append(args, fixturesetup.Fill(t, a, root))
			}
			top.SetArgs(args)
			code := execute(top, &errOut)
			if code != c.Expect.Code {
				t.Errorf("exit code = %d, want %d; stdout %q stderr %q", code, c.Expect.Code, out.String(), errOut.String())
			}
			if len(c.Expect.StdoutContains) == 0 || c.Expect.Stdout != "" {
				if out.String() != c.Expect.Stdout {
					t.Errorf("stdout = %q, want %q", out.String(), c.Expect.Stdout)
				}
			}
			if c.Expect.Stderr != "" && errOut.String() != c.Expect.Stderr {
				t.Errorf("stderr = %q, want %q", errOut.String(), c.Expect.Stderr)
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
			for rel, want := range c.Expect.Files {
				got, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != fixturesetup.Fill(t, want, root) {
					t.Errorf("%s = %q, want %q", rel, got, want)
				}
			}
		})
	}
}
