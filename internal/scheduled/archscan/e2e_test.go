package archscan

import (
	"context"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: io (real git with a bare origin, the fake forge, the real Postcheck). The
// job runs the way a tick runs it: gate, launch, stage, a fake agent that writes the result
// file, then the check after the run. These tests fail if the job and Postcheck disagree on
// what a run may leave behind: a scan issue must reach the forge without a commit or a PR and
// without the result file, and a result that fails Check must publish nothing.

const runBranch = "scheduled/architecture-scan"

// noRemote is a scheduled.Remote on which nothing was pushed, and which refuses a push: the job
// publishes on the forge, never on a branch.
type noRemote struct{ t *testing.T }

func (noRemote) Head(context.Context, string, string) (string, error) { return "", nil }

func (r noRemote) Push(_ context.Context, _, commit, branch string) error {
	r.t.Errorf("Push of %s to %s: the job must not push", commit, branch)
	return nil
}

// startRun does what the tick does before the agent: gate, launch, a workplace on the run
// branch, stage. It returns the start SHA.
func startRun(t *testing.T, r *rig) (scheduled.Env, scheduled.Verdict, string) {
	t.Helper()
	ctx := context.Background()
	env := r.scanEnv()
	j := bind(t)
	v, err := j.Gate(ctx, env)
	if err != nil || v.Skip != "" {
		t.Fatalf("Gate = %+v, %v; want a run", v, err)
	}
	if _, err := j.Launch(v, env); err != nil {
		t.Fatal(err)
	}
	start := gitIn(t, r.repo, nil, "rev-parse", "HEAD")
	gitIn(t, r.repo, nil, "checkout", "-q", "-b", runBranch)
	if err := j.Stage(ctx, env, v); err != nil {
		t.Fatal(err)
	}
	return env, v, start
}

func TestRunLifecycleViaPostcheck(t *testing.T) {
	// Fails if Cleanup or Check refuse a valid result, if Finish does not create the issue, if
	// it leaves the result file or the corpus behind (Postcheck would commit them), or if the
	// run ends with a commit, a push or a PR.
	r := scanRig(t)
	env, v, start := startRun(t, r)
	r.writeResult(resultDoc(candidate("A", "a", false), "")) // the fake agent
	res, err := scheduled.Postcheck(context.Background(), env.Run, bind(t), env, v, start, runBranch, noRemote{t})
	if err != nil || res.Failure != "" || res.Changed || res.Commit != "" {
		t.Fatalf("Postcheck = %+v, %v; want done without a commit", res, err)
	}
	if len(r.srv.Issues) != 1 {
		t.Fatalf("%d issues on the forge, want 1", len(r.srv.Issues))
	}
	for n, it := range r.srv.Issues {
		m, ok := scheduled.ParseMarker(liveBody(t, r, n))
		if !ok || m.Kind != Name || m.Run != v.RunID || m.Base != start || m.Scans != 1 {
			t.Errorf("issue #%d marker = %+v (%v), want this run on base %s", n, m, ok, start)
		}
		if got := labelsOf(it); len(got) != 1 || got[0] != scanLabel {
			t.Errorf("labels = %v, want [%s]", got, scanLabel)
		}
	}
	if resultExists(t, r) {
		t.Error("result file still there")
	}
	if st := gitIn(t, r.repo, nil, "status", "--porcelain"); st != "" {
		t.Errorf("work tree not clean: %q", st)
	}
	if head := gitIn(t, r.repo, nil, "rev-parse", "HEAD"); head != start {
		t.Errorf("HEAD moved to %s, want %s", head, start)
	}
}

func TestPostcheckFailedCheckClosesFailedNoIssue(t *testing.T) {
	// Fails if a result that does not parse reaches Finish (an issue or comment appears), or if
	// Postcheck reports no failure and so ends the run done.
	r := scanRig(t)
	r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduledMarker(), resultDoc(candidate("A", "a", false), ""))
	before := len(r.srv.Posts)
	env, v, start := startRun(t, r)
	r.writeResult("# not a scan\n")
	res, err := scheduled.Postcheck(context.Background(), env.Run, bind(t), env, v, start, runBranch, noRemote{t})
	if err != nil || res.Failure == "" || res.Changed {
		t.Fatalf("Postcheck = %+v, %v; want a failure with a reason", res, err)
	}
	if len(r.srv.Issues) != 1 {
		t.Errorf("%d issues, want only the seeded one", len(r.srv.Issues))
	}
	if cs := commentsOn(t, r, 7); len(cs) != 0 {
		t.Errorf("comments = %q, want none", cs)
	}
	if n := len(r.srv.Posts) - before; n != 0 {
		t.Errorf("%d writes to the forge, want none", n)
	}
}

func scheduledMarker() scheduled.Marker {
	return scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1}
}
