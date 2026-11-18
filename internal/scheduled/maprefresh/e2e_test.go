package maprefresh

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: io/filesystem (real git, a bare origin, the real Postcheck). The job runs the
// way a tick runs it: gate, launch, stage, a fake agent that writes the seven documents,
// then the check after the run. These tests fail if the job and Postcheck disagree on what is
// allowed, complete or unchanged: a refreshed map must reach the branch with its mirror and index
// and a run that changed nothing must end without a commit.

const e2eBranch = "scheduled/map-20261118T0400-1"

// bareRemote is a scheduled.Remote on the bare origin by path.
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

// startRun does what the tick does before the agent: gate, launch, a workplace on the branch.
func startRun(t *testing.T, r *rig, j *Job) (scheduled.Env, scheduled.Verdict, string) {
	t.Helper()
	ctx := context.Background()
	env := r.env()
	v, err := j.Gate(ctx, env)
	if err != nil || v.Skip != "" {
		t.Fatalf("Gate = %+v, %v; want a run", v, err)
	}
	if _, err := j.Launch(v, env); err != nil {
		t.Fatal(err)
	}
	dir, start := r.workplace()
	env.Dir = dir
	if err := j.Stage(ctx, env, v); err != nil {
		t.Fatal(err)
	}
	return env, v, start
}

func TestARefreshedMapReachesTheBranchWithItsMirrorAndIndex(t *testing.T) {
	r := newRig(t)
	r.mapIt()
	r.churn(4)
	j := bind(t, small)
	env, v, start := startRun(t, r, j)
	for _, n := range testDocs { // the fake agent
		put(t, env.Dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, e2eBranch, bareRemote{r.origin})
	if err != nil || res.Failure != "" || !res.Changed || res.Commit == "" {
		t.Fatalf("Postcheck = %+v, %v; want a pushed commit", res, err)
	}
	files := strings.Fields(gitIn(t, r.origin, "ls-tree", "-r", "--name-only", res.Commit))
	for _, n := range testDocs {
		for _, p := range []string{".planning/codebase/" + n + ".md", "docs/map/" + n + ".md"} {
			if !slices.Contains(files, p) {
				t.Errorf("%s is not in the pushed commit", p)
			}
		}
	}
	if !slices.Contains(files, "docs/map/index.md") {
		t.Error("docs/map/index.md is not in the pushed commit")
	}
	if idx := gitIn(t, r.origin, "show", res.Commit+":docs/map/index.md"); !strings.Contains(idx, "**fast**") {
		t.Errorf("index.md:\n%s\nwant mode fast", idx)
	}
}

func TestAnIncompleteMapFailsTheRunAndPushesNothing(t *testing.T) {
	r := newRig(t)
	r.mapIt()
	r.churn(4)
	j := bind(t, small)
	env, v, start := startRun(t, r, j)
	for _, n := range testDocs[:5] { // the agent stops short: two documents missing
		put(t, env.Dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	must(t, os.Remove(filepath.Join(env.Dir, ".planning/codebase/TESTING.md")))
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, e2eBranch, bareRemote{r.origin})
	if err != nil || res.Changed || !strings.Contains(res.Failure, "TESTING (missing)") {
		t.Fatalf("Postcheck = %+v, %v; want a failure naming TESTING", res, err)
	}
	if head, _ := (bareRemote{r.origin}).Head(context.Background(), "", e2eBranch); head != "" {
		t.Errorf("branch on origin at %s, want nothing pushed", head)
	}
}

func TestARunThatChangedNothingEndsWithoutACommit(t *testing.T) {
	// The agent rewrites the committed documents byte for byte; nothing is pushed and the run
	// ends unchanged (done without a PR).
	r := newRig(t)
	r.mapIt()
	r.churn(4)
	j := bind(t, small)
	env, v, start := startRun(t, r, j)
	for _, n := range testDocs {
		put(t, env.Dir, ".planning/codebase/"+n+".md", doc(n))
	}
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, e2eBranch, bareRemote{r.origin})
	if err != nil || res.Failure != "" || res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want unchanged", res, err)
	}
	if head, _ := (bareRemote{r.origin}).Head(context.Background(), "", e2eBranch); head != "" {
		t.Errorf("branch on origin at %s, want nothing pushed", head)
	}
}

func TestASymlinkLeftByTheAgentNeverReachesTheBranch(t *testing.T) {
	// The agent replaces a document by a link to a file outside the work tree: Cleanup unlinks it,
	// Check then fails the run for the missing document, and nothing is pushed.
	r := newRig(t)
	r.mapIt()
	r.churn(4)
	j := bind(t, small)
	env, v, start := startRun(t, r, j)
	for _, n := range testDocs {
		put(t, env.Dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	outside := filepath.Join(t.TempDir(), "x.md")
	put(t, filepath.Dir(outside), "x.md", strings.Repeat("l\n", 40))
	p := filepath.Join(env.Dir, ".planning/codebase/STACK.md")
	must(t, os.Remove(p))
	must(t, os.Symlink(outside, p))
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, e2eBranch, bareRemote{r.origin})
	if err != nil || res.Changed || !strings.Contains(res.Failure, "STACK (missing)") {
		t.Fatalf("Postcheck = %+v, %v; want a failure for the missing STACK", res, err)
	}
}
