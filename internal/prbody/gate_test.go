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
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// The twins of tests/pack/test_pr_body_gate.py that are no data: `gh`, git repos and time.

const origin = "https://github.com/Acme/widgets.git"

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

// stubBin writes executable scripts into a fresh directory and returns it.
func stubBin(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// prependPath puts dir first on PATH.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	count, ok, cause, err := fetchACCount(ctx, DefaultEnv(), repo, nr)
	if err != nil {
		t.Fatalf("fetchACCount: %v", err)
	}
	return fetched{count, ok, cause}
}

// --- fetch_ac_count -----------------------------------------------------------

func TestFetchACCountWithoutGH(t *testing.T) {
	withConfig(t, "Acme/widgets")
	t.Setenv("PATH", t.TempDir())
	eq(t, "fetch", fetch(t, context.Background(), "Acme/widgets", 5), fetched{0, false, "gh is missing"})
}

func TestFetchACCountGHFails(t *testing.T) {
	withConfig(t, "Acme/widgets")
	t.Setenv("PATH", stubBin(t, map[string]string{"gh": "#!/bin/sh\nexit 1\n"}))
	eq(t, "fetch", fetch(t, context.Background(), "Acme/widgets", 5), fetched{0, false, "gh api failed (rc 1)"})
}

func TestFetchACCountCounts(t *testing.T) {
	withConfig(t, "Acme/widgets")
	callLog := filepath.Join(t.TempDir(), "aanroep.txt")
	stub := "#!/bin/sh\necho \"$@\" > \"" + callLog + "\"\n" +
		"printf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [x] b' '- [ ] c' '### Definition of Done' '- [ ] d'\n"
	t.Setenv("PATH", stubBin(t, map[string]string{"gh": stub}))
	eq(t, "fetch", fetch(t, context.Background(), "Acme/widgets", 5), fetched{3, true, ""})
	got, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "argv", strings.Fields(string(got)), []string{"api", "repos/Acme/widgets/issues/5", "--jq", ".body"})
}

func TestFetchACCountNoACHeading(t *testing.T) {
	withConfig(t, "Acme/widgets")
	t.Setenv("PATH", stubBin(t, map[string]string{"gh": "#!/bin/sh\nprintf 'geen kop hier\\n'\n"}))
	eq(t, "fetch", fetch(t, context.Background(), "Acme/widgets", 5), fetched{0, false, "no acceptance criteria heading"})
}

// A gh that hangs, with a grandchild that holds stdout open: the whole group goes at the timeout.
func TestFetchACCountTimeout(t *testing.T) {
	withConfig(t, "Acme/widgets")
	setTimeouts(t, 300*time.Millisecond, gateDeadline)
	pidFile := hangingGH(t)
	start := time.Now()
	eq(t, "fetch", fetch(t, context.Background(), "Acme/widgets", 5), fetched{0, false, "timeout after 0.3 s"})
	assertGroupGone(t, start, pidFile)
}

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
	pc := PrCreate{Body: &body}
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
	d, _ := InScope(context.Background(), pc, project, repo)
	return d
}

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

func TestScopeOtherRepoFlag(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(project, "x/y"), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip", "skipped — other repo x/y"})
}

func TestScopeOutsideRepo(t *testing.T) {
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	d, reason := InScope(context.Background(), pcAt(outside), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip", "skipped — outside this repo"})
}

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
	d, reason := InScope(context.Background(), found[0], project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "non-literal --repo — spell the repo out"})
}

func TestScopeOutsideRepoWithR(t *testing.T) {
	base, project := newProject(t)
	outside := filepath.Join(base, "buiten")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	eq(t, "decision", decision(t, pcAt(outside, "Acme/widgets"), project, "Acme/widgets"), "check")
}

func TestScopeOtherGitRepo(t *testing.T) {
	base, project := newProject(t)
	fixturesetup.Build(t, base, []map[string]any{{"repo": "ander", "branch": "main"}})
	eq(t, "decision", decision(t, pcAt(filepath.Join(base, "ander")), project, "Acme/widgets"), "skip")
}

// forgeWorktree is forge_worktree of _testkit.py: a worktree of a separate bare clone of project,
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

func TestScopeForgeWorktreeOfTheSameRepo(t *testing.T) {
	base, project := newProject(t)
	fixturesetup.Git(t, project, "remote", "add", "origin", origin)
	work := forgeWorktree(t, project, base, origin)
	d, reason := InScope(context.Background(), pcAt(work), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"block",
		"`gh pr create` without --repo: in a clone of a fork gh then opens the PR in the upstream repo. Add `--repo Acme/widgets`."})
}

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
	d, reason := InScope(context.Background(), pcAt(work), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip",
		"skipped — outside this repo (git-common-dir " + common + " vs " + ref +
			"; origin https://github.com/Acme/gadgets.git vs " + origin + ")"})
}

func TestScopeUnknownCwdBlocks(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(""), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "working directory of `gh pr create` cannot be resolved"})
}

func TestScopeOtherGitErrorBlocks(t *testing.T) {
	_, project := newProject(t)
	// A git that fails without "not a git repository" is no proof of "outside".
	t.Setenv("PATH", stubBin(t, map[string]string{"git": "#!/bin/sh\necho kapot >&2\nexit 128\n"}))
	d, reason := InScope(context.Background(), pcAt(project), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"block", "repo of the project cannot be determined (git failed (rc 128): kapot)"})
}

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
	d, reason := InScope(context.Background(), pcAt(outside), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"skip", "skipped — outside this repo"})
}

func TestWithoutRepoPassesWithRepoFlag(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(project, "Acme/widgets"), project, "Acme/widgets")
	eq(t, "scope", [2]string{d, reason}, [2]string{"check", ""})
}

func TestWithoutRepoNotBlockedWithoutForgeRepo(t *testing.T) {
	_, project := newProject(t)
	d, reason := InScope(context.Background(), pcAt(project), project, "")
	eq(t, "scope", [2]string{d, reason}, [2]string{"check", ""})
}

// --- Main -------------------------------------------------------------------------

type mainResult struct {
	rc             int
	stdout, stderr string
}

func runMain(t *testing.T, project, command, cwd string) mainResult {
	t.Helper()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	var stdout, stderr bytes.Buffer
	rc := Main(context.Background(), cwd, strings.NewReader(command), &stdout, &stderr, DefaultEnv())
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

func TestMainNoGHPrCreate(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	eq(t, "main", runMain(t, project, "git status", project), mainResult{0, "", ""})
}

func TestMainOpenQuote(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	eq(t, "main", runMain(t, project, "gh pr create --repo Acme/widgets --body 'x", project), mainResult{3, "", ""})
}

func TestMainProblemBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --repo Acme/widgets --fill", project)
	eq(t, "main", got, mainResult{2, "", blocked("--fill gives no body to check")})
}

func TestMainNoIssuePasses(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create --repo Acme/widgets --body 'No issue: alleen docs gewijzigd'", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — No issue\n", ""})
}

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

func TestMainOtherRepoSkipped(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	got := runMain(t, project, "gh pr create -R x/y --fill", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: skipped — other repo x/y\n", ""})
}

func TestMainClosesPassesWithCount(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	prependPath(t, stubBin(t, map[string]string{"gh": "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [ ] b'\n"}))
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 2))
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F body.md", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (2 AC, 2 rows)\n", ""})
}

func TestMainClosesACMismatchBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	prependPath(t, stubBin(t, map[string]string{"gh": "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [ ] b' '- [ ] c'\n"}))
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 2))
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F body.md", project)
	eq(t, "main", got, mainResult{2, "", blocked("Closes #9: the issue has 3 acceptance criteria, the Evidence block 2 rows — one row per AC")})
}

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

func TestMainFallbackWithoutGHNoticesAndPasses(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F body.md", project)
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #9 (AC not counted, 1 rows)\n" +
		"pr-create-gate: AC count not measurable for #9 (gh is missing) — structure checked only\n", ""})
}

func threeCloses(withEvidence ...string) string {
	body := "Closes #5\nCloses #6\nCloses #7\n\n"
	for _, nr := range withEvidence {
		body += "### Evidence #" + nr + "\n\n| AC | Command | Result |\n|---|---|---|\n| 1. een | `grep -F a f` | a |\n\n"
	}
	return body
}

func TestMainDeadlineBoundsSlowGHFallback(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, time.Second)
	prependPath(t, stubBin(t, map[string]string{"gh": "#!/bin/sh\nsleep 3\n"}))
	writeFile(t, filepath.Join(project, "body.md"), threeCloses("5", "6", "7"))
	start := time.Now()
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F body.md", project)
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("took %v, the deadline is 1 s", elapsed)
	}
	notice := func(nr string) string {
		return "pr-create-gate: AC count not measurable for #" + nr + " (deadline reached) — structure checked only\n"
	}
	eq(t, "main", got, mainResult{0, "pr-create-gate: body checked — Closes #5 (AC not counted, 1 rows), " +
		"Closes #6 (AC not counted, 1 rows), Closes #7 (AC not counted, 1 rows)\n" +
		notice("5") + notice("6") + notice("7"), ""})
}

func TestMainDeadlineKeepsBlockerOfEarlierTarget(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, time.Second)
	prependPath(t, stubBin(t, map[string]string{"gh": "#!/bin/sh\nsleep 3\n"}))
	writeFile(t, filepath.Join(project, "body.md"), threeCloses("6", "7"))
	start := time.Now()
	got := runMain(t, project, "gh pr create --repo Acme/widgets -F body.md", project)
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("took %v, the deadline is 1 s", elapsed)
	}
	eq(t, "main", got, mainResult{2, "", blocked("Closes #5: heading `### Evidence #5` is missing")})
}

func TestMainDeadlineGitBlocksLoudly(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, time.Second)
	prependPath(t, stubBin(t, map[string]string{"git": "#!/bin/sh\nsleep 3\n"}))
	writeFile(t, filepath.Join(project, "body.md"), closesBody("9", 1))
	start := time.Now()
	got := runMain(t, project, "gh pr create -F body.md", project)
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("took %v, the deadline is 1 s", elapsed)
	}
	eq(t, "main", got, mainResult{2, "", blocked("repo of the project cannot be determined (COULD NOT CHECK: git timeout or deadline reached)")})
}

func TestMainDeadlineExpiredBeforeGitBlocks(t *testing.T) {
	withConfig(t, "Acme/widgets")
	_, project := newProject(t)
	setTimeouts(t, acTimeout, -time.Second)
	got := runMain(t, project, "gh pr create --body 'zie titel'", project)
	eq(t, "main", got, mainResult{2, "", blocked("repo of the project cannot be determined (COULD NOT CHECK: deadline reached)")})
}

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

	prependPath(t, stubBin(t, map[string]string{"gh": "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a'\n"}))
	writeFile(t, filepath.Join(project, "body.md"), strings.ReplaceAll(closesBody("9", 1), "| a |", "| a\xff |")+"\r\n")
	got = runMain(t, project, "gh pr create --repo Acme/widgets -F body.md", project)
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
	prependPath(t, stubBin(t, map[string]string{"gh": "#!/bin/sh\nsleep 30 &\necho $! > \"" + pidFile + "\"\nsleep 30\n"}))
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

// assertGroupGone fails unless the run ended before waitDelay could end it, and the background
// child of the stub is gone: the kill reached the whole group, not only gh.
func assertGroupGone(t *testing.T, start time.Time, pidFile string) {
	t.Helper()
	if elapsed := time.Since(start); elapsed >= waitDelay {
		t.Errorf("took %v: the wait ended through WaitDelay (%v), not through the group kill", elapsed, waitDelay)
	}
	pid, err := readPid(pidFile)
	if err != nil {
		t.Fatalf("pid of the background child: %v", err)
	}
	for end := time.Now().Add(500 * time.Millisecond); alive(pid) && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(pid) {
		t.Errorf("background child %d of gh still runs: the group was not killed", pid)
	}
}

// A gh that hangs past the deadline, with a grandchild on stdout: the whole group goes.
func TestRunGHKillsTheGroup(t *testing.T) {
	pidFile := hangingGH(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := RunGH(ctx, "api"); err == nil {
		t.Fatal("want an error after the timeout")
	}
	assertGroupGone(t, start, pidFile)
}

func TestStripCommentsCRLFAndComment(t *testing.T) {
	eq(t, "strip", StripComments("a\r\n<!-- x\r\ny -->b"), "a\nb")
}

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
		_, reason := InScope(context.Background(), PrCreate{Repo: &value}, t.TempDir(), "Acme/widgets")
		eq(t, value, reason, want)
	}
}
