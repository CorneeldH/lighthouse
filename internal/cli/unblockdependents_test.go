package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

// unblockRun runs `lighthouse unblock-dependents args...` with toml as lighthouse.toml and o as
// the forge opener, and returns the exit code, stdout and stderr.
func unblockRun(t *testing.T, toml string, o *forgeOpener, args ...string) (int, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	var out, errOut bytes.Buffer
	top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	top.AddCommand(newUnblockCommand(o))
	top.SetOut(&out)
	top.SetErr(&errOut)
	top.SetArgs(append([]string{"unblock-dependents"}, args...))
	code := execute(top, &errOut)
	return code, out.String(), errOut.String()
}

// tokens is a getenv with only the given variables set.
func tokens(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// githubWithDependent is a fake GitHub where the closed issue 1 blocks the open issue 2, which
// carries lbl.
func githubWithDependent(t *testing.T, lbl string) *forgetest.Server {
	srv := forgetest.New(t)
	srv.Issues = map[int]map[string]any{
		1: {"number": 1, "state": "closed", "state_reason": "completed"},
		2: {"number": 2, "state": "open", "labels": []any{map[string]any{"name": "2-area:x"}, map[string]any{"name": lbl}}},
	}
	srv.BlockedBy = map[int][]map[string]any{2: {{"number": 1, "state": "closed", "repository_url": srv.URL() + "/repos/Owner/repo"}}}
	return srv
}

func labelsOf(srv *forgetest.Server, n int) []string {
	var out []string
	for _, l := range srv.Issues[n]["labels"].([]any) {
		out = append(out, l.(map[string]any)["name"].(string))
	}
	return out
}

// test_blocked_label_from_config and test_blocked_label_default, on the forge of lighthouse.toml.
func TestUnblockReleasesWithTheLabelOfTheConfig(t *testing.T) {
	for _, tc := range []struct{ name, extra, label string }{
		{"from config", "[labels]\nblocked_issue = \"x\"\n", "x"},
		{"default", "", "blocked-issue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := githubWithDependent(t, tc.label)
			o := newForgeOpener(tokens(map[string]string{"GH_TOKEN": "t"}), srv.URL())
			code, out, errOut := unblockRun(t, "[forge]\nrepo = \"Owner/repo\"\n"+tc.extra, o, "--issue", "1")
			want := "## Release lane for #1\n\n- #2: released, `" + tc.label + "` comes off\n"
			if code != 0 || out != want || errOut != "" {
				t.Errorf("unblock-dependents = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
			}
			if got := labelsOf(srv, 2); len(got) != 1 || got[0] != "2-area:x" {
				t.Errorf("labels #2 = %q, want [2-area:x]", got)
			}
		})
	}
}

// A GitLab repo: GITLAB_TOKEN and the project of lighthouse.toml; a close without a reason
// releases.
func TestUnblockOnGitLab(t *testing.T) {
	srv := gitlabtest.New(t)
	srv.AddIssue(map[string]any{"iid": 1, "state": "closed", "labels": []string{}})
	srv.AddIssue(map[string]any{"iid": 2, "state": "opened", "labels": []string{"blocked-issue"}})
	srv.AddLink(2, map[string]any{"iid": 1, "state": "closed", "link_type": "is_blocked_by",
		"references": map[string]any{"full": "group/project#1"}})
	o := newForgeOpener(tokens(map[string]string{"GITLAB_TOKEN": "t"}), "")
	o.gitlabURL = srv.URL()
	code, out, errOut := unblockRun(t, "[forge]\nkind = \"gitlab\"\nrepo = \"group/project\"\n", o, "--issue", "1")
	want := "## Release lane for #1\n\n- #2: released, `blocked-issue` comes off\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("unblock-dependents = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	var unlabeled bool
	for _, w := range srv.Writes() {
		unlabeled = unlabeled || (w.Method == "PUT" && w.Body["remove_labels"] == "blocked-issue")
	}
	if !unlabeled {
		t.Errorf("writes = %+v; want a PUT that removes blocked-issue", srv.Writes())
	}
}

// test_step_summary_is_appended.
func TestUnblockAppendsTheStepSummary(t *testing.T) {
	srv := githubWithDependent(t, "blocked-issue")
	o := newForgeOpener(tokens(map[string]string{"GH_TOKEN": "t"}), srv.URL())
	summary := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(summary, []byte("earlier line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte("[forge]\nrepo = \"Owner/repo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	var out, errOut bytes.Buffer
	top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	top.AddCommand(newUnblockCommand(o))
	top.SetOut(&out)
	top.SetErr(&errOut)
	top.SetArgs([]string{"unblock-dependents", "--issue", "1"})
	if code := execute(top, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	got, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	want := "earlier line\n## Release lane for #1\n\n- #2: released, `blocked-issue` comes off\n"
	if string(got) != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

// Usage, a broken config and a missing token are exit 2 and touch no forge; test_help_is_english,
// test_missing_issue_is_a_usage_error, test_issue_must_be_a_number and test_broken_config_is_rc_2.
func TestUnblockExit2(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		env        map[string]string
		args       []string
		wantErr    []string
	}{
		{"missing --issue", "[forge]\nrepo = \"Owner/repo\"\n", map[string]string{"GH_TOKEN": "t"}, nil, []string{"--issue"}},
		{"issue not a number", "[forge]\nrepo = \"Owner/repo\"\n", map[string]string{"GH_TOKEN": "t"}, []string{"--issue", "x"}, []string{"issue"}},
		{"broken label", "[forge]\nrepo = \"Owner/repo\"\n[labels]\nblocked_issue = 3\n", map[string]string{"GH_TOKEN": "t"},
			[]string{"--issue", "1"}, []string{"unblock-dependents: ", "labels.blocked_issue"}},
		{"no forge repo", "[project]\nlanguage = \"en\"\n", map[string]string{"GH_TOKEN": "t"},
			[]string{"--issue", "1"}, []string{"unblock-dependents: ", "missing [forge] repo"}},
		{"no token", "[forge]\nrepo = \"Owner/repo\"\n", nil,
			[]string{"--issue", "1"}, []string{"unblock-dependents: no GitHub token: set GH_TOKEN or GITHUB_TOKEN\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := forgetest.New(t) // fails the test on any request
			o := newForgeOpener(tokens(tc.env), srv.URL())
			code, out, errOut := unblockRun(t, tc.toml, o, tc.args...)
			if code != 2 || out != "" {
				t.Errorf("unblock-dependents = %d, %q; want 2 and no stdout", code, out)
			}
			for _, w := range tc.wantErr {
				if !strings.Contains(errOut, w) {
					t.Errorf("stderr %q lacks %q", errOut, w)
				}
			}
		})
	}
}

func TestUnblockHelpIsEnglish(t *testing.T) {
	code, out, _ := unblockRun(t, "", newForgeOpener(tokens(nil), ""), "--help")
	if code != 0 || !strings.Contains(out, "Release the issues that waited on a just-closed issue") {
		t.Errorf("--help = %d, %q", code, out)
	}
}

// A forge that does not open is a red summary, never "nothing to release".
func TestUnblockForgeThatDoesNotOpen(t *testing.T) {
	o := newForgeOpener(tokens(map[string]string{"GH_TOKEN": "t"}), "")
	o.open = func(forge.Config) (forge.Forge, error) { return nil, errors.New("boom") }
	code, out, errOut := unblockRun(t, "[forge]\nrepo = \"Owner/repo\"\n", o, "--issue", "1")
	want := "## Release lane for #1\n\n**Error:** released issues not read: boom\n"
	if code != 1 || out != want || errOut != "" {
		t.Errorf("unblock-dependents = %d, %q, stderr %q; want 1, %q", code, out, errOut, want)
	}
}
