package dispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io (SQLite) with fake forge, workplace, job and git. These tests fail if apply
// starts a scheduled run in another order than claim, workplace, start state, stage, spawn; gives
// it an MCP config or drops the job's tools; posts a comment; or closes a failed or uncertain start
// as anything but retry with its reason.

const (
	schedSlot   = "20261031T0400"
	schedKey    = "scheduled:codebase-map:" + schedSlot
	schedBranch = "scheduled/codebase-map-" + schedSlot + "-1"
	headSHA     = "0123456789abcdef0123456789abcdef01234567"
)

// gitFake answers the git calls of StartState: HEAD is headSHA, the git dir does not exist.
// Every call is recorded as its arguments joined by spaces.
type gitFake struct {
	calls []string
	head  string // "" is headSHA
}

func (g *gitFake) run(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
	g.calls = append(g.calls, name+" "+strings.Join(args, " "))
	switch {
	case slices.Contains(args, "--verify"):
		if g.head != "" {
			return g.head + "\n", 0, nil
		}
		return headSHA + "\n", 0, nil
	case slices.Contains(args, "--absolute-git-dir"), slices.Contains(args, "--git-common-dir"):
		return "/nonexistent-lighthouse-test/.git\n", 0, nil
	}
	return "", 1, nil
}

// scheduledHarness is a harness whose ScheduledJob gives job for codebase-map of o/r, with git.
func scheduledHarness(t *testing.T) (*harness, *scheduledtest.Job, *gitFake) {
	t.Helper()
	h := newHarness(t)
	job := &scheduledtest.Job{JobName: "map-refresh"}
	git := &gitFake{}
	h.env.ScheduledJob = func(repo, schedule string) (scheduled.Job, scheduled.Env, error) {
		if repo != applyRepo || schedule != "codebase-map" {
			return nil, scheduled.Env{}, fmt.Errorf("no schedule %s in %s", schedule, repo)
		}
		return job, scheduled.Env{
			Repo:     repoconfig.Repo{Slug: applyRepo, TokenEnv: "WORK_TOKEN"},
			Schedule: repoconfig.Schedule{Name: "codebase-map", Job: "map-refresh"},
			Now:      applyNow, Run: git.run,
		}, nil
	}
	return h, job, git
}

func scheduledStart() Action {
	return Action{
		Kind: KindScheduledStart, Repo: applyRepo, Schedule: "codebase-map", Slot: schedSlot,
		Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: schedKey, Attempt: 1,
		Profile: "software", Command: "/gsd-map-codebase", Prompt: "/gsd-map-codebase",
		Branch: schedBranch, Language: "nl", Tools: []string{"Read", "Glob", "Write"},
		Instructions: "Ask nothing. Write only under docs/.\n",
		Verdict:      scheduled.Record{Verdict: scheduled.Verdict{Mode: "full"}, MaxRuntime: time.Hour}.Encode(),
	}
}

func TestApplyScheduledStart(t *testing.T) {
	h, job, _ := scheduledHarness(t)
	var staged scheduled.Env
	var stagedVerdict scheduled.Verdict
	job.StageFn = func(_ context.Context, env scheduled.Env, v scheduled.Verdict) error {
		staged, stagedVerdict = env, v
		return nil
	}
	var atSpawn runs.OpenRun
	h.wp.onSpawn = func() {
		open, err := runs.Open(context.Background(), h.db, applyRepo)
		if err != nil || len(open) != 1 {
			t.Fatalf("open runs at spawn = %+v, %v", open, err)
		}
		atSpawn = open[0]
	}
	res, err := Apply(context.Background(), h.env, []Action{scheduledStart()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
	if got, want := h.log.all(), []string{"Prepare", "Spawn"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q: no comment, no busy check", got, want)
	}
	if got, want := job.Calls(), []string{"Stage"}; !reflect.DeepEqual(got, want) {
		t.Errorf("job calls = %q, want %q", got, want)
	}
	if staged.Dir != "/forge/"+schedBranch || staged.Schedule.Name != "codebase-map" || stagedVerdict.Mode != "full" {
		t.Errorf("staged in %q for %q with %+v, want the workplace, the schedule and the verdict", staged.Dir, staged.Schedule.Name, stagedVerdict)
	}
	if want := []string{applyRepo + "  software"}; !reflect.DeepEqual(h.places, want) {
		t.Errorf("workplaces = %q, want %q: node \"\" in the schedule's profile", h.places, want)
	}
	// The row holds its workspace and start state before the agent runs.
	if atSpawn.WorkspaceID != "ws-"+schedBranch || atSpawn.StartSHA != headSHA {
		t.Errorf("row at spawn = %+v, want workspace ws-%s and start sha %s", atSpawn, schedBranch, headSHA)
	}
	rows := h.history(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
	r := rows[0]
	if !r.Open || r.Issue != 0 || r.Schedule != "codebase-map" || r.Trigger != runs.TriggerScheduled ||
		r.TriggerKey != schedKey || r.Attempt != 1 || r.Branch != schedBranch || r.Profile != "software" ||
		r.Node != "" || r.SessionID != freshSession || r.StartSHA != headSHA || r.Command != "/gsd-map-codebase" {
		t.Errorf("row = %+v", r)
	}
	rec, err := scheduled.DecodeRecord(r.Verdict)
	if err != nil || rec.Verdict.Mode != "full" || rec.MaxRuntime != time.Hour || len(rec.GitState) != 64 {
		t.Errorf("verdict record = %+v, %v; want mode full, max runtime 1h and a git state", rec, err)
	}
	open, err := runs.Open(context.Background(), h.db, applyRepo)
	if err != nil || len(open) != 1 || open[0].WorkplaceSession != "key-"+freshSession {
		t.Errorf("open = %+v, %v; want the session key recorded", open, err)
	}
	instructions := filepath.Join(h.env.StateDir, "runs", freshSession, "instructions.md")
	wantSpec := workplace.LaunchSpec{
		Repo: applyRepo, SessionID: freshSession, Mode: "start", Profile: "software",
		Prompt: "/gsd-map-codebase", InstructionsFile: instructions, MCPConfig: "",
		Tools: []string{"Read", "Glob", "Write"},
		// Finding B: no forge token reaches the agent, whatever variable holds it.
		Unset: []string{"GH_ENTERPRISE_TOKEN", "GH_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "WORK_TOKEN"},
	}
	if !reflect.DeepEqual(h.wp.spawned, []workplace.LaunchSpec{wantSpec}) {
		t.Errorf("spawned %+v\nwant    %+v", h.wp.spawned, wantSpec)
	}
	if text, err := os.ReadFile(instructions); err != nil || string(text) != "Ask nothing. Write only under docs/.\n" {
		t.Errorf("instructions = %q, %v; want the job's text", text, err)
	}
	if p := h.posters[applyRepo]; p != nil && len(p.bodies) > 0 {
		t.Errorf("posted %q, want nothing: a scheduled run has no issue", p.bodies)
	}
}

// A profile with an MCP config still starts a scheduled run without one.
func TestApplyScheduledStartWithoutMCP(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	if h.env.MCPConfig("software") == "" {
		t.Fatal("harness profile has no MCP config: the test proves nothing")
	}
	if _, err := Apply(context.Background(), h.env, []Action{scheduledStart()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(h.wp.spawned) != 1 || h.wp.spawned[0].MCPConfig != "" {
		t.Errorf("spawned %+v, want one spawn without MCPConfig", h.wp.spawned)
	}
}

// retried returns the single row, which must be closed as retry.
func retried(t *testing.T, h *harness) runs.Row {
	t.Helper()
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Retry {
		t.Fatalf("rows = %+v, want one run closed as retry", rows)
	}
	return rows[0]
}

func TestApplyScheduledStartFailuresRetry(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(h *harness, job *scheduledtest.Job, git *gitFake)
		reason string // the start of the run's reason
		spawns int
	}{
		{"prepare", func(h *harness, _ *scheduledtest.Job, _ *gitFake) {
			h.wp.prepareErr = errors.New("forge refused the workspace")
		},
			"prepare: forge refused the workspace", 0},
		{"start state", func(_ *harness, _ *scheduledtest.Job, git *gitFake) { git.head = "not-a-sha" },
			"start state: scheduled: HEAD \"not-a-sha\" is no commit sha", 0},
		{"stage", func(_ *harness, job *scheduledtest.Job, _ *gitFake) {
			job.StageFn = func(context.Context, scheduled.Env, scheduled.Verdict) error { return errors.New("corpus too big") }
		}, "stage: corpus too big", 0},
		{"not accepted", func(h *harness, _ *scheduledtest.Job, _ *gitFake) {
			h.wp.spawnErr = fmt.Errorf("target busy: %w", workplace.ErrNotAccepted)
		}, "launch not accepted: target busy: workplace: spawn not accepted", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, job, git := scheduledHarness(t)
			tc.setup(h, job, git)
			res, err := Apply(context.Background(), h.env, []Action{scheduledStart()})
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("Apply error = %v, want one naming %q", err, tc.reason)
			}
			if res.Started != 0 {
				t.Errorf("Started = %d, want 0", res.Started)
			}
			if r := retried(t, h); r.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", r.Reason, tc.reason)
			}
			if len(h.wp.spawned) != tc.spawns {
				t.Errorf("spawns = %d, want %d", len(h.wp.spawned), tc.spawns)
			}
		})
	}
}

// An uncertain spawn may have started an agent: apply forgets the launch, then ends its claude,
// before it closes the run as retry, so a second attempt never runs beside it.
func TestApplyScheduledUncertainEndsAndRetries(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	h.wp.spawnErr = fmt.Errorf("timeout: %w", workplace.ErrUncertain)
	_, err := Apply(context.Background(), h.env, []Action{scheduledStart()})
	if err == nil || !strings.Contains(err.Error(), "launch uncertain: timeout") {
		t.Errorf("Apply error = %v, want the uncertain launch", err)
	}
	if got, want := h.log.all(), []string{"Prepare", "Spawn", "Discard", "Kill"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if want := []string{"ws-" + schedBranch + " " + freshSession}; !reflect.DeepEqual(h.wp.killed, want) {
		t.Errorf("killed %q, want %q", h.wp.killed, want)
	}
	if r := retried(t, h); !strings.HasPrefix(r.Reason, "launch uncertain: timeout") {
		t.Errorf("reason = %q, want the uncertain launch", r.Reason)
	}
}

// A target that has not started yet when Kill looks finds nothing to end, and starts later from
// the launch it still has (kenn-forge reads the carrier). Forgetting the launch before the Kill
// leaves it nothing to start; with Kill first, an agent runs beside a run closed as retry.
func TestApplyScheduledUncertainLateStartFindsNoLaunch(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	h.wp.spawnErr = fmt.Errorf("timeout: %w", workplace.ErrUncertain)
	live := false
	h.wp.onKill = func() {
		// Kill finds nothing running; the target then starts if its launch is still there.
		live = len(h.wp.forgot) == 0
	}
	if _, err := Apply(context.Background(), h.env, []Action{scheduledStart()}); err == nil {
		t.Fatal("Apply: no error for an uncertain launch")
	}
	if live {
		t.Error("an agent started late from a launch that was not yet forgotten, beside a run closed as retry")
	}
	retried(t, h)
}

// A failed step names a file by its base name only: a reason must not carry a raw path.
func TestApplyScheduledReasonHasNoPath(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	file := filepath.Join(t.TempDir(), "state-is-a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h.env.StateDir = file // the instructions directory cannot be made under a file
	if _, err := Apply(context.Background(), h.env, []Action{scheduledStart()}); err == nil {
		t.Fatal("Apply: no error for unwritable instructions")
	}
	r := retried(t, h)
	if !strings.HasPrefix(r.Reason, "instructions: ") || strings.Contains(r.Reason, file) || strings.Contains(r.Reason, "/") {
		t.Errorf("reason = %q, want the instructions step without a path", r.Reason)
	}
}

// When apply cannot end the claude of an uncertain spawn, the run stays open: closing it would
// let the next tick start a second attempt beside a live agent.
func TestApplyScheduledUncertainKeepsRunWhenKillFails(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	h.wp.spawnErr = fmt.Errorf("timeout: %w", workplace.ErrUncertain)
	h.wp.killErr = errors.New("ssh down")
	_, err := Apply(context.Background(), h.env, []Action{scheduledStart()})
	if err == nil || !strings.Contains(err.Error(), "ssh down") {
		t.Errorf("Apply error = %v, want the kill failure", err)
	}
	rows := h.history(t)
	if len(rows) != 1 || !rows[0].Open {
		t.Errorf("rows = %+v, want the run still open", rows)
	}
}

// A schedule that already has an open run (a tick that died after its claim) is not claimed
// again, and nothing is prepared or spawned.
func TestApplyScheduledClaimLost(t *testing.T) {
	h, job, _ := scheduledHarness(t)
	earlier := scheduledStart()
	if _, err := runs.Take(context.Background(), h.db, runs.Claim{
		Repo: applyRepo, Schedule: "codebase-map", SessionID: oldSession, Mode: "start",
		Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:codebase-map:20261030T0400", Now: applyNow,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), h.env, []Action{earlier}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if calls := h.log.all(); len(calls) != 0 || len(job.Calls()) != 0 {
		t.Errorf("calls = %q, job calls = %q; want none", calls, job.Calls())
	}
	if !strings.Contains(h.out.String(), applyRepo+" "+schedKey+": not claimed") {
		t.Errorf("log %q, want the not-claimed line", h.out.String())
	}
	if rows := h.history(t); len(rows) != 1 {
		t.Errorf("rows = %+v, want only the earlier run", rows)
	}
}

// A schedule apply cannot resolve claims nothing; the error names the run, not issue 0.
func TestApplyScheduledUnknownJob(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	a := scheduledStart()
	a.Schedule, a.TriggerKey = "gone", "scheduled:gone:"+schedSlot
	res, err := Apply(context.Background(), h.env, []Action{a})
	if err == nil || !strings.Contains(err.Error(), "o/r scheduled:gone:"+schedSlot+" scheduled_start: job: no schedule gone") ||
		strings.Contains(err.Error(), "#0") {
		t.Errorf("Apply error = %v, want one naming the scheduled run and no issue 0", err)
	}
	if res.Failed[applyRepo] == nil {
		t.Error("Failed has no error for o/r")
	}
	if rows := h.history(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

// A deferred scheduled start is reported under its trigger key, not as issue 0.
func TestApplyScheduledDeferral(t *testing.T) {
	h, _, _ := scheduledHarness(t)
	d := Action{Kind: KindDefer, Repo: applyRepo, Schedule: "codebase-map", Slot: schedSlot, Reason: "slots: 4 of 4 in use"}
	res, err := Apply(context.Background(), h.env, []Action{d})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{applyRepo + " " + schedKey + ": slots: 4 of 4 in use"}; !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
}
