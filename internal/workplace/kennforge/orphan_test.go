package kennforge

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// An orphan Remove deletes with force=true and runs no git: the worktree is gone, and discardWork
// would read the workspace first. The Runner fails the test on any call.
func TestRemoveOrphanDeletesWithForce(t *testing.T) {
	f, a := newFakeForge(t)
	a.Run = func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		t.Errorf("unexpected command %s %v", name, args)
		return "", 1, nil
	}
	if err := a.Remove(t.Context(), "ws1", workplace.RemoveOptions{Orphan: true}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	const del = "DELETE /api/v1/workspaces/ws1"
	if n := f.count(del); n != 1 {
		t.Fatalf("%s called %d times, want 1", del, n)
	}
	if q := f.body(del + "?"); q != "force=true" {
		t.Errorf("query = %q, want force=true", q)
	}
	if n := f.count("GET /api/v1/workspaces/ws1"); n != 0 {
		t.Errorf("the workspace was read %d times, want 0", n)
	}
}

// A normal Remove does not force.
func TestRemoveWithoutOrphanDoesNotForce(t *testing.T) {
	f, a := newFakeForge(t)
	if err := a.Remove(t.Context(), "ws1", workplace.RemoveOptions{}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if q := f.body("DELETE /api/v1/workspaces/ws1?"); q != "" {
		t.Errorf("query = %q, want none", q)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = []string{"PATH=" + pathEnv(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func pathEnv() string {
	p, _ := exec.LookPath("git")
	return filepath.Dir(p)
}

// orphanFixture is an origin and, under the worktree root of the adapter, one sibling worktree of
// the clone: the only git left once the worktree of the workspace is gone.
func orphanFixture(t *testing.T) (*Adapter, string, string) {
	t.Helper()
	home := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", origin)
	seed := t.TempDir()
	gitIn(t, seed, "init", "-q", "-b", "main")
	gitIn(t, seed, "commit", "-q", "--allow-empty", "-m", "seed")
	gitIn(t, seed, "push", "-q", origin, "main")
	root := WorktreeRoot(home, "github", "github.com", "CorneeldH/lighthouse")
	sibling := filepath.Join(root, "work-other")
	gitIn(t, t.TempDir(), "clone", "-q", origin, sibling)
	a := &Adapter{Kind: "github", Repo: "CorneeldH/lighthouse", Home: home, Run: workplace.ExecRunner()}
	return a, origin, sibling
}

func TestBranchNowhere(t *testing.T) {
	a, origin, sibling := orphanFixture(t)
	ctx := t.Context()
	got, err := a.BranchNowhere(ctx, "ws1", "issue-7-x")
	if err != nil || !got {
		t.Fatalf("branch nowhere: %v, %v; want true", got, err)
	}
	// On origin only.
	gitIn(t, sibling, "push", "-q", origin, "HEAD:refs/heads/issue-7-x")
	if got, err := a.BranchNowhere(ctx, "ws1", "issue-7-x"); err != nil || got {
		t.Errorf("branch on origin: %v, %v; want false", got, err)
	}
	gitIn(t, sibling, "push", "-q", origin, ":refs/heads/issue-7-x")
	// In the clone only.
	gitIn(t, sibling, "branch", "issue-7-x")
	if got, err := a.BranchNowhere(ctx, "ws1", "issue-7-x"); err != nil || got {
		t.Errorf("branch in the clone: %v, %v; want false", got, err)
	}
}

// Without a sibling worktree there is no git to ask: an error, never "nowhere".
func TestBranchNowhereWithoutSiblingIsNotMeasurable(t *testing.T) {
	a := &Adapter{Kind: "github", Repo: "CorneeldH/lighthouse", Home: t.TempDir(), Run: workplace.ExecRunner()}
	got, err := a.BranchNowhere(t.Context(), "ws1", "issue-7-x")
	if err == nil || got || !strings.Contains(err.Error(), "no worktree") {
		t.Errorf("BranchNowhere = %v, %v; want an error naming the missing worktree", got, err)
	}
}

// cloneFixture is a bare clone with one linked worktree under the worktree root, as kenn-forge
// makes them.
func cloneFixture(t *testing.T) (home, clone string) {
	t.Helper()
	home = t.TempDir()
	clone = filepath.Join(t.TempDir(), "lighthouse.git")
	gitIn(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", clone)
	seed := t.TempDir()
	gitIn(t, seed, "init", "-q", "-b", "main")
	gitIn(t, seed, "commit", "-q", "--allow-empty", "-m", "seed")
	gitIn(t, seed, "push", "-q", clone, "main")
	root := WorktreeRoot(home, "github", "github.com", "CorneeldH/lighthouse")
	gitIn(t, clone, "worktree", "add", "-q", "--detach", filepath.Join(root, "work-a"), "main")
	return home, clone
}

func TestCheckCloneBareIsFine(t *testing.T) {
	home, _ := cloneFixture(t)
	got, err := CheckClone(t.Context(), workplace.ExecRunner(), home, "github", "", "CorneeldH/lighthouse")
	if err != nil || got != "" {
		t.Errorf("CheckClone = %q, %v; want no problem", got, err)
	}
}

// The state of #478: core.bare=false and core.worktree set in the shared config. The report names
// the clone, both keys and the two commands that repair it.
func TestCheckCloneReportsNonBare(t *testing.T) {
	home, clone := cloneFixture(t)
	gitIn(t, clone, "config", "core.bare", "false")
	gitIn(t, clone, "config", "core.worktree", "/some/worktree")
	got, err := CheckClone(t.Context(), workplace.ExecRunner(), home, "github", "", "CorneeldH/lighthouse")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{clone, "core.bare=false", "core.worktree=/some/worktree",
		"git -C " + clone + " config --unset core.worktree", "git -C " + clone + " config core.bare true"} {
		if !strings.Contains(got, want) {
			t.Errorf("report %q lacks %q", got, want)
		}
	}
}

// With no worktree under the root there is no clone to look at, and that is no problem.
func TestCheckCloneWithoutWorktreeIsSilent(t *testing.T) {
	got, err := CheckClone(t.Context(), workplace.ExecRunner(), t.TempDir(), "github", "", "CorneeldH/lighthouse")
	if err != nil || got != "" {
		t.Errorf("CheckClone = %q, %v; want silence", got, err)
	}
}

// core.worktree alone, with core.bare still true, is also reported: kenn-forge refuses on it.
func TestCheckCloneReportsWorktreeOnly(t *testing.T) {
	home, clone := cloneFixture(t)
	gitIn(t, clone, "config", "core.worktree", "/some/worktree")
	got, err := CheckClone(t.Context(), workplace.ExecRunner(), home, "github", "", "CorneeldH/lighthouse")
	if err != nil || !strings.Contains(got, "core.worktree=/some/worktree") {
		t.Errorf("CheckClone = %q, %v; want a report of core.worktree", got, err)
	}
}
