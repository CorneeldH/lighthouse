package worktreeaudit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

// The PR lookups on the forge: ListChanges for the state of every head branch, IssueChanges for
// the merged heads of one issue. The cases of parse.json (parse_pulls, parse_issue_pr_heads) live
// here with typed input; the shapes JSON could take are the forge's to read now.

func at(stamp string) time.Time {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParsePulls(t *testing.T) {
	for _, tt := range []struct {
		name    string
		changes []forge.Change
		want    map[string]PullInfo
	}{
		{"test_rest_payload_with_merged_at_counts_as_merged",
			[]forge.Change{{Number: 721, State: "merged", MergedAt: at("2026-08-24T18:05:13Z"), HeadRef: "worktree-issue-713-osor-artefacten"}},
			map[string]PullInfo{"worktree-issue-713-osor-artefacten": {Number: 721, State: "merged", MergedAt: ptr(1787594713.0)}}},
		{"test_highest_number_wins_within_the_same_state",
			[]forge.Change{{Number: 300, State: "open", HeadRef: "feat-tie"}, {Number: 400, State: "open", HeadRef: "feat-tie"}},
			map[string]PullInfo{"feat-tie": {Number: 400, State: "open"}}},
		{"test_open_wins_over_an_older_merged_pr_on_the_same_head",
			[]forge.Change{{Number: 100, State: "merged", MergedAt: at("2026-01-01T00:00:00Z"), HeadRef: "feat-x"}, {Number: 200, State: "open", HeadRef: "feat-x"}},
			map[string]PullInfo{"feat-x": {Number: 200, State: "open"}}},
		{"test_closed_unmerged_pr_is_not_merged",
			[]forge.Change{{Number: 5, State: "closed", HeadRef: "feat-y"}},
			map[string]PullInfo{"feat-y": {Number: 5, State: "closed"}}},
		{"test_merged_wins_over_a_closed_unmerged_pr",
			[]forge.Change{{Number: 5, State: "closed", HeadRef: "feat-z"}, {Number: 6, State: "merged", MergedAt: at("2026-02-02T00:00:00Z"), HeadRef: "feat-z"}},
			map[string]PullInfo{"feat-z": {Number: 6, State: "merged", MergedAt: ptr(1769990400.0)}}},
		{"test_merged_at_reaches_pullinfo_as_epoch",
			[]forge.Change{{Number: 457, State: "merged", MergedAt: at("2026-08-02T08:08:31Z"), HeadRef: "worktree-441-besluithistorie-docs"}},
			map[string]PullInfo{"worktree-441-besluithistorie-docs": {Number: 457, State: "merged", MergedAt: ptr(1785658111.0)}}},
		{"test_an_open_pr_has_no_merge_moment",
			[]forge.Change{{Number: 542, State: "open", HeadRef: "spike"}},
			map[string]PullInfo{"spike": {Number: 542, State: "open"}}},
		// The JSON case had a garbage merged_at; typed, that is a merged change without a moment
		// (a GitLab merge request can come so).
		{"test_parse_pulls_skips_a_pull_without_head_ref",
			[]forge.Change{{Number: 1, State: "open"}, {Number: 2, State: "open"}, {Number: 3, State: "open"}, {Number: 4, State: "merged", HeadRef: "g"}},
			map[string]PullInfo{"g": {Number: 4, State: "merged"}}},
		{"a closed state with a merge moment is merged",
			[]forge.Change{{Number: 8, State: "closed", MergedAt: at("2026-02-02T00:00:00Z"), HeadRef: "h"}},
			map[string]PullInfo{"h": {Number: 8, State: "merged", MergedAt: ptr(1769990400.0)}}},
		{"no changes", nil, map[string]PullInfo{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePulls(tt.changes)
			if err != nil {
				t.Fatalf("ParsePulls: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePulls = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A change in a state outside open, merged and closed stops the lookup: skipping an open PR could
// let a merged one win.
func TestParsePullsFailsOnAnUnknownState(t *testing.T) {
	got, err := ParsePulls([]forge.Change{{Number: 1, State: "open", HeadRef: "a"}, {Number: 2, State: "draft", HeadRef: "a"}})
	if err == nil || err.Error() != `change #2 has an unknown state "draft"` {
		t.Fatalf("ParsePulls = %v, %v; want the unknown state as an error", got, err)
	}
}

func TestParseIssuePRHeads(t *testing.T) {
	merged := func(head, body string, closes bool) forge.IssueChange {
		return forge.IssueChange{Number: 1, State: "MERGED", HeadRef: head, Repo: "o/r", Body: body, Closes: closes}
	}
	for _, tt := range []struct {
		name    string
		changes []forge.IssueChange
		want    []string
	}{
		{"test_parse_keeps_merged_same_repo_heads",
			[]forge.IssueChange{merged("issue-9-x", "", true), merged("fix-y", "Refs #9 — deel 1", false)},
			[]string{"issue-9-x", "fix-y"}},
		{"test_parse_cross_ref_needs_refs_line[0]", []forge.IssueChange{merged("fix-y", "zie #9", false)}, []string{}},
		{"test_parse_cross_ref_needs_refs_line[1]", []forge.IssueChange{merged("fix-y", "Refs #93", false)}, []string{}},
		{"test_parse_cross_ref_needs_refs_line[2]", []forge.IssueChange{merged("fix-y", "", false)}, []string{}},
		{"test_parse_cross_ref_refs_in_comment_does_not_count",
			[]forge.IssueChange{merged("fix-y", "<!-- Refs #9 — voorbeeld uit het template -->\nzie #9", false)}, []string{}},
		{"test_parse_cross_ref_refs_without_reason_counts", []forge.IssueChange{merged("fix-y", "Refs #9", false)}, []string{"fix-y"}},
		{"test_parse_skips_cross_repository_prs",
			[]forge.IssueChange{
				{Number: 1, State: "MERGED", HeadRef: "patch-1", CrossRepo: true, Repo: "o/r", Closes: true},
				{Number: 2, State: "MERGED", HeadRef: "patch-2", CrossRepo: true, Repo: "o/r", Body: "Refs #9"},
			}, []string{}},
		{"test_parse_skips_unmerged_and_non_pr",
			[]forge.IssueChange{
				{Number: 1, State: "OPEN", HeadRef: "issue-9-x", Repo: "o/r", Closes: true},
				{},
				{Number: 2, State: "CLOSED", HeadRef: "fix-y", Repo: "o/r", Body: "Refs #9"},
			}, []string{}},
		{"test_parse_skips_other_repos",
			[]forge.IssueChange{
				{Number: 1, State: "MERGED", HeadRef: "issue-9-x", Repo: "x/fork", Closes: true},
				{Number: 2, State: "MERGED", HeadRef: "fix-y", Repo: "x/fork", Body: "Refs #9"},
			}, []string{}},
		{"test_parse_issue_pr_heads_shapes[empty errors]", []forge.IssueChange{}, []string{}},
		{"test_parse_issue_pr_heads_shapes[null nodes and dedupe]",
			[]forge.IssueChange{{}, {}, merged("b", "Closes #9\nRefs #9", false)}, []string{"b"}},
		{"one head of two merged PRs counts once",
			[]forge.IssueChange{merged("a", "", true), {Number: 2, State: "MERGED", HeadRef: "a", Repo: "o/r", Body: "Refs #9"}},
			[]string{"a"}},
		{"the repo compares without case", []forge.IssueChange{{Number: 1, State: "MERGED", HeadRef: "a", Repo: "O/R", Closes: true}}, []string{"a"}},
		{"a closing PR needs no Refs line, wherever it comes in the list",
			[]forge.IssueChange{merged("m", "zie #9", false), merged("c", "", true)}, []string{"c"}},
		{"a merged change without a head is skipped", []forge.IssueChange{merged("", "", true)}, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseIssuePRHeads(tt.changes, "o/r", 9); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseIssuePRHeads = %q, want %q", got, tt.want)
			}
		})
	}
}

// githubEnv is an Env whose forge is the GitHub fake srv for o/r and whose config names o/r.
func githubEnv(t *testing.T, srv *forgetest.Server) Env {
	t.Helper()
	env, _ := collectEnv(t, &gitFake{answer: mainOnly}, "[forge]\nrepo = \"o/r\"\n")
	env.Forge = func(context.Context) (forge.Forge, error) {
		return forge.Open(forge.Config{Repo: "o/r", Token: "t", BaseURL: srv.URL()})
	}
	return env
}

func gitlabEnv(t *testing.T, srv *gitlabtest.Server) Env {
	t.Helper()
	env, _ := collectEnv(t, &gitFake{answer: mainOnly}, "[forge]\nkind = \"gitlab\"\nrepo = \"group/project\"\n")
	env.Forge = func(context.Context) (forge.Forge, error) {
		return forge.Open(forge.Config{Kind: "gitlab", Repo: "group/project", Token: "t", BaseURL: srv.URL()})
	}
	return env
}

func TestIssuePRHeadsOnGitHub(t *testing.T) {
	pr := func(n int, state, head, repo, body string, cross bool) map[string]any {
		return map[string]any{"number": n, "state": state, "headRefName": head, "isCrossRepository": cross,
			"body": body, "repository": map[string]any{"nameWithOwner": repo}}
	}
	srv := forgetest.New(t)
	srv.ClosingPRs = map[int][]map[string]any{9: {
		pr(1, "MERGED", "issue-9-x", "o/r", "", false),
		pr(2, "OPEN", "issue-9-open", "o/r", "", false),
		pr(3, "MERGED", "patch-1", "o/r", "", true),
	}}
	srv.CrossRefPRs = map[int][]map[string]any{9: {
		pr(4, "MERGED", "fix-y", "o/r", "Refs #9", false),
		pr(5, "MERGED", "fix-z", "o/r", "zie #9", false),
		pr(6, "MERGED", "elders", "x/other", "Refs #9", false),
	}}
	heads, ok := IssuePRHeads(context.Background(), githubEnv(t, srv), 9)
	if want := []string{"issue-9-x", "fix-y"}; !ok || !reflect.DeepEqual(heads, want) {
		t.Fatalf("IssuePRHeads = %q, %v; want %q, true", heads, ok, want)
	}
}

func TestIssuePRHeadsOnGitLab(t *testing.T) {
	mr := func(iid int, state, branch string, source int, body string) map[string]any {
		return map[string]any{"iid": iid, "state": state, "source_branch": branch, "source_project_id": source,
			"target_project_id": 1, "description": body, "references": map[string]any{"full": "group/project!" + itoa(iid)}}
	}
	srv := gitlabtest.New(t)
	srv.AddClosedBy("group/project", 9, mr(1, "merged", "issue-9-x", 1, ""))
	srv.AddClosedBy("group/project", 9, mr(2, "merged", "fork-branch", 2, ""))
	srv.AddRelatedMR(9, mr(1, "merged", "issue-9-x", 1, ""))
	srv.AddRelatedMR(9, mr(3, "merged", "fix-y", 1, "Refs #9"))
	srv.AddRelatedMR(9, mr(4, "merged", "fix-z", 1, "zie #9"))
	srv.AddRelatedMR(9, mr(5, "opened", "fix-open", 1, "Refs #9"))
	heads, ok := IssuePRHeads(context.Background(), gitlabEnv(t, srv), 9)
	if want := []string{"issue-9-x", "fix-y"}; !ok || !reflect.DeepEqual(heads, want) {
		t.Fatalf("IssuePRHeads = %q, %v; want %q, true", heads, ok, want)
	}
}

// A lookup that fails is never "no PRs": test_parse_null_issue_is_failure and
// test_parse_issue_pr_heads_shapes[no data|list] of parse.json, now that the forge reads the
// answer, plus a forge that does not open and a config without [forge] repo.
func TestIssuePRHeadsFailures(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T) Env
		err   string
	}{
		{"graphql errors", func(t *testing.T) Env {
			srv := forgetest.New(t)
			srv.FailOps = map[string]string{"IssueChanges": "Could not resolve to an Issue with the number of 9."}
			return githubEnv(t, srv)
		}, "Could not resolve to an Issue"},
		{"second page fails", func(t *testing.T) Env {
			srv := forgetest.New(t)
			var many []map[string]any
			for n := 1; n <= 101; n++ {
				many = append(many, map[string]any{"number": n, "state": "MERGED", "headRefName": "h" + itoa(n),
					"repository": map[string]any{"nameWithOwner": "o/r"}})
			}
			srv.ClosingPRs = map[int][]map[string]any{9: many}
			srv.IssueChangesPage2Fails = map[int]bool{9: true}
			return githubEnv(t, srv)
		}, "page two"},
		{"forge does not open", func(t *testing.T) Env {
			env := githubEnv(t, forgetest.New(t))
			env.Forge = func(context.Context) (forge.Forge, error) { return nil, errors.New("no GitHub token") }
			return env
		}, "no GitHub token"},
		{"no forge repo", func(t *testing.T) Env {
			env, _ := collectEnv(t, &gitFake{answer: mainOnly}, "[project]\nlanguage = \"en\"\n")
			env.Forge = func(context.Context) (forge.Forge, error) {
				t.Error("forge opened without [forge] repo")
				return nil, errors.New("unreachable")
			}
			return env
		}, "[forge] repo is not set"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.setup(t)
			heads, ok := IssuePRHeads(context.Background(), env, 9)
			if ok || heads != nil {
				t.Fatalf("IssuePRHeads = %q, %v; want a failed lookup", heads, ok)
			}
			stderr := env.Stderr.(interface{ String() string }).String()
			if !strings.HasPrefix(stderr, "worktree-audit: PR lookup for issue #9 failed — ") || !strings.Contains(stderr, tt.err) {
				t.Errorf("stderr %q, want the failure with %q", stderr, tt.err)
			}
		})
	}
}

// featX is a repo whose branch feat-x has no unpushed commits, a clean tree and no unique files:
// with its PR state known, the ladder can judge it; unknown, it says so.
func featX(args []string) *string {
	switch {
	case args[0] == "branch" && len(args) == 2:
		return ptr("main\nfeat-x\n")
	case args[0] == "rev-list" && args[1] == "--count":
		return ptr("0\n")
	}
	return mainOnly(args)
}

// lookupEnv is an Env over featX with toml as config and open as the forge.
func lookupEnv(t *testing.T, toml string, open func(context.Context) (forge.Forge, error)) (Env, *strings.Builder) {
	t.Helper()
	env, _ := collectEnv(t, &gitFake{answer: featX}, toml)
	var stderr strings.Builder
	env.Stderr = &stderr
	env.Forge = open
	return env, &stderr
}

// onlyFeatX is the cleanup verdict of feat-x after Collect.
func onlyFeatX(t *testing.T, env Env) string {
	t.Helper()
	audit, err := Collect(context.Background(), env, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := View{Policy: audit.Policy, Alive: env.Alive}.Only(audit.Rows, "feat-x", "/elders")
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "cleanup: ") {
			return line
		}
	}
	t.Fatalf("Only = %q, no cleanup line", text)
	return ""
}

func TestCollectReadsThePRStateFromTheForge(t *testing.T) {
	github := func(t *testing.T, pulls []map[string]any) func(context.Context) (forge.Forge, error) {
		srv := forgetest.New(t)
		srv.Pulls = pulls
		return func(context.Context) (forge.Forge, error) {
			return forge.Open(forge.Config{Repo: "o/r", Token: "t", BaseURL: srv.URL()})
		}
	}
	gitlab := func(t *testing.T, mrs []map[string]any) func(context.Context) (forge.Forge, error) {
		srv := gitlabtest.New(t)
		for _, m := range mrs {
			srv.AddMR(m)
		}
		return func(context.Context) (forge.Forge, error) {
			return forge.Open(forge.Config{Kind: "gitlab", Repo: "group/project", Token: "t", BaseURL: srv.URL()})
		}
	}
	const gh, gl = "[forge]\nrepo = \"o/r\"\n", "[forge]\nkind = \"gitlab\"\nrepo = \"group/project\"\n"
	for _, tt := range []struct {
		name, toml string
		open       func(t *testing.T) func(context.Context) (forge.Forge, error)
		want       string
	}{
		{"github merged", gh, func(t *testing.T) func(context.Context) (forge.Forge, error) {
			return github(t, []map[string]any{{"number": 7, "state": "closed", "merged_at": "2026-01-01T00:00:00Z", "head": map[string]any{"ref": "feat-x"}}})
		}, "cleanup: yes"},
		{"github open wins over merged", gh, func(t *testing.T) func(context.Context) (forge.Forge, error) {
			return github(t, []map[string]any{
				{"number": 7, "state": "closed", "merged_at": "2026-01-01T00:00:00Z", "head": map[string]any{"ref": "feat-x"}},
				{"number": 8, "state": "open", "head": map[string]any{"ref": "feat-x"}},
			})
		}, "cleanup: no — ⏸️ PR #8 open"},
		{"github no PR", gh, func(t *testing.T) func(context.Context) (forge.Forge, error) {
			return github(t, nil)
		}, "cleanup: yes"},
		{"gitlab merged", gl, func(t *testing.T) func(context.Context) (forge.Forge, error) {
			return gitlab(t, []map[string]any{{"iid": 7, "state": "merged", "merged_at": "2026-01-01T00:00:00Z", "source_branch": "feat-x"}})
		}, "cleanup: yes"},
		{"gitlab open wins over merged", gl, func(t *testing.T) func(context.Context) (forge.Forge, error) {
			return gitlab(t, []map[string]any{
				{"iid": 7, "state": "merged", "merged_at": "2026-01-01T00:00:00Z", "source_branch": "feat-x"},
				{"iid": 8, "state": "opened", "source_branch": "feat-x"},
			})
		}, "cleanup: no — ⏸️ PR #8 open"},
		{"gitlab closed is not merged", gl, func(t *testing.T) func(context.Context) (forge.Forge, error) {
			return gitlab(t, []map[string]any{{"iid": 7, "state": "closed", "source_branch": "feat-x"}})
		}, "cleanup: yes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env, stderr := lookupEnv(t, tt.toml, tt.open(t))
			if got := onlyFeatX(t, env); got != tt.want {
				t.Errorf("verdict %q, want %q", got, tt.want)
			}
			if stderr.String() != "" {
				t.Errorf("stderr %q, want none", stderr.String())
			}
		})
	}
}

// Review focus: a forge that does not open, or a read that fails, is "PR status unknown", never
// "no PR" (which would make feat-x safe to remove).
func TestCollectWithoutAPRLookupSaysUnknown(t *testing.T) {
	const unknown = "cleanup: no — ⚠️ PR status unknown — check"
	for _, tt := range []struct {
		name, toml string
		open       func(t *testing.T) func(context.Context) (forge.Forge, error)
		stderr     string
	}{
		{"forge does not open", "[forge]\nrepo = \"o/r\"\n", func(*testing.T) func(context.Context) (forge.Forge, error) {
			return func(context.Context) (forge.Forge, error) {
				return nil, errors.New("no GitHub token: set GH_TOKEN or GITHUB_TOKEN")
			}
		}, "worktree-audit: PR lookup failed — no GitHub token: set GH_TOKEN or GITHUB_TOKEN\n"},
		{"github ListChanges fails", "[forge]\nrepo = \"o/r\"\n", func(t *testing.T) func(context.Context) (forge.Forge, error) {
			srv := forgetest.New(t)
			srv.Fail = map[string]int{"GET /repos/o/r/pulls": 502}
			return func(context.Context) (forge.Forge, error) {
				return forge.Open(forge.Config{Repo: "o/r", Token: "t", BaseURL: srv.URL()})
			}
		}, "worktree-audit: PR lookup failed — "},
		{"gitlab ListChanges fails", "[forge]\nkind = \"gitlab\"\nrepo = \"group/project\"\n", func(t *testing.T) func(context.Context) (forge.Forge, error) {
			gone := httptest.NewServer(http.NotFoundHandler())
			gone.Close() // every request is refused
			return func(context.Context) (forge.Forge, error) {
				return forge.Open(forge.Config{Kind: "gitlab", Repo: "group/project", Token: "t", BaseURL: gone.URL})
			}
		}, "worktree-audit: PR lookup failed — "},
		{"a change in an unknown state", "[forge]\nrepo = \"o/r\"\n", func(t *testing.T) func(context.Context) (forge.Forge, error) {
			srv := forgetest.New(t)
			srv.Pulls = []map[string]any{
				{"number": 1, "state": "closed", "merged_at": "2026-01-01T00:00:00Z", "head": map[string]any{"ref": "feat-x"}},
				{"number": 2, "state": "draft", "head": map[string]any{"ref": "feat-x"}},
			}
			return func(context.Context) (forge.Forge, error) {
				return forge.Open(forge.Config{Repo: "o/r", Token: "t", BaseURL: srv.URL()})
			}
		}, "worktree-audit: PR lookup failed — change #2 has an unknown state \"draft\"\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env, stderr := lookupEnv(t, tt.toml, tt.open(t))
			if got := onlyFeatX(t, env); got != unknown {
				t.Errorf("verdict %q, want %q", got, unknown)
			}
			if !strings.HasPrefix(stderr.String(), tt.stderr) {
				t.Errorf("stderr %q, want it to start with %q", stderr.String(), tt.stderr)
			}
		})
	}
}
