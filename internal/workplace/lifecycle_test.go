package workplace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

var lcNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

const lcRepo = "o/r"

// fakeWP is a scriptable Workplace that records its calls.
type fakeWP struct {
	prepareErr, spawnErr, sessionsErr, stopErr, removeErr, discardErr error
	spawned                                                           Session
	sessions                                                          []Session
	spawns, stops, removes                                            int
	stopped                                                           []string
	removed                                                           []string
	discarded                                                         []string
}

func (f *fakeWP) Prepare(_ context.Context, branch string) (Workspace, error) {
	if f.prepareErr != nil {
		return Workspace{}, f.prepareErr
	}
	return Workspace{ID: "w1", Path: "/p/w1", Branch: branch}, nil
}

func (f *fakeWP) Spawn(_ context.Context, _ Workspace, _ LaunchSpec) (Session, error) {
	f.spawns++
	return f.spawned, f.spawnErr
}

func (f *fakeWP) Sessions(_ context.Context, _ string) ([]Session, error) {
	return f.sessions, f.sessionsErr
}

func (f *fakeWP) Stop(_ context.Context, id string, sessionIDs []string) error {
	f.stops++
	f.stopped = append(f.stopped, id+":"+strings.Join(sessionIDs, ","))
	return f.stopErr
}

func (f *fakeWP) Remove(_ context.Context, id string) error {
	f.removes++
	f.removed = append(f.removed, id)
	return f.removeErr
}

func (f *fakeWP) Discard(_ context.Context, id string) error {
	f.discarded = append(f.discarded, id)
	return f.discardErr
}

func takeRun(t *testing.T, db *sql.DB, issue int, started time.Time) (runs.Run, string) {
	t.Helper()
	sid := fmt.Sprintf("sid-%d", issue)
	r, err := runs.Take(context.Background(), db, runs.Claim{
		Repo: lcRepo, Issue: issue, SessionID: sid, Mode: "start", Trigger: "handoff",
		TriggerKey: fmt.Sprintf("start:%d", issue), Now: started,
	})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	return r, sid
}

type runRow struct{ outcome, reason, session, workspace string }

func rowOf(t *testing.T, db *sql.DB, id int64) runRow {
	t.Helper()
	var r runRow
	err := db.QueryRow(`SELECT coalesce(outcome,''), coalesce(reason,''), workplace_session, workspace_id
		FROM runs WHERE id = ?`, id).Scan(&r.outcome, &r.reason, &r.session, &r.workspace)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	return r
}

func TestLaunchNotAcceptedReleasesStartFailed(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow)
	w := &fakeWP{spawnErr: fmt.Errorf("forge said 422: %w", ErrNotAccepted)}
	out, err := Launch(context.Background(), db, w, run.ID, "issue-7", LaunchSpec{}, lcNow)
	if err != nil || out != NotAccepted {
		t.Fatalf("Launch = %v, %v; want NotAccepted, nil", out, err)
	}
	got := rowOf(t, db, run.ID)
	if got.outcome != "start_failed" || !strings.Contains(got.reason, "422") || got.session != "" {
		t.Errorf("row = %+v", got)
	}
	if w.spawns != 1 {
		t.Errorf("spawns = %d, want 1", w.spawns)
	}
}

func TestLaunchUncertainKeepsRowOpen(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow)
	w := &fakeWP{spawnErr: fmt.Errorf("timeout: %w", ErrUncertain)}
	out, err := Launch(context.Background(), db, w, run.ID, "issue-7", LaunchSpec{}, lcNow)
	if !errors.Is(err, ErrUncertain) || !strings.Contains(err.Error(), "timeout") || out != Uncertain {
		t.Fatalf("Launch = %v, %v; want Uncertain, error wrapping ErrUncertain with its text", out, err)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" || got.session != "" || got.workspace != "w1" {
		t.Errorf("row = %+v", got)
	}
	if w.spawns != 1 {
		t.Errorf("spawns = %d, want 1", w.spawns)
	}
}

func TestLaunchOtherSpawnErrorIsUncertain(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow)
	w := &fakeWP{spawnErr: errors.New("boom")}
	out, err := Launch(context.Background(), db, w, run.ID, "issue-7", LaunchSpec{}, lcNow)
	if err == nil || !strings.Contains(err.Error(), "boom") || out != Uncertain {
		t.Fatalf("Launch = %v, %v; want Uncertain, error with cause boom", out, err)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" {
		t.Errorf("row closed: %+v", got)
	}
}

func TestLaunchAcceptedSetsSession(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow)
	w := &fakeWP{spawned: Session{Key: "k1"}}
	out, err := Launch(context.Background(), db, w, run.ID, "issue-7", LaunchSpec{}, lcNow)
	if err != nil || out != Accepted {
		t.Fatalf("Launch = %v, %v; want Accepted, nil", out, err)
	}
	if got := rowOf(t, db, run.ID); got != (runRow{session: "k1", workspace: "w1"}) {
		t.Errorf("row = %+v", got)
	}
}

func TestLaunchSetWorkplaceFailsReleasesStartFailed(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow)
	if _, err := db.Exec(`CREATE TRIGGER no_workspace BEFORE UPDATE OF workspace_id ON runs
		BEGIN SELECT RAISE(ABORT, 'disk says no'); END`); err != nil {
		t.Fatal(err)
	}
	w := &fakeWP{}
	out, err := Launch(context.Background(), db, w, run.ID, "issue-7", LaunchSpec{}, lcNow)
	if out != NotAccepted || err == nil || !strings.Contains(err.Error(), "disk says no") {
		t.Fatalf("Launch = %v, %v; want NotAccepted, error with the cause", out, err)
	}
	got := rowOf(t, db, run.ID)
	if got.outcome != "start_failed" || !strings.HasPrefix(got.reason, "record workplace: ") {
		t.Errorf("row = %+v", got)
	}
	if w.spawns != 0 {
		t.Errorf("spawns = %d, want 0", w.spawns)
	}
}

func TestLaunchPrepareFails(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow)
	w := &fakeWP{prepareErr: errors.New("no branch")}
	out, err := Launch(context.Background(), db, w, run.ID, "issue-7", LaunchSpec{}, lcNow)
	if err != nil || out != NotAccepted {
		t.Fatalf("Launch = %v, %v; want NotAccepted, nil", out, err)
	}
	got := rowOf(t, db, run.ID)
	if got.outcome != "start_failed" || got.reason != "prepare: no branch" {
		t.Errorf("row = %+v", got)
	}
	if w.spawns != 0 {
		t.Errorf("spawns = %d, want 0", w.spawns)
	}
}

var lcLimits = Limits{MaxWait: 10 * time.Minute, MaxRuntime: 60 * time.Minute}

// openRunWithWorkspace takes a run and records its workspace, as Launch would.
func openRunWithWorkspace(t *testing.T, db *sql.DB, issue int, started time.Time, session string) (runs.Run, string) {
	t.Helper()
	run, sid := takeRun(t, db, issue, started)
	ctx := context.Background()
	if err := runs.SetWorkplace(ctx, db, run.ID, "w1", "issue-7"); err != nil {
		t.Fatal(err)
	}
	if session != "" {
		if err := runs.SetSession(ctx, db, run.ID, session); err != nil {
			t.Fatal(err)
		}
	}
	return run, sid
}

func TestCheckOpenUncertainFoundLater(t *testing.T) {
	db := openLeftoverDB(t)
	run, sid := openRunWithWorkspace(t, db, 7, lcNow.Add(-time.Minute), "")
	w := &fakeWP{sessions: []Session{{Key: "other", SessionID: "x"}, {Key: "k9", SessionID: sid}}}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil || len(closed) != 0 {
		t.Fatalf("CheckOpen = %v, %v", closed, err)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" || got.session != "k9" {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenUncertainPastMaxWait(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-11*time.Minute), "")
	w := &fakeWP{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil {
		t.Fatal(err)
	}
	want := []Closed{{RunID: run.ID, Issue: 7, Reason: ReasonUncertain}}
	if !reflect.DeepEqual(closed, want) {
		t.Errorf("closed = %+v, want %+v", closed, want)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "returned" || got.reason != "start uncertain: no session found" {
		t.Errorf("row = %+v", got)
	}
	if want := []string{"w1"}; !reflect.DeepEqual(w.discarded, want) {
		t.Errorf("discarded = %v, want %v", w.discarded, want)
	}
}

func TestCheckOpenDiscardFailsStillReturnsRow(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-11*time.Minute), "")
	w := &fakeWP{discardErr: errors.New("read-only state dir")}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err == nil || !strings.Contains(err.Error(), "read-only state dir") {
		t.Fatalf("err = %v, want the discard error", err)
	}
	if want := []Closed{{RunID: run.ID, Issue: 7, Reason: ReasonUncertain}}; !reflect.DeepEqual(closed, want) {
		t.Errorf("closed = %+v, want %+v", closed, want)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "returned" {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenUncertainWithinMaxWait(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-9*time.Minute), "")
	w := &fakeWP{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil || len(closed) != 0 || len(w.discarded) != 0 {
		t.Fatalf("CheckOpen = %v, %v, discarded %v", closed, err, w.discarded)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenUncertainPastBothLimits(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-2*time.Hour), "")
	w := &fakeWP{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(closed) != 1 || closed[0].Reason != ReasonUncertain {
		t.Errorf("closed = %+v", closed)
	}
	if w.stops != 0 {
		t.Errorf("stops = %d, want 0", w.stops)
	}
	if got := rowOf(t, db, run.ID); got.reason != ReasonUncertain {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenSessionsFailsLeavesRow(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-time.Hour), "")
	w := &fakeWP{sessionsErr: errors.New("forge down")}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err == nil || !strings.Contains(err.Error(), "forge down") || len(closed) != 0 {
		t.Fatalf("CheckOpen = %v, %v", closed, err)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenOverdueStops(t *testing.T) {
	db := openLeftoverDB(t)
	run, sid := openRunWithWorkspace(t, db, 7, lcNow.Add(-61*time.Minute), "k1")
	w := &fakeWP{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Closed{{RunID: run.ID, Issue: 7, Reason: ReasonTimeout}}; !reflect.DeepEqual(closed, want) {
		t.Errorf("closed = %+v, want %+v", closed, want)
	}
	if want := []string{"w1:" + sid}; !reflect.DeepEqual(w.stopped, want) {
		t.Errorf("stopped = %v, want %v", w.stopped, want)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "returned" || got.reason != "timeout (max_runtime)" {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenWithinRuntimeUntouched(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-59*time.Minute), "k1")
	w := &fakeWP{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil || len(closed) != 0 || w.stops != 0 {
		t.Fatalf("CheckOpen = %v, %v, stops %d", closed, err, w.stops)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" {
		t.Errorf("row = %+v", got)
	}
}

func TestCheckOpenStopFailsKeepsOpen(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-61*time.Minute), "k1")
	w := &fakeWP{stopErr: errors.New("systemctl failed")}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err == nil || !strings.Contains(err.Error(), "systemctl failed") || len(closed) != 0 {
		t.Fatalf("CheckOpen = %v, %v", closed, err)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" {
		t.Errorf("row = %+v", got)
	}
}

// stopFailsFirst fails Stop for the first workspace call only.
type stopFailsFirst struct{ fakeWP }

func (f *stopFailsFirst) Stop(ctx context.Context, id string, ids []string) error {
	err := f.fakeWP.Stop(ctx, id, ids)
	if f.stops == 1 {
		return errors.New("first stop failed")
	}
	return err
}

func TestCheckOpenOneFailureDoesNotBlockTheNext(t *testing.T) {
	db := openLeftoverDB(t)
	first, _ := openRunWithWorkspace(t, db, 7, lcNow.Add(-2*time.Hour), "k1")
	second, _ := openRunWithWorkspace(t, db, 8, lcNow.Add(-2*time.Hour), "k2")
	third, _ := openRunWithWorkspace(t, db, 9, lcNow.Add(-2*time.Hour), "k3")
	w := &stopFailsFirst{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err == nil || !strings.Contains(err.Error(), "first stop failed") {
		t.Fatalf("err = %v", err)
	}
	want := []Closed{{RunID: second.ID, Issue: 8, Reason: ReasonTimeout}, {RunID: third.ID, Issue: 9, Reason: ReasonTimeout}}
	if !reflect.DeepEqual(closed, want) {
		t.Errorf("closed = %+v, want %+v", closed, want)
	}
	if got := rowOf(t, db, first.ID); got.outcome != "" {
		t.Errorf("first row = %+v", got)
	}
}

func TestCheckOpenSkipsRowsWithoutWorkspace(t *testing.T) {
	db := openLeftoverDB(t)
	run, _ := takeRun(t, db, 7, lcNow.Add(-5*time.Hour))
	w := &fakeWP{}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil || len(closed) != 0 || w.stops != 0 {
		t.Fatalf("CheckOpen = %v, %v", closed, err)
	}
	if got := rowOf(t, db, run.ID); got.outcome != "" {
		t.Errorf("row = %+v", got)
	}
}

// gitRunner answers the git calls of Dirty and Unpushed.
func gitRunner(status, head string, ahead string) Runner {
	return func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		if name != "git" {
			return "", -1, fmt.Errorf("unexpected command %s", name)
		}
		joined := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(joined, "status --porcelain"):
			return status, 0, nil
		case strings.HasSuffix(joined, "rev-parse HEAD"):
			return head + "\n", 0, nil
		case strings.Contains(joined, "rev-list --count"):
			return ahead + "\n", 0, nil
		}
		return "", -1, fmt.Errorf("unexpected git %s", joined)
	}
}

func cleanRunner() Runner { return gitRunner("", "abc", "0") }

func cand(issue int) Candidate {
	return Candidate{Repo: lcRepo, Issue: issue, Workspace: Workspace{ID: "w1", Path: "/p/w1", Branch: "issue-7"}}
}

func leftoverReasons(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	ls, err := Leftovers(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, l := range ls {
		out[l.WorkspaceID] = l.Reason
	}
	return out
}

func TestCleanupRemovesClean(t *testing.T) {
	db := openLeftoverDB(t)
	run, sid := takeRun(t, db, 7, lcNow.Add(-time.Hour))
	if err := runs.SetSession(context.Background(), db, run.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(context.Background(), db, run.ID, runs.Done, "", lcNow); err != nil {
		t.Fatal(err)
	}
	if err := RecordLeftover(context.Background(), db, Leftover{Repo: lcRepo, Issue: 7, WorkspaceID: "w1", Reason: "old"}, lcNow); err != nil {
		t.Fatal(err)
	}
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: sid, State: "idle"}, {Key: "k2", SessionID: "foreign", State: "done"}}}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || !removed {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if want := []string{"w1:" + sid}; !reflect.DeepEqual(w.stopped, want) {
		t.Errorf("stopped = %v, want %v", w.stopped, want)
	}
	if !reflect.DeepEqual(w.removed, []string{"w1"}) {
		t.Errorf("removed = %v", w.removed)
	}
	if got := leftoverReasons(t, db); len(got) != 0 {
		t.Errorf("leftovers = %v", got)
	}
}

func TestCleanupInControllerUntouched(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{}
	c := cand(7)
	c.InController = true
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), c, lcNow)
	if err != nil || removed || w.stops != 0 || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d", removed, err, w.stops, w.removes)
	}
	if got := leftoverReasons(t, db); len(got) != 0 {
		t.Errorf("leftovers = %v", got)
	}
}

func TestCleanupOpenRunUntouched(t *testing.T) {
	db := openLeftoverDB(t)
	takeRun(t, db, 7, lcNow)
	w := &fakeWP{}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || removed || w.stops != 0 || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d", removed, err, w.stops, w.removes)
	}
}

func TestCleanupStaleForeignSessionStays(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{sessions: []Session{{Key: "kf", SessionID: "foreign", State: "idle", UpdatedAt: lcNow.Add(-72 * time.Hour)}}}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || removed || w.removes != 0 || w.stops != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d", removed, err, w.stops, w.removes)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "foreign session kf" {
		t.Errorf("reason = %q", got)
	}
}

// Own sessions are stopped before the git checks, so an own agent cannot
// commit after them; a dirty workspace is stopped and still stays.
func TestCleanupDirtyStays(t *testing.T) {
	db := openLeftoverDB(t)
	sid := closedOwnRun(t, db, "k1")
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: sid, State: "working"}}}
	removed, err := Cleanup(context.Background(), db, w, gitRunner(" M a.txt\n", "abc", "0"), cand(7), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if want := []string{"w1:" + sid}; !reflect.DeepEqual(w.stopped, want) {
		t.Errorf("stopped = %v, want %v", w.stopped, want)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "uncommitted changes" {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupUnpushedStays(t *testing.T) {
	db := openLeftoverDB(t)
	sid := closedOwnRun(t, db, "k1")
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: sid, State: "idle"}}}
	removed, err := Cleanup(context.Background(), db, w, gitRunner("", "abc", "2"), cand(7), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if want := []string{"w1:" + sid}; !reflect.DeepEqual(w.stopped, want) {
		t.Errorf("stopped = %v, want %v", w.stopped, want)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "unpushed commits" {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupMergedHeadRemoves(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{}
	c := cand(7)
	c.MergedHead = "abc"
	removed, err := Cleanup(context.Background(), db, w, gitRunner("", "abc", "5"), c, lcNow)
	if err != nil || !removed || w.removes != 1 {
		t.Fatalf("Cleanup = %v, %v, removes %d", removed, err, w.removes)
	}
}

func TestCleanupGitErrorStays(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{}
	broken := func(context.Context, time.Duration, string, ...string) (string, int, error) {
		return "", 128, nil
	}
	removed, err := Cleanup(context.Background(), db, w, broken, cand(7), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if got := leftoverReasons(t, db)["w1"]; !strings.HasPrefix(got, "not measurable: ") {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupUnpushedErrorStays(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{}
	run := func(_ context.Context, _ time.Duration, _ string, args ...string) (string, int, error) {
		if strings.Contains(strings.Join(args, " "), "rev-list") {
			return "", 128, nil
		}
		return "", 0, nil
	}
	removed, err := Cleanup(context.Background(), db, w, run, cand(7), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, removes %d", removed, err, w.removes)
	}
	if got := leftoverReasons(t, db)["w1"]; !strings.HasPrefix(got, "not measurable: ") || !strings.Contains(got, "128") {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupSessionsErrorStays(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{sessionsErr: errors.New("down")}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if got := leftoverReasons(t, db)["w1"]; !strings.HasPrefix(got, "not measurable: ") {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupStopFailsStays(t *testing.T) {
	db := openLeftoverDB(t)
	run, sid := takeRun(t, db, 7, lcNow.Add(-time.Hour))
	if err := runs.SetSession(context.Background(), db, run.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(context.Background(), db, run.ID, runs.Done, "", lcNow); err != nil {
		t.Fatal(err)
	}
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: sid, State: "idle"}}, stopErr: errors.New("nope")}
	// Stop comes before the git checks: after a failed stop git never runs.
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), cand(7), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "not measurable: stop: nope" {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupNoOwnSessionsSkipsStop(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{sessions: []Session{{Key: "k2", SessionID: "foreign", State: "done"}}}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || !removed || w.stops != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d", removed, err, w.stops)
	}
}

func TestCleanup409Stays(t *testing.T) {
	db := openLeftoverDB(t)
	ctx := context.Background()
	cases := []struct {
		ws     string
		runner Runner
		wp     *fakeWP
		want   string
	}{
		{"wa", gitRunner(" M x\n", "abc", "0"), &fakeWP{}, "uncommitted changes"},
		{"wb", gitRunner("", "abc", "3"), &fakeWP{}, "unpushed commits"},
		{"wc", cleanRunner(), &fakeWP{removeErr: fmt.Errorf("409: %w", ErrDirty)}, "forge refused: dirty worktree (409)"},
	}
	for i, c := range cases {
		cd := cand(100 + i)
		cd.Workspace.ID = c.ws
		removed, err := Cleanup(ctx, db, c.wp, c.runner, cd, lcNow)
		if err != nil || removed {
			t.Fatalf("%s: Cleanup = %v, %v", c.ws, removed, err)
		}
	}
	want := map[string]string{"wa": "uncommitted changes", "wb": "unpushed commits", "wc": "forge refused: dirty worktree (409)"}
	if got := leftoverReasons(t, db); !reflect.DeepEqual(got, want) {
		t.Errorf("leftovers = %v, want %v", got, want)
	}
}

func TestCleanupRemoveOtherErrorStays(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{removeErr: errors.New("500")}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err == nil || removed {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "not measurable: remove: 500" {
		t.Errorf("reason = %q", got)
	}
}

// resumeRows writes the history of a resume: a start row of issue 7 closed
// with key k1, then a resume row with the same session_id. It returns that
// session_id and the resume row.
func resumeRows(t *testing.T, db *sql.DB, resumeStarted time.Time) (string, runs.Run) {
	t.Helper()
	ctx := context.Background()
	const sid = "sid-shared"
	start, err := runs.Take(ctx, db, runs.Claim{Repo: lcRepo, Issue: 7, SessionID: sid, Mode: "start",
		Trigger: "handoff", TriggerKey: "start:7", Now: lcNow.Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, db, start.ID, "w1", "issue-7"); err != nil {
		t.Fatal(err)
	}
	if err := runs.SetSession(ctx, db, start.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, db, start.ID, runs.Handoff, "", lcNow.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	resume, err := runs.Take(ctx, db, runs.Claim{Repo: lcRepo, Issue: 7, SessionID: sid, Mode: "resume",
		Trigger: "answer", TriggerKey: "answer:7", Now: resumeStarted})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, db, resume.ID, "w1", "issue-7"); err != nil {
		t.Fatal(err)
	}
	return sid, resume
}

func TestResumeStartFailedLateAgentIsForeign(t *testing.T) {
	db := openLeftoverDB(t)
	ctx := context.Background()
	sid, resume := resumeRows(t, db, lcNow.Add(-time.Hour))
	if err := runs.Release(ctx, db, resume.ID, runs.StartFailed, "forge 503", lcNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	late := Session{Key: "k2", SessionID: sid, State: "working", UpdatedAt: lcNow}
	ownKeys, err := runs.SessionKeys(ctx, db, lcRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := Busy([]Session{late}, ownKeys, nil, nil, lcNow, time.Hour); !ok || got.Key != "k2" {
		t.Errorf("Busy = %v %v, want busy k2", got, ok)
	}
	if got, ok := ForeignLive([]Session{late}, ownKeys); !ok || got.Key != "k2" {
		t.Errorf("ForeignLive = %v %v, want live k2", got, ok)
	}
	w := &fakeWP{sessions: []Session{late}}
	removed, err := Cleanup(ctx, db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || removed || w.stops != 0 || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d", removed, err, w.stops, w.removes)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "foreign session k2" {
		t.Errorf("reason = %q", got)
	}
}

func TestCheckOpenIgnoresSessionOfEarlierRow(t *testing.T) {
	db := openLeftoverDB(t)
	sid, resume := resumeRows(t, db, lcNow.Add(-11*time.Minute))
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: sid, State: "done"}}}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Closed{{RunID: resume.ID, Issue: 7, Reason: ReasonUncertain}}; !reflect.DeepEqual(closed, want) {
		t.Errorf("closed = %+v, want %+v", closed, want)
	}
	if got := rowOf(t, db, resume.ID); got.session != "" || got.outcome != "returned" {
		t.Errorf("row = %+v, want returned without the earlier key", got)
	}
}

func TestCheckOpenResumeFindsItsNewSession(t *testing.T) {
	db := openLeftoverDB(t)
	sid, resume := resumeRows(t, db, lcNow.Add(-time.Minute))
	w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: sid, State: "done"}, {Key: "k2", SessionID: sid, State: "working"}}}
	closed, err := CheckOpen(context.Background(), db, w, lcRepo, lcLimits, lcNow)
	if err != nil || len(closed) != 0 {
		t.Fatalf("CheckOpen = %v, %v", closed, err)
	}
	if got := rowOf(t, db, resume.ID); got.session != "k2" || got.outcome != "" {
		t.Errorf("row = %+v, want open with k2", got)
	}
}

// closedOwnRun records a closed run of issue 7 that held key.
func closedOwnRun(t *testing.T, db *sql.DB, key string) string {
	t.Helper()
	run, sid := takeRun(t, db, 7, lcNow.Add(-time.Hour))
	if err := runs.SetSession(context.Background(), db, run.ID, key); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(context.Background(), db, run.ID, runs.Done, "", lcNow); err != nil {
		t.Fatal(err)
	}
	return sid
}

func TestCleanupOwnSessionWithoutSessionIDStays(t *testing.T) {
	db := openLeftoverDB(t)
	closedOwnRun(t, db, "k1")
	w := &fakeWP{sessions: []Session{{Key: "k1", State: "working"}}}
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), cand(7), lcNow)
	if err != nil || removed || w.stops != 0 || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d", removed, err, w.stops, w.removes)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "not measurable: own session k1 has no session id" {
		t.Errorf("reason = %q", got)
	}
}

func TestCleanupDoneOwnSessionWithoutSessionIDRemoves(t *testing.T) {
	db := openLeftoverDB(t)
	closedOwnRun(t, db, "k1")
	w := &fakeWP{sessions: []Session{{Key: "k1", State: "done"}}}
	removed, err := Cleanup(context.Background(), db, w, cleanRunner(), cand(7), lcNow)
	if err != nil || !removed || w.stops != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d", removed, err, w.stops)
	}
}

// noRunner fails the test on any subprocess: git must not run in the cwd.
func noRunner(t *testing.T) Runner {
	return func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		t.Errorf("unexpected command %s %v", name, args)
		return "", -1, errors.New("unexpected command")
	}
}

func TestCleanupWithoutWorkspaceIDErrors(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{}
	c := cand(7)
	c.Workspace.ID = ""
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), c, lcNow)
	if err == nil || removed || w.stops != 0 || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d; want an error and nothing done", removed, err, w.stops, w.removes)
	}
	if got := leftoverReasons(t, db); len(got) != 0 {
		t.Errorf("leftovers = %v, want none", got)
	}
}

func TestCleanupWithoutAbsolutePathStays(t *testing.T) {
	for _, path := range []string{"", "p/w1"} {
		db := openLeftoverDB(t)
		closedOwnRun(t, db, "k1")
		w := &fakeWP{sessions: []Session{{Key: "k1", SessionID: "sid-7", State: "idle"}}}
		c := cand(7)
		c.Workspace.Path = path
		removed, err := Cleanup(context.Background(), db, w, noRunner(t), c, lcNow)
		if err != nil || removed || w.stops != 0 || w.removes != 0 {
			t.Fatalf("%q: Cleanup = %v, %v, stops %d removes %d", path, removed, err, w.stops, w.removes)
		}
		if got := leftoverReasons(t, db)["w1"]; got != "not measurable: no workspace path" {
			t.Errorf("%q: reason = %q", path, got)
		}
	}
}

// Stop ends the whole unit of the worktree: never while a foreign session lives there.
func TestCleanupForeignLiveNoStopOfOwn(t *testing.T) {
	db := openLeftoverDB(t)
	sid := closedOwnRun(t, db, "k1")
	w := &fakeWP{sessions: []Session{
		{Key: "k1", SessionID: sid, State: "working"},
		{Key: "kf", SessionID: "foreign", State: "idle"},
	}}
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), cand(7), lcNow)
	if err != nil || removed || w.stops != 0 || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, stops %d removes %d", removed, err, w.stops, w.removes)
	}
	if got := leftoverReasons(t, db)["w1"]; got != "foreign session kf" {
		t.Errorf("reason = %q", got)
	}
}
