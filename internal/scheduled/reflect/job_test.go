package reflect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io/filesystem (real git with a bare origin, real session files, the real
// Postcheck). The regression surface is the gate (which sessions count, the window, the
// threshold), what the containment lets through (one document and index.md, never the corpus),
// and what the agent is told. Each test names the production change that makes it fail.

var now = time.Date(2026, 11, 18, 4, 0, 0, 0, time.UTC)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type rig struct {
	t                  *testing.T
	root, origin, repo string
	home               string
	history            []runs.Row
}

func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, root: root, origin: filepath.Join(root, "origin.git"), repo: filepath.Join(root, "repo"), home: filepath.Join(root, "home")}
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", r.origin)
	gitIn(t, root, "clone", "-q", r.origin, r.repo)
	gitIn(t, r.repo, "checkout", "-q", "-b", "main")
	put(t, r.repo, "README.md", "x\n")
	r.commit("init")
	return r
}

func (r *rig) commit(msg string) {
	r.t.Helper()
	gitIn(r.t, r.repo, "add", "-A")
	gitIn(r.t, r.repo, "commit", "-q", "-m", msg)
	gitIn(r.t, r.repo, "push", "-q", "origin", "main")
}

func (r *rig) env() scheduled.Env {
	return scheduled.Env{
		Repo:    repoconfig.Repo{Dir: r.repo, DefaultBranch: "main", Slug: "o/r", Kind: "github", Language: "en"},
		Now:     now,
		Run:     workplace.ExecRunner(),
		Home:    r.home,
		History: r.history,
	}
}

// session writes a session file of the repo, mod days before now, with one prompt.
func (r *rig) session(id string, daysAgo int, prompt string) {
	r.t.Helper()
	p := filepath.Join(workplace.ProjectDir(r.home, r.repo), id+".jsonl")
	touch(r.t, p, now.AddDate(0, 0, -daysAgo))
	put(r.t, filepath.Dir(p), id+".jsonl", userLine(prompt)+"\n")
	_ = os.Chtimes(p, now.AddDate(0, 0, -daysAgo), now.AddDate(0, 0, -daysAgo))
}

func bind(t *testing.T, params map[string]any) *Job {
	t.Helper()
	j, err := scheduled.Bind(New(), repoconfig.Schedule{Name: "reflect", Job: Name, Params: params})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return j.(*Job)
}

var three = map[string]any{"min_sessions": int64(3), "min_lines": int64(3)}

func TestGateCountsBothKindsAgainstMinSessionsAndNamesBoth(t *testing.T) {
	// Fails if the gate counts one kind only, if `<` becomes `<=`, or if the reason lacks a count.
	for _, tc := range []struct {
		files int
		skip  bool
	}{{2, true}, {3, false}} {
		t.Run(fmt.Sprint(tc.files), func(t *testing.T) {
			r := newRig(t)
			r.history = []runs.Row{{SessionID: "s1", Trigger: "issue"}}
			for i := range tc.files {
				r.session(fmt.Sprintf("s%d", i+1), 1, "hello")
			}
			v, err := bind(t, three).Gate(context.Background(), r.env())
			if err != nil {
				t.Fatal(err)
			}
			if (v.Skip != "") != tc.skip || v.Score != tc.files {
				t.Fatalf("verdict = %+v, want skip %v, score %d", v, tc.skip, tc.files)
			}
			if tc.skip && (!strings.Contains(v.Skip, "interactive 1") || !strings.Contains(v.Skip, "controller 1")) {
				t.Errorf("reason %q must name both counts", v.Skip)
			}
		})
	}
}

func TestGateIgnoresScheduledSessionsAndThoseOutsideTheWindow(t *testing.T) {
	// Fails if a reflect round's own session counts as new material, or the window is not applied.
	r := newRig(t)
	r.history = []runs.Row{{SessionID: "own", Trigger: runs.TriggerScheduled, Schedule: "reflect"}}
	r.session("own", 1, "x")
	r.session("old", 40, "x") // older than window_min_days (14) with no earlier document
	r.session("a", 2, "x")
	r.session("b", 3, "x")
	v, err := bind(t, three).Gate(context.Background(), r.env())
	if err != nil {
		t.Fatal(err)
	}
	if v.Skip == "" || v.Score != 2 {
		t.Fatalf("verdict = %+v, want a skip with 2 (not the scheduled session, not the old one)", v)
	}
}

func TestGateWindowFollowsTheLastDocumentOnOrigin(t *testing.T) {
	// The newest document is 30 days old: the window is 30 days, so a 25-day-old session counts
	// (it would not within the minimum of 14). Fails if the gate reads the date from the wrong
	// place or ignores it.
	r := newRig(t)
	put(t, r.repo, "docs/developer/reflect/2026-10-19-skill-kandidaten.md", "x\n")
	put(t, r.repo, "docs/developer/reflect/2026-09-01-skill-kandidaten.md", "x\n")
	r.commit("rounds")
	for i, d := range []int{25, 26, 27} {
		r.session(fmt.Sprintf("s%d", i), d, "x")
	}
	v, err := bind(t, three).Gate(context.Background(), r.env())
	if err != nil || v.Skip != "" || v.Score != 3 {
		t.Fatalf("verdict = %+v, %v; want a run on 3 sessions", v, err)
	}
}

func TestLaunchCarriesCorpusAndOutPathAndNoBash(t *testing.T) {
	r := newRig(t)
	plan, err := bind(t, nil).Launch(scheduled.Verdict{}, r.env())
	if err != nil {
		t.Fatal(err)
	}
	want := "/paia-reflect-skills --corpus .lighthouse/run/reflect-corpus.md --out docs/developer/reflect/2026-11-18-skill-kandidaten.md"
	if plan.Command != want {
		t.Errorf("Command = %q, want %q", plan.Command, want)
	}
	if slices.Contains(plan.Tools, "Bash") || plan.MaxRuntime != time.Hour {
		t.Errorf("plan = %+v: no Bash, one hour", plan)
	}
	if !strings.Contains(plan.Instructions, "do NOT push") && !strings.Contains(plan.Instructions, "Do NOT") {
		t.Errorf("instructions lack the unattended rules: %q", plan.Instructions)
	}
}

func TestAllowedIsOneDocumentAndTheIndex(t *testing.T) {
	j := bind(t, nil)
	for p, want := range map[string]bool{
		"docs/developer/reflect/2026-11-18-skill-kandidaten.md":     true,
		"docs/developer/reflect/index.md":                           true,
		"docs/developer/reflect/other.md":                           false,
		"docs/developer/reflect/sub/2026-11-18-skill-kandidaten.md": false,
		".lighthouse/run/reflect-corpus.md":                         false,
		"README.md":                                                 false,
	} {
		if got := j.Allowed(p); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", p, got, want)
		}
	}
}

const e2eBranch = "scheduled/reflect-20261118T0400-1"

type bareRemote struct{ origin string }

func (r bareRemote) Head(ctx context.Context, _, branch string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "ls-remote", r.origin, "refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return "", errors.New(string(out))
	}
	if f := strings.Fields(string(out)); len(f) > 0 {
		return f[0], nil
	}
	return "", nil
}

func (r bareRemote) Push(ctx context.Context, dir, commit, branch string) error {
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "push", "-q", r.origin, commit+":refs/heads/"+branch).CombinedOutput(); err != nil {
		return errors.New(string(out))
	}
	return nil
}

const docPath = "docs/developer/reflect/2026-11-18-skill-kandidaten.md"

// start does what the tick does before the agent.
func start(t *testing.T, r *rig, j *Job) (scheduled.Env, scheduled.Verdict, string) {
	t.Helper()
	ctx := context.Background()
	for i := range 3 {
		r.session(fmt.Sprintf("s%d", i), 1, fmt.Sprintf("prompt number %d", i))
	}
	env := r.env()
	v, err := j.Gate(ctx, env)
	if err != nil || v.Skip != "" {
		t.Fatalf("Gate = %+v, %v", v, err)
	}
	if _, err := j.Launch(v, env); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.root, "work")
	gitIn(t, r.repo, "worktree", "add", "-q", "-b", e2eBranch, dir, "origin/main")
	env.Dir = dir
	if err := j.Stage(ctx, env, v); err != nil {
		t.Fatal(err)
	}
	return env, v, gitIn(t, dir, "rev-parse", "HEAD")
}

func TestStageWritesTheCorpusAndCleanupRemovesIt(t *testing.T) {
	// Fails if the corpus is not where the command says, lacks the prompts, or survives Cleanup
	// (it would then reach the containment).
	r := newRig(t)
	j := bind(t, three)
	env, _, _ := start(t, r, j)
	b, err := os.ReadFile(filepath.Join(env.Dir, ".lighthouse/run/reflect-corpus.md"))
	if err != nil || !strings.Contains(string(b), "prompt number 1") || !strings.Contains(string(b), "## Interactief") {
		t.Fatalf("corpus = %q, %v", b, err)
	}
	if err := j.Cleanup(context.Background(), env, env.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(env.Dir, ".lighthouse/run/reflect-corpus.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("corpus after Cleanup: %v, want gone", err)
	}
	if out := gitIn(t, env.Dir, "status", "--porcelain", "-uall"); out != "" {
		t.Errorf("work tree after Cleanup is dirty: %q", out)
	}
}

func TestStageDoesNotWriteThroughASymlink(t *testing.T) {
	// A repo whose .lighthouse is a link must not send the corpus outside the work tree.
	r := newRig(t)
	outside := t.TempDir()
	must(t, os.Symlink(outside, filepath.Join(r.repo, ".lighthouse")))
	r.commit("link")
	j := bind(t, three)
	for i := range 3 {
		r.session(fmt.Sprintf("s%d", i), 1, "p")
	}
	env := r.env()
	dir := filepath.Join(r.root, "work")
	gitIn(t, r.repo, "worktree", "add", "-q", "-b", e2eBranch, dir, "origin/main")
	env.Dir = dir
	if err := j.Stage(context.Background(), env, scheduled.Verdict{}); err == nil {
		t.Error("Stage followed a symlink: want an error")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("files written outside the work tree: %v", entries)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestARoundReachesTheBranchWithItsIndexAndWithoutTheCorpus(t *testing.T) {
	r := newRig(t)
	j := bind(t, three)
	env, v, startSHA := start(t, r, j)
	put(t, env.Dir, docPath, "# Candidates\n"+strings.Repeat("line\n", 5))
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, startSHA, e2eBranch, bareRemote{r.origin})
	if err != nil || res.Failure != "" || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want a pushed commit", res, err)
	}
	files := strings.Fields(gitIn(t, r.origin, "ls-tree", "-r", "--name-only", res.Commit))
	for _, p := range []string{docPath, "docs/developer/reflect/index.md"} {
		if !slices.Contains(files, p) {
			t.Errorf("%s is not in the pushed commit: %v", p, files)
		}
	}
	if slices.Contains(files, ".lighthouse/run/reflect-corpus.md") {
		t.Error("the corpus reached the branch")
	}
	if idx := gitIn(t, r.origin, "show", res.Commit+":docs/developer/reflect/index.md"); !strings.Contains(idx, "2026-11-18-skill-kandidaten.md") {
		t.Errorf("index.md = %q, want the round listed", idx)
	}
}

func TestASecondDocumentOrAShortOneFailsTheRun(t *testing.T) {
	for name, tc := range map[string]struct {
		write func(t *testing.T, dir string)
		want  string
	}{
		"second document": {func(t *testing.T, dir string) {
			put(t, dir, docPath, "# a\n"+strings.Repeat("l\n", 5))
			put(t, dir, "docs/developer/reflect/2026-11-17-skill-kandidaten.md", "# b\n"+strings.Repeat("l\n", 5))
		}, "one document"},
		"too short": {func(t *testing.T, dir string) { put(t, dir, docPath, "# a\n") }, "lines"},
		"other file": {func(t *testing.T, dir string) {
			put(t, dir, docPath, "# a\n"+strings.Repeat("l\n", 5))
			put(t, dir, "docs/developer/reflect/extra.md", "x\n")
		}, "extra.md"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			j := bind(t, three)
			env, v, startSHA := start(t, r, j)
			tc.write(t, env.Dir)
			res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, startSHA, e2eBranch, bareRemote{r.origin})
			if err != nil || res.Changed || !strings.Contains(res.Failure, tc.want) {
				t.Fatalf("Postcheck = %+v, %v; want a failure containing %q", res, err, tc.want)
			}
			if head, _ := (bareRemote{r.origin}).Head(context.Background(), "", e2eBranch); head != "" {
				t.Errorf("branch on origin at %s, want nothing pushed", head)
			}
		})
	}
}

func TestAnUnboundJobRefuses(t *testing.T) {
	r := newRig(t)
	if _, err := New().Gate(context.Background(), r.env()); err == nil {
		t.Error("the registered, unconfigured job ran its gate")
	}
}

func TestARunThatWroteNothingEndsWithoutACommit(t *testing.T) {
	// Postcheck ends a run with an empty change set as done without a PR, before Check; the next
	// gate then still sees the same sessions. Fails if Cleanup leaves the corpus, which would make
	// the change set non-empty and the run fail on the containment.
	r := newRig(t)
	j := bind(t, three)
	env, v, startSHA := start(t, r, j)
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, startSHA, e2eBranch, bareRemote{r.origin})
	if err != nil || res.Failure != "" || res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want unchanged", res, err)
	}
}

func TestCheckNamesAMissingDocument(t *testing.T) {
	r := newRig(t)
	j := bind(t, three)
	env, _, _ := start(t, r, j)
	if err := j.Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "no document") {
		t.Errorf("Check = %v, want an error naming the missing document", err)
	}
}

func TestAResumedRoundPassesCheckOnItsPushedCommit(t *testing.T) {
	// After a push whose result was not recorded, the next tick finds the branch on origin and
	// checks the pushed commit in a clean checkout, where `git status` shows nothing. Fails if
	// Check finds the document only through the work tree status: the run would end failed with
	// a correct commit on origin.
	r := newRig(t)
	j := bind(t, three)
	env, v, startSHA := start(t, r, j)
	put(t, env.Dir, docPath, "# Candidates\n"+strings.Repeat("line\n", 5))
	remote := bareRemote{r.origin}
	first, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, startSHA, e2eBranch, remote)
	if err != nil || first.Failure != "" || !first.Changed {
		t.Fatalf("first Postcheck = %+v, %v", first, err)
	}
	again, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, startSHA, e2eBranch, remote)
	if err != nil || again.Failure != "" || !again.Changed || again.Commit != first.Commit {
		t.Fatalf("resume Postcheck = %+v, %v; want the same pushed commit, no failure", again, err)
	}
}
