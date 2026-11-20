package reflect

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io/filesystem. These tests fail if a session directory of the repo is missed
// (the gate then undercounts for ever), a directory of another clone is counted, or a session
// lands under the wrong kind.

func touch(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestFindSessionsCoversRepoWorktreesAndKennForgeDirs(t *testing.T) {
	home := t.TempDir()
	repo := repoconfig.Repo{Dir: "/srv/code/my_repo", Slug: "Owner/My_Repo", Kind: "github"}
	now := time.Now()
	proj := workplace.ProjectDir(home, repo.Dir)
	forge := workplace.ProjectDir(home, filepath.Join(home, ".kenn", "forge", "worktrees", "github", "github.com", "owner", "my_repo", "work-issue-3-x"))
	other := workplace.ProjectDir(home, "/srv/code/my_repo-239")                                                                                                // another clone: not ours
	sibling := workplace.ProjectDir(home, filepath.Join(home, ".kenn", "forge", "worktrees", "github", "github.com", "owner", "my_repo-web", "work-issue-9-y")) // another repo whose name starts with ours
	for _, p := range []string{
		filepath.Join(proj, "a.jsonl"),
		proj + "--claude-worktrees-feat-x/b.jsonl",
		forge + "/c.jsonl",
		filepath.Join(other, "d.jsonl"),
		filepath.Join(sibling, "e.jsonl"),
	} {
		touch(t, p, now)
	}
	touch(t, filepath.Join(proj, "a", "subagents", "agent-1.jsonl"), now) // a subagent is no session
	touch(t, filepath.Join(proj, "note.txt"), now)
	got, err := findSessions(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"a", "b", "c"}) {
		t.Errorf("sessions = %v, want a b c (not the other clone, not a sibling repo of the forge, no subagent, no .txt)", ids)
	}
}

func TestClassifyKindFollowsTheDatabaseNotThePath(t *testing.T) {
	history := []runs.Row{
		{SessionID: "ctl", Trigger: "issue"},
		{SessionID: "sch", Trigger: runs.TriggerScheduled, Schedule: "reflect"},
		{SessionID: "", Trigger: "issue"},
	}
	controller, scheduledIDs := classify(history)
	for id, want := range map[string]string{"ctl": kindController, "x": kindInteractive, "sch": kindInteractive} {
		if got := kind(id, controller); got != want {
			t.Errorf("kind(%q) = %q, want %q", id, got, want)
		}
	}
	if !scheduledIDs["sch"] || scheduledIDs["ctl"] {
		t.Errorf("scheduled = %v, want only sch", scheduledIDs)
	}
	if controller["sch"] || controller[""] {
		t.Errorf("controller = %v: a scheduled run or an empty id is no controller session", controller)
	}
}
