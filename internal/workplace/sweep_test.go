package workplace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

const (
	ws945     = "d7c0b0ea0de54804"
	branch945 = "work-issue-945-immich-smart-search-191b7750"
)

// doneRuns adds one closed run per key for issue, each on workspace and branch.
func doneRuns(t *testing.T, db *sql.DB, issue int, workspace, branch string, keys ...string) {
	t.Helper()
	ctx := context.Background()
	for _, key := range keys {
		r, err := runs.Take(ctx, db, runs.Claim{
			Repo: lcRepo, Issue: issue, SessionID: "sid-" + key, Mode: "start", Trigger: "handoff",
			TriggerKey: key, Now: lcNow,
		})
		if err != nil {
			t.Fatalf("Take %s: %v", key, err)
		}
		if err := runs.SetWorkplace(ctx, db, r.ID, workspace, branch); err != nil {
			t.Fatal(err)
		}
		if err := runs.Release(ctx, db, r.ID, runs.Done, "", lcNow); err != nil {
			t.Fatal(err)
		}
	}
}

// fixture945 is #945: four closed runs of issue 945 on one workspace.
func fixture945(t *testing.T, db *sql.DB) {
	t.Helper()
	doneRuns(t, db, 945, ws945, branch945, "start:945", "answer:1", "answer:2", "handoff:3")
}

// sweepIn is a SweepInput whose InController answers inController and whose MergedHead
// answers head for every branch, recording the branches it was asked for in asked.
func sweepIn(inController bool, head string, asked *[]string) SweepInput {
	return SweepInput{
		Repo:         lcRepo,
		InController: func(int) bool { return inController },
		MergedHead: func(_ context.Context, branch string) (string, error) {
			*asked = append(*asked, branch)
			return head, nil
		},
	}
}

func candidates(t *testing.T, db *sql.DB) []runs.WorkspaceRef {
	t.Helper()
	got, err := runs.Workspaces(context.Background(), db, lcRepo)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSweepRemovesOffController(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), cleanRunner(), sweepIn(false, "", &asked), lcNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !reflect.DeepEqual(w.removed, []string{ws945}) {
		t.Errorf("removed workspaces = %v, want [%s]", w.removed, ws945)
	}
	if want := []Leftover{{Repo: lcRepo, Issue: 945, WorkspaceID: ws945}}; !reflect.DeepEqual(removed, want) {
		t.Errorf("Sweep removed = %+v, want %+v", removed, want)
	}
	if !reflect.DeepEqual(asked, []string{branch945}) {
		t.Errorf("MergedHead asked for %v, want [%s]", asked, branch945)
	}
	if got := candidates(t, db); len(got) != 0 {
		t.Errorf("candidates after Sweep = %+v, want none", got)
	}
}

func TestSweepKeepsDirty(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), gitRunner(" M x.go", "abc", "0"), sweepIn(false, "", &asked), lcNow)
	if err != nil || len(removed) != 0 || w.removes != 0 {
		t.Fatalf("Sweep = %+v, %v, removes %d", removed, err, w.removes)
	}
	ls, err := Leftovers(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	want := []Leftover{{Repo: lcRepo, Issue: 945, WorkspaceID: ws945, Path: "/p/945", Reason: "uncommitted changes"}}
	if !reflect.DeepEqual(ls, want) {
		t.Errorf("leftovers = %+v, want %+v", ls, want)
	}
	if got := candidates(t, db); len(got) != 1 {
		t.Errorf("candidates after a kept workspace = %+v, want it still there", got)
	}
}

func TestSweepKeepsUnpushed(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945"}
	var asked []string
	if _, err := Sweep(context.Background(), db, only(w), gitRunner("", "abc", "2"), sweepIn(false, "", &asked), lcNow); err != nil {
		t.Fatal(err)
	}
	if w.removes != 0 {
		t.Errorf("removes = %d, want 0", w.removes)
	}
	if got := leftoverReasons(t, db)[ws945]; got != "unpushed commits" {
		t.Errorf("reason = %q", got)
	}
}

// Squash merge: HEAD is on no origin ref but is the head of the merged PR.
func TestSweepPassesMergedHead(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), gitRunner("", "abc", "2"), sweepIn(false, "abc", &asked), lcNow)
	if err != nil || len(removed) != 1 || w.removes != 1 {
		t.Fatalf("Sweep = %+v, %v, removes %d", removed, err, w.removes)
	}
}

func TestSweepSkipsController(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945", sessionsErr: errors.New("unexpected Sessions")}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(true, "", &asked), lcNow)
	if err != nil || len(removed) != 0 {
		t.Fatalf("Sweep = %+v, %v", removed, err)
	}
	if len(w.looked) != 0 || w.removes != 0 || len(asked) != 0 {
		t.Errorf("looked %v, removes %d, MergedHead asked %v; want none", w.looked, w.removes, asked)
	}
}

func TestSweepSkipsOpenRun(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	r, err := runs.Take(context.Background(), db, runs.Claim{
		Repo: lcRepo, Issue: 945, SessionID: "sid-945", Mode: "resume", Trigger: "answer", TriggerKey: "answer:9", Now: lcNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(context.Background(), db, r.ID, ws945, branch945); err != nil {
		t.Fatal(err)
	}
	w := &fakeWP{lookupPath: "/p/945", sessions: []Session{{Key: "k", SessionID: "sid-945", State: "working"}}}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(false, "", &asked), lcNow)
	if err != nil || len(removed) != 0 {
		t.Fatalf("Sweep = %+v, %v", removed, err)
	}
	if w.stops != 0 || w.removes != 0 || len(w.looked) != 0 {
		t.Errorf("stops %d, removes %d, looked %v; want none", w.stops, w.removes, w.looked)
	}
}

func TestSweepClearsLeftoverInController(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	if err := RecordLeftover(context.Background(), db, Leftover{Repo: lcRepo, Issue: 945, WorkspaceID: ws945, Path: "/p/945", Reason: "uncommitted changes"}, lcNow); err != nil {
		t.Fatal(err)
	}
	w := &fakeWP{lookupPath: "/p/945"}
	var asked []string
	if _, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(true, "", &asked), lcNow); err != nil {
		t.Fatal(err)
	}
	if got := leftoverReasons(t, db); len(got) != 0 {
		t.Errorf("leftovers = %v, want none", got)
	}
	if w.removes != 0 {
		t.Errorf("removes = %d, want 0", w.removes)
	}
}

func TestSweepMergedHeadFails(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945"}
	in := SweepInput{
		Repo:         lcRepo,
		InController: func(int) bool { return false },
		MergedHead: func(context.Context, string) (string, error) {
			return "", errors.New("graphql: rate limited")
		},
	}
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), in, lcNow)
	if err != nil || len(removed) != 0 || w.removes != 0 {
		t.Fatalf("Sweep = %+v, %v, removes %d", removed, err, w.removes)
	}
	if got := leftoverReasons(t, db)[ws945]; got != "not measurable: merged PR: graphql: rate limited" {
		t.Errorf("reason = %q", got)
	}
}

// A workspace without a branch is not asked for a merged PR.
func TestSweepEmptyBranchNoMergedHead(t *testing.T) {
	db := openLeftoverDB(t)
	doneRuns(t, db, 7, "ws-7", "", "start:7")
	w := &fakeWP{lookupPath: "/p/7"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), cleanRunner(), sweepIn(false, "", &asked), lcNow)
	if err != nil || len(removed) != 1 || len(asked) != 0 {
		t.Fatalf("Sweep = %+v, %v, MergedHead asked %v", removed, err, asked)
	}
}

// removeFails fails Remove of one workspace with a forge error that is not a 409.
type removeFails struct {
	fakeWP
	fail string
}

func (f *removeFails) Remove(ctx context.Context, id string, o RemoveOptions) error {
	if id == f.fail {
		return fmt.Errorf("delete workspace %s: status 500", id)
	}
	return f.fakeWP.Remove(ctx, id, o)
}

func TestSweepRemoveErrorKeepsGoing(t *testing.T) {
	db := openLeftoverDB(t)
	doneRuns(t, db, 7, "ws-7", "issue-7", "start:7")
	doneRuns(t, db, 8, "ws-8", "issue-8", "start:8")
	w := &removeFails{fakeWP: fakeWP{lookupPath: "/p/x"}, fail: "ws-7"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), cleanRunner(), sweepIn(false, "", &asked), lcNow)
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("Sweep err = %v, want the remove error of ws-7", err)
	}
	if want := []Leftover{{Repo: lcRepo, Issue: 8, WorkspaceID: "ws-8"}}; !reflect.DeepEqual(removed, want) {
		t.Errorf("Sweep removed = %+v, want %+v", removed, want)
	}
	if want := []runs.WorkspaceRef{{Issue: 7, WorkspaceID: "ws-7", Branch: "issue-7"}}; !reflect.DeepEqual(candidates(t, db), want) {
		t.Errorf("candidates = %+v, want only ws-7 left", candidates(t, db))
	}
	if got := leftoverReasons(t, db)["ws-7"]; !strings.HasPrefix(got, "not measurable: remove: ") {
		t.Errorf("reason of ws-7 = %q", got)
	}
}

func TestSweepGoneMarksRemoved(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupErr: fmt.Errorf("404: %w", ErrGone)}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(false, "", &asked), lcNow)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Leftover{{Repo: lcRepo, Issue: 945, WorkspaceID: ws945}}; !reflect.DeepEqual(removed, want) {
		t.Errorf("Sweep removed = %+v, want %+v", removed, want)
	}
	if w.removes != 0 {
		t.Errorf("removes = %d, want 0", w.removes)
	}
	if got := candidates(t, db); len(got) != 0 {
		t.Errorf("candidates = %+v, want none", got)
	}
}

// Two nodes with the same workspace id: Remove goes to the fake of the node of the ref, and
// marking one removed leaves the other ref.
func TestSweepUsesNodeOfRef(t *testing.T) {
	db := openLeftoverDB(t)
	ctx := context.Background()
	for i, node := range []string{"", "node-a"} {
		r, err := runs.Take(ctx, db, runs.Claim{
			Repo: lcRepo, Issue: 40 + i, SessionID: fmt.Sprintf("sid-%d", i), Mode: "start", Trigger: "handoff",
			TriggerKey: fmt.Sprintf("start:%d", 40+i), Node: node, Profile: "pr-" + node, Now: lcNow,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.SetWorkplace(ctx, db, r.ID, "w1", ""); err != nil {
			t.Fatal(err)
		}
		if err := runs.Release(ctx, db, r.ID, runs.Done, "", lcNow); err != nil {
			t.Fatal(err)
		}
	}
	self := &fakeWP{lookupPath: "/p/self"}
	nodeA := &fakeWP{lookupPath: "/p/a"}
	var asked [][2]string
	place := func(node, profile string) Workplace {
		asked = append(asked, [2]string{node, profile})
		if node == "node-a" {
			return nodeA
		}
		return self
	}
	var branches []string
	removed, err := Sweep(ctx, db, place, cleanRunner(), sweepIn(false, "", &branches), lcNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// node-a is remote: its workspace is kept without a Remove; self is removed.
	if !reflect.DeepEqual(self.removed, []string{"w1"}) || len(nodeA.removed) != 0 || nodeA.removes != 0 {
		t.Errorf("self removed %v, node-a removed %v (%d)", self.removed, nodeA.removed, nodeA.removes)
	}
	if want := []Leftover{{Repo: lcRepo, Issue: 40, WorkspaceID: "w1"}}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %+v, want %+v", removed, want)
	}
	if got := candidates(t, db); len(got) != 1 || got[0].Node != "node-a" {
		t.Errorf("candidates = %+v, want only node-a's w1", got)
	}
	if len(asked) == 0 || !slices.Contains(asked, [2]string{"node-a", "pr-node-a"}) {
		t.Errorf("place asked %v, want (node-a, pr-node-a) among them", asked)
	}
}

// scheduledRun claims a run of schedule on workspace and branch, with session key key when it
// is not empty, and closes it as retry unless open.
func scheduledRun(t *testing.T, db *sql.DB, schedule, slot, workspace, branch, key string, open bool) {
	t.Helper()
	ctx := context.Background()
	r, err := runs.Take(ctx, db, runs.Claim{
		Repo: lcRepo, Schedule: schedule, SessionID: "sid-" + schedule + "-" + slot, Mode: "start",
		Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:" + schedule + ":" + slot, Branch: branch, Now: lcNow,
	})
	if err != nil {
		t.Fatalf("Take %s %s: %v", schedule, slot, err)
	}
	if err := runs.SetWorkplace(ctx, db, r.ID, workspace, branch); err != nil {
		t.Fatal(err)
	}
	if key != "" {
		if err := runs.SetSession(ctx, db, r.ID, key); err != nil {
			t.Fatal(err)
		}
	}
	if open {
		return
	}
	if err := runs.Release(ctx, db, r.ID, runs.Retry, "silent", lcNow); err != nil {
		t.Fatal(err)
	}
}

// A scheduled workspace without an open row goes forcibly, without a git check or a merged-PR
// question: nothing in it must be kept.
func TestSweepForcesScheduledWithoutOpenRow(t *testing.T) {
	db := openLeftoverDB(t)
	scheduledRun(t, db, "nightly", "20261116T0400", "ws-n", "scheduled/nightly-20261116T0400-1", "", false)
	w := &fakeWP{lookupPath: "/p/n"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(false, "", &asked), lcNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !reflect.DeepEqual(w.forced, []string{"ws-n"}) {
		t.Errorf("forced removes = %v (all removes %v), want [ws-n]", w.forced, w.removed)
	}
	if want := []Leftover{{Repo: lcRepo, Schedule: "nightly", WorkspaceID: "ws-n"}}; !reflect.DeepEqual(removed, want) {
		t.Errorf("Sweep removed = %+v, want %+v", removed, want)
	}
	if len(asked) != 0 {
		t.Errorf("MergedHead asked for %v, want none for a scheduled workspace", asked)
	}
	if got := candidates(t, db); len(got) != 0 {
		t.Errorf("candidates after Sweep = %+v, want none", got)
	}
}

// In one sweep a dirty issue workspace stays and a dirty scheduled one goes.
func TestSweepDirtyIssueStaysScheduledGoes(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	scheduledRun(t, db, "nightly", "20261116T0400", "ws-n", "scheduled/nightly-20261116T0400-1", "", false)
	w := &fakeWP{lookupPath: "/p/x"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), gitRunner(" M x.go", "abc", "3"), sweepIn(false, "", &asked), lcNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !reflect.DeepEqual(w.removed, []string{"ws-n"}) || !reflect.DeepEqual(w.forced, []string{"ws-n"}) {
		t.Errorf("removed %v, forced %v; want only ws-n, forced", w.removed, w.forced)
	}
	if want := []Leftover{{Repo: lcRepo, Schedule: "nightly", WorkspaceID: "ws-n"}}; !reflect.DeepEqual(removed, want) {
		t.Errorf("Sweep removed = %+v, want %+v", removed, want)
	}
	if got := leftoverReasons(t, db)[ws945]; got != "uncommitted changes" {
		t.Errorf("reason of the issue workspace = %q, want uncommitted changes", got)
	}
}

// An open scheduled run holds its own workspace and no other: not that of schedule b, and not an
// issue workspace either.
func TestSweepOpenScheduledRunHoldsOnlyItsWorkspace(t *testing.T) {
	db := openLeftoverDB(t)
	scheduledRun(t, db, "a", "20261116T0400", "ws-a", "scheduled/a-20261116T0400-1", "", true)
	scheduledRun(t, db, "b", "20261116T0400", "ws-b", "scheduled/b-20261116T0400-1", "", false)
	doneRuns(t, db, 7, "ws-7", "issue-7", "start:7")
	w := &fakeWP{lookupPath: "/p/x"}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), cleanRunner(), sweepIn(false, "", &asked), lcNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// runs.Workspaces orders by issue: a scheduled workspace (issue 0) comes first.
	if !reflect.DeepEqual(w.removed, []string{"ws-b", "ws-7"}) || !reflect.DeepEqual(w.forced, []string{"ws-b"}) {
		t.Errorf("removed %v, forced %v; want ws-b and ws-7, only ws-b forced", w.removed, w.forced)
	}
	want := []Leftover{{Repo: lcRepo, Schedule: "b", WorkspaceID: "ws-b"}, {Repo: lcRepo, Issue: 7, WorkspaceID: "ws-7"}}
	if !reflect.DeepEqual(removed, want) {
		t.Errorf("Sweep removed = %+v, want %+v", removed, want)
	}
	if got := candidates(t, db); len(got) != 1 || got[0].WorkspaceID != "ws-a" {
		t.Errorf("candidates = %+v, want only ws-a", got)
	}
}

// An open run of schedule a holds the workspace of an earlier, closed attempt of a neither: every
// attempt has its own branch and workspace.
func TestSweepOpenScheduledRunLeavesEarlierAttempt(t *testing.T) {
	db := openLeftoverDB(t)
	scheduledRun(t, db, "a", "20261116T0400", "ws-a1", "scheduled/a-20261116T0400-1", "", false)
	scheduledRun(t, db, "a", "20261116T0400", "ws-a2", "scheduled/a-20261116T0400-2", "", true)
	w := &fakeWP{lookupPath: "/p/x"}
	var asked []string
	if _, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(false, "", &asked), lcNow); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !reflect.DeepEqual(w.forced, []string{"ws-a1"}) || len(w.removed) != 1 {
		t.Errorf("removed %v, forced %v; want only ws-a1, forced", w.removed, w.forced)
	}
}

// The agent of a closed scheduled run is the schedule's own: Cleanup stops it and removes the
// workspace, instead of keeping it for a foreign session.
func TestSweepStopsOwnScheduledSession(t *testing.T) {
	db := openLeftoverDB(t)
	scheduledRun(t, db, "nightly", "20261116T0400", "ws-n", "scheduled/nightly-20261116T0400-1", "k-n", false)
	w := &fakeWP{lookupPath: "/p/n", sessions: []Session{{Key: "k-n", SessionID: "sid-nightly-20261116T0400", State: "working"}}}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(false, "", &asked), lcNow)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !reflect.DeepEqual(w.stopped, []string{"ws-n:sid-nightly-20261116T0400"}) || len(removed) != 1 {
		t.Errorf("stopped %v, removed %+v, leftovers %v; want the own session stopped and ws-n removed",
			w.stopped, removed, leftoverReasons(t, db))
	}
}

// A forced Remove the workplace still refuses keeps the workspace with the reason.
func TestSweepForcedRemoveRefused(t *testing.T) {
	db := openLeftoverDB(t)
	scheduledRun(t, db, "nightly", "20261116T0400", "ws-n", "scheduled/nightly-20261116T0400-1", "", false)
	w := &fakeWP{lookupPath: "/p/n", removeErr: fmt.Errorf("worktree holds a worktree: %w", ErrDirty)}
	var asked []string
	removed, err := Sweep(context.Background(), db, only(w), noRunner(t), sweepIn(false, "", &asked), lcNow)
	if err != nil || len(removed) != 0 {
		t.Fatalf("Sweep = %+v, %v; want nothing removed and no error", removed, err)
	}
	ls, err := Leftovers(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	want := []Leftover{{Repo: lcRepo, Schedule: "nightly", WorkspaceID: "ws-n", Path: "/p/n", Reason: "forced remove refused: worktree holds a worktree: workplace: workspace is dirty"}}
	if !reflect.DeepEqual(ls, want) {
		t.Errorf("leftovers = %+v, want %+v", ls, want)
	}
}

// AdoptSessions skips the keys of the schedule's own earlier runs, as it does for an issue: a
// scheduled run shares no issue number to look them up by.
func TestAdoptSessionsScheduledSkipsKeyOfItsSchedule(t *testing.T) {
	db := openLeftoverDB(t)
	ctx := context.Background()
	var open runs.Run
	for i, slot := range []string{"20261115T0400", "20261116T0400"} {
		r, err := runs.Take(ctx, db, runs.Claim{
			Repo: lcRepo, Schedule: "nightly", SessionID: "sid-x", Mode: "start",
			Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:nightly:" + slot, Now: lcNow,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.SetWorkplace(ctx, db, r.ID, "ws-n", "scheduled/nightly-"+slot+"-1"); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := runs.SetSession(ctx, db, r.ID, "k1"); err != nil {
				t.Fatal(err)
			}
			if err := runs.Release(ctx, db, r.ID, runs.Done, "", lcNow); err != nil {
				t.Fatal(err)
			}
			continue
		}
		open = r
	}
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: "sid-x", State: "done"}, {Key: "k2", SessionID: "sid-x", State: "working"}}}
	if err := AdoptSessions(ctx, db, only(w), lcRepo); err != nil {
		t.Fatal(err)
	}
	if got := rowOf(t, db, open.ID); got.session != "k2" {
		t.Errorf("row = %+v, want the open run on k2, not the earlier run's k1", got)
	}
}
