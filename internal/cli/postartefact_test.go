package cli

import (
	"bytes"
	"encoding/json"
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

const postArtefactFixtures = "../../tests/pack/fixtures/scripts/post-artefact/cases.json"

// TestPostArtefactCases replays every case of cases.json, the fixture that post_artefact.py ran
// against first, through the real command: arguments, config, real `git` and exit code included.
func TestPostArtefactCases(t *testing.T) {
	for _, c := range scriptcase.Load(t, postArtefactFixtures) {
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
			// The cases predate workflow-push-gate: a controller run that runs this test adds its git calls.
			t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
			for k, v := range c.Env {
				t.Setenv(k, fixturesetup.Fill(t, v, root))
			}
			// The Python runner starts the script in the root of the case.
			t.Chdir(root)
			// The forge is the replay server: the binary reaches it as it does in tests/pack, by the
			// loopback override and a dummy token.
			t.Setenv("GH_TOKEN", "dummy")
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv(prbody.GitHubAPIURLEnv, scriptcase.ReplayForge(t, fillForgeCalls(t, c.ForgeCalls, root)))
			env := defaultPostArtefactEnv()
			// gh is never scripted any more: a call of it fails the case.
			env.Run = scriptcase.Replay(t, fillCalls(t, c.Calls, root))
			var out, errOut bytes.Buffer
			top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
			top.AddCommand(newPostArtefactCommand(env))
			top.SetOut(&out)
			top.SetErr(&errOut)
			args := []string{"post-artefact"}
			for _, a := range c.Argv {
				args = append(args, fixturesetup.Fill(t, a, root))
			}
			top.SetArgs(args)
			code := execute(top, &errOut)
			if code != c.Expect.Code {
				t.Errorf("exit code = %d, want %d; stdout %q stderr %q", code, c.Expect.Code, out.String(), errOut.String())
			}
			if len(c.Expect.StdoutContains) == 0 || c.Expect.Stdout != "" {
				if want := fixturesetup.Fill(t, c.Expect.Stdout, root); out.String() != want {
					t.Errorf("stdout = %q, want %q", out.String(), want)
				}
			}
			if want := fixturesetup.Fill(t, c.Expect.Stderr, root); (want != "" || len(c.Expect.StderrContains) == 0) && errOut.String() != want {
				t.Errorf("stderr = %q, want %q", errOut.String(), want)
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

func TestPostArtefactUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"one argument", []string{"post-artefact", "1"}},
		{"three arguments", []string{"post-artefact", "1", "a.md", "b.md"}},
		{"nr is not a number", []string{"post-artefact", "x", "a.md"}},
		{"unknown flag", []string{"post-artefact", "--nope", "1", "a.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			root := NewRoot(strings.NewReader(""), &out, &errOut)
			root.SetArgs(tc.args)
			if code := execute(root, &errOut); code != 2 {
				t.Errorf("exit code = %d, want 2; stderr %q", code, errOut.String())
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", out.String())
			}
		})
	}
}

func TestPyIntReadsAnIssueNumberLikePython(t *testing.T) {
	for in, want := range map[string]int{"12": 12, " 12\n": 12, "+7": 7, "1_000": 1000, "007": 7} {
		if got, ok := pyInt(in); !ok || got != want {
			t.Errorf("pyInt(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "x", "1x", "--1", "+-1", "_1", "1_", "1__0", "1.5", "0x10"} {
		if got, ok := pyInt(in); ok {
			t.Errorf("pyInt(%q) = %d, want a refusal", in, got)
		}
	}
}

// fillCalls fills the placeholders of the scripted calls: the commit under test is only known
// after the setup ran, so the calls name it as {rev:work:HEAD}.
func fillCalls(t *testing.T, calls []scriptcase.Call, root string) []scriptcase.Call {
	t.Helper()
	out := make([]scriptcase.Call, len(calls))
	for i, c := range calls {
		c.Argv = append([]string(nil), c.Argv...)
		for j, a := range c.Argv {
			c.Argv[j] = fixturesetup.Fill(t, a, root)
		}
		if c.Stdin != nil {
			filled := fixturesetup.Fill(t, *c.Stdin, root)
			c.Stdin = &filled
		}
		c.Stdout = fixturesetup.Fill(t, c.Stdout, root)
		c.Stderr = fixturesetup.Fill(t, c.Stderr, root)
		out[i] = c
	}
	return out
}

// fillForgeCalls fills the placeholders of the scripted forge calls, as fillCalls does.
func fillForgeCalls(t *testing.T, calls []scriptcase.ForgeCall, root string) []scriptcase.ForgeCall {
	t.Helper()
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	var out []scriptcase.ForgeCall
	if err := json.Unmarshal([]byte(fixturesetup.Fill(t, string(raw), root)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postArtefactToml(t *testing.T, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
}

// The token goes with every request, so only a loopback API URL is followed: any other value is
// ignored and the real GitHub API is used.
func TestPostArtefactForgeFollowsOnlyALoopbackAPIURL(t *testing.T) {
	postArtefactToml(t, "[forge]\nrepo = \"o/r\"\n")
	t.Setenv("GH_TOKEN", "dummy")
	for _, tc := range []struct{ name, url, want string }{
		{"loopback", "http://127.0.0.1:9", "http://127.0.0.1:9"},
		{"elsewhere", "https://evil.example.com/api", "https://api.github.com"},
		{"unset", "", "https://api.github.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(prbody.GitHubAPIURLEnv, tc.url)

			f, err := defaultPostArtefactEnv().Forge("")

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
func TestPostArtefactForgeFallsBackToGhAuthToken(t *testing.T) {
	postArtefactToml(t, "[forge]\nrepo = \"o/r\"\n")
	none := func(string) string { return "" }

	o := newForgeOpener(none, "")
	o.ghToken = func() string { return "from-gh" }
	f, err := postArtefactEnvWith(o).Forge("")
	if err != nil {
		t.Fatal(err)
	}
	if gh, ok := f.(*forge.GitHub); !ok || gh.Token != "from-gh" {
		t.Errorf("forge = %#v, want a GitHub with the token of gh auth token", f)
	}

	_, err = postArtefactEnvWith(newForgeOpener(none, "")).Forge("")
	var te *forge.TokenError
	if !errors.As(err, &te) {
		t.Errorf("err = %v, want a *forge.TokenError", err)
	}
}

// A GitLab repo opens a GitLab forge, with the token of GITLAB_TOKEN and never gh's.
func TestPostArtefactForgeOpensGitLab(t *testing.T) {
	postArtefactToml(t, "[forge]\nkind = \"gitlab\"\nhost = \"git.example.org\"\nrepo = \"g/p\"\n")
	getenv := func(k string) string {
		if k == "GITLAB_TOKEN" {
			return "glpat"
		}
		return ""
	}
	o := newForgeOpener(getenv, "")
	o.ghToken = func() string { t.Error("gh auth token asked for a GitLab repo"); return "x" }

	f, err := postArtefactEnvWith(o).Forge("")

	if err != nil {
		t.Fatal(err)
	}
	if f.Kind() != "gitlab" {
		t.Errorf("kind = %q, want gitlab", f.Kind())
	}
}
