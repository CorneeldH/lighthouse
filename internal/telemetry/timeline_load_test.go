package telemetry

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

func takeAt(t *testing.T, repo string, issue int, trigger, key string, at time.Time) runs.Claim {
	t.Helper()
	return runs.Claim{
		Repo: repo, Issue: issue, SessionID: runs.NewSessionID(), Mode: "resume",
		Trigger: trigger, TriggerKey: key, Now: at,
	}
}

func TestLoadTimelineFromDB(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	// Run 1 at t0 (start:7), question at 30, run closes at 35.
	r1 := takeRun(t, db, "o/r", 7, "start:7", "start")
	if err := runs.Release(ctx, db, r1, runs.Question, "", mins(35)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	r2, err := runs.Take(ctx, db, takeAt(t, "o/r", 7, "answer", "answer:9", mins(70)))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := runs.Release(ctx, db, r2.ID, runs.Done, "", mins(130)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	for _, e := range []EventRecord{
		{Origin: Origin{Source: "ev", SourceID: "q1", RecordedAt: t0}, Repo: "o/r", Issue: 7, RunID: r1, Kind: EventQuestion, Ref: "q1", At: mins(30)},
		{Origin: Origin{Source: "ev", SourceID: "a1", RecordedAt: t0}, Repo: "o/r", Issue: 7, RunID: r2.ID, Kind: EventAnswer, Ref: "q1", At: mins(60)},
		{Origin: Origin{Source: "ev", SourceID: "d1", RecordedAt: t0}, Repo: "o/r", Issue: 7, RunID: r2.ID, Kind: EventDone, Ref: "2", At: mins(100)},
	} {
		if _, _, err := RecordEvent(ctx, db, e); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}
	tid, ok, err := FindTrajectoryID(ctx, db, "O/R", 7)
	if err != nil || !ok {
		t.Fatalf("FindTrajectoryID = %d, %v, %v", tid, ok, err)
	}
	got, err := LoadTimeline(ctx, db, tid, time.Time{}, mins(200))
	if err != nil {
		t.Fatalf("LoadTimeline: %v", err)
	}
	want := Timeline(TimelineInput{
		Runs: []TimelineRun{
			{ID: r1, Trigger: "handoff", TriggerKey: "start:7", Outcome: "question", StartedAt: t0, EndedAt: endAt(35)},
			{ID: r2.ID, Trigger: "answer", TriggerKey: "answer:9", Outcome: "done", StartedAt: mins(70), EndedAt: endAt(130)},
		},
		Events: []TimelineEvent{
			{Kind: EventQuestion, RunID: r1, Ref: "q1", At: mins(30)},
			{Kind: EventAnswer, RunID: r2.ID, Ref: "q1", AnswerTo: "q1", At: mins(60)},
			{Kind: EventDone, RunID: r2.ID, Ref: "2", At: mins(100)},
		},
	}, mins(200))
	if !reflect.DeepEqual(got.ByCause, want.ByCause) {
		t.Errorf("ByCause = %+v; want %+v", got.ByCause, want.ByCause)
	}
	if !got.Window.Start.Equal(want.Window.Start) || !got.Window.End.Equal(want.Window.End) {
		t.Errorf("Window = %v; want %v", got.Window, want.Window)
	}
	if len(got.Questions) != 1 || len(got.Answers) != 1 {
		t.Errorf("questions %d, answers %d; want 1, 1", len(got.Questions), len(got.Answers))
	}
}

func TestLoadTimelineIncludesSession(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r := takeRun(t, db, "o/r", 7, "start:7", "start")
	if err := runs.Release(ctx, db, r, runs.Handoff, "", mins(10)); err != nil {
		t.Fatal(err)
	}
	s := session("s1", Line{Branch: "issue-7-x"})
	s.StartedAt = mins(10)
	end := mins(40)
	s.EndedAt = &end
	if _, _, err := RecordSession(ctx, db, s); err != nil {
		t.Fatal(err)
	}
	tid, _, _ := FindTrajectoryID(ctx, db, "o/r", 7)
	got, err := LoadTimeline(ctx, db, tid, time.Time{}, mins(100))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Window.End.Equal(mins(40)) {
		t.Errorf("window end = %v; want 40m (session end)", got.Window.End)
	}
}

func TestLoadTimelineExcludesOtherTrajectory(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r := takeRun(t, db, "o/r", 7, "start:7", "start")
	if err := runs.Release(ctx, db, r, runs.Handoff, "", mins(10)); err != nil {
		t.Fatal(err)
	}
	other := takeRun(t, db, "o/r", 8, "start:8", "start")
	if err := runs.Release(ctx, db, other, runs.Handoff, "", mins(500)); err != nil {
		t.Fatal(err)
	}
	s := session("s8", Line{Branch: "issue-8-x"})
	s.StartedAt = mins(600)
	end := mins(700)
	s.EndedAt = &end
	if _, _, err := RecordSession(ctx, db, s); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecordEvent(ctx, db, EventRecord{Origin: Origin{Source: "ev", SourceID: "q8", RecordedAt: t0},
		Repo: "o/r", Issue: 8, RunID: other, Kind: EventQuestion, Ref: "q8", At: mins(5)}); err != nil {
		t.Fatal(err)
	}
	tid, _, _ := FindTrajectoryID(ctx, db, "o/r", 7)
	got, err := LoadTimeline(ctx, db, tid, time.Time{}, mins(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Window.Start.Equal(t0) || !got.Window.End.Equal(mins(10)) {
		t.Errorf("window = %v..%v; want 0..10", got.Window.Start, got.Window.End)
	}
	if len(got.Questions) != 0 {
		t.Errorf("questions = %d; want 0", len(got.Questions))
	}
}

func TestFindTrajectoryIDMissing(t *testing.T) {
	db := openDB(t)
	id, ok, err := FindTrajectoryID(context.Background(), db, "o/r", 99)
	if err != nil || ok || id != 0 {
		t.Errorf("FindTrajectoryID = %d, %v, %v; want 0, false, nil", id, ok, err)
	}
}
