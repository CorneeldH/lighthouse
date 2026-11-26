package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
)

// Classification: io (SQLite) with a fake workplace and a fake in-process job. These tests fail
// if apply starts an agent, prepares a workplace or writes an instructions file for a
// scheduled.Direct job; runs it without the controller's database or before its claim; closes it
// as anything but done after a success and failed after an error; or lets one repo's run stand in
// for another's.

const directOther = "o/other"

// directHarness is a harness whose ScheduledJob gives job for schedule recovery of applyRepo and
// of directOther.
func directHarness(t *testing.T) (*harness, *scheduledtest.DirectJob) {
	t.Helper()
	h := newHarness(t)
	job := &scheduledtest.DirectJob{Job: scheduledtest.Job{JobName: "measure-recovery"}}
	h.env.ScheduledJob = func(repo, schedule string) (scheduled.Job, scheduled.Env, error) {
		if (repo != applyRepo && repo != directOther) || schedule != "recovery" {
			return nil, scheduled.Env{}, fmt.Errorf("no schedule %s in %s", schedule, repo)
		}
		return job, scheduled.Env{
			Repo:     repoconfig.Repo{Slug: repo},
			Schedule: repoconfig.Schedule{Name: "recovery", Job: "measure-recovery"},
		}, nil
	}
	return h, job
}

func directStart(repo string) Action {
	return Action{
		Kind: KindScheduledStart, Repo: repo, Schedule: "recovery", Slot: schedSlot,
		Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:recovery:" + schedSlot, Attempt: 1,
		Profile: "software", Branch: "scheduled/recovery-" + schedSlot + "-1",
		Verdict: scheduled.Record{}.Encode(),
	}
}

func TestStartScheduledDirect(t *testing.T) {
	h, job := directHarness(t)
	var ran []string
	job.RunFn = func(ctx context.Context, db *sql.DB, env scheduled.Env) error {
		if db != h.env.DB {
			t.Error("Run got another database than the controller's")
		}
		if !env.Now.Equal(applyNow) {
			t.Errorf("Run at %v, want the apply clock %v", env.Now, applyNow)
		}
		open, err := runs.Open(ctx, db, env.Repo.Slug)
		if err != nil || len(open) != 1 {
			t.Errorf("open runs of %s during Run = %+v, %v; want the claimed one", env.Repo.Slug, open, err)
		}
		ran = append(ran, env.Repo.Slug)
		return nil
	}
	if _, err := Apply(context.Background(), h.env, []Action{directStart(applyRepo), directStart(directOther)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{applyRepo, directOther}; !reflect.DeepEqual(ran, want) {
		t.Errorf("ran for %q, want %q: one run per repo", ran, want)
	}
	if got := h.log.all(); len(got) != 0 {
		t.Errorf("workplace calls = %q, want none: no agent, no workplace", got)
	}
	if len(h.places) != 0 || len(h.wp.spawned) != 0 {
		t.Errorf("workplaces %q, spawned %d; want none", h.places, len(h.wp.spawned))
	}
	if _, err := os.Stat(filepath.Join(h.env.StateDir, "runs", freshSession)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run directory of the session: %v; want no instructions file", err)
	}
	for _, repo := range []string{applyRepo, directOther} {
		rows, err := runs.History(context.Background(), h.db, repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Done || rows[0].Schedule != "recovery" ||
			rows[0].TriggerKey != "scheduled:recovery:"+schedSlot {
			t.Errorf("rows of %s = %+v, want one run of recovery closed as done", repo, rows)
		}
	}
}

func TestStartScheduledDirectFailure(t *testing.T) {
	h, job := directHarness(t)
	job.RunFn = func(context.Context, *sql.DB, scheduled.Env) error {
		return errors.New("git fetch origin main: exit 128")
	}
	_, err := Apply(context.Background(), h.env, []Action{directStart(applyRepo)})
	if err == nil {
		t.Fatal("Apply = nil, want the run error")
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Failed || rows[0].Reason != "run: git fetch origin main: exit 128" {
		t.Errorf("rows = %+v, want one run closed as failed with the run error", rows)
	}
	if len(h.wp.spawned) != 0 {
		t.Errorf("spawned %d, want none", len(h.wp.spawned))
	}
}

func TestStartScheduledDirectClaimedRunsNothing(t *testing.T) {
	h, job := directHarness(t)
	if _, err := runs.Take(context.Background(), h.db, runs.Claim{
		Repo: applyRepo, Schedule: "recovery", SessionID: "other", Mode: "start", Trigger: runs.TriggerScheduled,
		TriggerKey: "scheduled:recovery:" + schedSlot, Now: applyNow,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), h.env, []Action{directStart(applyRepo)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := job.Calls(); len(got) != 0 {
		t.Errorf("job calls = %q, want none: another process holds the claim", got)
	}
}
