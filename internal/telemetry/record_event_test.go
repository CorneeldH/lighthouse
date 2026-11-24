package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func eventOf(runID int64, kind EventKind, sourceID, ref string) EventRecord {
	return EventRecord{
		Origin: Origin{Source: "events", SourceID: sourceID, RecordedAt: t0},
		Repo:   "o/r", Issue: 7, RunID: runID, Kind: kind, Ref: ref, At: t0,
	}
}

func TestRecordEventIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	run := takeRun(t, db, "o/r", 7, "start:7", "start")
	e := eventOf(run, EventQuestion, "question:1", "q1")
	id, isNew, err := RecordEvent(ctx, db, e)
	if err != nil || !isNew {
		t.Fatalf("first: id %d, new %v, err %v", id, isNew, err)
	}
	id2, isNew2, err := RecordEvent(ctx, db, e)
	if err != nil || isNew2 || id2 != id {
		t.Errorf("second: id %d, new %v, err %v; want id %d, not new", id2, isNew2, err, id)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
}

func TestRecordEventSecondAnswerIgnored(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	run := takeRun(t, db, "o/r", 7, "start:7", "start")
	first, isNew, err := RecordEvent(ctx, db, eventOf(run, EventAnswer, "answer:2", "q1"))
	if err != nil || !isNew {
		t.Fatalf("first: id %d, new %v, err %v", first, isNew, err)
	}
	later := eventOf(run, EventAnswer, "answer:3", "q1")
	later.At = t0.Add(time.Hour)
	id, isNew, err := RecordEvent(ctx, db, later)
	if err != nil || isNew || id != first {
		t.Errorf("second answer: id %d, new %v, err %v; want id %d, not new, no error", id, isNew, err, first)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
}

func TestRecordEventTrajectoryMismatch(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	run := takeRun(t, db, "o/r", 7, "start:7", "start")
	e := eventOf(run, EventQuestion, "question:1", "q1")
	e.Issue = 8
	if _, _, err := RecordEvent(ctx, db, e); !errors.Is(err, ErrTrajectoryMismatch) {
		t.Errorf("err = %v, want ErrTrajectoryMismatch", err)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE issue = 8`); n != 0 {
		t.Errorf("trajectories for issue 8 = %d, want 0", n)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 0 {
		t.Errorf("events = %d, want 0", n)
	}
}

func TestRecordEventValidation(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	run := takeRun(t, db, "o/r", 7, "start:7", "start")
	sid, _, err := RecordSession(ctx, db, session("s1", Line{Branch: "issue-7-x"}))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	cases := map[string]func(e *EventRecord){
		"done with session": func(e *EventRecord) { e.Kind, e.RunID, e.SessionID = EventDone, 0, sid },
		"both owners":       func(e *EventRecord) { e.SessionID = sid },
		"no owner":          func(e *EventRecord) { e.RunID = 0 },
		"empty ref":         func(e *EventRecord) { e.Ref = "" },
		"zero at":           func(e *EventRecord) { e.At = time.Time{} },
		"unknown kind":      func(e *EventRecord) { e.Kind = "later" },
	}
	for name, mutate := range cases {
		e := eventOf(run, EventQuestion, "question:1", "q1")
		mutate(&e)
		if _, _, err := RecordEvent(ctx, db, e); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 0 {
		t.Errorf("events = %d, want 0", n)
	}
}

func TestRecordEventStoresUTC(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	run := takeRun(t, db, "o/r", 7, "start:7", "start")
	e := eventOf(run, EventQuestion, "question:1", "q1")
	e.At = time.Date(2026, 11, 19, 12, 0, 0, 0, time.FixedZone("x", 2*3600))
	id, _, err := RecordEvent(ctx, db, e)
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	var at string
	if err := db.QueryRow(`SELECT at FROM trajectory_events WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if at != "2026-11-19T10:00:00Z" {
		t.Errorf("at = %q, want 2026-11-19T10:00:00Z", at)
	}
}
