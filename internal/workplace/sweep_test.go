package workplace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
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
	removed, err := Sweep(context.Background(), db, w, cleanRunner(), sweepIn(false, "", &asked), lcNow)
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
	removed, err := Sweep(context.Background(), db, w, gitRunner(" M x.go", "abc", "0"), sweepIn(false, "", &asked), lcNow)
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
	if _, err := Sweep(context.Background(), db, w, gitRunner("", "abc", "2"), sweepIn(false, "", &asked), lcNow); err != nil {
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
	removed, err := Sweep(context.Background(), db, w, gitRunner("", "abc", "2"), sweepIn(false, "abc", &asked), lcNow)
	if err != nil || len(removed) != 1 || w.removes != 1 {
		t.Fatalf("Sweep = %+v, %v, removes %d", removed, err, w.removes)
	}
}

func TestSweepSkipsController(t *testing.T) {
	db := openLeftoverDB(t)
	fixture945(t, db)
	w := &fakeWP{lookupPath: "/p/945", sessionsErr: errors.New("unexpected Sessions")}
	var asked []string
	removed, err := Sweep(context.Background(), db, w, noRunner(t), sweepIn(true, "", &asked), lcNow)
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
	removed, err := Sweep(context.Background(), db, w, noRunner(t), sweepIn(false, "", &asked), lcNow)
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
	if _, err := Sweep(context.Background(), db, w, noRunner(t), sweepIn(true, "", &asked), lcNow); err != nil {
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
	removed, err := Sweep(context.Background(), db, w, noRunner(t), in, lcNow)
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
	removed, err := Sweep(context.Background(), db, w, cleanRunner(), sweepIn(false, "", &asked), lcNow)
	if err != nil || len(removed) != 1 || len(asked) != 0 {
		t.Fatalf("Sweep = %+v, %v, MergedHead asked %v", removed, err, asked)
	}
}

// removeFails fails Remove of one workspace with a forge error that is not a 409.
type removeFails struct {
	fakeWP
	fail string
}

func (f *removeFails) Remove(ctx context.Context, id string) error {
	if id == f.fail {
		return fmt.Errorf("delete workspace %s: status 500", id)
	}
	return f.fakeWP.Remove(ctx, id)
}

func TestSweepRemoveErrorKeepsGoing(t *testing.T) {
	db := openLeftoverDB(t)
	doneRuns(t, db, 7, "ws-7", "issue-7", "start:7")
	doneRuns(t, db, 8, "ws-8", "issue-8", "start:8")
	w := &removeFails{fakeWP: fakeWP{lookupPath: "/p/x"}, fail: "ws-7"}
	var asked []string
	removed, err := Sweep(context.Background(), db, w, cleanRunner(), sweepIn(false, "", &asked), lcNow)
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
	removed, err := Sweep(context.Background(), db, w, noRunner(t), sweepIn(false, "", &asked), lcNow)
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
