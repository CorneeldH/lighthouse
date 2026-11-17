//go:build linux

package local

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	lhstore "github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

var sweepNow = time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)

func openSweepDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := lhstore.OpenShared(context.Background(), filepath.Join(t.TempDir(), "lh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB
}

// sweepRun records a run of the issue or the schedule on ws; it is closed unless open.
func sweepRun(t *testing.T, db *sql.DB, issue int, schedule string, ws workplace.Workspace, open bool) {
	t.Helper()
	ctx := context.Background()
	c := runs.Claim{Repo: "o/r", Issue: issue, SessionID: runs.NewSessionID(), Mode: "start", Trigger: "handoff",
		TriggerKey: "start:" + ws.Branch, Branch: ws.Branch, Now: sweepNow}
	outcome := runs.Done
	if schedule != "" {
		c.Schedule, c.Trigger, c.TriggerKey = schedule, runs.TriggerScheduled, "scheduled:"+schedule+":20261116T0400"
		outcome = runs.Retry
	}
	r, err := runs.Take(ctx, db, c)
	if err != nil {
		t.Fatalf("Take %s: %v", ws.Branch, err)
	}
	if err := runs.SetWorkplace(ctx, db, r.ID, ws.ID, ws.Branch); err != nil {
		t.Fatal(err)
	}
	if !open {
		if err := runs.Release(ctx, db, r.ID, outcome, "", sweepNow); err != nil {
			t.Fatal(err)
		}
	}
}

// The sweep against the local adapter and real git: a dirty issue workspace stays, a dirty
// scheduled workspace without an open row goes from disk, and the open run of schedule a holds
// its own workspace but not that of schedule b.
func TestSweepScheduledWorkspacesOnLocal(t *testing.T) {
	repo, _ := newRepo(t)
	h := newFakeHost(t)
	a, _ := h.adapter(t, repo)
	db := openSweepDB(t)

	issue := prepare(t, a, "issue-7-x")
	schedA := prepare(t, a, "scheduled/a-20261116T0400-1")
	schedB := prepare(t, a, "scheduled/b-20261116T0400-1")
	for _, ws := range []workplace.Workspace{issue, schedA, schedB} {
		writeFile(t, filepath.Join(ws.Path, "uncommitted.md"), "work in progress")
	}
	sweepRun(t, db, 7, "", issue, false)
	sweepRun(t, db, 0, "a", schedA, true)
	sweepRun(t, db, 0, "b", schedB, false)

	in := workplace.SweepInput{
		Repo:         "o/r",
		InController: func(int) bool { return false },
		MergedHead:   func(context.Context, string) (string, error) { return "", nil },
	}
	place := func(string, string) workplace.Workplace { return a }
	removed, err := workplace.Sweep(t.Context(), db, place, workplace.ExecRunner(), in, sweepNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(removed) != 1 || removed[0].WorkspaceID != schedB.ID || removed[0].Schedule != "b" {
		t.Errorf("removed = %+v, want only the workspace of schedule b", removed)
	}
	if exists(schedB.Path) {
		t.Errorf("worktree of schedule b still on disk, want it removed")
	}
	if _, err := a.Lookup(t.Context(), schedB.ID); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup of schedule b: err = %v, want ErrGone", err)
	}
	for name, ws := range map[string]workplace.Workspace{"issue 7": issue, "schedule a": schedA} {
		if !exists(filepath.Join(ws.Path, "uncommitted.md")) {
			t.Errorf("work of %s gone, want its worktree kept", name)
		}
	}
	ls, err := workplace.Leftovers(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 || ls[0].WorkspaceID != issue.ID || ls[0].Reason != "uncommitted changes" {
		t.Errorf("leftovers = %+v, want only issue 7 as uncommitted changes", ls)
	}
}
