package workflowguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// repo is a clone of a bare origin: main has one commit, and branch issue-1-x has one commit of
// its own, both pushed, so origin/issue-1-x is the base of the pushed range.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	r := &repo{t: t, dir: filepath.Join(tmp, "work")}
	bare := filepath.Join(tmp, "origin.git")
	r.run(tmp, "init", "-q", "--bare", "-b", "main", bare)
	r.run(tmp, "clone", "-q", bare, r.dir)
	r.git("checkout", "-q", "-b", "main")
	r.commit("README", "readme\n")
	r.git("push", "-q", "origin", "main")
	r.git("checkout", "-q", "-b", "issue-1-x")
	r.commit("a.txt", "a\n")
	r.git("push", "-q", "-u", "origin", "issue-1-x")
	return r
}

func (r *repo) run(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) git(args ...string) string { r.t.Helper(); return r.run(r.dir, args...) }

// commit writes name (directories included) and commits it.
func (r *repo) commit(name, text string) {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.git("add", "--", name)
	r.git("commit", "-q", "-m", "write "+name)
}

func (r *repo) remove(name string) {
	r.t.Helper()
	r.git("rm", "-q", "--", name)
	r.git("commit", "-q", "-m", "remove "+name)
}

// onMain commits files on main and pushes it, then returns to issue-1-x.
func (r *repo) onMain(files map[string]string) {
	r.t.Helper()
	r.git("checkout", "-q", "main")
	for name, text := range files {
		r.commit(name, text)
	}
	r.git("push", "-q", "origin", "main")
	r.git("checkout", "-q", "issue-1-x")
}

func (r *repo) mergeMain() { r.t.Helper(); r.git("merge", "-q", "--no-edit", "origin/main") }

// check runs Check on HEAD of issue-1-x, as workflow-push-gate does for `git push`.
func (r *repo) check() []string {
	r.t.Helper()
	gitFn := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", r.dir}, args...)...).Output()
		return string(out), err
	}
	_, paths, err := Check(gitFn, r.git("rev-parse", "HEAD"), "issue-1-x", "main")
	if err != nil {
		r.t.Fatalf("Check: %v", err)
	}
	return paths
}

func wantPaths(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("touched %q, want %q", got, want)
	}
}

// Commits that are on origin/main already are not the branch's change: a merge that only
// brings main's workflows in touches nothing.
func TestCheckIgnoresWorkflowsAMergeOfMainBringsIn(t *testing.T) {
	r := newRepo(t)
	r.onMain(map[string]string{
		".github/workflows/ci.yml":      "on: push\n",
		".github/workflows/release.yml": "on: workflow_dispatch\n",
	})
	r.mergeMain()
	wantPaths(t, r.check())
}

func TestCheckCountsOwnWorkflowEditBesideAMergeOfMain(t *testing.T) {
	r := newRepo(t)
	r.onMain(map[string]string{
		".github/workflows/ci.yml":      "on: push\n",
		".github/workflows/release.yml": "on: workflow_dispatch\n",
	})
	r.mergeMain()
	r.commit(".github/workflows/ci.yml", "on: pull_request\n")
	wantPaths(t, r.check(), ".github/workflows/ci.yml")
}

// GitHub sees every pushed commit: a workflow the branch adds and removes again counts, also
// when a merge of main follows.
func TestCheckCountsOwnWorkflowAddedAndRemovedBeforeAMergeOfMain(t *testing.T) {
	r := newRepo(t)
	r.commit(".github/workflows/own.yml", "on: push\n")
	r.remove(".github/workflows/own.yml")
	r.onMain(map[string]string{".github/workflows/ci.yml": "on: push\n"})
	r.mergeMain()
	wantPaths(t, r.check(), ".github/workflows/own.yml")
}

// A merge that resolves a workflow conflict to something main does not have is the branch's
// own change.
func TestCheckCountsAWorkflowConflictResolvedUnlikeMain(t *testing.T) {
	r := newRepo(t)
	r.onMain(map[string]string{".github/workflows/ci.yml": "on: push\n"})
	r.commit(".github/workflows/ci.yml", "on: pull_request\n")
	r.git("push", "-q", "origin", "issue-1-x")
	r.onMain(map[string]string{".github/workflows/ci.yml": "on: workflow_dispatch\n"})
	if out, err := exec.Command("git", "-C", r.dir, "merge", "-q", "--no-edit", "origin/main").CombinedOutput(); err == nil {
		t.Fatalf("merge did not conflict:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(r.dir, ".github/workflows/ci.yml"), []byte("on: [push, pull_request]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.git("add", "--", ".github/workflows/ci.yml")
	r.git("commit", "-q", "--no-edit")
	wantPaths(t, r.check(), ".github/workflows/ci.yml")
}

// Without origin/main nothing is known to be main's: every workflow in the range counts.
func TestCheckCountsAMergeOfMainWithoutOriginMain(t *testing.T) {
	r := newRepo(t)
	r.onMain(map[string]string{".github/workflows/ci.yml": "on: push\n"})
	r.git("merge", "-q", "--no-edit", "main")
	r.git("update-ref", "-d", "refs/remotes/origin/main")
	wantPaths(t, r.check(), ".github/workflows/ci.yml")
}
