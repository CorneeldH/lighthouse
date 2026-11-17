package dispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io (SQLite, a real git workplace with a local bare remote, fake forge and
// workplace). These tests fail if the end of a scheduled run runs the check while its agent may
// still write, runs git in a workplace whose git config changed, pushes or opens a PR other than
// the spec's, records pr_at late, merges without the measured head, leaves a branch or PR behind
// on a failed run, posts a comment, or lets a raw error or the token reach the log.

const (
	finSlot   = "20261031T0400"
	finKey    = "scheduled:codebase-map:" + finSlot
	finBody   = "No issue: geplande run codebase-map (" + finSlot + ")"
	finToken  = "ghp_" + "finishtokenfinishtokenfinishtoken12345"
	finPRHead = "abc1234abc1234abc1234abc1234abc1234abc12"
)

// finishRig is a harness with one open scheduled run of codebase-map on finBranch in a real git
// workplace, its start state recorded, and a remote that pushes to a local bare repository.
type finishRig struct {
	*harness
	job    *scheduledtest.Job
	work   string
	bare   string
	start  string
	runID  int64
	remote *rigRemote
}

// rigRemote is a scheduled.Remote on the bare repository, by its path.
type rigRemote struct {
	bare    string
	pushes  int
	pushErr error
	onPush  func()
}

func (r *rigRemote) Head(ctx context.Context, _, branch string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "ls-remote", r.bare, "refs/heads/"+branch).Output()
	if err != nil {
		return "", err
	}
	if f := strings.Fields(string(out)); len(f) > 0 {
		return f[0], nil
	}
	return "", nil
}

func (r *rigRemote) Push(ctx context.Context, dir, commit, branch string) error {
	r.pushes++
	if r.onPush != nil {
		r.onPush()
	}
	if r.pushErr != nil {
		return r.pushErr
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "push", "-q", r.bare, commit+":refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

func rigGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newFinishRig(t *testing.T) *finishRig {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	h := newHarness(t)
	root := t.TempDir()
	r := &finishRig{harness: h, work: filepath.Join(root, "work"), bare: filepath.Join(root, "origin.git")}
	rigGit(t, root, "init", "-q", "--bare", r.bare)
	rigGit(t, root, "init", "-q", "-b", "main", r.work)
	rigGit(t, r.work, "config", "user.name", "Test")
	rigGit(t, r.work, "config", "user.email", "test@example.com")
	r.write(t, "out/keep.md", "keep\n")
	rigGit(t, r.work, "add", "-A")
	rigGit(t, r.work, "commit", "-q", "-m", "init")
	rigGit(t, r.work, "checkout", "-q", "-b", schedBranch)
	r.remote = &rigRemote{bare: r.bare}
	r.job = &scheduledtest.Job{JobName: "map-refresh", AllowedPre: []string{"out/"}}
	h.env.ScheduledJob = func(repo, schedule string) (scheduled.Job, scheduled.Env, error) {
		if repo != applyRepo || schedule != "codebase-map" {
			return nil, scheduled.Env{}, fmt.Errorf("no schedule %s in %s", schedule, repo)
		}
		return r.job, scheduled.Env{Repo: repoconfig.Repo{Slug: applyRepo},
			Schedule: repoconfig.Schedule{Name: "codebase-map", Job: "map-refresh"}, Now: applyNow,
			Run: workplace.ExecRunner()}, nil
	}
	h.env.Remote = func(repo string) (scheduled.Remote, error) {
		if repo != applyRepo {
			return nil, fmt.Errorf("no remote for %s", repo)
		}
		return r.remote, nil
	}
	h.wp.paths = map[string]string{"ws-1": r.work}

	ctx := context.Background()
	run, err := runs.Take(ctx, h.db, runs.Claim{Repo: applyRepo, Schedule: "codebase-map", SessionID: freshSession,
		Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: finKey, Command: "/gsd-map-codebase",
		Profile: "software", Branch: schedBranch, Now: applyNow.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r.runID = run.ID
	sha, state, err := scheduled.StartState(ctx, workplace.ExecRunner(), r.work)
	if err != nil {
		t.Fatal(err)
	}
	r.start = sha
	rec := scheduled.Record{Verdict: scheduled.Verdict{Mode: "full"}, MaxRuntime: time.Hour, GitState: state}
	for _, err := range []error{
		runs.SetWorkplace(ctx, h.db, run.ID, "ws-1", schedBranch),
		runs.SetStartSHA(ctx, h.db, run.ID, sha),
		runs.SetVerdict(ctx, h.db, run.ID, rec.Encode()),
		runs.SetSession(ctx, h.db, run.ID, "key-1"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *finishRig) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(r.work, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pushed records sha as the commit the run's check pushed.
func (r *finishRig) pushed(t *testing.T, sha string) {
	t.Helper()
	row := r.row(t)
	rec, err := scheduled.DecodeRecord(row.Verdict)
	if err != nil {
		t.Fatal(err)
	}
	rec.Pushed = sha
	if err := runs.SetVerdict(context.Background(), r.db, r.runID, rec.Encode()); err != nil {
		t.Fatal(err)
	}
}

// action is a scheduled action of kind on the rig's run.
func (r *finishRig) action(kind Kind) Action {
	return Action{Kind: kind, Repo: applyRepo, RunID: r.runID, Schedule: "codebase-map", Slot: finSlot,
		Branch: schedBranch, Language: "nl"}
}

// row is the rig's run as the history has it.
func (r *finishRig) row(t *testing.T) runs.Row {
	t.Helper()
	rows := r.history(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
	return rows[0]
}

// remoteBranch is the commit of the run's branch on the bare remote, "" when absent.
func (r *finishRig) remoteBranch(t *testing.T) string {
	t.Helper()
	sha, err := r.remote.Head(context.Background(), "", schedBranch)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// noComment fails when anything was posted: a scheduled run has no issue (extra c).
func (r *finishRig) noComment(t *testing.T) {
	t.Helper()
	if slices.Contains(r.log.all(), "CreateComment") {
		t.Errorf("calls = %q: a scheduled run posted a comment", r.log.all())
	}
}

// Row 1, the happy path. Fails if the agent is not ended before the check, the check does not
// run, the commit does not reach the remote, the PR has another body or title, pr_at is not
// recorded when the PR opens, or a comment is posted.
func TestApplyScheduledFinishOpensThePR(t *testing.T) {
	r := newFinishRig(t)
	r.write(t, "out/map.md", "the map\n")
	var atKill []string
	killed := false
	r.wp.onKill = func() { atKill, killed = r.job.Calls(), true }
	res, err := Apply(context.Background(), r.env, []Action{r.action(KindScheduledFinish)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %v", res.Failed)
	}
	if want := []string{"Kill", "Lookup ws-1", "CreatePR"}; !reflect.DeepEqual(r.log.all(), want) {
		t.Errorf("calls = %q, want %q", r.log.all(), want)
	}
	if !killed || len(atKill) != 0 {
		t.Errorf("job calls at Kill = %q, want none: the agent ends before the check", atKill)
	}
	if want := []string{"Cleanup", "Check", "Finish"}; !reflect.DeepEqual(r.job.Calls(), want) {
		t.Errorf("job calls = %q, want %q", r.job.Calls(), want)
	}
	if got := r.wp.killed; !reflect.DeepEqual(got, []string{"ws-1 " + freshSession}) {
		t.Errorf("killed = %q, want the run's claude", got)
	}
	head := rigGit(t, r.work, "rev-parse", schedBranch)
	if head == r.start || r.remoteBranch(t) != head || r.remote.pushes != 1 {
		t.Errorf("remote branch = %q, pushes = %d; want the commit %q on top of the start", r.remoteBranch(t), r.remote.pushes, head)
	}
	p := r.poster(applyRepo)
	if want := []string{schedBranch + " chore: geplande run codebase-map (" + finSlot + ")"}; !reflect.DeepEqual(p.created, want) {
		t.Errorf("created = %q, want %q", p.created, want)
	}
	if !reflect.DeepEqual(p.prBodies, []string{finBody}) {
		t.Errorf("PR body = %q, want %q", p.prBodies, finBody)
	}
	row := r.row(t)
	if !row.Open || !row.PRAt.Equal(applyNow) {
		t.Errorf("row = open %v, pr_at %v; want open with pr_at %v", row.Open, row.PRAt, applyNow)
	}
	// Finding F: the merge is bound to this commit.
	if rec, err := scheduled.DecodeRecord(row.Verdict); err != nil || rec.Pushed != head || rec.GitState == "" {
		t.Errorf("record = %+v, %v; want pushed %s and the git state kept", rec, err, head)
	}
	r.noComment(t)
}

// Finding 4. Fails if the check runs when the agent could not be ended: something could still
// change the work tree between the check and the commit.
func TestApplyScheduledFinishWaitsForTheKill(t *testing.T) {
	r := newFinishRig(t)
	r.write(t, "out/map.md", "the map\n")
	r.wp.killErr = errors.New("forge unreachable")
	_, err := Apply(context.Background(), r.env, []Action{r.action(KindScheduledFinish)})
	if err == nil || !strings.Contains(err.Error(), "forge unreachable") {
		t.Errorf("Apply = %v, want the kill error", err)
	}
	if len(r.job.Calls()) != 0 || r.remote.pushes != 0 || len(r.poster(applyRepo).created) != 0 {
		t.Errorf("job calls %q, pushes %d, PRs %q after a failed kill; want none", r.job.Calls(), r.remote.pushes, r.poster(applyRepo).created)
	}
	if row := r.row(t); !row.Open || !row.PRAt.IsZero() {
		t.Errorf("row = %+v, want it open without pr_at", row)
	}
}

// Finding 3. Fails if git runs in a workplace whose git config changed since the start: here a
// clean filter on every path would run on `git add` and leave a marker, and the run must end
// failed without a push, its branch deleted.
func TestApplyScheduledFinishRefusesChangedGitConfig(t *testing.T) {
	r := newFinishRig(t)
	r.write(t, "out/map.md", "the map\n")
	marker := filepath.Join(t.TempDir(), "filter-ran")
	rigGit(t, r.work, "config", "filter.x.clean", "touch "+marker+"; cat")
	attrs := filepath.Join(r.work, ".git", "info", "attributes")
	if err := os.MkdirAll(filepath.Dir(attrs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attrs, []byte("* filter=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), r.env, []Action{r.action(KindScheduledFinish)}); err != nil {
		t.Fatalf("Apply = %v, want a closed run and no error", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the planted filter ran")
	}
	if len(r.job.Calls()) != 0 || r.remote.pushes != 0 {
		t.Errorf("job calls %q, pushes %d; want none", r.job.Calls(), r.remote.pushes)
	}
	row := r.row(t)
	if row.Open || row.Outcome != runs.Failed || row.Reason != "git state of the workplace changed since the start" {
		t.Errorf("row = open %v, %s %q; want failed on the git state", row.Open, row.Outcome, row.Reason)
	}
	if p := r.poster(applyRepo); !reflect.DeepEqual(p.deleted, []string{schedBranch}) || len(p.created) != 0 {
		t.Errorf("deleted %q, created %q; want the branch deleted and no PR", p.deleted, p.created)
	}
	r.noComment(t)
}

// Row 1, nothing changed: done without a PR and without a push.
func TestApplyScheduledFinishNothingChanged(t *testing.T) {
	r := newFinishRig(t)
	if _, err := Apply(context.Background(), r.env, []Action{r.action(KindScheduledFinish)}); err != nil {
		t.Fatal(err)
	}
	row := r.row(t)
	if row.Open || row.Outcome != runs.Done {
		t.Errorf("row = open %v, %s; want done", row.Open, row.Outcome)
	}
	if r.remote.pushes != 0 || len(r.poster(applyRepo).created) != 0 {
		t.Errorf("pushes %d, PRs %q; want none", r.remote.pushes, r.poster(applyRepo).created)
	}
	r.noComment(t)
}

// Row 1, the check fails, and finding 5: an agent that pushed the branch itself leaves it on
// the remote, where the resume path refuses it. The run ends failed with the check's reason, the
// branch is deleted and the PR the run had is closed. A failed run is no apply error (finding 9).
func TestApplyScheduledFinishFailureClosesEverything(t *testing.T) {
	r := newFinishRig(t)
	r.write(t, "src/evil.go", "package evil\n")
	rigGit(t, r.work, "add", "-A")
	rigGit(t, r.work, "commit", "-q", "-m", "agent")
	rigGit(t, r.work, "push", "-q", r.bare, "HEAD:refs/heads/"+schedBranch)
	a := r.action(KindScheduledFinish)
	a.PR = 41
	if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
		t.Fatalf("Apply = %v, want no error for a failed run", err)
	}
	row := r.row(t)
	if row.Open || row.Outcome != runs.Failed || !strings.HasPrefix(row.Reason, "postcheck: ") || !strings.Contains(row.Reason, `"src/evil.go"`) {
		t.Errorf("row = open %v, %s %q; want failed with the check's reason", row.Open, row.Outcome, row.Reason)
	}
	p := r.poster(applyRepo)
	if !reflect.DeepEqual(p.closed, []int{41}) || !reflect.DeepEqual(p.deleted, []string{schedBranch}) || len(p.created) != 0 {
		t.Errorf("closed %v, deleted %q, created %q; want PR 41 closed, the branch deleted, no new PR", p.closed, p.deleted, p.created)
	}
	if !strings.Contains(r.out.String(), "run 1 failed: postcheck: ") {
		t.Errorf("log = %q, want the failed line", r.out.String())
	}
	r.noComment(t)
}

// Finding 2. Fails if a transient error of the check reaches the log or Apply's error with its
// raw text (here the token in a push error), or if the run closes or gets pr_at on it.
func TestApplyScheduledFinishTransientErrorIsRedacted(t *testing.T) {
	r := newFinishRig(t)
	r.write(t, "out/map.md", "the map\n")
	r.remote.pushErr = errors.New("rejected for " + finToken)
	res, err := Apply(context.Background(), r.env, []Action{r.action(KindScheduledFinish)})
	if err == nil || !strings.Contains(err.Error(), "postcheck: ") {
		t.Fatalf("Apply = %v, want the check's error", err)
	}
	for name, text := range map[string]string{"error": err.Error(), "failed": fmt.Sprint(res.Failed), "log": r.out.String()} {
		if strings.Contains(text, finToken) {
			t.Errorf("%s holds the token: %q", name, text)
		}
	}
	if chainHolds(err, finToken) {
		t.Error("an error in the chain still holds the raw push text")
	}
	if row := r.row(t); !row.Open || !row.PRAt.IsZero() {
		t.Errorf("row = %+v, want it open without pr_at", row)
	}
}

// A PR the run already has (pr_at was not recorded) is taken, not opened again, but only when its
// head is the commit on the branch (finding F); pr_at follows. A PR on another head is foreign:
// it is closed, the branch deleted, the run failed. A CreatePR that fails leaves pr_at empty and
// the run open.
func TestApplyScheduledFinishPRAt(t *testing.T) {
	// pushedByAnEarlierTick puts the run's checked commit on the remote, as a finish whose
	// CreatePR or pr_at failed leaves it, and returns that commit.
	pushedByAnEarlierTick := func(t *testing.T, r *finishRig) string {
		t.Helper()
		r.write(t, "out/map.md", "the map\n")
		rigGit(t, r.work, "add", "-A")
		rigGit(t, r.work, "commit", "-q", "-m", "map")
		rigGit(t, r.work, "push", "-q", r.bare, "HEAD:refs/heads/"+schedBranch)
		return rigGit(t, r.work, "rev-parse", "HEAD")
	}
	t.Run("existing PR on the checked commit", func(t *testing.T) {
		r := newFinishRig(t)
		head := pushedByAnEarlierTick(t, r)
		a := r.action(KindScheduledFinish)
		a.PR, a.HeadSHA = 41, head
		if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
			t.Fatal(err)
		}
		if p := r.poster(applyRepo); slices.Contains(r.log.all(), "CreatePR") || len(p.closed) != 0 {
			t.Errorf("calls = %q, want no new PR", r.log.all())
		}
		row := r.row(t)
		if !row.Open || !row.PRAt.Equal(applyNow) {
			t.Errorf("row pr_at = %v, want %v", row.PRAt, applyNow)
		}
		if rec, err := scheduled.DecodeRecord(row.Verdict); err != nil || rec.Pushed != head {
			t.Errorf("record = %+v, %v; want pushed %s", rec, err, head)
		}
	})
	t.Run("existing PR on another head", func(t *testing.T) {
		r := newFinishRig(t)
		pushedByAnEarlierTick(t, r)
		a := r.action(KindScheduledFinish)
		a.PR, a.HeadSHA = 41, finPRHead
		if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
			t.Fatalf("Apply = %v, want a failed run and no error", err)
		}
		p := r.poster(applyRepo)
		if slices.Contains(r.log.all(), "CreatePR") || !reflect.DeepEqual(p.closed, []int{41}) || !reflect.DeepEqual(p.deleted, []string{schedBranch}) {
			t.Errorf("calls = %q, closed %v, deleted %q; want the foreign PR closed and the branch deleted", r.log.all(), p.closed, p.deleted)
		}
		if row := r.row(t); row.Open || row.Outcome != runs.Failed || row.Reason != "PR #41 is not on the checked commit" || !row.PRAt.IsZero() {
			t.Errorf("row = open %v, %s %q, pr_at %v", row.Open, row.Outcome, row.Reason, row.PRAt)
		}
	})
	t.Run("CreatePR fails", func(t *testing.T) {
		r := newFinishRig(t)
		r.write(t, "out/map.md", "the map\n")
		r.poster(applyRepo).createErr = errors.New("422")
		if _, err := Apply(context.Background(), r.env, []Action{r.action(KindScheduledFinish)}); err == nil {
			t.Error("Apply = nil, want the CreatePR error")
		}
		if row := r.row(t); !row.Open || !row.PRAt.IsZero() {
			t.Errorf("row = %+v, want it open without pr_at", row)
		}
	})
}

// Row 2. Fails if the merge is not on the measured head, the branch is not deleted after it, or
// the run does not end done.
func TestApplyScheduledMerge(t *testing.T) {
	r := newFinishRig(t)
	r.pushed(t, finPRHead)
	a := r.action(KindScheduledMerge)
	a.PR, a.HeadSHA = 41, finPRHead
	if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
		t.Fatal(err)
	}
	p := r.poster(applyRepo)
	if want := []string{"MergePR", "DeleteBranch"}; !reflect.DeepEqual(r.log.all(), want) {
		t.Errorf("calls = %q, want %q", r.log.all(), want)
	}
	if !reflect.DeepEqual(p.merges, []string{"41 " + finPRHead}) || !reflect.DeepEqual(p.deleted, []string{schedBranch}) {
		t.Errorf("merges %q, deleted %q", p.merges, p.deleted)
	}
	if row := r.row(t); row.Open || row.Outcome != runs.Done {
		t.Errorf("row = open %v, %s; want done", row.Open, row.Outcome)
	}
}

// Rows 3 and 4. A refused merge closes the PR, deletes the branch and fails the run with the
// forge's reason; a moved head does nothing but defer, so the next tick measures again.
func TestApplyScheduledMergeRefusedOrMoved(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		r := newFinishRig(t)
		r.pushed(t, finPRHead)
		r.poster(applyRepo).mergeErr = &forge.MergeRefused{Message: "Review required"}
		a := r.action(KindScheduledMerge)
		a.PR, a.HeadSHA = 41, finPRHead
		if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
			t.Fatalf("Apply = %v, want no error", err)
		}
		p := r.poster(applyRepo)
		if !reflect.DeepEqual(p.closed, []int{41}) || !reflect.DeepEqual(p.deleted, []string{schedBranch}) {
			t.Errorf("closed %v, deleted %q; want PR 41 closed and the branch deleted", p.closed, p.deleted)
		}
		if row := r.row(t); row.Open || row.Outcome != runs.Failed || row.Reason != "merge refused: Review required" {
			t.Errorf("row = open %v, %s %q", row.Open, row.Outcome, row.Reason)
		}
	})
	t.Run("head moved", func(t *testing.T) {
		r := newFinishRig(t)
		r.pushed(t, finPRHead)
		r.poster(applyRepo).mergeErr = fmt.Errorf("409: %w", forge.ErrHeadMoved)
		a := r.action(KindScheduledMerge)
		a.PR, a.HeadSHA = 41, finPRHead
		res, err := Apply(context.Background(), r.env, []Action{a})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"MergePR"}; !reflect.DeepEqual(r.log.all(), want) {
			t.Errorf("calls = %q, want only the merge", r.log.all())
		}
		if row := r.row(t); !row.Open {
			t.Error("the run closed on a moved head")
		}
		if len(res.Deferred) != 1 || !strings.HasPrefix(res.Deferred[0], applyRepo+" "+finKey+": ") {
			t.Errorf("Deferred = %q, want the merge deferred", res.Deferred)
		}
	})
	t.Run("other error", func(t *testing.T) {
		r := newFinishRig(t)
		r.pushed(t, finPRHead)
		r.poster(applyRepo).mergeErr = errors.New("502")
		a := r.action(KindScheduledMerge)
		a.PR, a.HeadSHA = 41, finPRHead
		if _, err := Apply(context.Background(), r.env, []Action{a}); err == nil {
			t.Error("Apply = nil, want the merge error")
		}
		if row := r.row(t); !row.Open || len(r.poster(applyRepo).deleted) != 0 {
			t.Errorf("row open %v, deleted %q; want the run open and the branch kept", row.Open, r.poster(applyRepo).deleted)
		}
	})
}

// Finding F, in apply as well as in decide. Fails if a merge goes ahead on a head that is not the
// commit the check pushed (a foreign push after it), or without a recorded one: no MergePR, the
// PR is closed, the branch deleted, the run failed.
func TestApplyScheduledMergeOnlyThePushedCommit(t *testing.T) {
	for name, tc := range map[string]struct{ pushed, head string }{
		"foreign head":              {"0123456789012345678901234567890123456789", finPRHead},
		"nothing recorded":          {"", finPRHead},
		"nothing recorded, no head": {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := newFinishRig(t)
			r.pushed(t, tc.pushed)
			a := r.action(KindScheduledMerge)
			a.PR, a.HeadSHA = 41, tc.head
			if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
				t.Fatalf("Apply = %v, want a failed run and no error", err)
			}
			p := r.poster(applyRepo)
			if len(p.merges) != 0 || !reflect.DeepEqual(p.closed, []int{41}) || !reflect.DeepEqual(p.deleted, []string{schedBranch}) {
				t.Errorf("merges %q, closed %v, deleted %q; want no merge, PR 41 closed, the branch deleted", p.merges, p.closed, p.deleted)
			}
			if row := r.row(t); row.Open || row.Outcome != runs.Failed || row.Reason != "branch changed after push" {
				t.Errorf("row = open %v, %s %q", row.Open, row.Outcome, row.Reason)
			}
		})
	}
}

// Rows 5-7. A close ends the agent first, then closes the PR and deletes the branch, then closes
// the run with its outcome and reason. Anything that fails before keeps the run open.
func TestApplyScheduledClose(t *testing.T) {
	r := newFinishRig(t)
	a := r.action(KindScheduledClose)
	a.Outcome, a.ReasonArg, a.PR, a.End = runs.Retry, "no activity in max_wait", 41, true
	if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Kill", "ClosePR", "DeleteBranch"}; !reflect.DeepEqual(r.log.all(), want) {
		t.Errorf("calls = %q, want %q", r.log.all(), want)
	}
	if row := r.row(t); row.Open || row.Outcome != runs.Retry || row.Reason != "no activity in max_wait" {
		t.Errorf("row = open %v, %s %q", row.Open, row.Outcome, row.Reason)
	}
	r.noComment(t)

	for name, fail := range map[string]func(*finishRig){
		"kill":   func(r *finishRig) { r.wp.killErr = errors.New("kill failed") },
		"close":  func(r *finishRig) { r.poster(applyRepo).closeErr = errors.New("close failed") },
		"delete": func(r *finishRig) { r.poster(applyRepo).deleteErr = errors.New("delete failed") },
	} {
		t.Run(name+" fails", func(t *testing.T) {
			r := newFinishRig(t)
			fail(r)
			if _, err := Apply(context.Background(), r.env, []Action{a}); err == nil {
				t.Error("Apply = nil, want the error")
			}
			if row := r.row(t); !row.Open {
				t.Error("the run closed although a step before failed")
			}
		})
	}
}

// Finding 1, uncertain start. Fails if the launch is not forgotten before the agent is ended, or
// if the run closes when either fails: attempt 2 must never run beside a live agent.
func TestApplyScheduledCloseUncertain(t *testing.T) {
	r := newFinishRig(t)
	a := r.action(KindScheduledClose)
	a.Outcome, a.ReasonArg, a.End, a.Discard = runs.Retry, "start uncertain: no session found", true, true
	if _, err := Apply(context.Background(), r.env, []Action{a}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Discard", "Kill", "DeleteBranch"}; !reflect.DeepEqual(r.log.all(), want) {
		t.Errorf("calls = %q, want %q", r.log.all(), want)
	}
	if row := r.row(t); row.Open || row.Outcome != runs.Retry {
		t.Errorf("row = open %v, %s; want retry", row.Open, row.Outcome)
	}
	for name, fail := range map[string]func(*finishRig){
		"discard": func(r *finishRig) { r.wp.discardErr = errors.New("discard failed") },
		"kill":    func(r *finishRig) { r.wp.killErr = errors.New("kill failed") },
	} {
		t.Run(name+" fails", func(t *testing.T) {
			r := newFinishRig(t)
			fail(r)
			if _, err := Apply(context.Background(), r.env, []Action{a}); err == nil {
				t.Error("Apply = nil, want the error")
			}
			if row := r.row(t); !row.Open {
				t.Error("the run closed while its agent may live")
			}
		})
	}
}

// Finding 1, dead claim. A run without a workplace has no agent to end and nothing to forget:
// it closes without touching a workplace.
func TestApplyScheduledCloseDeadClaim(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run, err := runs.Take(ctx, h.db, runs.Claim{Repo: applyRepo, Schedule: "codebase-map", SessionID: freshSession,
		Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: finKey, Profile: "software", Branch: schedBranch,
		Now: applyNow.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	a := Action{Kind: KindScheduledClose, Repo: applyRepo, RunID: run.ID, Schedule: "codebase-map", Slot: finSlot,
		Branch: schedBranch, Outcome: runs.Retry, ReasonArg: "claimed without a workplace", End: true, Discard: true}
	if _, err := Apply(ctx, h.env, []Action{a}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"DeleteBranch"}; !reflect.DeepEqual(h.log.all(), want) {
		t.Errorf("calls = %q, want %q", h.log.all(), want)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Retry || rows[0].Reason != "claimed without a workplace" {
		t.Errorf("rows = %+v, want the claim closed as retry", rows)
	}
	// A run another process already closed is no error.
	if _, err := Apply(ctx, h.env, []Action{a}); err != nil {
		t.Errorf("second close = %v, want nil", err)
	}
}

// chainHolds reports whether the text of err or of any error it wraps holds s.
func chainHolds(err error, s string) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), s) {
		return true
	}
	switch u := err.(type) {
	case interface{ Unwrap() error }:
		return chainHolds(u.Unwrap(), s)
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			if chainHolds(e, s) {
				return true
			}
		}
	}
	return false
}
