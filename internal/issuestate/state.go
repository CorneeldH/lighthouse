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

// The states of an issue.
const (
	Done         State = "done"
	Active       State = "active"
	NeedsShaping State = "needs_shaping"
	NeedsHuman   State = "needs_human"
	Blocked      State = "blocked"
	Ready        State = "ready"
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

// Result is the state of an issue.
type Result struct {
	State State
}

// Of is the state of an issue: the first row of the state table that fits, six states in all.
// A closed issue is done before any row. An active issue is needs_shaping with the needs_mind label,
// needs_human with an open question or the hands label, else active. A non-active issue is
// needs_shaping with the needs_mind label, blocked with a
// blocked label or open blocker, needs_human with the hands label, else ready.
func Of(in Input, l repoconfig.Labels) Result {
	has := func(names ...string) bool {
		for _, n := range names {
			if n != "" && slices.Contains(in.Labels, n) {
				return true
			}
		}
		return false
	}
	switch {
	case in.Closed:
		return Result{State: Done}
	case in.Run == "open" || in.Run == "parked" || in.Status == statusInProgress || in.Status == statusController:
		switch {
		case has(l.NeedsMind):
			return Result{State: NeedsShaping}
		case in.OpenQuestion || has(l.NeedsHands):
			return Result{State: NeedsHuman}
		}
		return Result{State: Active}
	case has(l.NeedsMind):
		return Result{State: NeedsShaping}
	case has(l.BlockedIssue, l.BlockedExternal) || len(OpenBlockers(in.Blockers)) > 0:
		return Result{State: Blocked}
	case has(l.NeedsHands):
		return Result{State: NeedsHuman}
	}
	return Result{State: Ready}
}

// HasNeedsLabel reports whether names holds any needs_* label (mind or hands). An empty configured name never matches.
func HasNeedsLabel(names []string, l repoconfig.Labels) bool {
	for _, n := range []string{l.NeedsMind, l.NeedsHands} {
		if n != "" && slices.Contains(names, n) {
			return true
		}
	}
	return false
}

// Stop is the route-issue gate 0 stop.
type Stop struct {
	Label    string // labels.blocked_issue or labels.blocked_external when present
	Blockers []int  // open blocked_by numbers
}

// GateZero reports the gate 0 stop: a blocked_issue label (before blocked_external) stops with
// that label; else open blockers without needs_mind stop. Blockers is
// OpenBlockers in both cases. Unlike Of, a blocked label wins here over needs_mind on an
// issue that carries both.
func GateZero(labels []string, blockers []forge.Blocker, l repoconfig.Labels) (Stop, bool) {
	has := func(name string) bool { return name != "" && slices.Contains(labels, name) }
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
	if len(s.Blockers) > 0 && !has(l.NeedsMind) {
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
