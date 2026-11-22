package mapplanned

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: io/filesystem (real git, a bare origin, the fake forge, the real Postcheck).
// The job runs the way a tick runs it: gate, launch, stage, a fake agent that writes the
// result file or a document, then the check after the run. These tests fail if the job and
// Postcheck disagree on what is allowed or complete: questions are posted without a commit, a
// placed item reaches the branch with the fragment but without the staging files, an edited
// corpus grants nothing, and a resumed run passes on its pushed commit with a fresh instance.

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

// startRun does what the tick does before the agent: gate, launch, a workplace on the branch,
// stage. The rig has the loop row, changelog.d/ and open issue 12 on origin.
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

func e2eRig(t *testing.T) *rig {
	t.Helper()
	r := loopRig(t, "[]")
	put(t, r.repo, "changelog.d/keep.md", "---\ntype: internal\n---\nx\n")
	r.commit()
	return r
}

func TestARunWithOnlyQuestionsEndsWithoutACommit(t *testing.T) {
	// Fails if Cleanup does not post (a run without a document change never reaches Finish),
	// or leaves a staging file that turns the run into a containment failure.
	r := e2eRig(t)
	j := bind(t)
	env, v, start := startRun(t, r, j)
	writeResult(t, env.Dir, `{"questions":[`+placeQ12+`]}`)
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, runBranch, bareRemote{r.origin})
	if err != nil || res.Failure != "" || res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want unchanged (done without a PR)", res, err)
	}
	if q := r.posts(12); len(q) != 1 {
		t.Errorf("posts on #12 = %d, want 1", len(q))
	}
	if head, _ := (bareRemote{r.origin}).Head(context.Background(), "", runBranch); head != "" {
		t.Errorf("branch on origin at %s, want nothing pushed", head)
	}
}

func TestARunReachesTheBranchWithTheDocument(t *testing.T) {
	// Fails if the placed item does not reach the branch, if the fragment is missing, or if the
	// corpus or result file is committed.
	r := e2eRig(t)
	j := bind(t)
	env, v, start := startRun(t, r, j)
	put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 12}]`)
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, runBranch, bareRemote{r.origin})
	if err != nil || res.Failure != "" || !res.Changed || res.Commit == "" {
		t.Fatalf("Postcheck = %+v, %v; want a pushed commit", res, err)
	}
	got := strings.Fields(gitOut(t, r.origin, "diff-tree", "-r", "--no-commit-id", "--name-only", res.Commit))
	slices.Sort(got)
	want := []string{"changelog.d/scheduled-map-planned-20261122T0500-1.md", "docs/site/data/loop.json"}
	if !slices.Equal(got, want) {
		t.Errorf("commit changes %v, want %v", got, want)
	}
}

func TestAnEditedCorpusGrantsNothing(t *testing.T) {
	// Fails if Check takes the corpus as authority: the agent adds #99 to the corpus and puts
	// it on the document, but #99 does not carry the row's label on the forge.
	r := e2eRig(t)
	r.issue(99, "other", "")
	j := bind(t)
	env, v, start := startRun(t, r, j)
	put(t, env.Dir, corpusPath, `{"issues":[{"number":99,"label":"loop","document":"docs/site/data/loop.json"}]}`)
	put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 12}, {"issue": 99}]`)
	res, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, runBranch, bareRemote{r.origin})
	if err != nil || res.Changed || !strings.Contains(res.Failure, "#99") {
		t.Fatalf("Postcheck = %+v, %v; want a failure naming #99", res, err)
	}
}

func TestAResumedRunPassesCheckOnItsPushedCommit(t *testing.T) {
	// After a push whose result was not recorded, the next tick binds a fresh instance and
	// checks the pushed commit in a clean, detached checkout. Fails if Check needs state from
	// Stage or the first Postcheck, compares with HEAD instead of the merge-base, or refuses the
	// fragment Finish wrote.
	r := e2eRig(t)
	j := bind(t)
	env, v, start := startRun(t, r, j)
	put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 12}]`)
	remote := bareRemote{r.origin}
	first, err := scheduled.Postcheck(context.Background(), env.Run, j, env, v, start, runBranch, remote)
	if err != nil || first.Failure != "" || !first.Changed {
		t.Fatalf("first Postcheck = %+v, %v", first, err)
	}
	again, err := scheduled.Postcheck(context.Background(), env.Run, bind(t), env, v, start, runBranch, remote)
	if err != nil || again.Failure != "" || !again.Changed || again.Commit != first.Commit {
		t.Fatalf("resume Postcheck = %+v, %v; want the same pushed commit, no failure", again, err)
	}
}
