package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

// auditWant is what one run of worktree-audit gives. With a contains list set, stdout and stderr
// are not compared whole; otherwise both are, also when empty, except that stderrPrefix, when
// set, only has to start stderr (a forge error names the random port of the fake).
type auditWant struct {
	code                           int
	stdout, stderr, stderrPrefix   string
	stdoutContains, stderrContains []string
}

// auditRun runs `lighthouse worktree-audit args...` in a repo built from setup under
// <tmp>/root, with config as lighthouse.toml and o as the forge opener. Every string may name
// {root}. It returns the exit code, stdout, stderr and the root.
func auditRun(t *testing.T, setup []map[string]any, config string, env map[string]string, o *forgeOpener, args ...string) (int, string, string, string) {
	t.Helper()
	// git reports the resolved path, so resolve the temp dir too: on macOS t.TempDir() lies
	// under /var, a symlink to /private/var. The main checkout's row shows the name root.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	fixturesetup.Build(t, root, setup)
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte(fixturesetup.Fill(t, config, root)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("HOME", t.TempDir())
	for k, v := range env {
		t.Setenv(k, fixturesetup.Fill(t, v, root))
	}
	t.Chdir(root)
	var out, errOut bytes.Buffer
	top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	top.AddCommand(newWorktreeAuditCommand(worktreeAuditEnv(o)))
	top.SetOut(&out)
	top.SetErr(&errOut)
	full := []string{"worktree-audit"}
	for _, a := range args {
		full = append(full, fixturesetup.Fill(t, a, root))
	}
	top.SetArgs(full)
	code := execute(top, &errOut)
	return code, out.String(), errOut.String(), root
}

func (w auditWant) check(t *testing.T, code int, stdout, stderr, root string) {
	t.Helper()
	if code != w.code {
		t.Errorf("exit code = %d, want %d; stdout %q stderr %q", code, w.code, stdout, stderr)
	}
	if len(w.stdoutContains)+len(w.stderrContains) == 0 {
		if want := strings.ReplaceAll(w.stdout, "{root}", root); stdout != want {
			t.Errorf("stdout\n got %q\nwant %q", stdout, want)
		}
		if w.stderrPrefix != "" {
			if !strings.HasPrefix(stderr, w.stderrPrefix) || strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr %q, want one line that starts with %q", stderr, w.stderrPrefix)
			}
		} else if want := strings.ReplaceAll(w.stderr, "{root}", root); stderr != want {
			t.Errorf("stderr\n got %q\nwant %q", stderr, want)
		}
	}
	for _, want := range w.stdoutContains {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %q", stdout, want)
		}
	}
	for _, want := range w.stderrContains {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q lacks %q", stderr, want)
		}
	}
}

// The seeds of the GitHub fake: the pull requests of o/r in REST shape, and what IssueChanges
// does.
func pull(n int, state, mergedAt, head string) map[string]any {
	p := map[string]any{"number": n, "state": state, "head": map[string]any{"ref": head}}
	if mergedAt != "" {
		p["merged_at"] = mergedAt
	}
	return p
}

func noPulls(*forgetest.Server) {}

func open9(srv *forgetest.Server) { srv.Pulls = []map[string]any{pull(9, "open", "", "issue-9-x")} }

func merged9(srv *forgetest.Server) {
	srv.Pulls = []map[string]any{pull(9, "closed", "2099-01-01T00:00:00Z", "issue-9-x")}
}

func merged9AndOpen12(srv *forgetest.Server) {
	merged9(srv)
	srv.Pulls = append(srv.Pulls, pull(12, "open", "", "feat-open"))
}

// headlessAndMerged9 is merged9 after a pull without a head ref, which the lookup skips (the
// typed twin of a line that is no JSON).
func headlessAndMerged9(srv *forgetest.Server) {
	merged9(srv)
	srv.Pulls = append([]map[string]any{{"number": 3, "state": "open", "head": map[string]any{}}}, srv.Pulls...)
}

func pullsFail(srv *forgetest.Server) { srv.Fail = map[string]int{"GET /repos/o/r/pulls": 502} }

func issueChangesFail(seed func(*forgetest.Server)) func(*forgetest.Server) {
	return func(srv *forgetest.Server) {
		seed(srv)
		srv.FailOps = map[string]string{"IssueChanges": "Could not resolve to an Issue with the number of 9."}
	}
}

// issueChangesPage2Fails gives issue 9 a second page of closing PRs that fails.
func issueChangesPage2Fails(seed func(*forgetest.Server)) func(*forgetest.Server) {
	return func(srv *forgetest.Server) {
		seed(srv)
		var many []map[string]any
		for n := 100; n <= 200; n++ {
			many = append(many, map[string]any{"number": n, "state": "MERGED", "headRefName": "h" + strconv.Itoa(n),
				"repository": map[string]any{"nameWithOwner": "o/r"}})
		}
		srv.ClosingPRs = map[int][]map[string]any{9: many}
		srv.IssueChangesPage2Fails = map[int]bool{9: true}
	}
}

// TestWorktreeAuditCases runs the cases of the former cases.json (the fixture worktree_audit.py
// ran against first) through the real command: real git against the setup repo, the GitHub fake
// as the forge. A case without forge must not open one; noToken has no token to open it with.
// test_repo_slug_reads_lighthouse_and_not_origin is TestWorktreeAuditReadsThePRsOfTheConfiguredRepo.
func TestWorktreeAuditCases(t *testing.T) {
	for _, c := range []struct {
		name    string
		argv    []string
		env     map[string]string
		config  string
		setup   []map[string]any
		forge   func(*forgetest.Server)
		noToken bool
		want    auditWant
	}{
		{
			name: "test_runs_as_installed_file",
			argv: []string{"--help"},
			want: auditWant{code: 0, stdoutContains: []string{"cleanup: yes|no", "--issue"}},
		},
		{
			name: "test_view_flags_are_mutually_exclusive",
			argv: []string{"--cleanup", "--sessions"},
			want: auditWant{code: 2, stderrContains: []string{"argument --sessions: not allowed with argument --cleanup"}},
		},
		{
			name: "test_issue_takes_an_int",
			argv: []string{"--issue", "x"},
			want: auditWant{code: 2, stderrContains: []string{"argument --issue: invalid int value: 'x'"}},
		},
		{
			name: "test_worktree_list_failure_is_reported_by_name[not a repo]",
			argv: []string{"--kpi"},
			want: auditWant{code: 1, stdout: "", stderr: "git worktree list --porcelain failed — cannot read the repo.\n"},
		},
		{
			name:   "test_broken_config_stops_collect_loudly[command]",
			config: "[worktree_audit]\nprotected_refs = \"x\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}},
			want:   auditWant{code: 2, stderrContains: []string{"worktree-audit: ", "lighthouse.toml: key worktree_audit.protected_refs must be of type list[str]\n"}},
		},
		{
			name:   "test_without_forge_repo_collect_says_it_skips_the_pr_lookup[command]",
			config: "[project]\nlanguage = \"en\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			want:   auditWant{code: 0, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| root | main | — | ? | ? | — | — | 🔒 never touch |\n| issue-9-x | issue-9-x | — | 1 | no | — | — | ⚠️ orphan commit without parent — likely test leakage |\n\nCommands — read the table first; this script runs nothing.\n\nNothing with verdict ✅ — no commands.\n", stderr: "worktree-audit: PR lookup skipped — [forge] repo is not set, so every PR state is unknown\n"},
		},
		{
			name:   "test_main_exitcode[open pr exits 1]",
			argv:   []string{"--only", "issue-9-x"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  open9,
			want:   auditWant{code: 1, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | #9 open | 1 | no | — | — | ⚠️ 1 local commits — check |\n\ncleanup: no — ⚠️ 1 local commits — check\n", stderr: ""},
		},
		{
			name:   "test_main_exitcode[merged pr exits 0]",
			argv:   []string{"--only", "issue-9-x"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | #9 merged | 1 | no | — | — | ✅ safe to remove [squash-merge] |\n\ncleanup: yes\ngit worktree remove \"{root}/.claude/worktrees/issue-9-x\"\ngit branch -D issue-9-x\n", stderr: ""},
		},
		{
			name:   "test_main_exitcode[unknown branch exits 1]",
			argv:   []string{"--only", "gone"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 1, stdout: "cleanup: no — branch gone not found\n", stderr: ""},
		},
		{
			name:   "test_main_exit_2_on_failed_lookup",
			argv:   []string{"--issue", "9"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}},
			forge:  issueChangesFail(noPulls),
			want:   auditWant{code: 2, stdout: "cleanup: no — PR lookup for issue #9 failed\n", stderr: "worktree-audit: PR lookup for issue #9 failed — forge graphql: Could not resolve to an Issue with the number of 9.\n"},
		},
		{
			name:   "test_main_exit_0_with_blocks",
			argv:   []string{"--issue", "9"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "branch: issue-9-x\n| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | #9 merged | 1 | no | — | — | ✅ safe to remove [squash-merge] |\n\ncleanup: yes\ngit worktree remove \"{root}/.claude/worktrees/issue-9-x\"\ngit branch -D issue-9-x\n", stderr: ""},
		},
		{
			name:   "test_run_gh_issue_pr_heads_argv[not json]: the forge answers an error",
			argv:   []string{"--issue", "9"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  issueChangesFail(merged9),
			want:   auditWant{code: 2, stdout: "cleanup: no — PR lookup for issue #9 failed\n", stderr: "worktree-audit: PR lookup for issue #9 failed — forge graphql: Could not resolve to an Issue with the number of 9.\n"},
		},
		{
			name:   "test_run_gh_issue_pr_heads_argv[timeout]: the second page fails",
			argv:   []string{"--issue", "9"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  issueChangesPage2Fails(merged9),
			want: auditWant{code: 2, stdout: "cleanup: no — PR lookup for issue #9 failed\n",
				stderr: "worktree-audit: PR lookup for issue #9 failed — forge graphql: something went wrong on page two\n"},
		},
		{
			name:   "test_pulls_lookup_failure_is_unknown[gh fails]",
			argv:   []string{"--only", "issue-9-x"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  pullsFail,
			want: auditWant{code: 1, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | — | 1 | no | — | — | ⚠️ orphan commit without parent — likely test leakage |\n\ncleanup: no — ⚠️ orphan commit without parent — likely test leakage\n",
				stderrPrefix: "worktree-audit: PR lookup failed — forge GET "},
		},
		{
			name:    "test_pulls_lookup_failure_is_unknown[timeout]: no token, the forge does not open",
			argv:    []string{"--only", "issue-9-x"},
			config:  "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:   []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			noToken: true,
			want: auditWant{code: 1, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | — | 1 | no | — | — | ⚠️ orphan commit without parent — likely test leakage |\n\ncleanup: no — ⚠️ orphan commit without parent — likely test leakage\n",
				stderr: "worktree-audit: PR lookup failed — no GitHub token: set GH_TOKEN or GITHUB_TOKEN\n"},
		},
		{
			name:   "test_pulls_skip_lines_that_are_no_json: a pull without a head is skipped",
			argv:   []string{"--only", "issue-9-x"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  headlessAndMerged9,
			want:   auditWant{code: 0, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | #9 merged | 1 | no | — | — | ✅ safe to remove [squash-merge] |\n\ncleanup: yes\ngit worktree remove \"{root}/.claude/worktrees/issue-9-x\"\ngit branch -D issue-9-x\n", stderr: ""},
		},
		{
			name:   "test_missing_sessions_dir_is_no_sessions[command]",
			argv:   []string{"--sessions"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "| last session | live? | worktree | resume | verdict |\n|---|---|---|---|---|\n| — | — | — | no sessions found | — |\n", stderr: ""},
		},
		{
			name:   "test_broken_session_json_is_skipped[command]",
			argv:   []string{"--sessions"},
			env:    map[string]string{"HOME": "{root}/home"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}, map[string]any{"file": "home/.claude/sessions/kapot.json", "text": "{"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "| last session | live? | worktree | resume | verdict |\n|---|---|---|---|---|\n| — | — | — | no sessions found | — |\n", stderr: ""},
		},
		{
			name:   "test_cleanup_on_a_real_repo",
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n\n[worktree_audit]\nprotected_paths = [\".port-registry\"]\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}, map[string]any{"worktree": ".claude/worktrees/feat-open", "of": ".", "branch": "feat-open"}, map[string]any{"file": ".claude/worktrees/feat-open/wip.txt", "text": "x"}, map[string]any{"file": ".claude/worktrees/stale-leftover/README.md", "text": "x"}, map[string]any{"file": ".claude/worktrees/worktrees/.port-registry", "text": "9=20009\n"}},
			forge:  merged9AndOpen12,
			want:   auditWant{code: 0, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| root | main | — | ? | ? | — | — | 🔒 never touch |\n| worktrees | worktrees | — | ? | ? | — | — | 🔒 never touch |\n| feat-open | feat-open | #12 open | 1 | yes | — | — | ⛔ save first |\n| issue-9-x | issue-9-x | #9 merged | 1 | no | — | — | ✅ safe to remove [squash-merge] |\n| stale-leftover | stale-leftover | — | ? | ? | — | — | ❓ orphan — manual |\n\nCommands — read the table first; this script runs nothing.\n\n```bash\ngit worktree remove \"{root}/.claude/worktrees/issue-9-x\"\n```\n```bash\ngit branch -D issue-9-x\n```\n\nPort blocks (registered in worktrees/.port-registry, read-only):\n  issue-9-x  port 20009  {root}/.claude/worktrees/issue-9-x\nThe process that actually holds the port is not looked up here — an empty list above does not mean that nothing runs.\n", stderr: ""},
		},
		{
			name:   "test_origin_and_kpi_on_a_real_repo[origin]",
			argv:   []string{"--origin"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n\nCommands — read the table first; this script runs nothing.\n\nNothing with verdict ✅ — no commands.\n", stderr: ""},
		},
		{
			name:   "test_origin_and_kpi_on_a_real_repo[kpi]",
			argv:   []string{"--kpi"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "worktrees=1 safe=1 remote_branches=0 warn=0\n", stderr: ""},
		},
		{
			name:   "test_repo_flag_names_the_main_checkout",
			argv:   []string{"--repo", "{root}", "--kpi"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  merged9,
			want:   auditWant{code: 0, stdout: "worktrees=1 safe=1 remote_branches=0 warn=0\n", stderr: ""},
		},
		{
			name:   "test_default_branch_from_config[command]",
			argv:   []string{"--only", "issue-9-x"},
			config: "[forge]\nkind = \"github\"\nrepo = \"o/r\"\ndefault_branch = \"trunk\"\n",
			setup:  []map[string]any{map[string]any{"repo": ".", "branch": "main"}, map[string]any{"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}},
			forge:  open9,
			want:   auditWant{code: 1, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n|---|---|---|---|---|---|---|---|\n| issue-9-x | issue-9-x | #9 open | 1 | no | — | — | ⚠️ 1 local commits — check |\n\ncleanup: no — ⚠️ 1 local commits — check\n", stderr: ""},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := forgetest.New(t)
			o := newForgeOpener(tokens(map[string]string{"GH_TOKEN": "t"}), srv.URL())
			switch {
			case c.noToken:
				o = newForgeOpener(tokens(nil), srv.URL())
			case c.forge == nil:
				o.open = func(forge.Config) (forge.Forge, error) {
					t.Error("forge opened")
					return nil, os.ErrInvalid
				}
			default:
				c.forge(srv)
			}
			code, stdout, stderr, root := auditRun(t, c.setup, c.config, c.env, o, c.argv...)
			c.want.check(t, code, stdout, stderr, root)
		})
	}
}

// test_repo_slug_reads_lighthouse_and_not_origin: the PRs come from `[forge] repo`, not from the
// origin remote.
func TestWorktreeAuditReadsThePRsOfTheConfiguredRepo(t *testing.T) {
	srv := forgetest.New(t)
	o := newForgeOpener(tokens(map[string]string{"GH_TOKEN": "t"}), srv.URL())
	setup := []map[string]any{{"repo": ".", "branch": "main"}, {"origin": ".", "url": "https://github.com/someone/else.git"}}
	code, stdout, stderr, root := auditRun(t, setup, "[forge]\nkind = \"github\"\nrepo = \"Other/repo\"\n", nil, o, "--kpi")
	auditWant{code: 0, stdout: "worktrees=0 safe=0 remote_branches=0 warn=0\n"}.check(t, code, stdout, stderr, root)
	if n, gone := srv.Count("/repos/Other/repo/pulls"), srv.Count("/repos/someone/else/pulls"); n != 1 || gone != 0 {
		t.Errorf("pulls of Other/repo read %d times, of someone/else %d; want 1 and 0", n, gone)
	}
}

// Without a token the forge does not open: every PR state is unknown, never "no PR", and --issue
// is exit 2.
func TestWorktreeAuditWithoutTokenIsUnknown(t *testing.T) {
	srv := forgetest.New(t) // fails the test on any request it does not know
	o := newForgeOpener(tokens(nil), srv.URL())
	setup := []map[string]any{{"repo": ".", "branch": "main"}, {"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}}
	const config = "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n"
	const noToken = "no GitHub token: set GH_TOKEN or GITHUB_TOKEN"
	code, stdout, stderr, root := auditRun(t, setup, config, nil, o, "--issue", "9")
	auditWant{code: 2, stdout: "cleanup: no — PR lookup for issue #9 failed\n",
		stderr: "worktree-audit: PR lookup failed — " + noToken + "\n" +
			"worktree-audit: PR lookup for issue #9 failed — " + noToken + "\n"}.check(t, code, stdout, stderr, root)
}

// The same command on GitLab: the merge requests of the project give the PR state, and the
// merge requests of the issue its branches.
func TestWorktreeAuditOnGitLab(t *testing.T) {
	setup := []map[string]any{{"repo": ".", "branch": "main"}, {"worktree": ".claude/worktrees/issue-9-x", "of": ".", "branch": "issue-9-x"}}
	const config = "[forge]\nkind = \"gitlab\"\nrepo = \"group/project\"\n"
	const merged = "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n" +
		"|---|---|---|---|---|---|---|---|\n" +
		"| issue-9-x | issue-9-x | #9 merged | 1 | no | — | — | ✅ safe to remove [squash-merge] |\n\n" +
		"cleanup: yes\ngit worktree remove \"{root}/.claude/worktrees/issue-9-x\"\ngit branch -D issue-9-x\n"
	for _, tt := range []struct {
		name string
		args []string
		mrs  []map[string]any
		want auditWant
	}{
		{"merged exits 0", []string{"--only", "issue-9-x"},
			[]map[string]any{{"iid": 9, "state": "merged", "merged_at": "2099-01-01T00:00:00Z", "source_branch": "issue-9-x"}},
			auditWant{code: 0, stdout: merged}},
		{"open exits 1", []string{"--only", "issue-9-x"},
			[]map[string]any{{"iid": 9, "state": "opened", "source_branch": "issue-9-x"}},
			auditWant{code: 1, stdout: "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |\n" +
				"|---|---|---|---|---|---|---|---|\n" +
				"| issue-9-x | issue-9-x | #9 open | 1 | no | — | — | ⚠️ 1 local commits — check |\n\n" +
				"cleanup: no — ⚠️ 1 local commits — check\n"}},
		{"issue with blocks", []string{"--issue", "9"},
			[]map[string]any{{"iid": 9, "state": "merged", "merged_at": "2099-01-01T00:00:00Z", "source_branch": "issue-9-x"}},
			auditWant{code: 0, stdout: "branch: issue-9-x\n" + merged}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := gitlabtest.New(t)
			for _, m := range tt.mrs {
				srv.AddMR(m)
			}
			o := newForgeOpener(tokens(map[string]string{"GITLAB_TOKEN": "t"}), "")
			o.gitlabURL = srv.URL()
			code, stdout, stderr, root := auditRun(t, setup, config, nil, o, tt.args...)
			tt.want.check(t, code, stdout, stderr, root)
		})
	}
}
