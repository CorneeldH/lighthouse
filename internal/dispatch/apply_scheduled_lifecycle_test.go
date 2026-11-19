//go:build linux

package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/local"
)

// Classification: io (SQLite, real git, the real local adapter, a bare remote; the forge, the
// job and the spawn of claude are fakes). This test walks one scheduled run through its whole
// life: it fails if the git state recorded at the start differs from the one measured at the end
// for an untouched workplace, if the record loses a field on its way (verdict mode, max runtime,
// git state, pushed commit), if the check does not push the agent's file, if the merge is not
// bound to the pushed commit, or if the sweep leaves the dirty worktree of the finished run.

// spawnStub is the real local adapter whose Spawn starts no claude.
type spawnStub struct {
	*local.Adapter
	spawned int
}

func (s *spawnStub) Spawn(_ context.Context, _ workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	s.spawned++
	return workplace.Session{Key: "key-" + l.SessionID}, nil
}

func TestScheduledRunLifecycleOnTheLocalAdapter(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin, repoDir := filepath.Join(root, "origin.git"), filepath.Join(root, "repo")
	rigGit(t, root, "init", "-q", "--bare", "-b", "main", origin)
	rigGit(t, root, "clone", "-q", origin, repoDir)
	rigGit(t, repoDir, "config", "user.name", "Test")
	rigGit(t, repoDir, "config", "user.email", "test@example.com")
	rigGit(t, repoDir, "checkout", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(repoDir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "out", "keep.md"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rigGit(t, repoDir, "add", "-A")
	rigGit(t, repoDir, "commit", "-q", "-m", "init")
	rigGit(t, repoDir, "push", "-q", "origin", "main")

	wp := &spawnStub{Adapter: &local.Adapter{
		RepoDir: repoDir, DefaultBranch: "main", Repo: applyRepo, StateDir: t.TempDir(), Home: t.TempDir(),
		Run: workplace.ExecRunner(),
	}}
	h := newHarness(t)
	h.env.Workplace = func(string, string, string) workplace.Workplace { return wp }
	job := &scheduledtest.Job{JobName: "map-refresh", AllowedPre: []string{"out/"}}
	h.env.ScheduledJob = func(repo, schedule string) (scheduled.Job, scheduled.Env, error) {
		return job, scheduled.Env{Repo: repoconfig.Repo{Slug: applyRepo},
			Schedule: repoconfig.Schedule{Name: "codebase-map", Job: "map-refresh"}, Now: applyNow,
			Run: workplace.ExecRunner()}, nil
	}
	h.env.Remote = func(string) (scheduled.Remote, error) { return &rigRemote{bare: origin}, nil }

	// Plan: Decide turns a due schedule into the start.
	repo := RepoInput{
		Snapshot: Snapshot{Repo: applyRepo}, Language: "nl",
		Schedules: []ScheduledDue{{
			Schedule: "codebase-map", Profile: "software", Slot: schedSlot,
			Verdict: scheduled.Verdict{Mode: "full"},
			Plan: scheduled.LaunchPlan{Command: "/gsd-map-codebase", Tools: []string{"Read", "Write"},
				Instructions: "Write only under out/.\n", MaxRuntime: time.Hour},
		}},
	}
	planned := decideScheduled(Input{Now: applyNow}, repo)
	if len(planned) != 1 || planned[0].Kind != KindScheduledStart {
		t.Fatalf("planned = %+v, want one scheduled start", planned)
	}
	start := planned[0].Action

	// Start: claim, prepare, record the start state, stage, spawn.
	if _, err := Apply(ctx, h.env, []Action{start}); err != nil {
		t.Fatalf("start: %v", err)
	}
	open, err := runs.Open(ctx, h.db, applyRepo)
	if err != nil || len(open) != 1 {
		t.Fatalf("open runs = %+v, %v; want one", open, err)
	}
	run := open[0]
	if wp.spawned != 1 || run.Branch != schedBranch || run.WorkspaceID == "" || run.WorkplaceSession == "" {
		t.Fatalf("run = %+v, spawned %d; want a spawned run on %s with its workspace", run, wp.spawned, schedBranch)
	}
	startRec, err := scheduled.DecodeRecord(run.Verdict)
	if err != nil || startRec.GitState == "" || startRec.Verdict.Mode != "full" || startRec.MaxRuntime != time.Hour {
		t.Fatalf("record after the start = %+v, %v; want mode full, 1h and a git state", startRec, err)
	}
	ws, err := wp.Lookup(ctx, run.WorkspaceID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ws.Path == repoDir {
		t.Fatal("the run works in the main checkout")
	}
	// The git state measured now, with the agent not yet done, is the one recorded.
	if state, err := scheduled.GitState(ctx, workplace.ExecRunner(), ws.Path); err != nil || state != startRec.GitState {
		t.Fatalf("GitState = %q, %v; want the recorded %q", state, err, startRec.GitState)
	}

	// The agent writes its file; the run finishes: kill, git state, check, commit, push, PR.
	if err := os.WriteFile(filepath.Join(ws.Path, "out", "map.md"), []byte("the map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finish := start
	finish.Kind, finish.RunID = KindScheduledFinish, run.ID
	if _, err := Apply(ctx, h.env, []Action{finish}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	row := h.history(t)[0]
	if !row.Open || row.PRAt.IsZero() {
		t.Fatalf("row after finish = open %v, pr_at %v (reason %q); want open with pr_at", row.Open, row.PRAt, row.Reason)
	}
	rec, err := scheduled.DecodeRecord(row.Verdict)
	if err != nil || rec.Pushed == "" || rec.GitState != startRec.GitState || rec.Verdict.Mode != "full" || rec.MaxRuntime != time.Hour {
		t.Fatalf("record after finish = %+v, %v; want the start record plus the pushed commit", rec, err)
	}
	if head := rigGit(t, origin, "rev-parse", "refs/heads/"+schedBranch); head != rec.Pushed {
		t.Errorf("remote branch at %s, record pushed %s; want the same commit", head, rec.Pushed)
	}
	if show := rigGit(t, origin, "show", rec.Pushed+":out/map.md"); show != "the map" {
		t.Errorf("pushed map.md = %q, want the agent's file", show)
	}
	prs := h.poster(applyRepo).created
	if len(prs) != 1 {
		t.Fatalf("PRs created = %q, want one", prs)
	}

	// PR phase: a green PR on the pushed commit is what Decide merges.
	merged := settleScheduled(Input{Now: applyNow.Add(time.Minute)}, RepoInput{
		Snapshot: Snapshot{Repo: applyRepo, ScheduledPRs: map[string][]forge.PR{schedBranch: {{
			Number: 901, State: "OPEN", Mergeable: "MERGEABLE", Checks: "SUCCESS", HeadRef: schedBranch, HeadSHA: rec.Pushed,
		}}}},
		Language: "nl", History: h.history(t),
	})
	if len(merged) != 1 || merged[0].Kind != KindScheduledMerge || merged[0].HeadSHA != rec.Pushed {
		t.Fatalf("settled = %+v, want a merge of the pushed commit", merged)
	}
	if _, err := Apply(ctx, h.env, []Action{merged[0].Action}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := h.poster(applyRepo).merges; len(got) != 1 || got[0] != "901 "+rec.Pushed {
		t.Errorf("merges = %q, want PR 901 on the pushed commit", got)
	}
	if row := h.history(t)[0]; row.Open || row.Outcome != runs.Done {
		t.Fatalf("row after the merge = open %v, %s %q; want done", row.Open, row.Outcome, row.Reason)
	}

	// Sweep: the finished run's workspace goes, though the agent left it dirty.
	if err := os.WriteFile(filepath.Join(ws.Path, "leftover.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := workplace.Sweep(ctx, h.db, func(string, string) workplace.Workplace { return wp }, workplace.ExecRunner(),
		workplace.SweepInput{Repo: applyRepo, InController: func(int) bool { return false },
			MergedHead: func(context.Context, string) (string, error) { return "", nil }}, applyNow)
	if err != nil || len(removed) != 1 || removed[0].WorkspaceID != run.WorkspaceID {
		t.Fatalf("Sweep = %+v, %v; want the run's workspace removed", removed, err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Errorf("worktree after the sweep: stat %v, want it gone", err)
	}
}
