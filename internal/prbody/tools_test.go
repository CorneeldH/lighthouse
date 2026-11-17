//go:build !windows

package prbody

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The three create commands the gate recognises, the GitLab closing words and targets, and the
// AC count through the forge interface (#260).

// created is the part of a PrCreate these tests check; nil pointers are "".
type created struct {
	Tool, Cwd, Repo, Body, BodyFile, Problem, RepoProblem string
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func findAll(t *testing.T, command, cwd string) []created {
	t.Helper()
	found, err := FindPrCreates(command, cwd)
	if err != nil {
		t.Fatalf("FindPrCreates(%q): %v", command, err)
	}
	out := []created{}
	for _, pc := range found {
		out = append(out, created{pc.Tool, deref(pc.Cwd), deref(pc.Repo), deref(pc.Body), deref(pc.BodyFile), pc.Problem, pc.RepoProblem})
	}
	return out
}

func TestFindPrCreatesLighthouseForge(t *testing.T) {
	cases := []struct {
		name, command string
		want          []created
	}{
		{"plain", "lighthouse forge pr create --title t --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"equals form", "lighthouse forge pr create --title=t --body-file=p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"after &&", "git push -u origin HEAD && lighthouse forge pr create --title t --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"after cd", "cd /x && lighthouse forge pr create --title t --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/x", BodyFile: "/x/p"}}},
		{"after heredoc", "cat > p <<'EOF'\nCloses #1\nEOF\nlighthouse forge pr create --title t --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"installed path", "/usr/local/bin/lighthouse forge pr create --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"go run", "go run ./cmd/lighthouse forge pr create --title t --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		// lighthouse forge has no --body and no -R: neither is a body or a repo.
		{"no --body", "lighthouse forge pr create --title t --body 'Closes #1'",
			[]created{{Tool: "lighthouse", Cwd: "/w", Problem: "no body"}}},
		{"no -R", "lighthouse forge pr create -R x/y --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"stdin", "lighthouse forge pr create --body-file -",
			[]created{{Tool: "lighthouse", Cwd: "/w", Problem: "a body via stdin cannot be checked"}}},
		{"GH_REPO assignment is not its repo", "GH_REPO=x/y lighthouse forge pr create --body-file p",
			[]created{{Tool: "lighthouse", Cwd: "/w", BodyFile: "/w/p"}}},
		{"issue create is no PR", "lighthouse forge issue create --title t --body-file p", []created{}},
		{"pr view is no create", "lighthouse forge pr view 3", []created{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eq(t, c.command, findAll(t, c.command, "/w"), c.want)
		})
	}
}

func TestFindPrCreatesGlabMR(t *testing.T) {
	cases := []struct {
		name, command string
		want          []created
	}{
		{"short flags", `glab mr create -d "Closes #1" -R g/p`,
			[]created{{Tool: "glab", Cwd: "/w", Repo: "g/p", Body: "Closes #1"}}},
		{"long flags", `glab mr create --description='No issue: alleen docs' --repo=g/sub/p`,
			[]created{{Tool: "glab", Cwd: "/w", Repo: "g/sub/p", Body: "No issue: alleen docs"}}},
		{"mr new", `glab mr new -d x`, []created{{Tool: "glab", Cwd: "/w", Body: "x"}}},
		// -b is the target branch of glab, not a body.
		{"-b is no body", `glab mr create -b main -t t`, []created{{Tool: "glab", Cwd: "/w", Problem: "no body"}}},
		{"--fill", `glab mr create --fill -d x`, []created{{Tool: "glab", Cwd: "/w", Problem: "--fill gives no body to check"}}},
		{"-f", `glab mr create -f`, []created{{Tool: "glab", Cwd: "/w", Problem: "-f gives no body to check"}}},
		{"--web", `glab mr create --web`, []created{{Tool: "glab", Cwd: "/w", Problem: "--web gives no body to check"}}},
		{"non-literal description", `glab mr create -d "$B"`,
			[]created{{Tool: "glab", Cwd: "/w", Problem: "non-literal body — use --body-file"}}},
		{"non-literal repo", `glab mr create -d x -R "$R"`,
			[]created{{Tool: "glab", Cwd: "/w", Body: "x", RepoProblem: "non-literal --repo — spell the repo out"}}},
		// glab does not read GH_REPO.
		{"GH_REPO is not its repo", `GH_REPO=x/y glab mr create -d x`, []created{{Tool: "glab", Cwd: "/w", Body: "x"}}},
		{"gh still found", `gh pr create -R a/b -b x`, []created{{Tool: "gh", Cwd: "/w", Repo: "a/b", Body: "x"}}},
		{"mr list is no create", `glab mr list`, []created{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eq(t, c.command, findAll(t, c.command, "/w"), c.want)
		})
	}
}

var gitlabRepo = Repo{Kind: "gitlab", Slug: "g/p", Host: "gitlab.example"}

func TestGlabRepoFlagScope(t *testing.T) {
	cases := []struct {
		value, decision, reason string
	}{
		{"g/p", "check", ""},
		{"G/P", "check", ""},
		{"https://gitlab.example/g/p.git", "check", ""},
		{"git@gitlab.example:g/p.git", "check", ""},
		{"other/p", "skip", "skipped — other repo other/p"},
		{"g/p/sub", "skip", "skipped — other repo g/p/sub"},
		{"p", "block", "--repo 'p' does not have the form OWNER/REPO, GROUP/NAMESPACE/REPO or a URL"},
	}
	for _, c := range cases {
		value := c.value
		pc := PrCreate{Tool: "glab", Repo: &value}
		d, reason := InScope(context.Background(), pc, t.TempDir(), gitlabRepo)
		eq(t, c.value, [2]string{d, reason}, [2]string{c.decision, c.reason})
	}
	found, err := FindPrCreates(`glab mr create -R "$R" -d x`, "/w")
	if err != nil || len(found) != 1 {
		t.Fatalf("FindPrCreates: %v, %d found", err, len(found))
	}
	d, reason := InScope(context.Background(), found[0], t.TempDir(), gitlabRepo)
	eq(t, "non-literal", [2]string{d, reason}, [2]string{"block", "non-literal --repo — spell the repo out"})
}

func TestGlabSubgroupRepoIsOwnProject(t *testing.T) {
	own := Repo{Kind: "gitlab", Slug: "g/sub/p"}
	for value, want := range map[string]string{
		"g/sub/p":                        "check",
		"G/Sub/P":                        "check",
		"https://gitlab.com/g/sub/p":     "check",
		"https://gitlab.com/g/sub/p.git": "check",
		"git@gitlab.com:g/sub/p.git":     "check",
		"sub/p":                          "skip",
		"g/sub":                          "skip",
	} {
		pc := PrCreate{Tool: "glab", Repo: &value}
		d, _ := InScope(context.Background(), pc, t.TempDir(), own)
		eq(t, value, d, want)
	}
	for value, want := range map[string]*string{
		"g/sub/p":                    ptr("g/sub/p"),
		"https://gitlab.com/g/sub/p": ptr("g/sub/p"),
		"git@gitlab.com:G/Sub/P.git": ptr("g/sub/p"),
		"p":                          nil,
	} {
		got, ok := NormalizeRepo(value, "gitlab")
		eq(t, "gitlab "+value, orNilIf(got, ok), want)
	}
	// On GitHub three segments are HOST/OWNER/REPO, as gh reads them.
	got, ok := NormalizeRepo("g/sub/p", "github")
	eq(t, "github", orNilIf(got, ok), ptr("sub/p"))
}

func ptr(s string) *string { return &s }

func TestForgePrCreateIgnoresGHRepo(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	writeFile(t, filepath.Join(project, "body.md"), "zie titel\n")
	noForm := blocked("no valid form in the PR body: `Closes #N` with `### Evidence #N`," +
		" `Refs #N — <reason>` or `No issue: <reason>`.")
	t.Setenv("GH_REPO", "other/x")
	got := runMain(t, project, "lighthouse forge pr create --title t --body-file body.md", project)
	eq(t, "process GH_REPO", got, mainResult{2, "", noForm})
	got = runMain(t, project, "GH_REPO=other/x lighthouse forge pr create --title t --body-file body.md", project)
	eq(t, "assigned GH_REPO", got, mainResult{2, "", noForm})
	// glab reads no GH_REPO either: without -R it is the fork rule of glab, not "other repo".
	got = runMain(t, project, "glab mr create -d 'zie titel'", project)
	eq(t, "glab", got, mainResult{2, "", blocked("`glab mr create` without --repo: in a clone of a fork glab then" +
		" opens the MR in the upstream repo. Add `--repo Acme/widgets`.")})
	// gh does read it.
	got = runMain(t, project, "gh pr create --body 'zie titel'", project)
	eq(t, "gh", got, mainResult{0, "pr-create-gate: skipped — other repo other/x\n", ""})
}

func TestForgePrCreateValidBodyPasses(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	writeFile(t, filepath.Join(project, "body.md"), "No issue: alleen docs gewijzigd\n")
	got := runMain(t, project, "lighthouse forge pr create --title t --body-file body.md", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — No issue\n", ""})
}

func TestForgePrCreateUnknownCwdBlocks(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), PrCreate{Tool: "lighthouse"}, project, Repo{Slug: "Acme/widgets"})
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "working directory of `lighthouse forge pr create` cannot be resolved"})
}

func TestClosesTargetsGitLabWords(t *testing.T) {
	github := Repo{Kind: "github", Slug: "g/p"}
	cases := []struct {
		body          string
		gitlab, onHub []int
	}{
		{"Implements #7", []int{7}, nil},
		{"implement #7", []int{7}, nil},
		{"Implemented #7", []int{7}, nil},
		{"implementing #7", []int{7}, nil},
		{"Closing #7", []int{7}, nil},
		{"fixing #7", []int{7}, nil},
		{"Resolving #7", []int{7}, nil},
		{"Closes #7", []int{7}, []int{7}},
		{"fixes: #7", []int{7}, []int{7}},
		{"Closes issue #7", []int{7}, nil},
		{"Closes issues #7 and #8", []int{7, 8}, nil},
		// GitLab closes every issue of a list after one closing word.
		{"Closes #5, #6 and #7", []int{5, 6, 7}, []int{5}},
		{"Closes #5 #6", []int{5, 6}, []int{5}},
		{"Implements #5, g/p#6", []int{5, 6}, nil},
		{"Closes #5, other/p#6", []int{5}, []int{5}},
		{"Closes #5 for #6", []int{5}, []int{5}},
		{"Reimplements #7", nil, nil},
		{"<!-- Implements #7 -->", nil, nil},
	}
	for _, c := range cases {
		eq(t, "gitlab "+c.body, orEmpty(ClosesTargets(c.body, gitlabRepo)), orEmpty(c.gitlab))
		eq(t, "github "+c.body, orEmpty(ClosesTargets(c.body, github)), orEmpty(c.onHub))
	}
}

func TestClosesTargetsSubgroup(t *testing.T) {
	own := Repo{Kind: "gitlab", Slug: "g/sub/p", Host: "gitlab.example"}
	for body, want := range map[string][]int{
		"Closes g/sub/p#7": {7},
		"Closes G/Sub/P#7": {7},
		"Closes https://gitlab.example/g/sub/p/-/issues/7": {7},
		"Closes https://gitlab.com/g/sub/p/-/issues/7":     nil,
		"Closes https://gitlab.example/g/sub/q/-/issues/7": nil,
		"Closes other/sub/p#7":                             nil,
		"Closes sub/p#7":                                   nil,
		"Closes g/sub/p/x#7":                               nil,
	} {
		eq(t, body, orEmpty(ClosesTargets(body, own)), orEmpty(want))
	}
	// Without forge.host the issue URL is on gitlab.com.
	onCom := Repo{Kind: "gitlab", Slug: "g/sub/p"}
	eq(t, "gitlab.com", ClosesTargets("Closes https://gitlab.com/g/sub/p/-/issues/7", onCom), []int{7})
	// A GitHub repo regex takes no subgroup path: `sub/p` is not the tail of `g/sub/p`.
	eq(t, "github", len(ClosesTargets("Closes g/sub/p#7", Repo{Kind: "github", Slug: "sub/p"})), 0)
}

// issueFake is an Env.Issue that answers only issue nr of slug, with body or err.
func issueFake(t *testing.T, slug string, nr int, body string, err error) func(context.Context, Repo, int) (string, error) {
	t.Helper()
	return func(_ context.Context, repo Repo, n int) (string, error) {
		if repo.Slug != slug || n != nr {
			t.Errorf("Issue(%q, %d), want (%q, %d)", repo.Slug, n, slug, nr)
			return "", errors.New("unexpected issue")
		}
		return body, err
	}
}

const threeACs = "### Acceptatiecriteria\n- [ ] a\n- [x] b\n- [ ] c\n### Definition of Done\n- [ ] d\n"

func fetchWith(t *testing.T, ctx context.Context, env Env, repo Repo, nr int) fetched {
	t.Helper()
	count, ok, cause, err := fetchACCount(ctx, env, repo, nr)
	if err != nil {
		t.Fatalf("fetchACCount: %v", err)
	}
	return fetched{count, ok, cause}
}

func TestFetchACCountViaForge(t *testing.T) {
	withConfig(t, "Acme/widgets")
	acme := Repo{Kind: "github", Slug: "Acme/widgets"}
	env := Env{Issue: issueFake(t, "Acme/widgets", 5, threeACs, nil)}
	eq(t, "fake", fetchWith(t, context.Background(), env, acme, 5), fetched{3, true, ""})

	// The production Issue: the GitHub REST API, with the token of the environment.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Acme/widgets/issues/5" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"message":"wrong request"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"number":5,"title":"t","state":"open","body":"### Acceptatiecriteria\n- [ ] a\n- [ ] b\n"}`))
	}))
	defer srv.Close()
	t.Setenv("LIGHTHOUSE_GITHUB_API_URL", srv.URL)
	t.Setenv("GH_TOKEN", "tok")
	eq(t, "api", fetchWith(t, context.Background(), DefaultEnv(), acme, 5), fetched{2, true, ""})

	// token_env names the one variable to read.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("MY_TOKEN", "tok")
	eq(t, "token_env", fetchWith(t, context.Background(), DefaultEnv(), Repo{Slug: "Acme/widgets", TokenEnv: "MY_TOKEN"}, 5),
		fetched{2, true, ""})

	// No token in the environment: the token of `gh auth token` (the Env.GH seam).
	t.Setenv("MY_TOKEN", "")
	gh := func(_ context.Context, argv ...string) (string, int, error) {
		if len(argv) != 2 || argv[0] != "auth" || argv[1] != "token" {
			t.Errorf("gh %v, want gh auth token", argv)
			return "", 1, nil
		}
		return "tok\n", 0, nil
	}
	eq(t, "gh auth token", fetchWith(t, context.Background(), Env{GH: gh}, acme, 5), fetched{2, true, ""})
}

func TestFetchACCountTokenMissingPassesLoudly(t *testing.T) {
	withConfig(t, "Acme/widgets")
	ghFails := func(context.Context, ...string) (string, int, error) { return "", 1, nil }
	ghMissing := func(context.Context, ...string) (string, int, error) { return "", 0, os.ErrNotExist }
	acme := Repo{Kind: "github", Slug: "Acme/widgets"}
	want := fetched{0, false, "no GitHub token: set GH_TOKEN or GITHUB_TOKEN"}
	eq(t, "gh fails", fetchWith(t, context.Background(), Env{GH: ghFails}, acme, 5), want)
	eq(t, "gh missing", fetchWith(t, context.Background(), Env{GH: ghMissing}, acme, 5), want)
	eq(t, "token_env", fetchWith(t, context.Background(), Env{GH: ghFails}, Repo{Slug: "Acme/widgets", TokenEnv: "MY_TOKEN"}, 5),
		fetched{0, false, "no GitHub token: set MY_TOKEN"})
	// GitLab has no gh fallback.
	ghUsed := func(context.Context, ...string) (string, int, error) {
		t.Error("gh called for a GitLab repo")
		return "tok", 0, nil
	}
	eq(t, "gitlab", fetchWith(t, context.Background(), Env{GH: ghUsed}, Repo{Kind: "gitlab", Slug: "g/p"}, 5),
		fetched{0, false, "no GitLab token: set GITLAB_TOKEN"})

	_, project := newProject(t)
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, Env{GH: ghFails})
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (AC not counted, 1 rows)\n" +
		"pr-create-gate: AC count not measurable for #9 (no GitHub token: set GH_TOKEN or GITHUB_TOKEN) — structure checked only\n", ""})
}

func TestFetchACCountForgeErrorPassesLoudly(t *testing.T) {
	withConfig(t, "Acme/widgets")
	acme := Repo{Kind: "github", Slug: "Acme/widgets"}
	env := Env{Issue: issueFake(t, "Acme/widgets", 5, "", errors.New("forge GET x: status 500\nmore"))}
	eq(t, "error", fetchWith(t, context.Background(), env, acme, 5),
		fetched{0, false, "forge error: forge GET x: status 500; more"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("LIGHTHOUSE_GITHUB_API_URL", srv.URL)
	t.Setenv("GH_TOKEN", "tok")
	eq(t, "500", fetchWith(t, context.Background(), DefaultEnv(), acme, 5),
		fetched{0, false, "forge error: forge GET " + srv.URL + "/repos/Acme/widgets/issues/5: status 500: Server Error"})

	setTimeouts(t, 300*time.Millisecond, gateDeadline)
	hang := Env{Issue: func(ctx context.Context, _ Repo, _ int) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	eq(t, "timeout", fetchWith(t, context.Background(), hang, acme, 5), fetched{0, false, "timeout after 0.3 s"})

	_, project := newProject(t)
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project,
		Env{Issue: issueFake(t, "Acme/widgets", 9, "", errors.New("status 502"))})
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (AC not counted, 1 rows)\n" +
		"pr-create-gate: AC count not measurable for #9 (forge error: status 502) — structure checked only\n", ""})
}

func TestFetchACCountHeadingsTableErrorBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(t.TempDir(), "gone.toml"))
	acme := Repo{Kind: "github", Slug: "Acme/widgets"}
	env := Env{Issue: issueFake(t, "Acme/widgets", 9, threeACs, nil)}
	if _, _, _, err := fetchACCount(context.Background(), env, acme, 9); err == nil {
		t.Fatal("fetchACCount: want the error of the headings table")
	}
	_, project := newProject(t)
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, env)
	if got.rc != 1 || got.stdout != "" || got.stderr == "" {
		t.Errorf("main = %#v, want rc 1 with the error on stderr", got)
	}
}

func TestFetchACCountNoHeadingPasses(t *testing.T) {
	withConfig(t, "Acme/widgets")
	acme := Repo{Kind: "github", Slug: "Acme/widgets"}
	env := Env{Issue: issueFake(t, "Acme/widgets", 9, "geen kop hier\n", nil)}
	eq(t, "fetch", fetchWith(t, context.Background(), env, acme, 9), fetched{0, false, "no acceptance criteria heading"})
	_, project := newProject(t)
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, env)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (AC not counted, 1 rows)\n" +
		"pr-create-gate: AC count not measurable for #9 (no acceptance criteria heading) — structure checked only\n", ""})
}

// The ClosesTargets of a GitLab repo reach Main: `Implements #9` there needs its Evidence block.
func TestMainGitLabClosingWordNeedsEvidence(t *testing.T) {
	root := t.TempDir()
	writeFixtureConfig(t, root, "")
	writeFile(t, filepath.Join(root, "lighthouse.toml"),
		"[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"gitlab\"\nrepo = \"g/sub/p\"\nhost = \"gitlab.example\"\n")
	_, project := newProject(t)
	got := runMain(t, project, "glab mr create -R g/sub/p -d 'Implements #9'", project)
	eq(t, "main", got, mainResult{2, "", blocked("Closes #9: heading `### Evidence #9` is missing")})
}
