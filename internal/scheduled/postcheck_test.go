package scheduled_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification for every test in this file: io/filesystem (a real git repository with a local
// bare origin). The regression surface is what lands in the commit and on origin, and the
// Failure reason; each test names the production change it guards.

const testBranch = "scheduled/nightly-20261117T0400-1"

type fixture struct {
	t      *testing.T
	work   string
	origin string // the bare remote, by path
	start  string
	pushes int
	// pushErr, when set, is what Push returns instead of pushing.
	pushErr error
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	root := t.TempDir()
	f := &fixture{t: t, work: filepath.Join(root, "work")}
	origin := filepath.Join(root, "origin.git")
	f.origin = origin
	runGit(t, root, "init", "-q", "--bare", origin)
	runGit(t, root, "init", "-q", "-b", "main", f.work)
	f.git("config", "user.name", "Test")
	f.git("config", "user.email", "test@example.com")
	f.write("out/keep.md", "keep\n")
	f.write("out/sub/doc.md", "doc\n")
	f.write("src/main.go", "package main\n")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "init")
	f.git("remote", "add", "origin", origin)
	f.git("push", "-q", "origin", "main")
	f.git("checkout", "-q", "-b", testBranch)
	f.start = f.git("rev-parse", "HEAD")
	return f
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	return runGit(f.t, f.work, args...)
}

func (f *fixture) write(rel, body string) {
	f.t.Helper()
	p := filepath.Join(f.work, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// remote is the fixture's scheduled.Remote: the bare origin by its path, never the
// workplace's origin remote.
func (f *fixture) remote() scheduled.Remote { return fixtureRemote{f} }

type fixtureRemote struct{ f *fixture }

func (r fixtureRemote) Head(ctx context.Context, _, branch string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "ls-remote", r.f.origin, "refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return "", errors.New(string(out))
	}
	if fields := strings.Fields(string(out)); len(fields) > 0 {
		return fields[0], nil
	}
	return "", nil
}

func (r fixtureRemote) Push(ctx context.Context, dir, commit, branch string) error {
	r.f.pushes++
	if r.f.pushErr != nil {
		return r.f.pushErr
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "push", "-q", r.f.origin, commit+":refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

func (f *fixture) postcheck(job *scheduledtest.Job) (scheduled.PostcheckResult, error) {
	f.t.Helper()
	env := scheduled.Env{
		Schedule: repoconfig.Schedule{Name: "nightly", Job: "test"},
		Dir:      f.work,
		Run:      workplace.ExecRunner(),
	}
	return scheduled.Postcheck(context.Background(), env.Run, job, env, scheduled.Verdict{}, f.start, testBranch, f.remote())
}

// originBranch is the SHA of the branch on origin, "" when it is not there.
func (f *fixture) originBranch() string {
	f.t.Helper()
	out := f.git("ls-remote", "origin", "refs/heads/"+testBranch)
	if out == "" {
		return ""
	}
	return strings.Fields(out)[0]
}

// committed is the list of paths the commits on top of start change.
func (f *fixture) committed() []string {
	f.t.Helper()
	out := f.git("diff", "--name-only", f.start, "HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// assertNothingLanded checks the containment promise of a Failure: no commit, nothing on origin.
func (f *fixture) assertNothingLanded(res scheduled.PostcheckResult, err error, fragments ...string) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("Postcheck error = %v, want a Failure", err)
	}
	for _, frag := range fragments {
		if !strings.Contains(res.Failure, frag) {
			f.t.Errorf("Failure = %q, want it to contain %q", res.Failure, frag)
		}
	}
	if res.Changed {
		f.t.Error("Changed = true on a Failure")
	}
	if f.pushes != 0 {
		f.t.Errorf("push called %d times on a Failure", f.pushes)
	}
	if head := f.git("rev-parse", "HEAD"); head != f.start {
		f.t.Errorf("HEAD = %s, want start %s (no commit on a Failure)", head, f.start)
	}
	if b := f.originBranch(); b != "" {
		f.t.Errorf("branch on origin at %s after a Failure", b)
	}
}

// (a) Fails if Postcheck commits a path outside the changed allowed set, drops one, skips the
// Cleanup or Finish step, calls the steps out of order, or does not push.
func TestPostcheckCommitsExactlyTheAllowedPaths(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "new\n")
	f.write("out/keep.md", "changed\n")
	f.write(".lighthouse/run/corpus.md", "staging\n")
	job := &scheduledtest.Job{
		AllowedPre: []string{"out/"},
		CleanupFn: func(_ context.Context, _ scheduled.Env, dir string) error {
			return os.Remove(filepath.Join(dir, ".lighthouse/run/corpus.md"))
		},
		FinishFn: func(_ context.Context, _ scheduled.Env, dir string, _ scheduled.Verdict) error {
			return os.WriteFile(filepath.Join(dir, "out/index.md"), []byte("index\n"), 0o644)
		},
	}
	res, err := f.postcheck(job)
	if err != nil || res.Failure != "" || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want Changed", res, err)
	}
	if got, want := f.committed(), []string{"out/a.md", "out/index.md", "out/keep.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("committed %v, want %v", got, want)
	}
	if got := f.git("rev-list", "--count", f.start+"..HEAD"); got != "1" {
		t.Errorf("%s commits on top of start, want 1", got)
	}
	if got := f.git("log", "-1", "--format=%s"); got != "chore: geplande run nightly" {
		t.Errorf("commit subject = %q", got)
	}
	if f.pushes != 1 || f.originBranch() != f.git("rev-parse", "HEAD") {
		t.Errorf("pushes = %d, origin = %q; want the commit on origin", f.pushes, f.originBranch())
	}
	if got, want := job.Calls(), []string{"Cleanup", "Check", "Finish"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if st := f.git("status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("work tree not clean after the commit:\n%s", st)
	}
}

// (b) Fails if the HEAD-guard is gone: the agent's own commit would stay and Postcheck would see
// nothing changed, or keep the agent's commit message.
func TestPostcheckHeadGuardResetsTheAgentsCommit(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "agent\n")
	f.git("add", "out/a.md")
	f.git("commit", "-q", "-m", "agent commit")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	if err != nil || res.Failure != "" || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want Changed", res, err)
	}
	if got := f.git("rev-list", "--count", f.start+"..HEAD"); got != "1" {
		t.Errorf("%s commits on top of start, want 1", got)
	}
	if got := f.git("log", "-1", "--format=%s"); got != "chore: geplande run nightly" {
		t.Errorf("commit subject = %q, want Postcheck's own commit", got)
	}
	if got, want := f.committed(), []string{"out/a.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("committed %v, want %v", got, want)
	}
}

// (b) Fails if an agent commit is trusted: a disallowed path hidden in a commit must reach the
// containment as a working-tree change.
func TestPostcheckHeadGuardExposesADisallowedCommit(t *testing.T) {
	f := newFixture(t)
	f.write("src/evil.go", "package main\n")
	f.git("add", "src/evil.go")
	f.git("commit", "-q", "-m", "agent commit")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	f.assertNothingLanded(res, err, "src/evil.go", "not allowed")
}

// Fails if the HEAD-guard resets whatever branch the agent left checked out instead of the run's
// branch: the commit must land on the run's branch with start as parent.
func TestPostcheckHeadGuardReturnsToTheRunBranch(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-q", "-b", "elsewhere")
	f.write("out/a.md", "agent\n")
	f.git("add", "out/a.md")
	f.git("commit", "-q", "-m", "agent commit")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	if err != nil || res.Failure != "" || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want Changed", res, err)
	}
	if got := f.git("symbolic-ref", "HEAD"); got != "refs/heads/"+testBranch {
		t.Errorf("HEAD on %s, want the run branch", got)
	}
	if got := f.git("rev-parse", testBranch+"^"); got != f.start {
		t.Errorf("parent of the run branch = %s, want start %s", got, f.start)
	}
}

// (c) Fails if Allowed is not enforced on every changed path, or if a Failure still commits or
// pushes.
func TestPostcheckRejectsAPathOutsideAllowed(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "fine\n")
	f.write("src/x.go", "package main\n")
	job := &scheduledtest.Job{AllowedPre: []string{"out/"}}
	res, err := f.postcheck(job)
	f.assertNothingLanded(res, err, "src/x.go", "not allowed")
	if got := job.Calls(); !reflect.DeepEqual(got, []string{"Cleanup"}) {
		t.Errorf("calls = %v, want Check and Finish skipped", got)
	}
}

// Fails if the files Finish writes skip the containment: only a set validated after Finish may be
// committed.
func TestPostcheckRejectsAFinishWriteOutsideAllowed(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "fine\n")
	job := &scheduledtest.Job{
		AllowedPre: []string{"out/"},
		FinishFn: func(_ context.Context, _ scheduled.Env, dir string, _ scheduled.Verdict) error {
			return os.WriteFile(filepath.Join(dir, "src/gen.go"), []byte("package main\n"), 0o644)
		},
	}
	res, err := f.postcheck(job)
	f.assertNothingLanded(res, err, "src/gen.go", "not allowed")
}

// (d) Fails if a Check error does not make the run fail.
func TestPostcheckFailsWhenCheckFails(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "short\n")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}, CheckErr: errors.New("document a.md has 1 line")})
	f.assertNothingLanded(res, err, "check", "document a.md has 1 line")
}

// (e) Fails if the secret scan is skipped, or runs before Finish (the second case only Finish
// writes the secret), or if the reason echoes the secret.
func TestPostcheckRejectsASecret(t *testing.T) {
	cases := map[string]struct {
		agent, finish string
	}{
		"written by the agent":  {agent: "token sk_live_abc123\n"},
		"written during Finish": {finish: "token sk_live_abc123\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write("out/a.md", "line one\n"+tc.agent)
			job := &scheduledtest.Job{AllowedPre: []string{"out/"}}
			if tc.finish != "" {
				job.FinishFn = func(_ context.Context, _ scheduled.Env, dir string, _ scheduled.Verdict) error {
					return os.WriteFile(filepath.Join(dir, "out/index.md"), []byte(tc.finish), 0o644)
				}
			}
			res, err := f.postcheck(job)
			want := `"out/a.md":2: Stripe live key`
			if tc.finish != "" {
				want = `"out/index.md":1: Stripe live key`
			}
			f.assertNothingLanded(res, err, "secret scan", want)
			if strings.Contains(res.Failure, "abc123") {
				t.Errorf("Failure echoes the secret: %q", res.Failure)
			}
		})
	}
}

// (f) Fails if Postcheck reports a change, commits or pushes when the agent changed nothing (a
// staging file Cleanup removes does not count), or runs Finish then: a Finish that always writes
// an index would turn every idle run into a PR.
func TestPostcheckNothingChanged(t *testing.T) {
	f := newFixture(t)
	f.write(".lighthouse/run/corpus.md", "staging\n")
	job := &scheduledtest.Job{
		AllowedPre: []string{"out/"},
		CleanupFn: func(_ context.Context, _ scheduled.Env, dir string) error {
			return os.Remove(filepath.Join(dir, ".lighthouse/run/corpus.md"))
		},
		FinishFn: func(_ context.Context, _ scheduled.Env, dir string, _ scheduled.Verdict) error {
			return os.WriteFile(filepath.Join(dir, "out/index.md"), []byte("index\n"), 0o644)
		},
	}
	res, err := f.postcheck(job)
	if err != nil || res.Failure != "" || res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want unchanged", res, err)
	}
	if got := job.Calls(); !reflect.DeepEqual(got, []string{"Cleanup"}) {
		t.Errorf("calls = %v, want Check and Finish skipped", got)
	}
	if f.pushes != 0 || f.git("rev-parse", "HEAD") != f.start {
		t.Errorf("pushes = %d, HEAD moved = %v", f.pushes, f.git("rev-parse", "HEAD") != f.start)
	}
}

// (g) Review focus 1. Fails if the containment follows a symlink out of the work tree (or into
// its .git) instead of rejecting it, also when the symlink is a directory in the path or does not
// resolve; and fails the other way if a symlink inside the work tree is rejected.
func TestPostcheckSymlinks(t *testing.T) {
	outside := t.TempDir()
	cases := map[string]struct {
		setup func(f *fixture)
		want  []string // Failure fragments; nil means the run must pass
	}{
		"file link to an existing file outside": {
			setup: func(f *fixture) { f.symlink(outsideFile(t, outside, "hostname"), "out/x.md") },
			want:  []string{"out/x.md", "outside the work tree"},
		},
		"dangling link": {
			setup: func(f *fixture) { f.symlink("does-not-exist", "out/x.md") },
			want:  []string{"out/x.md", "cannot be resolved"},
		},
		"directory in the path": {
			setup: func(f *fixture) {
				if err := os.RemoveAll(filepath.Join(f.work, "out/sub")); err != nil {
					t.Fatal(err)
				}
				f.symlink(outside, "out/sub")
			},
			want: []string{"out/sub", "outside the work tree"},
		},
		"link into .git": {
			setup: func(f *fixture) { f.symlink("../.git/config", "out/x.md") },
			want:  []string{"out/x.md", "into .git"},
		},
		"link to an inside path Allowed refuses": {
			setup: func(f *fixture) { f.symlink("../src/main.go", "out/x.md") },
			want:  []string{`symlink "out/x.md" resolves to "src/main.go", which is not allowed`},
		},
		"link inside the work tree": {
			setup: func(f *fixture) { f.symlink("keep.md", "out/x.md") },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.setup(f)
			res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
			if tc.want == nil {
				if err != nil || res.Failure != "" || !res.Changed {
					t.Fatalf("Postcheck = %+v, %v; want Changed", res, err)
				}
				return
			}
			f.assertNothingLanded(res, err, tc.want...)
		})
	}
}

// outsideFile writes a file name in dir, a directory outside the work tree, and returns its
// path: a link target that exists on every OS, unlike /etc/hostname, which macOS lacks.
func outsideFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) symlink(target, rel string) {
	f.t.Helper()
	p := filepath.Join(f.work, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		f.t.Fatal(err)
	}
}

// (h) Review focus 4. Fails if a deletion (porcelain D) is dropped from the changed set: Check
// would not run on a work tree with a missing document, and the deletion would not be committed.
func TestPostcheckDeletionReachesCheck(t *testing.T) {
	requireKeep := func(_ context.Context, _ scheduled.Env, dir string) error {
		if _, err := os.Stat(filepath.Join(dir, "out/keep.md")); err != nil {
			return errors.New("required document keep.md is missing")
		}
		return nil
	}
	t.Run("required", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Remove(filepath.Join(f.work, "out/keep.md")); err != nil {
			t.Fatal(err)
		}
		job := &scheduledtest.Job{AllowedPre: []string{"out/"}, CheckFn: requireKeep}
		res, err := f.postcheck(job)
		f.assertNothingLanded(res, err, "required document keep.md is missing")
	})
	t.Run("not required", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Remove(filepath.Join(f.work, "out/sub/doc.md")); err != nil {
			t.Fatal(err)
		}
		res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}, CheckFn: requireKeep})
		if err != nil || res.Failure != "" || !res.Changed {
			t.Fatalf("Postcheck = %+v, %v; want Changed", res, err)
		}
		if got := f.committed(); !reflect.DeepEqual(got, []string{"out/sub/doc.md"}) {
			t.Errorf("committed %v, want the deletion", got)
		}
		if out := f.git("ls-tree", "-r", "--name-only", "HEAD", "out/sub"); out != "" {
			t.Errorf("out/sub/doc.md still in HEAD: %q", out)
		}
	})
}

// (i) Fails if a second Postcheck after a successful push commits or pushes again instead of
// resuming.
func TestPostcheckResumesAPushedBranch(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "new\n")
	job := &scheduledtest.Job{AllowedPre: []string{"out/"}}
	if res, err := f.postcheck(job); err != nil || !res.Changed {
		t.Fatalf("first Postcheck = %+v, %v", res, err)
	}
	head := f.git("rev-parse", "HEAD")
	res, err := f.postcheck(job)
	if err != nil || res.Failure != "" || !res.Changed {
		t.Fatalf("second Postcheck = %+v, %v; want Changed", res, err)
	}
	if f.pushes != 1 {
		t.Errorf("push called %d times, want 1", f.pushes)
	}
	if got := f.git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want the pushed commit %s", got, head)
	}
}

// Fails if resuming trusts any commit that is on origin at HEAD: an agent that commits and pushes
// itself must still meet the containment, Check and the secret scan.
func TestPostcheckResumeVerifiesTheAgentsPushedCommit(t *testing.T) {
	cases := map[string]struct {
		path, body string
		checkErr   error
		want       []string
	}{
		"disallowed path": {path: "src/evil.go", body: "package main\n", want: []string{"src/evil.go", "not allowed"}},
		"secret":          {path: "out/a.md", body: "sk_live_abc123\n", want: []string{"secret scan", `"out/a.md":1: Stripe live key`}},
		"check fails":     {path: "out/a.md", body: "x\n", checkErr: errors.New("too short"), want: []string{"check", "too short"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write(tc.path, tc.body)
			f.git("add", tc.path)
			f.git("commit", "-q", "-m", "agent commit")
			f.git("push", "-q", "origin", "HEAD:refs/heads/"+testBranch)
			res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}, CheckErr: tc.checkErr})
			if err != nil {
				t.Fatalf("Postcheck error = %v, want a Failure", err)
			}
			for _, frag := range tc.want {
				if !strings.Contains(res.Failure, frag) {
					t.Errorf("Failure = %q, want it to contain %q", res.Failure, frag)
				}
			}
			if f.pushes != 0 {
				t.Errorf("push called %d times", f.pushes)
			}
		})
	}
}

// Fails if the branch already on origin at another commit than HEAD is overwritten or reused.
func TestPostcheckResumeRefusesAnotherCommitOnOrigin(t *testing.T) {
	f := newFixture(t)
	f.write("out/other.md", "other\n")
	f.git("add", "out/other.md")
	f.git("commit", "-q", "-m", "elsewhere")
	f.git("push", "-q", "origin", "HEAD:refs/heads/"+testBranch)
	f.git("reset", "-q", "--hard", f.start)
	f.write("out/a.md", "new\n")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	if err != nil {
		t.Fatalf("Postcheck error = %v", err)
	}
	if !strings.Contains(res.Failure, "already on origin at another commit") || f.pushes != 0 {
		t.Errorf("Failure = %q, pushes = %d", res.Failure, f.pushes)
	}
}

// Fails if Postcheck lets git run a hook or an fsmonitor command from the target repository's
// git dir: both are code the agent could have planted.
func TestPostcheckRunsNoRepositoryCode(t *testing.T) {
	f := newFixture(t)
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\necho ran >> " + marker + "\n"
	gitDir := f.git("rev-parse", "--absolute-git-dir")
	for _, hook := range []string{"pre-commit", "commit-msg", "post-commit", "post-checkout"} {
		if err := os.WriteFile(filepath.Join(gitDir, "hooks", hook), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	monitor := filepath.Join(t.TempDir(), "fsmonitor")
	if err := os.WriteFile(monitor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	f.git("config", "core.fsmonitor", monitor)
	f.write("out/a.md", "new\n")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	if err != nil || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v", res, err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Errorf("repository code ran during Postcheck: %q", b)
	}
}

// Fails if Postcheck cannot commit in a repository without a committer identity.
func TestPostcheckCommitsWithoutAnIdentity(t *testing.T) {
	f := newFixture(t)
	f.git("config", "--unset", "user.name")
	f.git("config", "--unset", "user.email")
	f.write("out/a.md", "new\n")
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	if err != nil || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v", res, err)
	}
	if got := f.git("log", "-1", "--format=%an <%ae>"); got != "Lighthouse <lighthouse@localhost>" {
		t.Errorf("author = %q", got)
	}
}

// Fails if a malformed start SHA or branch reaches git, where "-…" would be read as an option.
func TestPostcheckRejectsMalformedArguments(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "new\n")
	job := &scheduledtest.Job{AllowedPre: []string{"out/"}}
	env := scheduled.Env{Dir: f.work, Run: workplace.ExecRunner()}
	cases := map[string]struct{ start, branch, want string }{
		"option as start": {start: "--hard", branch: testBranch, want: "invalid start SHA"},
		"empty start":     {start: "", branch: testBranch, want: "invalid start SHA"},
		"option branch":   {start: f.start, branch: "-x", want: "invalid branch"},
		"branch with ..":  {start: f.start, branch: "a..b", want: "invalid branch"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := scheduled.Postcheck(context.Background(), env.Run, job, env, scheduled.Verdict{}, tc.start, tc.branch, f.remote())
			if err != nil || !strings.Contains(res.Failure, tc.want) {
				t.Errorf("Postcheck = %+v, %v; want Failure %q", res, err, tc.want)
			}
		})
	}
	if f.pushes != 0 || len(job.Calls()) != 0 {
		t.Errorf("pushes = %d, calls = %v", f.pushes, job.Calls())
	}
}

// spyRunner runs git for real and, right after `git add`, also stages extra: a stand-in for
// anything that changes the index between the checks and the commit.
func spyRunner(t *testing.T, dir, extra string) workplace.Runner {
	real := workplace.ExecRunner()
	return func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
		out, code, err := real(ctx, timeout, name, args...)
		if slices.Contains(args, "add") {
			p := filepath.Join(dir, extra)
			if werr := os.WriteFile(p, []byte("extra\n"), 0o644); werr != nil {
				t.Error(werr)
			}
			runGit(t, dir, "add", extra)
		}
		return out, code, err
	}
}

// Fails if the commit is made from whatever the index holds instead of exactly the checked set,
// or if such a mismatch is a transient error instead of a Failure. The extra path is Allowed and
// clean, so only the set comparison can catch it.
func TestPostcheckCommitsOnlyTheCheckedSet(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "new\n")
	env := scheduled.Env{Dir: f.work, Run: spyRunner(t, f.work, "out/extra.md")}
	res, err := scheduled.Postcheck(context.Background(), env.Run, &scheduledtest.Job{AllowedPre: []string{"out/"}}, env, scheduled.Verdict{}, f.start, testBranch, f.remote())
	f.assertNothingLanded(res, err, `staged paths differ from the checked set: "out/extra.md"`)
}

// Fails if the secret scan reads the work tree instead of the blobs git stores: a
// working-tree-encoding attribute makes git convert UTF-16 on disk to UTF-8 in the commit.
func TestPostcheckScansWhatGitStores(t *testing.T) {
	f := newFixture(t)
	f.write("out/.gitattributes", "*.md working-tree-encoding=UTF-16LE\n")
	var utf16 []byte
	for _, r := range "token sk_live_abc123\n" {
		utf16 = append(utf16, byte(r), 0)
	}
	f.write("out/a.md", string(utf16))
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	f.assertNothingLanded(res, err, "secret scan", `"out/a.md":1: Stripe live key`)
}

// Fails if a token in a file name is not caught, or if it reaches the Failure: an Allowed name
// fails on the scan, a refused name on the containment, and neither echoes the token.
func TestPostcheckTokenInAFileName(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	cases := map[string]struct{ path, want string }{
		"allowed name":     {path: "out/" + token + ".md", want: "secret scan: file name contains a GitHub personal token"},
		"not allowed name": {path: "src/" + token + ".go", want: "containment: path (file name withheld: GitHub personal token) is not allowed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write(tc.path, "harmless\n")
			res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
			f.assertNothingLanded(res, err, tc.want)
			if strings.Contains(res.Failure, token) {
				t.Errorf("Failure echoes the token: %q", res.Failure)
			}
		})
	}
}

// pushAgentCommit commits what change does as the agent and pushes it to the run branch.
func (f *fixture) pushAgentCommit(change func()) {
	f.t.Helper()
	change()
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "agent commit")
	f.git("push", "-q", "origin", "HEAD:refs/heads/"+testBranch)
}

// Fails if resuming reads the work tree instead of the pushed commit: after the push the agent
// hides the commit's content behind a harmless work tree, with an index flag that silences git
// status (assume-unchanged, skip-worktree) or without one.
func TestPostcheckResumeChecksThePushedCommit(t *testing.T) {
	secret := func(f *fixture) { f.write("out/a.md", "sk_live_abc123\n") }
	link := func(f *fixture) { f.symlink("/etc/hostname", "out/a.md") }
	harmless := func(f *fixture) {
		if err := os.Remove(filepath.Join(f.work, "out/a.md")); err != nil {
			f.t.Fatal(err)
		}
		f.write("out/a.md", "harmless\n")
	}
	const hiddenMsg = `resume: "out/a.md" marked assume-unchanged or skip-worktree`
	cases := map[string]struct {
		commit func(f *fixture)
		flag   string
		want   string
	}{
		"secret, assume-unchanged":  {commit: secret, flag: "--assume-unchanged", want: hiddenMsg},
		"secret, skip-worktree":     {commit: secret, flag: "--skip-worktree", want: hiddenMsg},
		"symlink, assume-unchanged": {commit: link, flag: "--assume-unchanged", want: hiddenMsg},
		"symlink, skip-worktree":    {commit: link, flag: "--skip-worktree", want: hiddenMsg},
		"secret, no flag":           {commit: secret, want: `secret scan: "out/a.md":1: Stripe live key`},
		"symlink, no flag":          {commit: link, want: `symlink "out/a.md" targets a path outside the work tree`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.pushAgentCommit(func() { tc.commit(f) })
			if tc.flag != "" {
				f.git("update-index", tc.flag, "out/a.md")
			}
			harmless(f)
			if tc.flag != "" {
				if st := f.git("status", "--porcelain"); st != "" {
					t.Fatalf("setup: git status not silenced: %q", st)
				}
			}
			res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
			if err != nil || !strings.Contains(res.Failure, tc.want) || res.Changed || f.pushes != 0 {
				t.Errorf("Postcheck = %+v, %v (pushes %d); want Failure %q", res, err, f.pushes, tc.want)
			}
		})
	}
}

// Fails if a resumed commit's symlink may target an inside path Allowed refuses, or if a gitlink
// (submodule) in the commit passes.
func TestPostcheckResumeObjectRules(t *testing.T) {
	cases := map[string]struct {
		commit func(f *fixture)
		want   string
	}{
		"symlink to a refused path": {
			commit: func(f *fixture) { f.symlink("../src/main.go", "out/x.md") },
			want:   `symlink "out/x.md" targets "src/main.go", which is not allowed`,
		},
		"symlink into .git": {
			commit: func(f *fixture) { f.symlink("../.git/config", "out/x.md") },
			want:   `symlink "out/x.md" targets .git`,
		},
		"submodule": {
			commit: func(f *fixture) {
				// An empty directory keeps `git add -A` from staging the gitlink's removal.
				if err := os.MkdirAll(filepath.Join(f.work, "out/mod"), 0o755); err != nil {
					t.Fatal(err)
				}
				f.git("update-index", "--add", "--cacheinfo", "160000,"+f.start+",out/mod")
			},
			want: `"out/mod" is a submodule`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.pushAgentCommit(func() { tc.commit(f) })
			res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
			if err != nil || !strings.Contains(res.Failure, tc.want) || f.pushes != 0 {
				t.Errorf("Postcheck = %+v, %v (pushes %d); want Failure %q", res, err, f.pushes, tc.want)
			}
		})
	}
}

// Fails if Check on resume runs on the work tree instead of a checkout of the pushed commit: the
// commit deletes a required document that the work tree still has.
func TestPostcheckResumeChecksTheCommitNotTheWorkTree(t *testing.T) {
	f := newFixture(t)
	f.pushAgentCommit(func() {
		if err := os.Remove(filepath.Join(f.work, "out/keep.md")); err != nil {
			t.Fatal(err)
		}
	})
	f.write("out/keep.md", "keep\n")
	var checked string
	job := &scheduledtest.Job{
		AllowedPre: []string{"out/"},
		CheckFn: func(_ context.Context, env scheduled.Env, dir string) error {
			checked = env.Dir
			if _, err := os.Stat(filepath.Join(dir, "out/keep.md")); err != nil {
				return errors.New("required document keep.md is missing")
			}
			return nil
		},
	}
	res, err := f.postcheck(job)
	if err != nil || !strings.Contains(res.Failure, "check: required document keep.md is missing") {
		t.Errorf("Postcheck = %+v, %v; want the Check Failure", res, err)
	}
	if checked == f.work {
		t.Error("Check got the workplace as env.Dir, want the checkout of the commit")
	}
	if out := f.git("worktree", "list", "--porcelain"); strings.Count(out, "worktree ") != 1 {
		t.Errorf("temporary checkout left registered:\n%s", out)
	}
}

// Review round 2, A. Fails if resuming judges symlinks only by their text: a chain whose `..`
// follows another symlink resolves lexically inside out/ but on disk outside the tree, and must
// be refused on the checkout before Check follows it.
func TestPostcheckResumeSymlinkChain(t *testing.T) {
	f := newFixture(t)
	f.pushAgentCommit(func() {
		f.write("out/a/b/real.md", "x\n")
		f.symlink("..", "out/a/b/d")           // lexically out/a
		f.symlink("d/../../../x", "out/a/b/l") // lexically out/x; on disk outside the tree
	})
	job := &scheduledtest.Job{AllowedPre: []string{"out/"}}
	res, err := f.postcheck(job)
	if err != nil || !strings.Contains(res.Failure, `containment: symlink "out/a/b/l"`) || res.Changed {
		t.Errorf("Postcheck = %+v, %v; want a containment Failure on out/a/b/l", res, err)
	}
	if slices.Contains(job.Calls(), "Check") {
		t.Error("Check ran on a checkout with an escaping symlink")
	}
}

// Review round 2, A. Fails if resuming trusts a symlink that already existed at start: a new
// link through it leaves the tree although its text stays under out/.
func TestPostcheckResumeLinkThroughAStartSymlink(t *testing.T) {
	f := newFixture(t)
	ext := t.TempDir()
	outsideFile(t, ext, "hostname")
	f.symlink(ext, "out/ext")
	f.git("add", "out/ext")
	f.git("commit", "-q", "-m", "base link")
	f.start = f.git("rev-parse", "HEAD")
	f.pushAgentCommit(func() { f.symlink("ext/hostname", "out/x.md") })
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	if err != nil || !strings.Contains(res.Failure, `containment: symlink "out/x.md" points outside the work tree`) {
		t.Errorf("Postcheck = %+v, %v; want the outside Failure", res, err)
	}
}

// Review round 2, B. Fails if a file system error carries the raw path (with a token in a
// directory name, or the absolute workplace path) into the Failure.
func TestPostcheckWithholdsAPathInAnInspectError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permission this test needs")
	}
	f := newFixture(t)
	token := "ghp_" + strings.Repeat("a", 36)
	f.write("out/"+token+"/f.md", "x\n")
	dir := filepath.Join(f.work, "out", token)
	if err := os.Chmod(dir, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}})
	f.assertNothingLanded(res, err, "containment: cannot inspect (file name withheld: GitHub personal token): permission denied")
	if strings.Contains(res.Failure, token) || strings.Contains(res.Failure, f.work) {
		t.Errorf("Failure echoes the raw path: %q", res.Failure)
	}
}

// Review round 2, B. Fails if text from a job or the push function reaches the Failure or the
// error with a secret or a line break in it, or if the error loses what it wraps.
func TestPostcheckRedactsJobAndPushText(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	t.Run("check error", func(t *testing.T) {
		f := newFixture(t)
		f.write("out/a.md", "x\n")
		res, err := f.postcheck(&scheduledtest.Job{AllowedPre: []string{"out/"}, CheckErr: errors.New("bad " + token + "\nforged line")})
		f.assertNothingLanded(res, err, `check: bad (withheld: GitHub personal token)\nforged line`)
		if strings.Contains(res.Failure, token) || strings.Contains(res.Failure, "\n") {
			t.Errorf("Failure = %q", res.Failure)
		}
	})
	t.Run("push error", func(t *testing.T) {
		f := newFixture(t)
		f.write("out/a.md", "x\n")
		sentinel := errors.New("rejected " + token)
		f.pushErr = sentinel
		env := scheduled.Env{Dir: f.work, Run: workplace.ExecRunner()}
		_, err := scheduled.Postcheck(context.Background(), env.Run, &scheduledtest.Job{AllowedPre: []string{"out/"}}, env, scheduled.Verdict{}, f.start, testBranch,
			f.remote())
		if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "rejected (withheld: GitHub personal token)") {
			t.Errorf("error = %v", err)
		}
		if !errors.Is(err, sentinel) {
			t.Error("error no longer wraps the push error")
		}
	})
}

// Finding F of the task 9 review. Fails if Postcheck does not report the commit it pushed, or
// on resume the commit already on the remote: the tick binds the merge to that commit, so a later
// foreign push to the branch is never merged.
func TestPostcheckReportsThePushedCommit(t *testing.T) {
	f := newFixture(t)
	f.write("out/a.md", "new\n")
	job := &scheduledtest.Job{AllowedPre: []string{"out/"}}
	res, err := f.postcheck(job)
	if err != nil || !res.Changed {
		t.Fatalf("Postcheck = %+v, %v", res, err)
	}
	if res.Commit == "" || res.Commit != f.originBranch() {
		t.Errorf("Commit = %q, want the pushed %q", res.Commit, f.originBranch())
	}
	again, err := f.postcheck(job)
	if err != nil || !again.Changed || again.Commit != res.Commit {
		t.Errorf("resumed Postcheck = %+v, %v; want Commit %q", again, err, res.Commit)
	}
	f.write("out/b.md", "x\n")
	none, _ := scheduled.Postcheck(context.Background(), workplace.ExecRunner(), job,
		scheduled.Env{Dir: f.work, Run: workplace.ExecRunner()}, scheduled.Verdict{}, f.start, "a..b", f.remote())
	if none.Commit != "" {
		t.Errorf("a Failure carries Commit %q", none.Commit)
	}
}
