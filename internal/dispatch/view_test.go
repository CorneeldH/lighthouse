package dispatch

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func openViewStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "lh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRunViews(t *testing.T) {
	pre := Pre{
		Items:  []Item{{Issue: 3, Title: "Drie"}, {Issue: 5, Title: "Vijf"}},
		Stands: map[int64]workplace.Stand{10: workplace.Waiting},
	}
	_, _, sdd := sddWorktree(t, "Task 1: complete\nTask 2: complete\n")
	open := []runs.Brief{{ID: 10, Issue: 3, Command: sdd, Trigger: "board", Profile: "p", WorkspaceID: "w"}}
	// Run 7 was measured open in the precheck and parked by apply: a parked view has no stand.
	pre.Stands[7] = workplace.Done
	parked := []runs.Brief{{ID: 7, Issue: 5, Command: "/lighthouse-review-plan /x/y.md"}}
	got := RunViews(pre, open, parked)
	if len(got) != 2 {
		t.Fatalf("RunViews = %+v, want 2 views", got)
	}
	if v := got[0]; v.Issue != 3 || v.State != "open" || v.Title != "Drie" || v.Stand != "waiting" || v.Command != sdd || v.WorkspaceID != "w" ||
		v.Phase != "sdd" || v.Task != 3 || v.Tasks != 4 || v.Step != "build" {
		t.Errorf("view 0 = %+v", v)
	}
	if v := got[1]; v.Issue != 5 || v.State != "parked" || v.Title != "Vijf" || v.Stand != "" ||
		v.Phase != "plan-review" || v.Task != 0 || v.Tasks != 0 || v.Step != "" {
		t.Errorf("view 1 = %+v", v)
	}
}

// #309: a forge stand done with background work or an open turn in the transcript is not done
// for the tick (runCtx.done), so the view shows it as working.
func TestRunViewsDoneWithWorkAfterStop(t *testing.T) {
	cases := []struct {
		name       string
		transcript *workplace.Transcript
		measured   bool
		want       string
	}{
		{"background work", &workplace.Transcript{Background: 1}, true, "working"},
		{"open turn", &workplace.Transcript{TurnOpen: true}, true, "working"},
		{"settled transcript", &workplace.Transcript{}, true, "done"},
		{"transcript not measurable", nil, true, "done"},
		{"run not measured", nil, false, "done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pre := Pre{Stands: map[int64]workplace.Stand{4: workplace.Done}}
			if tc.measured {
				pre.Runs = map[int64]RunState{4: {Transcript: tc.transcript}}
			}
			got := RunViews(pre, []runs.Brief{{ID: 4, Issue: 9}}, nil)
			if len(got) != 1 || got[0].Stand != tc.want {
				t.Errorf("RunViews = %+v, want one view with stand %q", got, tc.want)
			}
		})
	}
}

func TestRunViewsUnmeasured(t *testing.T) {
	got := RunViews(Pre{}, []runs.Brief{{ID: 1, Issue: 2}}, nil)
	if len(got) != 1 || got[0].Stand != "" || got[0].State != "open" {
		t.Errorf("RunViews = %+v, want one open view with stand \"\"", got)
	}
}

// #945: four done runs and no open or parked one give no Running runs card, so the issue is
// not shown as running or waiting once it left Controller.
func TestRunViewsNoRunNoView(t *testing.T) {
	ctx := context.Background()
	db := openViewStore(t).DB
	for _, key := range []string{"start:945", "answer:1", "answer:2", "handoff:3"} {
		r, err := runs.Take(ctx, db, runs.Claim{Repo: "o/r", Issue: 945, SessionID: runs.NewSessionID(), Mode: "start",
			Trigger: "handoff", TriggerKey: key, Now: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.SetWorkplace(ctx, db, r.ID, "d7c0b0ea0de54804", "work-issue-945-immich-smart-search-191b7750"); err != nil {
			t.Fatal(err)
		}
		if err := runs.Release(ctx, db, r.ID, runs.Done, "", time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	open, err := runs.OpenBriefs(ctx, db, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if got := RunViews(Pre{}, open, nil); got == nil || len(got) != 0 {
		t.Errorf("RunViews = %#v, want an empty list", got)
	}
}

func TestRunViewsOpenWinsOverParked(t *testing.T) {
	got := RunViews(Pre{}, []runs.Brief{{ID: 1, Issue: 3}}, []runs.Brief{{ID: 2, Issue: 3}})
	if len(got) != 1 || got[0].State != "open" {
		t.Errorf("RunViews = %+v, want one open view", got)
	}
}

func TestSaveRunViewRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := openViewStore(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	if err := SaveRunView(ctx, st.DB, "o/a", []RunView{{Issue: 10, State: "open", Phase: "sdd", Task: 3, Tasks: 4, Step: "review"}, {Issue: 9, State: "parked"}}, now); err != nil {
		t.Fatal(err)
	}
	if err := SaveRunView(ctx, st.DB, "o/b", []RunView{{Issue: 1, State: "open"}}, now); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadView(ctx, st.DB, []string{"o/b", "o/a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		repo  string
		issue int
	}{{"o/b", 1}, {"o/a", 9}, {"o/a", 10}}
	if len(got) != len(want) {
		t.Fatalf("LoadView = %+v, want %d views", got, len(want))
	}
	for i, w := range want {
		if got[i].Repo != w.repo || got[i].Issue != w.issue || !got[i].At.Equal(now.UTC()) {
			t.Errorf("view %d = %+v, want %s #%d at %v", i, got[i], w.repo, w.issue, now.UTC())
		}
	}
	got, _, err = LoadView(ctx, st.DB, []string{"o/a"})
	if err == nil && len(got) == 2 {
		if v := got[1]; v.Phase != "sdd" || v.Task != 3 || v.Tasks != 4 || v.Step != "review" {
			t.Errorf("progress did not round-trip: %+v", v)
		}
	}
	if err != nil || len(got) != 2 {
		t.Errorf("LoadView(o/a) = %+v, %v, want only o/a", got, err)
	}
}

func TestSaveRunViewEmptyClears(t *testing.T) {
	ctx := context.Background()
	st := openViewStore(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := SaveRunView(ctx, st.DB, "o/a", []RunView{{Issue: 1, State: "open"}}, now); err != nil {
		t.Fatal(err)
	}
	if err := SaveRunView(ctx, st.DB, "o/a", nil, now); err != nil {
		t.Fatal(err)
	}
	r, d, err := LoadView(ctx, st.DB, []string{"o/a"})
	if err != nil || r == nil || d == nil || len(r) != 0 || len(d) != 0 {
		t.Errorf("LoadView = %#v, %#v, %v, want empty non-nil slices", r, d, err)
	}
}

func TestLoadViewDeferred(t *testing.T) {
	ctx := context.Background()
	st := openViewStore(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	act := Action{Kind: KindDefer, Repo: "o/a", Issue: 4, Reason: "slots full"}
	if err := SaveDeferred(ctx, st.DB, "o/a", []Action{act}, map[int]string{4: "Vier"}, now); err != nil {
		t.Fatal(err)
	}
	_, d, err := LoadView(ctx, st.DB, []string{"o/a"})
	if err != nil {
		t.Fatal(err)
	}
	want := DeferredView{Repo: "o/a", Issue: 4, Title: "Vier", Reason: "slots full", At: now}
	if len(d) != 1 || d[0] != want {
		t.Errorf("deferred = %+v, want [%+v]", d, want)
	}
}

func TestLoadViewBadValue(t *testing.T) {
	ctx := context.Background()
	st := openViewStore(t)
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO tick_state (repo, kind, key, value, updated_at) VALUES ('o/a', 'run', '12', '{', '2026-11-01T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadView(ctx, st.DB, []string{"o/a"})
	if err == nil || !strings.Contains(err.Error(), "o/a") || !strings.Contains(err.Error(), "12") {
		t.Errorf("err = %v, want one naming o/a and key 12", err)
	}
}
