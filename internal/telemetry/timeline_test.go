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

func TestTimelineNoStoredEvents(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)},
			{ID: 2, Trigger: "answer", TriggerKey: "answer:9", Outcome: "merged", StartedAt: mins(70), EndedAt: endAt(100)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(60), CauseHuman: dur(40)})
	if !got.ByCause[CauseHuman].Estimate {
		t.Error("human without stored events is not an Estimate")
	}
	if len(got.Questions) != 1 || !got.Questions[0].Estimate || !got.Questions[0].Answered || !got.Questions[0].At.Equal(mins(30)) {
		t.Errorf("Questions = %+v; want one estimated, answered question at 30", got.Questions)
	}
	if len(got.Answers) != 1 || !got.Answers[0].At.Equal(mins(70)) || got.Answers[0].RunID != 2 {
		t.Errorf("Answers = %+v; want one answer at 70 on run 2", got.Answers)
	}
}

func TestTimelineAnswerWithoutQuestion(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)},
			{ID: 2, Trigger: "answer", TriggerKey: "answer:9", Outcome: "merged", StartedAt: mins(70), EndedAt: endAt(100)},
		},
		Events: []TimelineEvent{{Kind: EventAnswer, RunID: 2, Ref: "q-old", AnswerTo: "q-old", At: mins(50)}},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(60), CauseHuman: dur(20), CausePickup: dur(20)})
	if !got.ByCause[CauseHuman].Estimate {
		t.Error("human from ended_at of the question run is not an Estimate")
	}
	if got.ByCause[CausePickup].Estimate {
		t.Error("pickup from a stored answer is an Estimate")
	}
	if len(got.Questions) != 1 || !got.Questions[0].Answered {
		t.Errorf("Questions = %+v; want one answered question", got.Questions)
	}
	if len(got.Answers) != 1 || !got.Answers[0].At.Equal(mins(50)) {
		t.Errorf("Answers = %+v; want the stored answer at 50", got.Answers)
	}
}

func TestTimelineRepairIsNoAnswer(t *testing.T) {
	question := TimelineRun{ID: 1, Trigger: "handoff", TriggerKey: "push:7", Command: "push-workflows", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)}
	t.Run("repair", func(t *testing.T) {
		in := TimelineInput{Runs: []TimelineRun{
			question,
			{ID: 2, Trigger: "answer", TriggerKey: "repair:4", Outcome: "merged", StartedAt: mins(50), EndedAt: endAt(60)},
		}}
		got := Timeline(in, mins(200))
		if len(got.Answers) != 0 {
			t.Errorf("Answers = %+v; a repair run answers no human", got.Answers)
		}
		if len(got.Questions) != 1 || got.Questions[0].Answered {
			t.Errorf("Questions = %+v; want one unanswered question", got.Questions)
		}
		// The question stays unanswered; per the spec it ends at the start
		// of the next run, the repair run.
		assertCauses(t, got, map[Cause]time.Duration{CauseCIReview: dur(30), CauseHuman: dur(20), CauseActive: dur(10)})
	})
	t.Run("pushanswer", func(t *testing.T) {
		in := TimelineInput{Runs: []TimelineRun{
			question,
			{ID: 2, Trigger: "answer", TriggerKey: "pushanswer:5", Command: "push-workflows", Outcome: "pushed", StartedAt: mins(50), EndedAt: endAt(60)},
		}}
		got := Timeline(in, mins(200))
		if len(got.Answers) != 1 || got.Answers[0].RunID != 2 {
			t.Errorf("Answers = %+v; want one answer on the pushanswer run", got.Answers)
		}
		if len(got.Questions) != 1 || !got.Questions[0].Answered {
			t.Errorf("Questions = %+v; want one answered question", got.Questions)
		}
		assertCauses(t, got, map[Cause]time.Duration{CauseCIReview: dur(40), CauseHuman: dur(20)})
		if !got.ByCause[CauseHuman].Estimate {
			t.Error("human up to the pushanswer run is not an Estimate")
		}
	})
}

func TestTimelineRestartKeepsPickupToLastRun(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)},
			{ID: 2, Trigger: "answer", TriggerKey: "answer:9", Outcome: "start_failed", StartedAt: mins(70), EndedAt: endAt(70)},
			{ID: 3, Trigger: "answer", TriggerKey: "answer:9", Outcome: "merged", StartedAt: mins(80), EndedAt: endAt(100)},
		},
		Events: []TimelineEvent{
			{Kind: EventQuestion, RunID: 1, Ref: "q1", At: mins(30)},
			{Kind: EventAnswer, RunID: 2, Ref: "q1", AnswerTo: "q1", At: mins(60)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(50), CauseHuman: dur(30), CausePickup: dur(20)})
	if len(got.Answers) != 1 {
		t.Errorf("Answers = %+v; a restart is the same answer", got.Answers)
	}
}

func TestTimelineUsagePause(t *testing.T) {
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "parked", StartedAt: mins(0), EndedAt: endAt(30)},
			{ID: 2, Trigger: "usage", TriggerKey: "usage:7", Outcome: "merged", StartedAt: mins(90), EndedAt: endAt(100)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(40), CauseUsage: dur(60)})
	if got.ByCause[CauseUsage].Estimate {
		t.Error("usage is an Estimate; want measured")
	}
}

func TestTimelineUnansweredQuestion(t *testing.T) {
	questionRun := TimelineRun{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(35)}
	question := TimelineEvent{Kind: EventQuestion, RunID: 1, Ref: "q1", At: mins(30)}
	check := func(t *testing.T, got TrajectoryTimeline, human time.Duration, running bool) {
		t.Helper()
		if len(got.Questions) != 1 || got.Questions[0].Answered || got.Questions[0].Estimate {
			t.Errorf("Questions = %+v; want one stored, unanswered question", got.Questions)
		}
		if len(got.Answers) != 0 {
			t.Errorf("Answers = %+v; want none", got.Answers)
		}
		total := got.ByCause[CauseHuman]
		if total.Duration != human || total.Running != running {
			t.Errorf("human = %+v; want %v, Running %v", total, human, running)
		}
		assertSumIsWindow(t, got)
	}
	t.Run("next run", func(t *testing.T) {
		in := TimelineInput{
			Runs:   []TimelineRun{questionRun, {ID: 2, Trigger: "retry", TriggerKey: "retry:7", Outcome: "merged", StartedAt: mins(80), EndedAt: endAt(90)}},
			Events: []TimelineEvent{question},
		}
		check(t, Timeline(in, mins(200)), dur(50), false)
	})
	t.Run("returned run", func(t *testing.T) {
		in := TimelineInput{
			Runs: []TimelineRun{questionRun, {ID: 2, Trigger: "handoff", TriggerKey: "push:7", Command: "push-workflows",
				Outcome: "returned", Reason: "pusher_silent", StartedAt: mins(10), EndedAt: endAt(60)}},
			Events: []TimelineEvent{question},
		}
		check(t, Timeline(in, mins(200)), dur(30), false)
	})
	t.Run("closed issue", func(t *testing.T) {
		in := TimelineInput{Runs: []TimelineRun{questionRun}, Events: []TimelineEvent{question}, ClosedAt: mins(70)}
		got := Timeline(in, mins(200))
		check(t, got, dur(40), false)
		if !got.Window.End.Equal(mins(70)) {
			t.Errorf("Window.End = %v; want the close at 70", got.Window.End)
		}
	})
	t.Run("running to now", func(t *testing.T) {
		in := TimelineInput{Runs: []TimelineRun{questionRun}, Events: []TimelineEvent{question}}
		got := Timeline(in, mins(100))
		check(t, got, dur(70), true)
		if !got.Window.End.Equal(mins(100)) {
			t.Errorf("Window.End = %v; want now", got.Window.End)
		}
	})
}

func TestTimelineSessionQuestion(t *testing.T) {
	session := TimelineSession{ID: 1, StartedAt: mins(0), EndedAt: endAt(100)}
	events := []TimelineEvent{
		{Kind: EventQuestion, SessionID: 1, Ref: "1:tu1", At: mins(20)},
		{Kind: EventAnswer, SessionID: 1, Ref: "1:tu1", AnswerTo: "1:tu1", At: mins(50)},
	}
	t.Run("session only", func(t *testing.T) {
		got := Timeline(TimelineInput{Sessions: []TimelineSession{session}, Events: events}, mins(200))
		assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(70), CauseHuman: dur(30)})
		if len(got.Questions) != 1 || got.Questions[0].SessionID != 1 || !got.Questions[0].Answered {
			t.Errorf("Questions = %+v; want one answered session question", got.Questions)
		}
		if len(got.Answers) != 1 || got.Answers[0].SessionID != 1 {
			t.Errorf("Answers = %+v; want one session answer", got.Answers)
		}
	})
	t.Run("run over the question", func(t *testing.T) {
		in := TimelineInput{
			Runs:     []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(10), EndedAt: endAt(40)}},
			Sessions: []TimelineSession{session},
			Events:   events,
		}
		got := Timeline(in, mins(200))
		assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(90), CauseHuman: dur(10)})
	})
}

func TestTimelineTakeovers(t *testing.T) {
	type want struct {
		at     int
		reason string
	}
	cases := []struct {
		name string
		in   TimelineInput
		want []want
	}{
		{"returned then session", TimelineInput{
			Runs:     []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "returned", Reason: "agent_gave_up", StartedAt: mins(0), EndedAt: endAt(30)}},
			Sessions: []TimelineSession{{ID: 1, StartedAt: mins(40), EndedAt: endAt(60)}},
		}, []want{{30, "agent_gave_up"}}},
		{"two sessions without a run between", TimelineInput{
			Runs: []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "failed", StartedAt: mins(0), EndedAt: endAt(30)}},
			Sessions: []TimelineSession{
				{ID: 1, StartedAt: mins(40), EndedAt: endAt(50)},
				{ID: 2, StartedAt: mins(60), EndedAt: endAt(70)},
			},
		}, []want{{40, "session"}}},
		{"session before the first run", TimelineInput{
			Runs:     []TimelineRun{{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(20), EndedAt: endAt(30)}},
			Sessions: []TimelineSession{{ID: 1, StartedAt: mins(0), EndedAt: endAt(10)}},
		}, nil},
		{"returned without session", TimelineInput{
			Runs: []TimelineRun{{ID: 1, Trigger: "handoff", TriggerKey: "push:7", Command: "push-workflows", Outcome: "returned", Reason: "pusher_silent", StartedAt: mins(0), EndedAt: endAt(30)}},
		}, []want{{30, "pusher_silent"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Timeline(tc.in, mins(200)).Takeovers
			if len(got) != len(tc.want) {
				t.Fatalf("Takeovers = %+v; want %d", got, len(tc.want))
			}
			for i, w := range tc.want {
				if !got[i].At.Equal(mins(w.at)) || got[i].Reason != w.reason {
					t.Errorf("Takeovers[%d] = %+v; want at %d, reason %q", i, got[i], w.at, w.reason)
				}
			}
		})
	}
}

func TestTimelineTieFlagsDoNotDependOnOrder(t *testing.T) {
	// Two runs over the same 0..30: one Done at its end (not inclusive),
	// one without Done (inclusive). The tie is the only source of the flag.
	withDone := TimelineRun{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "merged", StartedAt: mins(0), EndedAt: endAt(30)}
	without := TimelineRun{ID: 2, Trigger: "retry", TriggerKey: "retry:7", Outcome: "merged", StartedAt: mins(0), EndedAt: endAt(30)}
	done := []TimelineEvent{{Kind: EventDone, RunID: 1, Ref: "1", At: mins(30)}}
	for _, runs := range [][]TimelineRun{{withDone, without}, {without, withDone}} {
		got := Timeline(TimelineInput{Runs: runs, Events: done}, mins(60))
		if !got.ByCause[CauseActive].InclusiveCIReview {
			t.Errorf("runs %d,%d: active = %+v; want InclusiveCIReview from the tied run without Done", runs[0].ID, runs[1].ID, got.ByCause[CauseActive])
		}
	}
}

func TestTimelineSecondPushanswerRunIsNoExtraAnswer(t *testing.T) {
	// The pusher starts a second pushanswer run for the same question, on
	// another answer comment; RecordEvent kept only the first answer.
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "handoff", TriggerKey: "push:7", Command: "push-workflows", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)},
			{ID: 2, Trigger: "answer", TriggerKey: "pushanswer:5", Command: "push-workflows", Outcome: "pushed", StartedAt: mins(50), EndedAt: endAt(60)},
			{ID: 3, Trigger: "answer", TriggerKey: "pushanswer:6", Command: "push-workflows", Outcome: "pushed", StartedAt: mins(70), EndedAt: endAt(80)},
		},
		Events: []TimelineEvent{
			{Kind: EventQuestion, RunID: 1, Ref: "q1", At: mins(30)},
			{Kind: EventAnswer, RunID: 2, Ref: "q1", AnswerTo: "q1", At: mins(45)},
		},
	}
	got := Timeline(in, mins(200))
	if len(got.Answers) != 1 || got.Answers[0].RunID != 2 {
		t.Errorf("Answers = %+v; want only the stored answer on run 2", got.Answers)
	}
}

func TestTimelineUnansweredEndsAtRunStartingAtQuestion(t *testing.T) {
	// The next run starts at the very moment of the question: the wait is
	// zero, not running to now.
	in := TimelineInput{
		Runs: []TimelineRun{
			{ID: 1, Trigger: "start", TriggerKey: "start:7", Outcome: "question", StartedAt: mins(0), EndedAt: endAt(30)},
			{ID: 2, Trigger: "retry", TriggerKey: "retry:7", Outcome: "merged", StartedAt: mins(30), EndedAt: endAt(40)},
		},
	}
	got := Timeline(in, mins(200))
	assertCauses(t, got, map[Cause]time.Duration{CauseActive: dur(40)})
	if len(got.Questions) != 1 || got.Questions[0].Answered {
		t.Errorf("Questions = %+v; want one unanswered question", got.Questions)
	}
}
