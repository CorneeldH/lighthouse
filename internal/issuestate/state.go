// Package issuestate holds the deterministic rules of the issue status model: the state of an
// issue, the rank rule for its blocking label and giving an issue back to `To do`. The rules come
// from the status model spec of issue 433 (its state table, the give-back by the controller and
// the one place for the rules); the rank is the one in the developer label taxonomy.
//
// tests/pack/fixtures/issue-state/cases.json holds the test cases of Of. The web UI (issueState())
// reads the same file, so both carry one rule.
package issuestate

import (
	"slices"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// State is the state of an issue.
type State string

// The states of an issue. Blocked is not a state: it is the flag Result.Blocked.
const (
	Done       State = "done"
	NeedsMind  State = "needs_mind"
	NeedsHands State = "needs_hands"
	Active     State = "active"
	Ready      State = "ready"
)

// Board statuses that make an issue active.
const (
	statusInProgress = "In progress"
	statusController = "Controller"
)

// Input is what the state of an issue follows from.
type Input struct {
	Closed       bool
	Labels       []string // label names on the issue
	Status       string   // board Status name; "" without one
	Run          string   // "open", "parked" or "" (dispatch.RunView.State)
	Blockers     []forge.Blocker
	OpenQuestion bool // protocol.Classify(...).State == protocol.StateQuestion
}

// Result is the state of an issue and whether a blockade lies on it.
type Result struct {
	State   State
	Blocked bool
}

// Of is the state of an issue: the first row that fits.
//
//  1. closed: done.
//  2. the needs_mind label or an open question: needs_mind.
//  3. the needs_hands label without a blockade: needs_hands.
//  4. an open or parked run, board Status Controller, or Status In progress without a blockade: active.
//  5. else ready.
//
// A blockade is a blocked_issue or blocked_external label or an open blocker. Blocked is true
// for a blockade, except on a closed issue. A blockade does not change the state of needs_mind,
// of a run or of Controller; it only keeps needs_hands and In progress without a run from showing.
func Of(in Input, l repoconfig.Labels) Result {
	has := func(names ...string) bool {
		for _, n := range names {
			if n != "" && slices.Contains(in.Labels, n) {
				return true
			}
		}
		return false
	}
	if in.Closed {
		return Result{State: Done}
	}
	blocked := has(l.BlockedIssue, l.BlockedExternal) || len(OpenBlockers(in.Blockers)) > 0
	live := in.Run == "open" || in.Run == "parked" || in.Status == statusController
	switch {
	case has(l.NeedsMind) || in.OpenQuestion:
		return Result{State: NeedsMind, Blocked: blocked}
	case has(l.NeedsHands) && !blocked:
		return Result{State: NeedsHands}
	case live || (in.Status == statusInProgress && !blocked):
		return Result{State: Active, Blocked: blocked}
	}
	return Result{State: Ready, Blocked: blocked}
}

// Stop is the route-issue gate 0 stop.
type Stop struct {
	Label    string // labels.blocked_issue or labels.blocked_external when present
	Blockers []int  // open blocked_by numbers
}

// GateZero reports the gate 0 stop: a blocked_issue label (before blocked_external) stops with
// that label; else open blockers stop. Blockers is OpenBlockers in both cases. An issue that
// carries needs_mind does not stop, with or without a blocked label or an open blocker: it
// routes on to the shaping gate.
func GateZero(labels []string, blockers []forge.Blocker, l repoconfig.Labels) (Stop, bool) {
	has := func(name string) bool { return name != "" && slices.Contains(labels, name) }
	if has(l.NeedsMind) {
		return Stop{}, false
	}
	s := Stop{Blockers: OpenBlockers(blockers)}
	switch {
	case has(l.BlockedIssue):
		s.Label = l.BlockedIssue
	case has(l.BlockedExternal):
		s.Label = l.BlockedExternal
	}
	if s.Label != "" {
		return s, true
	}
	if len(s.Blockers) > 0 {
		return s, true
	}
	return Stop{}, false
}

// OpenBlockers is the issue numbers of the blockers with state OPEN, in input order; nil for
// none. A merged PR of a blocker does not make it closed.
func OpenBlockers(bs []forge.Blocker) []int {
	var out []int
	for _, b := range bs {
		if b.State == "OPEN" {
			out = append(out, b.Issue)
		}
	}
	return out
}
