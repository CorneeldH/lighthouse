package telemetry

// This file holds the timeline of one trajectory: every moment of its window
// gets exactly one cause, so parallel runs, sessions and resumptions never
// count twice. Timeline is pure; it reads nothing from the database.

import (
	"sort"
	"strings"
	"time"
)

// Cause is why a moment of a trajectory passed.
type Cause string

const (
	CauseActive      Cause = "active"
	CauseHuman       Cause = "human"
	CauseUsage       Cause = "usage"
	CauseCIReview    Cause = "ci_review"
	CausePickup      Cause = "pickup"
	CauseUnexplained Cause = "unexplained"
)

// Precedence on overlap: the lowest rank wins. Active work never waits: a run
// beats everything, an open session (which does not prove anyone works)
// ranks below waiting on CI and review.
const (
	rankRunActive = iota
	rankHuman
	rankUsage
	rankCIReview
	rankSessionActive
	rankPickup
)

// Interval is a half-open stretch of time [Start, End).
type Interval struct{ Start, End time.Time }

// TimelineRun is one run of the trajectory. EndedAt nil: the run is open.
type TimelineRun struct {
	ID                                            int64
	Trigger, TriggerKey, Command, Outcome, Reason string
	StartedAt                                     time.Time
	EndedAt                                       *time.Time
}

// TimelineSession holds when one interactive session of the trajectory
// started and when it ended; a nil EndedAt means it has not ended yet.
type TimelineSession struct {
	ID        int64
	StartedAt time.Time
	EndedAt   *time.Time
}

// TimelineEvent is one stored trajectory event.
type TimelineEvent struct {
	Kind             EventKind
	RunID, SessionID int64
	Ref              string
	At               time.Time
	AnswerTo         string // answer only: equal to Ref, for readability
}

// TimelineInput is everything of one trajectory the timeline is built from.
type TimelineInput struct {
	Runs     []TimelineRun
	Sessions []TimelineSession
	Events   []TimelineEvent
	ClosedAt time.Time // zero: the issue is not closed
}

// Moment is one question, answer or takeover.
type Moment struct {
	At                 time.Time
	RunID, SessionID   int64
	Answered, Estimate bool   // meaningful for Questions only
	Reason             string // Takeovers only
}

// CauseTotal is the time one cause won, with what is in it: an estimate, an
// interval still running at now, or active time that may include CI/review.
type CauseTotal struct {
	Duration                             time.Duration
	Estimate, Running, InclusiveCIReview bool
}

// TrajectoryTimeline is the timeline of one trajectory. A cause without
// time is absent from ByCause, never zero.
type TrajectoryTimeline struct {
	Window                        Interval
	Questions, Answers, Takeovers []Moment
	ByCause                       map[Cause]CauseTotal
}

// span is one candidate interval for a cause, before precedence.
type span struct {
	Interval
	cause                        Cause
	rank                         int
	estimate, running, inclusive bool
}

// Timeline lays every interval of the trajectory on one axis and gives each
// moment of the window the cause of highest precedence; a moment no interval
// covers is unexplained. Open runs and sessions run to now.
func Timeline(in TimelineInput, now time.Time) TrajectoryTimeline {
	out := TrajectoryTimeline{ByCause: map[Cause]CauseTotal{}}
	window, ok := timelineWindow(in, now)
	if !ok {
		return out
	}
	questions, answers, human := questionsAndAnswers(in, now)
	var spans []span
	spans = append(spans, runSpans(in, now)...)
	spans = append(spans, sessionSpans(in, now)...)
	spans = append(spans, human...)
	spans = append(spans, usageSpans(in)...)
	spans = append(spans, pickupSpans(in)...)
	// An unanswered question can outlast every run and session: it holds
	// the window open to its end (the close of the issue, or now).
	for _, s := range human {
		if s.End.After(window.End) {
			window.End = s.End
		}
	}
	out.Window = window
	out.Questions, out.Answers = questions, answers
	out.Takeovers = takeovers(in)
	out.ByCause = sweep(window, spans)
	return out
}

// timelineWindow runs from the first start of a run or session to the last
// end; an open run or session holds it open to now. ok is false without any.
func timelineWindow(in TimelineInput, now time.Time) (Interval, bool) {
	var w Interval
	found := false
	widen := func(start time.Time, end *time.Time) {
		e := now
		if end != nil {
			e = *end
		}
		if !found || start.Before(w.Start) {
			w.Start = start
		}
		if !found || e.After(w.End) {
			w.End = e
		}
		found = true
	}
	for _, r := range in.Runs {
		widen(r.StartedAt, r.EndedAt)
	}
	for _, s := range in.Sessions {
		widen(s.StartedAt, s.EndedAt)
	}
	if w.End.Before(w.Start) {
		w.End = w.Start // clock skew or a corrupt row: never a negative window
	}
	return w, found
}

// isPusherRow reports a row of the workflow pusher or a merge run: it runs
// no agent session, so it is never active.
func isPusherRow(r TimelineRun) bool {
	return r.Command == "push-workflows" || strings.HasPrefix(r.TriggerKey, "merge:")
}

// runSpans gives a pusher row ci_review over its whole length, and an agent
// run active up to its first Done (ci_review after it) or up to its stored
// question; without either, a closed run is active marked inclusive of
// CI/review. An open run without Done still works (no tick saw Done yet), so
// it is not inclusive.
func runSpans(in TimelineInput, now time.Time) []span {
	var out []span
	for _, r := range in.Runs {
		end, running := now, r.EndedAt == nil
		if !running {
			end = *r.EndedAt
		}
		if isPusherRow(r) {
			out = append(out, span{Interval: Interval{r.StartedAt, end}, cause: CauseCIReview, rank: rankCIReview, running: running})
			continue
		}
		if done, ok := firstEvent(in.Events, EventDone, r.ID); ok && !done.Before(r.StartedAt) && !done.After(end) {
			out = append(out,
				span{Interval: Interval{r.StartedAt, done}, cause: CauseActive, rank: rankRunActive},
				span{Interval: Interval{done, end}, cause: CauseCIReview, rank: rankCIReview, running: running})
			continue
		}
		if r.Outcome == "question" {
			if q, ok := firstEvent(in.Events, EventQuestion, r.ID); ok && !q.Before(r.StartedAt) && !q.After(end) {
				out = append(out, span{Interval: Interval{r.StartedAt, q}, cause: CauseActive, rank: rankRunActive})
				continue
			}
		}
		out = append(out, span{Interval: Interval{r.StartedAt, end}, cause: CauseActive, rank: rankRunActive, running: running, inclusive: !running})
	}
	return out
}

// sessionSpans gives each session active; its questions lose to human by
// precedence, which takes them out of the session.
func sessionSpans(in TimelineInput, now time.Time) []span {
	var out []span
	for _, s := range in.Sessions {
		end, running := now, s.EndedAt == nil
		if !running {
			end = *s.EndedAt
		}
		out = append(out, span{Interval: Interval{s.StartedAt, end}, cause: CauseActive, rank: rankSessionActive, running: running})
	}
	return out
}

// question is one human question of the trajectory, stored or estimated
// from a run that ended on outcome question without a stored question.
type question struct {
	Moment
	ref    string
	stored bool
}

// answerCandidate is a dispatcher answer no stored question claims by ref:
// a stored answer to an older question, or an answer run without a stored
// answer. It ends the estimated wait of the next dispatcher question. A
// candidate from an answer run carries its answer moment, which counts only
// when a question consumes it: a second pushanswer run on an answered
// question is no extra answer.
type answerCandidate struct {
	at     time.Time
	used   bool
	answer *Moment
}

// isAnswerKey reports a run that took up a human answer. A repair run
// carries trigger answer as well but answers no human.
func isAnswerKey(key string) bool {
	return strings.HasPrefix(key, "answer:") || strings.HasPrefix(key, "pushanswer:")
}

// questionsAndAnswers lists the questions and answers of the trajectory and
// gives every question its human wait: measured from a stored question to
// the stored answer with its ref; estimated when either is missing, up to
// the next dispatcher answer; unanswered up to unansweredEnd.
func questionsAndAnswers(in TimelineInput, now time.Time) ([]Moment, []Moment, []span) {
	firstAnswer := map[string]TimelineEvent{}
	answeredRuns := map[int64]bool{}
	var answers []Moment
	for _, e := range in.Events {
		if e.Kind != EventAnswer {
			continue
		}
		answers = append(answers, Moment{At: e.At, RunID: e.RunID, SessionID: e.SessionID})
		if a, ok := firstAnswer[e.Ref]; !ok || e.At.Before(a.At) {
			firstAnswer[e.Ref] = e
		}
		if e.RunID != 0 {
			answeredRuns[e.RunID] = true
		}
	}

	var qs []question
	storedRefs := map[string]bool{}
	questionRuns := map[int64]bool{}
	for _, e := range in.Events {
		if e.Kind != EventQuestion {
			continue
		}
		qs = append(qs, question{Moment: Moment{At: e.At, RunID: e.RunID, SessionID: e.SessionID}, ref: e.Ref, stored: true})
		storedRefs[e.Ref] = true
		if e.RunID != 0 {
			questionRuns[e.RunID] = true
		}
	}
	for _, r := range in.Runs {
		if r.Outcome == "question" && r.EndedAt != nil && !questionRuns[r.ID] {
			qs = append(qs, question{Moment: Moment{At: *r.EndedAt, RunID: r.ID, Estimate: true}})
		}
	}
	sort.SliceStable(qs, func(i, j int) bool { return qs[i].At.Before(qs[j].At) })

	var cands []answerCandidate
	for _, e := range in.Events {
		if e.Kind == EventAnswer && e.RunID != 0 && !storedRefs[e.Ref] && firstAnswer[e.Ref] == e {
			cands = append(cands, answerCandidate{at: e.At})
		}
	}
	for _, r := range firstAnswerRuns(in.Runs) {
		if anyAnswered(in.Runs, r.TriggerKey, answeredRuns) {
			continue
		}
		cands = append(cands, answerCandidate{at: r.StartedAt, answer: &Moment{At: r.StartedAt, RunID: r.ID}})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].at.Before(cands[j].at) })

	var spans []span
	for i := range qs {
		q := &qs[i]
		if a, ok := firstAnswer[q.ref]; ok && q.stored {
			q.Answered = true
			if a.At.After(q.At) {
				spans = append(spans, span{Interval: Interval{q.At, a.At}, cause: CauseHuman, rank: rankHuman})
			}
			continue
		}
		if q.RunID != 0 {
			if c := nextCandidate(cands, q.At, nextDispatcherQuestion(qs, i)); c != nil {
				c.used, q.Answered = true, true
				if c.answer != nil {
					answers = append(answers, *c.answer)
				}
				if c.at.After(q.At) {
					spans = append(spans, span{Interval: Interval{q.At, c.at}, cause: CauseHuman, rank: rankHuman, estimate: true})
				}
				continue
			}
		}
		end, running := unansweredEnd(in, q.Moment, now)
		if end.After(q.At) {
			spans = append(spans, span{Interval: Interval{q.At, end}, cause: CauseHuman, rank: rankHuman, estimate: !q.stored, running: running})
		}
	}
	sort.SliceStable(answers, func(i, j int) bool { return answers[i].At.Before(answers[j].At) })
	moments := make([]Moment, len(qs))
	for i, q := range qs {
		moments[i] = q.Moment
	}
	return moments, answers, spans
}

// firstAnswerRuns returns, per answer trigger key, the run that started
// first: a restart of that run (same key) is the same answer.
func firstAnswerRuns(runs []TimelineRun) []TimelineRun {
	first := map[string]TimelineRun{}
	var keys []string
	for _, r := range runs {
		if !isAnswerKey(r.TriggerKey) {
			continue
		}
		f, ok := first[r.TriggerKey]
		if !ok {
			keys = append(keys, r.TriggerKey)
		}
		if !ok || r.StartedAt.Before(f.StartedAt) {
			first[r.TriggerKey] = r
		}
	}
	out := make([]TimelineRun, 0, len(keys))
	for _, k := range keys {
		out = append(out, first[k])
	}
	return out
}

// anyAnswered reports a stored answer on any run with trigger key key.
func anyAnswered(runs []TimelineRun, key string, answeredRuns map[int64]bool) bool {
	for _, r := range runs {
		if r.TriggerKey == key && answeredRuns[r.ID] {
			return true
		}
	}
	return false
}

// nextDispatcherQuestion returns the time of the first dispatcher question
// after qs[i], or nil when there is none.
func nextDispatcherQuestion(qs []question, i int) *time.Time {
	for j := i + 1; j < len(qs); j++ {
		if qs[j].RunID != 0 {
			return &qs[j].At
		}
	}
	return nil
}

// nextCandidate returns the first unused answer candidate at or after from
// and before limit (nil: no limit), so an answer never skips a question.
func nextCandidate(cands []answerCandidate, from time.Time, limit *time.Time) *answerCandidate {
	for i := range cands {
		c := &cands[i]
		if c.used || c.at.Before(from) {
			continue
		}
		if limit != nil && !c.at.Before(*limit) {
			return nil
		}
		return c
	}
	return nil
}

// unansweredEnd ends the wait on question q without answer at the first of,
// at or after q: the start of another run or session, the end of a returned
// run, the close of the issue. Without any it runs to now.
func unansweredEnd(in TimelineInput, q Moment, now time.Time) (time.Time, bool) {
	var end time.Time
	found := false
	consider := func(t time.Time) {
		if t.Before(q.At) {
			return
		}
		if !found || t.Before(end) {
			end, found = t, true
		}
	}
	for _, r := range in.Runs {
		if r.ID != q.RunID {
			consider(r.StartedAt)
		}
		if r.Outcome == "returned" && r.EndedAt != nil {
			consider(*r.EndedAt)
		}
	}
	for _, s := range in.Sessions {
		if s.ID != q.SessionID {
			consider(s.StartedAt)
		}
	}
	if !in.ClosedAt.IsZero() {
		consider(in.ClosedAt)
	}
	if found {
		return end, false
	}
	return now, true
}

// usageSpans gives a usage pause: from the end of a parked run to the start
// of the first later run with trigger usage.
func usageSpans(in TimelineInput) []span {
	var out []span
	for _, p := range in.Runs {
		if p.Outcome != "parked" || p.EndedAt == nil {
			continue
		}
		var resume time.Time
		found := false
		for _, r := range in.Runs {
			if r.Trigger == "usage" && !r.StartedAt.Before(*p.EndedAt) && (!found || r.StartedAt.Before(resume)) {
				resume, found = r.StartedAt, true
			}
		}
		if found && resume.After(*p.EndedAt) {
			out = append(out, span{Interval: Interval{*p.EndedAt, resume}, cause: CauseUsage, rank: rankUsage})
		}
	}
	return out
}

// takeovers lists every handover from the dispatcher to a human in time
// order: a run that ends returned, and a session that starts while a
// dispatcher run ran since the last takeover.
func takeovers(in TimelineInput) []Moment {
	type step struct {
		at    time.Time
		order int // at equal time: returned ends, then run starts, then sessions
		run   *TimelineRun
		sess  *TimelineSession
	}
	var steps []step
	for i := range in.Runs {
		r := &in.Runs[i]
		steps = append(steps, step{at: r.StartedAt, order: 1, run: r})
		if r.Outcome == "returned" && r.EndedAt != nil {
			steps = append(steps, step{at: *r.EndedAt, order: 0, run: r})
		}
	}
	for i := range in.Sessions {
		steps = append(steps, step{at: in.Sessions[i].StartedAt, order: 2, sess: &in.Sessions[i]})
	}
	sort.SliceStable(steps, func(i, j int) bool {
		if !steps[i].at.Equal(steps[j].at) {
			return steps[i].at.Before(steps[j].at)
		}
		return steps[i].order < steps[j].order
	})
	var out []Moment
	ranSince := false
	for _, s := range steps {
		switch {
		case s.order == 0:
			out = append(out, Moment{At: s.at, RunID: s.run.ID, Reason: s.run.Reason})
			ranSince = false
		case s.order == 1:
			ranSince = true
		case ranSince:
			out = append(out, Moment{At: s.at, SessionID: s.sess.ID, Reason: "session"})
			ranSince = false
		}
	}
	return out
}

// pickupSpans gives the measured pickup of a dispatcher answer: from the
// answer to the start of the last run with the trigger key of the run that
// took it up (a restart of that run moves the end).
func pickupSpans(in TimelineInput) []span {
	var out []span
	for _, a := range in.Events {
		if a.Kind != EventAnswer || a.RunID == 0 {
			continue
		}
		start, ok := pickupStart(in.Runs, a.RunID)
		if !ok || !start.After(a.At) {
			continue
		}
		out = append(out, span{Interval: Interval{a.At, start}, cause: CausePickup, rank: rankPickup})
	}
	return out
}

func pickupStart(runs []TimelineRun, runID int64) (time.Time, bool) {
	var key string
	var start time.Time
	found := false
	for _, r := range runs {
		if r.ID == runID {
			key, start, found = r.TriggerKey, r.StartedAt, true
		}
	}
	if !found || key == "" {
		return start, found
	}
	for _, r := range runs {
		if r.TriggerKey == key && r.StartedAt.After(start) {
			start = r.StartedAt
		}
	}
	return start, true
}

// firstEvent returns the earliest event of kind on run runID.
func firstEvent(events []TimelineEvent, kind EventKind, runID int64) (time.Time, bool) {
	var first time.Time
	found := false
	for _, e := range events {
		if e.Kind == kind && e.RunID == runID && (!found || e.At.Before(first)) {
			first, found = e.At, true
		}
	}
	return first, found
}

// sweep cuts the window at every span boundary and gives each elementary
// segment to the covering span of lowest rank; on equal rank a measured span
// beats an estimate. An uncovered segment is unexplained.
func sweep(window Interval, spans []span) map[Cause]CauseTotal {
	points := []time.Time{window.Start, window.End}
	for _, s := range spans {
		for _, p := range []time.Time{s.Start, s.End} {
			if p.After(window.Start) && p.Before(window.End) {
				points = append(points, p)
			}
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
	totals := map[Cause]CauseTotal{}
	for i := 0; i+1 < len(points); i++ {
		from, to := points[i], points[i+1]
		if !to.After(from) {
			continue
		}
		win := span{cause: CauseUnexplained}
		covered := false
		for _, s := range spans {
			if s.Start.After(from) || !s.End.After(from) {
				continue
			}
			if !covered || s.rank < win.rank || (s.rank == win.rank && win.estimate && !s.estimate) {
				win, covered = s, true
			}
		}
		// Spans that tie with the winner give their flags too, so the
		// result does not depend on the order of the spans.
		for _, s := range spans {
			if covered && !s.Start.After(from) && s.End.After(from) && s.rank == win.rank && s.estimate == win.estimate {
				win.running = win.running || s.running
				win.inclusive = win.inclusive || s.inclusive
			}
		}
		t := totals[win.cause]
		t.Duration += to.Sub(from)
		t.Estimate = t.Estimate || win.estimate
		t.Running = t.Running || win.running
		t.InclusiveCIReview = t.InclusiveCIReview || win.inclusive
		totals[win.cause] = t
	}
	return totals
}
