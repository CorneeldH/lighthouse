//go:build !windows

package prbody

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

// The twins of tests/pack/test_pr_body_gate.py that are no data: `gh`, git repos and time.

const origin = "https://github.com/Acme/widgets.git"

// acmeRepo is forge.repo Acme/widgets on GitHub.
var acmeRepo = Repo{Kind: "github", Slug: "Acme/widgets"}

// closesBody is a body with `Closes #nr` and an Evidence table of n rows.
func closesBody(nr string, n int) string {
	rows := []string{"| 1. een | `grep -F a f` | a |", "| 2. twee | `ls` | f |", "| 3. drie | `true` | ok |"}
	return "## Related issues\n\nCloses #" + nr + "\n\n### Evidence #" + nr + "\n\n" +
		"| AC | Command | Result |\n|---|---|---|\n" + strings.Join(rows[:n], "\n") + "\n"
}

// withConfig points the config at a lighthouse.toml with forge.repo (none for "") outside the
// repos of the test.
func withConfig(t *testing.T, repo string) {
	t.Helper()
	writeFixtureConfig(t, t.TempDir(), repo)
}

// newProject is a real repo with one commit, at <base>/project.
func newProject(t *testing.T) (base, project string) {
	t.Helper()
	base = t.TempDir()
	fixturesetup.Build(t, base, []map[string]any{{"repo": "project", "branch": "main"}})
	return base, filepath.Join(base, "project")
}

// stubBin writes POSIX scripts (with or without their #!/bin/sh line) into a fresh directory and
// returns it. reason names the POSIX behaviour the scripts imitate.
func stubBin(t *testing.T, reason string, scripts map[string]string) string {
	t.Helper()
	stubbin.RequirePOSIX(t, reason)
	dir := filepath.Join(t.TempDir(), "bin")
	for name, text := range scripts {
		stubbin.MakeScript(t, dir, name, strings.TrimPrefix(text, "#!/bin/sh\n"))
	}
	return dir
}

// prependPath puts dir first on PATH.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	stubbin.Prepend(t, dir)
}

// setTimeouts overrides the gate's timeouts for one test.
func setTimeouts(t *testing.T, ac, total time.Duration) {
	t.Helper()
	oldAC, oldTotal := acTimeout, gateDeadline
	acTimeout, gateDeadline = ac, total
	t.Cleanup(func() { acTimeout, gateDeadline = oldAC, oldTotal })
}

type fetched struct {
	count int
	ok    bool
	cause string
}

func fetch(t *testing.T, ctx context.Context, repo string, nr int) fetched {
	t.Helper()
	count, ok, cause, err := fetchACCount(ctx, DefaultEnv(), Repo{Kind: "github", Slug: repo}, nr)
	if err != nil {
		t.Fatalf("fetchACCount: %v", err)
	}
	return fetched{count, ok, cause}
}

// --- fetch_ac_count -----------------------------------------------------------

// twin of test_fetch_ac_count_deadline_expired
func TestFetchACCountDeadlineExpired(t *testing.T) {
	withConfig(t, "Acme/widgets")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	eq(t, "fetch", fetch(t, ctx, "Acme/widgets", 5), fetched{0, false, "deadline reached"})
}

func TestFetchACCountWithoutForgeRepo(t *testing.T) {
	withConfig(t, "")
	eq(t, "fetch", fetch(t, context.Background(), "", 4), fetched{0, false, "no forge.repo in lighthouse.toml"})
}

// From an unknown cwd a relative `cd` stays unknown, an absolute one is known (as Python's None).
func TestFindFromUnknownCwd(t *testing.T) {
	found, err := FindPrCreates("cd sub && gh pr create --repo Acme/widgets -F b.md; cd /tmp && gh pr create -F b.md", "")
	if err != nil || len(found) != 2 {
		t.Fatalf("FindPrCreates: %v, %d found", err, len(found))
	}
	eq(t, "relative cd", [2]any{found[0].Cwd, found[0].Problem}, [2]any{(*string)(nil), "path of the body file cannot be resolved"})
	eq(t, "absolute cd", [2]string{*found[1].Cwd, *found[1].BodyFile}, [2]string{"/tmp", "/tmp/b.md"})
}

// --- in_scope -------------------------------------------------------------------

func pcAt(cwd string, repo ...string) PrCreate {
	body := "x"
	pc := PrCreate{Tool: "gh", Body: &body}
	if cwd != "" {
		pc.Cwd = &cwd
	}
	if len(repo) > 0 {
		pc.Repo = &repo[0]
	}
	return pc
}

func decision(t *testing.T, pc PrCreate, project, repo string) string {
	t.Helper()
	d, _ := InScope(context.Background(), pc, project, Repo{Kind: "github", Slug: repo})
	return d
}

// twin of test_scope_own_repo
func TestScopeOwnRepo(t *testing.T) {
	base, project := newProject(t)
	wt := filepath.Join(base, "wt")
	fixturesetup.Git(t, project, "worktree", "add", "-q", wt, "-b", "feat")
	// Without --repo a `gh pr create` in this repo is blocked (#224).
	eq(t, "project", decision(t, pcAt(project), project, "Acme/widgets"), "block")
	eq(t, "worktree", decision(t, pcAt(wt), project, "Acme/widgets"), "block")
	// A subdirectory that does not exist yet counts as the repo of its ancestor.
	eq(t, "new dir", decision(t, pcAt(filepath.Join(wt, "nieuw")), project, "Acme/widgets"), "block")
}

// twin of test_scope_other_repo_flag
func TestScopeOtherRepoFlag(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(project, "x/y"), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip", "skipped — other repo x/y"})
}

// twin of test_scope_outside_repo
func TestScopeOutsideRepo(t *testing.T) {
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	d, reason := InScope(context.Background(), pcAt(outside), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip", "skipped — outside this repo"})
}

// twin of test_scope_non_literal_repo_blocks_also_outside_the_repo
func TestScopeNonLiteralRepoBlocksAlsoOutsideTheRepo(t *testing.T) {
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := FindPrCreates(`gh pr create -R "$R" -F b.md`, outside)
	if err != nil || len(found) != 1 {
		t.Fatalf("FindPrCreates: %v, %d found", err, len(found))
	}
	d, reason := InScope(context.Background(), found[0], project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "non-literal --repo — spell the repo out"})
}

// twin of test_scope_outside_repo_with_R
func TestScopeOutsideRepoWithR(t *testing.T) {
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	eq(t, "decision", decision(t, pcAt(outside, "Acme/widgets"), project, "Acme/widgets"), "check")
}

// twin of test_scope_other_git_repo
func TestScopeOtherGitRepo(t *testing.T) {
	base, project := newProject(t)
	fixturesetup.Build(t, base, []map[string]any{{"repo": "ander", "branch": "main"}})
	eq(t, "decision", decision(t, pcAt(filepath.Join(base, "ander")), project, "Acme/widgets"), "skip")
}

// forgeWorktree is forge_worktree of tests/pack/python/_testkit.py: a worktree of a separate bare clone of project,
// with its own git-common-dir; the clone's origin is originURL.
func forgeWorktree(t *testing.T, project, base, originURL string) string {
	t.Helper()
	clone := filepath.Join(base, "clone.git")
	fixturesetup.Git(t, base, "clone", "-q", "--bare", project, clone)
	fixturesetup.Git(t, clone, "remote", "set-url", "origin", originURL)
	work := filepath.Join(base, "work")
	fixturesetup.Git(t, clone, "worktree", "add", "-q", work, "-b", "work")
	return work
}

// twin of test_scope_forge_worktree_of_the_same_repo
func TestScopeForgeWorktreeOfTheSameRepo(t *testing.T) {
	base, project := newProject(t)
	fixturesetup.Git(t, project, "remote", "add", "origin", origin)
	work := forgeWorktree(t, project, base, origin)
	d, reason := InScope(context.Background(), pcAt(work), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"block",
		"`gh pr create` without --repo: in a clone of a fork gh then opens the PR in the upstream repo. Add `--repo Acme/widgets`."})
}

// twin of test_scope_other_origin_names_common_dirs_and_remotes
func TestScopeOtherOriginNamesCommonDirsAndRemotes(t *testing.T) {
	base, project := newProject(t)
	fixturesetup.Git(t, project, "remote", "add", "origin", origin)
	work := forgeWorktree(t, project, base, "https://github.com/Acme/gadgets.git")
	common, err := filepath.EvalSymlinks(filepath.Join(base, "clone.git"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := filepath.EvalSymlinks(filepath.Join(project, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	d, reason := InScope(context.Background(), pcAt(work), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip",
		"skipped — outside this repo (git-common-dir " + common + " vs " + ref +
			"; origin https://github.com/Acme/gadgets.git vs " + origin + ")"})
}

// twin of test_scope_unknown_cwd_blocks
func TestScopeUnknownCwdBlocks(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(""), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "working directory of `gh pr create` cannot be resolved"})
}

// twin of test_scope_other_git_error_blocks
func TestScopeOtherGitErrorBlocks(t *testing.T) {
	_, project := newProject(t)
	// A git that fails without "not a git repository" is no proof of "outside".
	// The PATH holds only the stub, as before: no other tool is reachable.
	stubbin.Alone(t, filepath.Dir(stubbin.Make(t, filepath.Join(t.TempDir(), "bin"), "git", stubbin.Spec{Stderr: "kapot\n", Exit: 128})))
	d, reason := InScope(context.Background(), pcAt(project), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "repo of the project cannot be determined (git failed (rc 128): kapot)"})
}

// twin of test_not_a_git_repository_under_other_locale
func TestNotAGitRepositoryUnderOtherLocale(t *testing.T) {
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command("git", "-C", outside, "rev-parse", "--git-common-dir")
	probe.Env = append(os.Environ(), "LANG=de_DE.UTF-8", "LC_ALL=", "LANGUAGE=")
	var stderr bytes.Buffer
	probe.Stderr = &stderr
	if probe.Run() == nil || strings.Contains(stderr.String(), "not a git repository") {
		t.Skip("git speaks no German here (no de_DE locale or translation)")
	}
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LANGUAGE", "")
	d, reason := InScope(context.Background(), pcAt(outside), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip", "skipped — outside this repo"})
}

// twin of test_without_repo_passes_with_repo_flag
func TestWithoutRepoPassesWithRepoFlag(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(project, "Acme/widgets"), project, acmeRepo)
	eq(t, "scope", [2]string{d, reason}, [2]string{"check", ""})
}

// twin of test_without_repo_not_blocked_without_forge_repo
func TestWithoutRepoNotBlockedWithoutForgeRepo(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(project), project, Repo{Kind: "github"})
	eq(t, "scope", [2]string{d, reason}, [2]string{"check", ""})
}

// --- Main -------------------------------------------------------------------------

type mainResult struct {
	rc             int
	stdout, stderr string
}

func runMain(t *testing.T, project, command, cwd string) mainResult {
	t.Helper()
	return runMainEnv(t, project, command, cwd, DefaultEnv())
}

// runMainEnv is Main with env, from the project directory project.
func runMainEnv(t *testing.T, project, command, cwd string, env Env) mainResult {
	t.Helper()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	var stdout, stderr bytes.Buffer
	rc := Main(context.Background(), cwd, strings.NewReader(command), &stdout, &stderr, env)
	return mainResult{rc, stdout.String(), stderr.String()}
}

// blocked is BLOCKED_MESSAGE with reason.
func blocked(reason string) string {
	return "pr-create-gate: BLOCKED — " + reason + "\n\n" +
		"Write the body with /lighthouse-write-pr <nr> and pass it with --body-file <path>.\n" +
		"Forms: `Closes #N` + `### Evidence #N` (one row per AC), `Refs #N — <reason>`, `No issue: <reason>`.\n"
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// twin of test_main_body_file_does_not_exist
func TestMainBodyFileDoesNotExist(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F nope.md", project)
	eq(t, "main", got, mainResult{2, "", blocked("body file " + project + "/nope.md does not exist")})
}

func TestMainBodyFileIsADirectory(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F .", project)
	eq(t, "main", got, mainResult{2, "", blocked("body file " + project + " cannot be read (Is a directory)")})
}

// twin of test_main_no_gh_pr_create
func TestMainNoGHPrCreate(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	eq(t, "main", runMain(t, project, "git status", project), mainResult{0, "", ""})
}

// twin of test_main_open_quote
func TestMainOpenQuote(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	eq(t, "main", runMain(t, project, "gh pr create --repo Acme/widgets --body 'x", project), mainResult{3, "", ""})
}

// twin of test_main_problem_blocks
func TestMainProblemBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --repo Acme/widgets --fill", project)
	eq(t, "main", got, mainResult{2, "", blocked("--fill gives no body to check")})
}

// twin of test_main_no_issue_passes
func TestMainNoIssuePasses(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --repo Acme/widgets --body 'No issue: alleen docs gewijzigd'", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — No issue\n", ""})
}

// twin of test_main_non_literal_repo_outside_repo_blocks
func TestMainNonLiteralRepoOutsideRepoBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	got := runMain(t, project, `GH_REPO="$R" gh pr create --body 'No issue: alleen docs gewijzigd'`, outside)
	eq(t, "main", got, mainResult{2, "", blocked("non-literal GH_REPO — spell the repo out")})
}

// twin of test_main_other_repo_skipped
func TestMainOtherRepoSkipped(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create -R x/y --fill", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: skipped — other repo x/y\n", ""})
}

// twin of test_main_closes_passes_with_count
func TestMainClosesPassesWithCount(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	env := Env{Issue: issueFake(t, "Acme/widgets", 9, "### Acceptatiecriteria\n- [ ] a\n- [ ] b\n", nil)}
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 2))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, env)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (2 AC, 2 rows)\n", ""})
}

// twin of test_main_closes_ac_mismatch_blocks
func TestMainClosesACMismatchBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	env := Env{Issue: issueFake(t, "Acme/widgets", 9, "### Acceptatiecriteria\n- [ ] a\n- [ ] b\n- [ ] c\n", nil)}
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 2))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, env)
	eq(t, "main", got, mainResult{2, "", blocked("Closes #9: the issue has 3 acceptance criteria, the Evidence block 2 rows — one row per AC")})
}

// twin of test_main_several_creates_one_blocker_blocks
func TestMainSeveralCreatesOneBlockerBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	cmd := "gh pr create --repo Acme/widgets --body 'No issue: alleen docs gewijzigd'; gh pr create --repo Acme/widgets -t x"
	eq(t, "main", runMain(t, project, cmd, project), mainResult{2, "", blocked("no body")})
}

func TestMainSeveralBlockersJoined(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	cmd := "gh pr create --repo Acme/widgets --fill; gh pr create --repo Acme/widgets -t x"
	eq(t, "main", runMain(t, project, cmd, project), mainResult{2, "", blocked("--fill gives no body to check; no body")})
}

func threeCloses(withEvidence ...string) string {
	body := "Closes #5\nCloses #6\nCloses #7\n\n"
	for _, nr := range withEvidence {
		body += "### Evidence #" + nr + "\n\n| AC | Command | Result |\n|---|---|---|\n| 1. een | `grep -F a f` | a |\n\n"
	}
	return body
}

// slowIssue is an Env.Issue that counts each start and then hangs until its context ends.
func slowIssue(starts *atomic.Int32) Env {
	return Env{Issue: func(ctx context.Context, _ Repo, _ int) (string, error) {
		starts.Add(1)
		<-ctx.Done()
		return "", ctx.Err()
	}}
}

// Three Closes targets, the forge hangs: without a total deadline this costs 3 x the timeout. No
// wall-clock bound (#338): the deadline is total when the hung read of the first target is the only
// one started; the later targets find the budget spent.
func TestMainDeadlineBoundsSlowGHFallback(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, time.Second)
	var starts atomic.Int32
	writeFile(t, filepath.Join(project, "body.md"), threeCloses("5", "6", "7"))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, slowIssue(&starts))
	if n := starts.Load(); n > 1 {
		t.Errorf("issue read started %d times, want at most 1: the deadline is not total", n)
	}
	notice := func(nr string) string {
		return "pr-create-gate: AC count not measurable for #" + nr + " (deadline reached) — structure checked only\n"
	}
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #5 (AC not counted, 1 rows), " +
		"Closes #6 (AC not counted, 1 rows), Closes #7 (AC not counted, 1 rows)\n" +
		notice("5") + notice("6") + notice("7"), ""})
}

// The forge hangs at the AC count: the gate stops waiting after the default ACTimeoutS of 5 s, well
// inside the total deadline, and passes loudly. It runs the real 5 s: the default is the subject.
func TestMainSlowForgePassesLoudlyAfterTheACTimeout(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	var starts atomic.Int32
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, slowIssue(&starts))
	if n := starts.Load(); n != 1 {
		t.Errorf("issue read started %d times, want 1", n)
	}
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (AC not counted, 1 rows)\n" +
		"pr-create-gate: AC count not measurable for #9 (timeout after 5 s) — structure checked only\n", ""})
}

// twin of test_main_deadline_keeps_blocker_of_earlier_target
func TestMainDeadlineKeepsBlockerOfEarlierTarget(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, time.Second)
	var starts atomic.Int32
	writeFile(t, filepath.Join(project, "body.md"), threeCloses("6", "7"))
	got := runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, slowIssue(&starts))
	// The total deadline, without a wall-clock bound (#338): one hung read at most.
	if n := starts.Load(); n > 1 {
		t.Errorf("issue read started %d times, want at most 1: the deadline is not total", n)
	}
	eq(t, "main", got, mainResult{2, "", blocked("Closes #5: heading `### Evidence #5` is missing")})
}

// No wall-clock bound (#338): git's own timeout is an hour here, so only the total deadline of
// 1 s can end the hung git with this message. A git that is not cut off runs 120 s and then exits
// 0, which gives another answer.
func TestMainDeadlineGitBlocksLoudly(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, time.Second)
	oldGit := gitTimeout
	gitTimeout = time.Hour
	t.Cleanup(func() { gitTimeout = oldGit })
	prependPath(t, stubBin(t, "sleep 120 as a hung git", map[string]string{"git": "#!/bin/sh\nsleep 120\n"}))
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMain(t, project, "gh pr create -F body.md", project)
	eq(t, "main", got, mainResult{2, "", blocked("repo of the project cannot be determined (COULD NOT CHECK: git timeout or deadline reached)")})
}

// twin of test_main_deadline_expired_before_git_blocks
func TestMainDeadlineExpiredBeforeGitBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, -time.Second)
	got := runMain(t, project, "gh pr create --body 'zie titel'", project)
	eq(t, "main", got, mainResult{2, "", blocked("repo of the project cannot be determined (COULD NOT CHECK: deadline reached)")})
}

// twin of test_missing_toml_fails_loud
func TestMainMissingTomlFailsLoud(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	gone := filepath.Join(t.TempDir(), "weg.toml")
	t.Setenv("LIGHTHOUSE_TOML", gone)
	got := runMain(t, project, "gh pr create --repo Acme/widgets --body 'Closes #1'", project)
	eq(t, "main", got, mainResult{1, "", "pr-create-gate: " + gone + ": file is missing\n"})
}

// Without a `gh pr create` the config is never read: a broken toml does not matter then.
func TestMainMissingTomlWithoutPrCreatePasses(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	t.Setenv("LIGHTHOUSE_TOML", filepath.Join(t.TempDir(), "weg.toml"))
	eq(t, "main", runMain(t, project, "git status", project), mainResult{0, "", ""})
}

// twin of test_without_repo_passes_with_gh_repo_env
func TestMainUsesGHRepoOfTheProcess(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	body := "gh pr create --body 'No issue: alleen docs gewijzigd'"
	got := runMain(t, project, "GH_REPO=Acme/widgets "+body, project)
	eq(t, "assignment", got, mainResult{0, "pr-create-gate: body checked — No issue\n", ""})
	t.Setenv("GH_REPO", "Acme/widgets")
	eq(t, "process", runMain(t, project, body, project), mainResult{0, "pr-create-gate: body checked — No issue\n", ""})
	t.Setenv("GH_REPO", "x/y")
	eq(t, "other", runMain(t, project, body, project), mainResult{0, "pr-create-gate: skipped — other repo x/y\n", ""})
}

// twin of test_without_repo_block_names_the_exact_repo
func TestMainWithoutRepoBlockNamesTheExactRepo(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --body 'No issue: alleen docs gewijzigd'", project)
	eq(t, "main", got, mainResult{2, "", blocked("`gh pr create` without --repo: in a clone of a fork gh then opens the PR in the upstream repo. Add `--repo Acme/widgets`.")})
}

// Review focus 3: a body file with CRLF, a lone CR and bytes that are no UTF-8 is read as Python
// reads it (errors="replace", universal newlines): one U+FFFD per maximal invalid subpart.
func TestCheckCreateReadsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	writeFile(t, path, "a\r\nb\rc\xff\xfed\xe2\x82X\xed\xa0\x80\xf0\x9f\x98")
	body, problem := readBodyFile(path)
	eq(t, "read", [2]string{body, problem}, [2]string{"a\nb\nc��d�X����", ""})

	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	// Ten invalid bytes are a reason of ten characters, as in Python; one U+FFFD per run would be one.
	writeFile(t, filepath.Join(project, "short.md"), "No issue: \xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\r\n")
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F short.md", project)
	eq(t, "ten invalid bytes", got, mainResult{0, "pr-create-gate: body checked — No issue\n", ""})

	env := Env{Issue: issueFake(t, "Acme/widgets", 9, "### Acceptatiecriteria\n- [ ] a\n", nil)}
	writeFile(t, filepath.Join(project, "body.md"), strings.ReplaceAll(closesBody("9", 1), "| a |", "| a\xff |")+"\r\n")
	got = runMainEnv(t, project, "gh pr create --repo Acme/widgets -F body.md", project, env)
	eq(t, "closes", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (1 AC, 1 rows)\n", ""})
}

// AC 3: the gate's own deadline stays under the timeout of its hook registration, else the hook
// timeout lets the tool call through (fail-open).
func TestDeadlineUnderHookTimeout(t *testing.T) {
	raw, err := os.ReadFile("../../pack/core/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string  `json:"command"`
				Timeout float64 `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var timeouts []float64
	for _, group := range doc.Hooks["PreToolUse"] {
		for _, h := range group.Hooks {
			if strings.HasSuffix(h.Command, "/run.sh pr-create-gate") {
				timeouts = append(timeouts, h.Timeout)
			}
		}
	}
	if len(timeouts) != 1 {
		t.Fatalf("want one pr-create-gate registration, found %d", len(timeouts))
	}
	if float64(DeadlineS) >= timeouts[0] {
		t.Errorf("DeadlineS %d is not under the hook timeout %v", DeadlineS, timeouts[0])
	}
}

// hangingGH puts a gh first on PATH that hangs and leaves a background child holding stdout open;
// the child's pid lands in the returned file. The child is killed at cleanup whatever happens.
func hangingGH(t *testing.T) string {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	prependPath(t, stubBin(t, "background child and process group", map[string]string{"gh": "#!/bin/sh\nsleep 30 &\necho $! > \"" + pidFile + "\"\nsleep 30\n"}))
	t.Cleanup(func() {
		if pid, err := readPid(pidFile); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return pidFile
}

func readPid(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(raw)))
}

// alive reports a process that exists and is no zombie (a container's init may not reap it).
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // no /proc (darwin): kill -0 is the answer
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	return len(fields) == 0 || fields[0] != "Z"
}

// assertGroupGone fails unless the background child of the stub is gone: the kill reached the
// whole group, not only gh. No wall-clock bound against waitDelay (#338): a run that WaitDelay
// ended instead of the group kill leaves that child alive, and this check sees it.
func assertGroupGone(t *testing.T, pidFile string) {
	t.Helper()
	pid, err := readPid(pidFile)
	if err != nil {
		t.Fatalf("pid of the background child: %v", err)
	}
	gone := false
	// 10 s: the loop ends as soon as the child is gone; only a slow runner needs more than 2 s.
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if !alive(pid) {
			gone = true
			break
		}
	}
	if !gone {
		t.Errorf("background child %d of gh still runs: the group was not killed", pid)
	}
}

// A gh that hangs past the deadline, with a grandchild on stdout: the whole group goes.
func TestRunGHKillsTheGroup(t *testing.T) {
	pidFile := hangingGH(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, _, err := RunGH(ctx, "api"); err == nil {
		t.Fatal("want an error after the timeout")
	}
	assertGroupGone(t, pidFile)
}

// twin of test_strip_comments_crlf_and_comment
func TestStripCommentsCRLFAndComment(t *testing.T) {
	eq(t, "strip", StripComments("a\r\n<!-- x\r\ny -->b"), "a\nb")
}

// twin of test_has_evidence_block
func TestHasEvidenceBlock(t *testing.T) {
	refs := "Refs #5 — close-issue toetst na de merge\n\n### Evidence #5\n\n" +
		"| AC | Command | Result |\n|---|---|---|\n| 1. een | `grep -F a f` | a |\n"
	eq(t, "block of #5", HasEvidenceBlock(refs, 5), true)
	eq(t, "no block of #6", HasEvidenceBlock(refs, 6), false)
	eq(t, "no heading", HasEvidenceBlock("Refs #5 — een reden genoeg", 5), false)
	eq(t, "heading in a comment", HasEvidenceBlock("Refs #5 — een reden genoeg\n<!--\n### Evidence #5\n-->", 5), false)
}

// A U+FFFD in the text is printable, as in Python; an invalid byte is escaped the way Python's
// surrogateescape shows it.
func TestRepoReprInBlocker(t *testing.T) {
	for value, want := range map[string]string{
		"a\uFFFDb": "--repo 'a\uFFFDb' does not have the form [HOST/]OWNER/REPO",
		"a\xffb":   `--repo 'a\udcffb' does not have the form [HOST/]OWNER/REPO`,
	} {
		_, reason := InScope(context.Background(), PrCreate{Tool: "gh", Repo: &value}, t.TempDir(), acmeRepo)
		eq(t, value, reason, want)
	}
}
