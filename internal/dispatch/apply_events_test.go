package dispatch

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// eventRow is one trajectory_events row as the dispatcher writes it.
type eventRow struct {
	source, sourceID, kind, ref, at string
	runID                           int64
}

// events reads every trajectory event of h, oldest first.
func (h *harness) events(t *testing.T) []eventRow {
	t.Helper()
	rows, err := h.db.Query(`SELECT source, source_id, kind, ref, at, run_id FROM trajectory_events ORDER BY id`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		var run sql.NullInt64
		if err := rows.Scan(&e.source, &e.sourceID, &e.kind, &e.ref, &e.at, &run); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		e.runID = run.Int64
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("events: %v", err)
	}
	return out
}

var (
	askedAt    = time.Date(2026, 10, 31, 8, 30, 0, 0, time.UTC)
	answeredAt = time.Date(2026, 10, 31, 8, 45, 0, 0, time.UTC)
)

// The close of a run on its Vraag stores one question event on that run, after the run closed:
// source_id question:<id>, ref the Vraag id, at the Vraag's created_at.
func TestApplyWritesQuestionEvent(t *testing.T) {
	h := newHarness(t)
	run := h.seed(t, 7, oldSession, "start:7", "")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: run, Outcome: runs.Question,
		EventRef: "63", EventAt: askedAt}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if r := h.history(t)[0]; r.Open || r.Outcome != runs.Question {
		t.Fatalf("run = %+v, want closed as question", r)
	}
	want := []eventRow{{source: "dispatch", sourceID: "question:63", kind: "question", ref: "63",
		at: "2026-10-31T08:30:00Z", runID: run}}
	if got := h.events(t); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A close of any other outcome stores nothing.
func TestApplyCloseOtherOutcomeWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	run := h.seed(t, 7, oldSession, "start:7", "")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: run, Outcome: runs.Handoff}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := h.events(t); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

// A resume on an answer stores one answer event on the new run, right after its claim:
// source_id answer:<answer id>, ref the Vraag id, at the answer's created_at. The second attempt
// of that trigger key stores nothing more.
func TestApplyWritesAnswerEvent(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Question)
	a := resumeAction()
	a.EventRef, a.EventAt = "63", answeredAt
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rows := h.history(t)
	resumed := rows[len(rows)-1]
	if resumed.TriggerKey != "answer:88" {
		t.Fatalf("rows = %+v, want the resume last", rows)
	}
	want := []eventRow{{source: "dispatch", sourceID: "answer:88", kind: "answer", ref: "63",
		at: "2026-10-31T08:45:00Z", runID: resumed.ID}}
	if got := h.events(t); !slices.Equal(got, want) {
		t.Fatalf("events = %+v, want %+v", got, want)
	}

	// The first attempt failed to start; the second takes the same key and stores nothing.
	if err := runs.Release(context.Background(), h.db, resumed.ID, runs.StartFailed, "x", applyNow); err != nil {
		t.Fatal(err)
	}
	retry := resumeAction()
	retry.Attempt = 2
	if _, err := Apply(context.Background(), h.env, []Action{retry}); err != nil {
		t.Fatalf("Apply retry: %v", err)
	}
	if got := h.events(t); !slices.Equal(got, want) {
		t.Errorf("events after the retry = %+v, want only %+v", got, want)
	}
}

// A push on an answer `push` stores the answer event on its push row.
func TestApplyWritesAnswerEventAfterPush(t *testing.T) {
	h, _ := pushHarness(t)
	a := pushAction()
	a.Trigger, a.TriggerKey, a.CommentID, a.PermitSHA = triggerPushAnswer, "pushanswer:71", 71, strings.Repeat("a", 40)
	a.EventRef, a.EventAt = "70", answeredAt
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	row := h.history(t)[0]
	want := []eventRow{{source: "dispatch", sourceID: "answer:71", kind: "answer", ref: "70",
		at: "2026-10-31T08:45:00Z", runID: row.ID}}
	if got := h.events(t); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A push_recover on a pushanswer: key stores the answer event on its row.
func TestApplyWritesAnswerEventAfterPushRecover(t *testing.T) {
	h, _ := pushHarness(t)
	var asked []string
	h.env.LsRemote = lsRemoteFake(strings.Repeat("a", 40), nil, &asked)
	a := pushRecoverAction()
	a.EventRef, a.EventAt = "70", answeredAt
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	row := h.history(t)[0]
	want := []eventRow{{source: "dispatch", sourceID: "answer:71", kind: "answer", ref: "70",
		at: "2026-10-31T08:45:00Z", runID: row.ID}}
	if got := h.events(t); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A note_done stores the done event of its run once: a second tick adds no row and keeps the at
// of the first.
func TestApplyWritesDoneEventOnce(t *testing.T) {
	h := newHarness(t)
	run := h.seed(t, 7, oldSession, "start:7", "")
	a := Action{Kind: KindNoteDone, Repo: applyRepo, Issue: 7, RunID: run}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	h.env.Now = func() time.Time { return applyNow.Add(time.Minute) }
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply second tick: %v", err)
	}
	id := strconv.FormatInt(run, 10)
	want := []eventRow{{source: "dispatch", sourceID: "done:" + id, kind: "done", ref: id,
		at: "2026-10-31T09:00:00Z", runID: run}}
	if got := h.events(t); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
	if r := h.history(t)[0]; !r.Open {
		t.Errorf("run = %+v, want still open: a note_done closes nothing", r)
	}
}

// On the tick that closes a Done run, Apply runs the note_done before the close: the done event
// lies before the run's end, so the timeline splits the run into active and ci_review instead of
// marking it active including CI/review.
func TestApplyDoneOnClosingTick(t *testing.T) {
	h := newHarness(t)
	run := h.seed(t, 7, oldSession, "start:7", "")
	tick := applyNow
	h.env.Now = func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
	acts := []Action{
		{Kind: KindNoteDone, Repo: applyRepo, Issue: 7, RunID: run},
		{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: run, Outcome: runs.Handoff},
	}
	if _, err := Apply(context.Background(), h.env, acts); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	id, ok, err := telemetry.FindTrajectoryID(context.Background(), h.db, applyRepo, 7)
	if err != nil || !ok {
		t.Fatalf("FindTrajectoryID = %d, %v, %v", id, ok, err)
	}
	tl, err := telemetry.LoadTimeline(context.Background(), h.db, id, time.Time{}, tick.Add(time.Hour))
	if err != nil {
		t.Fatalf("LoadTimeline: %v", err)
	}
	active, ci := tl.ByCause[telemetry.CauseActive], tl.ByCause[telemetry.CauseCIReview]
	if active.InclusiveCIReview || ci.Duration <= 0 {
		t.Errorf("ByCause = %+v, want active up to Done and ci_review after it", tl.ByCause)
	}
}

// The dispatcher's own events, written by Apply in the shapes it stores (source dispatch,
// question:<id>, answer:<comment id> with the Vraag id as ref, done:<run id>), load into the
// timeline of AC4: a start run that asks at 30 and closes at 35, an answer at 60 resumed at
// 70, Done at 100 and the close at 130 give active 60m, human 30m, pickup 10m and ci_review 30m.
func TestApplyEventsLoadIntoTimeline(t *testing.T) {
	h := newHarness(t)
	t0 := applyNow.Add(-time.Hour) // the start of the seeded run
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	tick := func(m int, a Action) {
		t.Helper()
		h.env.Now = func() time.Time { return at(m) }
		if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
			t.Fatalf("Apply %s at %d: %v", a.Kind, m, err)
		}
	}
	start := h.seed(t, 7, oldSession, "start:7", "")
	tick(35, Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: start, Outcome: runs.Question,
		EventRef: "63", EventAt: at(30)})
	resume := resumeAction()
	resume.EventRef, resume.EventAt = "63", at(60)
	tick(70, resume)
	rows := h.history(t)
	resumed := rows[len(rows)-1].ID
	tick(100, Action{Kind: KindNoteDone, Repo: applyRepo, Issue: 7, RunID: resumed})
	tick(130, Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: resumed, Outcome: runs.Handoff})

	id, ok, err := telemetry.FindTrajectoryID(context.Background(), h.db, applyRepo, 7)
	if err != nil || !ok {
		t.Fatalf("FindTrajectoryID = %d, %v, %v", id, ok, err)
	}
	tl, err := telemetry.LoadTimeline(context.Background(), h.db, id, time.Time{}, at(200))
	if err != nil {
		t.Fatalf("LoadTimeline: %v", err)
	}
	want := map[telemetry.Cause]time.Duration{
		telemetry.CauseActive: 60 * time.Minute, telemetry.CauseHuman: 30 * time.Minute,
		telemetry.CausePickup: 10 * time.Minute, telemetry.CauseCIReview: 30 * time.Minute,
	}
	if len(tl.ByCause) != len(want) {
		t.Errorf("ByCause = %+v, want only %v", tl.ByCause, want)
	}
	for c, d := range want {
		got := tl.ByCause[c]
		if got.Duration != d || got.Estimate || got.Running || got.InclusiveCIReview {
			t.Errorf("ByCause[%s] = %+v, want %v measured", c, got, d)
		}
	}
	if len(tl.Questions) != 1 || !tl.Questions[0].At.Equal(at(30)) || !tl.Questions[0].Answered ||
		len(tl.Answers) != 1 || !tl.Answers[0].At.Equal(at(60)) || tl.Answers[0].Estimate {
		t.Errorf("Questions = %+v, Answers = %+v, want the stored Vraag at 30 answered at 60", tl.Questions, tl.Answers)
	}
}

// A done event that could not be stored is not tried again in a later tick: one attempt, one
// log line, across two Apply calls on the same state dir.
func TestApplyDoneWriteFailureNotRetried(t *testing.T) {
	h := newHarness(t)
	run := h.seed(t, 7, oldSession, "start:7", "")
	detach(t, h, run)
	a := Action{Kind: KindNoteDone, Repo: applyRepo, Issue: 7, RunID: run}
	for range 2 {
		if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	if n := strings.Count(h.out.String(), "event not recorded"); n != 1 {
		t.Errorf("log %q has %d 'event not recorded' lines, want 1", h.out.String(), n)
	}
}

// A run without a trajectory makes RecordEvent refuse: the close still applies, Apply reports no
// error and the log says the event was not recorded.
func TestApplyContinuesWhenEventWriteFails(t *testing.T) {
	h := newHarness(t)
	run := h.seed(t, 7, oldSession, "start:7", "")
	detach(t, h, run)
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: run, Outcome: runs.Question,
		EventRef: "63", EventAt: askedAt}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v, want a failed event to be no action error", err)
	}
	if r := h.history(t)[0]; r.Open || r.Outcome != runs.Question {
		t.Errorf("run = %+v, want closed as question", r)
	}
	if !strings.Contains(h.out.String(), "event not recorded") {
		t.Errorf("log %q, want 'event not recorded'", h.out.String())
	}
	if got := h.events(t); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

// detach takes the trajectory off run, so RecordEvent refuses every event of it.
func detach(t *testing.T, h *harness, run int64) {
	t.Helper()
	if _, err := h.db.Exec(`UPDATE runs SET trajectory_id = NULL WHERE id = ?`, run); err != nil {
		t.Fatal(err)
	}
}
