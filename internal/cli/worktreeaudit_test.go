package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/prbody"
	"github.com/CorneeldH/lighthouse/internal/scriptcase"
)

const worktreeAuditFixtures = "../../tests/pack/fixtures/scripts/worktree-audit/cases.json"

// TestWorktreeAuditCases replays every case of cases.json, the fixture that worktree_audit.py ran
// against first, through the real command: real git against the setup repo, the forge on a replay
// server (reached as in tests/pack, by the loopback override and a dummy token).
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
			t.Setenv("GH_TOKEN", "dummy")
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv(prbody.GitHubAPIURLEnv, scriptcase.ReplayForge(t, fillForgeCalls(t, c.ForgeCalls, root)))
			env := defaultWorktreeAuditEnv()
			// gh is never scripted any more: a call of it fails the case.
			if len(c.Calls) != 0 {
				t.Fatal("a worktree-audit case scripts gh calls")
			}
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

func worktreeAuditToml(t *testing.T, text string) {
	t.Helper()
	postArtefactToml(t, text)
}

// The token goes with every request, so only a loopback API URL is followed: any other value is
// ignored and the real GitHub API is used.
func TestWorktreeAuditForgeFollowsOnlyALoopbackAPIURL(t *testing.T) {
	worktreeAuditToml(t, "[forge]\nrepo = \"o/r\"\n")
	t.Setenv("GH_TOKEN", "dummy")
	for _, tc := range []struct{ name, url, want string }{
		{"loopback", "http://127.0.0.1:9", "http://127.0.0.1:9"},
		{"elsewhere", "https://evil.example.com/api", "https://api.github.com"},
		{"unset", "", "https://api.github.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(prbody.GitHubAPIURLEnv, tc.url)

			f, err := defaultWorktreeAuditEnv().Forge()

			if err != nil {
				t.Fatal(err)
			}
			if gh, ok := f.(*forge.GitHub); !ok || gh.BaseURL != tc.want {
				t.Errorf("forge = %#v, want a GitHub with BaseURL %q", f, tc.want)
			}
		})
	}
}

// Someone who is only logged in through `gh auth login` keeps working; without that token too
// the error is a *forge.TokenError.
func TestWorktreeAuditForgeFallsBackToGhAuthToken(t *testing.T) {
	worktreeAuditToml(t, "[forge]\nrepo = \"o/r\"\n")
	none := func(string) string { return "" }

	o := newForgeOpener(none, "")
	o.ghToken = func() string { return "from-gh" }
	f, err := worktreeAuditEnvWith(o).Forge()
	if err != nil {
		t.Fatal(err)
	}
	if gh, ok := f.(*forge.GitHub); !ok || gh.Token != "from-gh" {
		t.Errorf("forge = %#v, want a GitHub with the token of gh auth token", f)
	}

	_, err = worktreeAuditEnvWith(newForgeOpener(none, "")).Forge()
	var te *forge.TokenError
	if !errors.As(err, &te) {
		t.Errorf("err = %v, want a *forge.TokenError", err)
	}
}

// A GitLab repo opens a GitLab forge, with the token of GITLAB_TOKEN and never gh's.
func TestWorktreeAuditForgeOpensGitLab(t *testing.T) {
	worktreeAuditToml(t, "[forge]\nkind = \"gitlab\"\nhost = \"git.example.org\"\nrepo = \"g/p\"\n")
	getenv := func(k string) string {
		if k == "GITLAB_TOKEN" {
			return "glpat"
		}
		return ""
	}
	o := newForgeOpener(getenv, "")
	o.ghToken = func() string { t.Error("gh auth token asked for a GitLab repo"); return "x" }

	f, err := worktreeAuditEnvWith(o).Forge()

	if err != nil {
		t.Fatal(err)
	}
	if f.Kind() != "gitlab" {
		t.Errorf("kind = %q, want gitlab", f.Kind())
	}
}
