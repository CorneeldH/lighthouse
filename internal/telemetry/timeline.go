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
	out.Window = window
	var spans []span
	spans = append(spans, runSpans(in, now)...)
	spans = append(spans, sessionSpans(in, now)...)
	spans = append(spans, humanSpans(in)...)
	spans = append(spans, pickupSpans(in)...)
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
	return w, found
}

// isPusherRow reports a row of the workflow pusher or a merge run: it runs
// no agent session, so it is never active.
func isPusherRow(r TimelineRun) bool {
	return r.Command == "push-workflows" || strings.HasPrefix(r.TriggerKey, "merge:")
}

// runSpans gives a pusher row ci_review over its whole length, and an agent
// run active up to its first Done (ci_review after it) or up to its stored
// question; without either, active marked inclusive of CI/review.
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
		if done, ok := firstEvent(in.Events, EventDone, r.ID); ok && !done.Before(r.StartedAt) && done.Before(end) {
			out = append(out,
				span{Interval: Interval{r.StartedAt, done}, cause: CauseActive, rank: rankRunActive},
				span{Interval: Interval{done, end}, cause: CauseCIReview, rank: rankCIReview, running: running})
			continue
		}
		if r.Outcome == "question" {
			if q, ok := firstEvent(in.Events, EventQuestion, r.ID); ok && !q.Before(r.StartedAt) && q.Before(end) {
				out = append(out, span{Interval: Interval{r.StartedAt, q}, cause: CauseActive, rank: rankRunActive})
				continue
			}
		}
		out = append(out, span{Interval: Interval{r.StartedAt, end}, cause: CauseActive, rank: rankRunActive, running: running, inclusive: true})
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

// humanSpans gives the measured wait on a human: from a stored question to
// the stored answer with the same ref.
func humanSpans(in TimelineInput) []span {
	var out []span
	for _, q := range in.Events {
		if q.Kind != EventQuestion {
			continue
		}
		a, ok := answerTo(in.Events, q.Ref)
		if !ok || !a.At.After(q.At) {
			continue
		}
		out = append(out, span{Interval: Interval{q.At, a.At}, cause: CauseHuman, rank: rankHuman})
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

func answerTo(events []TimelineEvent, ref string) (TimelineEvent, bool) {
	for _, e := range events {
		if e.Kind == EventAnswer && e.Ref == ref {
			return e, true
		}
	}
	return TimelineEvent{}, false
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
		t := totals[win.cause]
		t.Duration += to.Sub(from)
		t.Estimate = t.Estimate || win.estimate
		t.Running = t.Running || win.running
		t.InclusiveCIReview = t.InclusiveCIReview || win.inclusive
		totals[win.cause] = t
	}
	return totals
}
