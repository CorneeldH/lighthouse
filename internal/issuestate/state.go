// Package issuestate holds the deterministic rules of the issue status model: the state of an
// issue, the rank rule for its blocking label and giving an issue back to `To do`. The rules come
// from the status model spec of issue 433 (its state table, the give-back by the controller and
// the one place for the rules); the rank is the one in docs/developer/labels.md.
//
// tests/fixtures/issue-state/cases.json holds the test cases of Of. The web UI (issueState())
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
	Done       State = "done"
	Active     State = "active"
	NeedsHuman State = "needs_human"
	Blocked    State = "blocked"
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

// Result is the state of an issue; NeedsHuman is only true when State is Active.
type Result struct {
	State      State
	NeedsHuman bool
}

// Of is the state of an issue: the first row of the state table that fits. A closed issue is
// done before any row.
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
		flag := in.OpenQuestion || has(l.NeedsShaping, l.NeedsDecision, l.NeedsHands, l.NeedsTesting)
		return Result{State: Active, NeedsHuman: flag}
	case has(l.NeedsShaping, l.NeedsDecision):
		return Result{State: NeedsHuman}
	case has(l.BlockedIssue, l.BlockedExternal) || len(OpenBlockers(in.Blockers)) > 0:
		return Result{State: Blocked}
	case has(l.NeedsHands, l.NeedsTesting):
		return Result{State: NeedsHuman}
	}
	return Result{State: Ready}
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
