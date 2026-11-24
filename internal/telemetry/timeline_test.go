package telemetry

import (
	"testing"
	"time"
)

// mins returns t0 plus n minutes.
func mins(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

// endAt returns a pointer to mins(n), for an EndedAt.
func endAt(n int) *time.Time { t := mins(n); return &t }

func dur(n int) time.Duration { return time.Duration(n) * time.Minute }

// assertCauses checks every cause in want, that no other cause is present,
// and that the causes add up to the window.
func assertCauses(t *testing.T, got TrajectoryTimeline, want map[Cause]time.Duration) {
	t.Helper()
	for c, d := range want {
		total, ok := got.ByCause[c]
		if !ok {
			t.Errorf("ByCause lacks %s; want %v", c, d)
			continue
		}
		if total.Duration != d {
			t.Errorf("ByCause[%s] = %v; want %v", c, total.Duration, d)
		}
	}
	for c, total := range got.ByCause {
		if _, ok := want[c]; !ok {
			t.Errorf("ByCause has unexpected %s = %v", c, total.Duration)
		}
	}
	assertSumIsWindow(t, got)
}

func assertSumIsWindow(t *testing.T, got TrajectoryTimeline) {
	t.Helper()
	var sum time.Duration
	for _, total := range got.ByCause {
		sum += total.Duration
	}
	if w := got.Window.End.Sub(got.Window.Start); sum != w {
		t.Errorf("sum of causes = %v; want window %v", sum, w)
	}
}

func ac4Input(questionRunEnd int) TimelineInput {
	return TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(questionRunEnd)},
			{ID: 2, Trigger: "answer", TriggerKey: "answer:9", Outcome: "merged", StartedAt: mins(70), EndedAt: endAt(130)},
		},
		Events: []TimelineEvent{
			{Kind: EventQuestion, RunID: 1, Ref: "q1", At: mins(30)},
			{Kind: EventAnswer, RunID: 2, Ref: "q1", AnswerTo: "q1", At: mins(60)},
			{Kind: EventDone, RunID: 2, Ref: "2", At: mins(100)},
		},
	}
}

func TestTimelineAC4(t *testing.T) {
	got := Timeline(ac4Input(35), mins(200))
	assertCauses(t, got, map[Cause]time.Duration{
		CauseActive: dur(60), CauseHuman: dur(30), CausePickup: dur(10), CauseCIReview: dur(30),
	})
	if w := got.Window; !w.Start.Equal(mins(0)) || !w.End.Equal(mins(130)) {
		t.Errorf("Window = %v..%v; want 0..130", w.Start, w.End)
	}
}

func TestTimelineQuestionRunEndsAtQuestion(t *testing.T) {
	// The tick closed the question run at 35, five minutes after the
	// question; those five minutes are human, not active and human both.
	got := Timeline(ac4Input(35), mins(200))
	if d := got.ByCause[CauseActive].Duration; d != dur(60) {
		t.Errorf("active = %v; want 60m (run 1 ends at the question, 30m)", d)
	}
	if d := got.ByCause[CauseHuman].Duration; d != dur(30) {
		t.Errorf("human = %v; want 30m (from the question at 30, not 35)", d)
	}
	assertSumIsWindow(t, got)
}

func TestTimelineDoneBeforeClose(t *testing.T) {
	in := TimelineInput{
		Runs:   []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(0), EndedAt: endAt(100)}},
		Events: []TimelineEvent{{Kind: EventDone, RunID: 1, Ref: "1", At: mins(40)}},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(40), CauseCIReview: dur(60)})
	if got.ByCause[CauseActive].InclusiveCIReview {
		t.Error("active with a Done is marked inclusive of CI/review")
	}
}

func TestTimelinePusherRowIsCIReview(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "handoff", TriggerKey: "push:7", Command: "push-workflows", Outcome: "pushed", StartedAt: mins(0), EndedAt: endAt(20)},
			{ID: 2, Trigger: "handoff", TriggerKey: "merge:5:abc", Outcome: "merged", StartedAt: mins(20), EndedAt: endAt(50)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseCIReview: dur(50)})
}

func TestTimelineRunWithoutDone(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(0), EndedAt: endAt(80)}},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(80)})
	if !got.ByCause[CauseActive].InclusiveCIReview {
		t.Error("active of a run without Done is not marked inclusive of CI/review")
	}
}

func TestTimelineGapIsUnexplained(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "failed", StartedAt: mins(0), EndedAt: endAt(10)},
			{ID: 2, Trigger: "retry", TriggerKey: "retry:7", Outcome: "merged", StartedAt: mins(20), EndedAt: endAt(30)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(20), CauseUnexplained: dur(10)})
}

func TestTimelineNoCauseMissingFromMap(t *testing.T) {
	got := Timeline(ac4Input(35), mins(200))
	if total, ok := got.ByCause[CauseUsage]; ok {
		t.Errorf("ByCause has usage = %v without a usage interval", total.Duration)
	}
}

func TestTimelineOpenRunRunsToNow(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", StartedAt: mins(0)}},
	}
	got := Timeline(in, mins(45))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(45)})
	if !got.ByCause[CauseActive].Running {
		t.Error("active of an open run is not Running")
	}
	if !got.Window.End.Equal(mins(45)) {
		t.Errorf("Window.End = %v; want now", got.Window.End)
	}
}

func TestTimelineSessionOverlap(t *testing.T) {
	run := TimelineRun{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(0), EndedAt: endAt(30)}
	t.Run("closed session", func(t *testing.T) {
		in := TimelineInput{
			Runs:     []TimelineRun{run},
			Sessions: []TimelineSession{{ID: 1, StartedAt: mins(10), EndedAt: endAt(50)}},
		}
		got := Timeline(in, mins(200))
		assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(50)})
		if got.ByCause[CauseActive].Running {
			t.Error("active of a closed session is Running")
		}
	})
	t.Run("open session", func(t *testing.T) {
		in := TimelineInput{
			Runs:     []TimelineRun{run},
			Sessions: []TimelineSession{{ID: 1, StartedAt: mins(10)}},
		}
		got := Timeline(in, mins(70))
		assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(70)})
		if !got.ByCause[CauseActive].Running {
			t.Error("active of an open session is not Running")
		}
		if !got.Window.End.Equal(mins(70)) {
			t.Errorf("Window.End = %v; want now", got.Window.End)
		}
	})
}

func TestTimelineHumanBeatsCIReview(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "handoff", TriggerKey: "push:7", Command: "push-workflows", Outcome: "pushed", StartedAt: mins(0), EndedAt: endAt(100)},
		},
		Events: []TimelineEvent{
			{Kind: EventQuestion, RunID: 1, Ref: "q1", At: mins(20)},
			{Kind: EventAnswer, RunID: 1, Ref: "q1", AnswerTo: "q1", At: mins(60)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseHuman: dur(40), CauseCIReview: dur(60)})
}

func TestTimelineEmpty(t *testing.T) {
	got := Timeline(TimelineInput{}, mins(10))
	if len(got.ByCause) != 0 {
		t.Errorf("ByCause = %v; want empty", got.ByCause)
	}
}

func TestTimelineWindowStartsAtFirstRunOrSession(t *testing.T) {
	in := TimelineInput{
		Runs:     []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(30), EndedAt: endAt(60)}},
		Sessions: []TimelineSession{{ID: 1, StartedAt: mins(20), EndedAt: endAt(25)}},
	}
	got := Timeline(in, mins(200))
	if !got.Window.Start.Equal(mins(20)) {
		t.Errorf("Window.Start = %v; want the first session start", got.Window.Start)
	}
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(35), CauseUnexplained: dur(5)})
}

func TestTimelineEventAtEndedAtCounts(t *testing.T) {
	t.Run("done", func(t *testing.T) {
		in := TimelineInput{
			Runs:   []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(0), EndedAt: endAt(100)}},
			Events: []TimelineEvent{{Kind: EventDone, RunID: 1, Ref: "1", At: mins(100)}},
		}
		got := Timeline(in, mins(200))
		assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(100)})
		if got.ByCause[CauseActive].InclusiveCIReview {
			t.Error("active with a Done at ended_at is marked inclusive of CI/review")
		}
	})
	t.Run("question", func(t *testing.T) {
		in := TimelineInput{
			Runs:   []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)}},
			Events: []TimelineEvent{{Kind: EventQuestion, RunID: 1, Ref: "q1", At: mins(30)}},
			// Closed at the question: the question ends there, so the
			// window holds only the run.
			ClosedAt: mins(30),
		}
		got := Timeline(in, mins(200))
		assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(30)})
		if got.ByCause[CauseActive].InclusiveCIReview {
			t.Error("active of a question run with its question at ended_at is marked inclusive of CI/review")
		}
	})
}

func TestTimelineOpenRunWithoutDoneIsNotInclusive(t *testing.T) {
	// No Done yet on an open run: the session has not reported Done, so it
	// still works; its active time cannot hold CI/review waiting.
	in := TimelineInput{
		Runs: []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", StartedAt: mins(0)}},
	}
	got := Timeline(in, mins(45))
	if total := got.ByCause[CauseActive]; total.InclusiveCIReview || !total.Running {
		t.Errorf("active of an open run without Done = %+v; want Running, not InclusiveCIReview", total)
	}
}

func TestTimelineClockSkewKeepsWindowOrdered(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(50), EndedAt: endAt(20)}},
	}
	got := Timeline(in, mins(200))
	if got.Window.End.Before(got.Window.Start) {
		t.Errorf("Window = %v..%v; End before Start", got.Window.Start, got.Window.End)
	}
	assertCauses(t, got, map[Cause]time.Duration{})
}
