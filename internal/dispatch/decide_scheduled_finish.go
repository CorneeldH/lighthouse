package dispatch

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// ScheduledPRTimeout is pr_timeout (spec §4): how long the PR of a scheduled run may stay
// unmerged after it was opened. A pause or a closed brake in that time counts.
const ScheduledPRTimeout = 60 * time.Minute

// settleScheduled gives the end of each open scheduled run of repo: at most one action per run,
// the first rule of the spec §4 table that holds. None of them is a start: the brake lets them
// through and they take no slot.
//
// The agent phase lasts while pr_at is empty: the retry rules (the usage park, a gone
// workplace, a claim a dead tick left without a workplace, rules 15, 9, 10, 19 on the record's
// max_runtime, 16, 17, 18) close the run as retry (failed on the last attempt) after its agent
// is ended; a done session gets KindScheduledFinish. max_runtime holds up to and including the
// check, so it goes before the finish. After pr_at only the run's own PR and pr_timeout count:
// merged is done, red or conflicting is failed, closed without a merge is failed, green under a
// brake that is not paused merges, and past pr_timeout the run is failed.
func settleScheduled(in Input, repo RepoInput) []planned {
	var out []planned
	for _, row := range repo.History {
		if !row.Open || row.Schedule == "" {
			continue
		}
		c := scheduledCtx{runCtx: newRunCtx(in, repo, Item{}, row, Action{}), prs: repo.Snapshot.ScheduledPRs[row.Branch]}
		c.base = c.action(repo)
		if a, ok := c.settle(); ok {
			out = append(out, planned{Action: a})
		}
	}
	return out
}

// scheduledCtx is what the end rules of one open scheduled run read: the measurements of
// runCtx (the item is empty) and the PRs of its branch.
type scheduledCtx struct {
	runCtx
	prs []forge.PR
}

// action is the base of every action on the run: its repo, run, schedule, slot and branch.
func (c scheduledCtx) action(repo RepoInput) Action {
	slot := c.row.TriggerKey[strings.LastIndex(c.row.TriggerKey, ":")+1:]
	return Action{Repo: repo.Snapshot.Repo, RunID: c.row.ID, Schedule: c.row.Schedule, Slot: slot,
		Branch: c.row.Branch, Language: repo.Language}
}

func (c scheduledCtx) settle() (Action, bool) {
	if !c.row.PRAt.IsZero() {
		return c.prPhase()
	}
	return c.agentPhase()
}

// agentPhase decides a run without pr_at. A row the snapshot's open runs lack (it was claimed
// after the board half of the gather) waits for the next tick.
func (c scheduledCtx) agentPhase() (Action, bool) {
	if c.open.ID != c.row.ID {
		return Action{}, false
	}
	retry := func(reason string, end, discard bool) (Action, bool) {
		a := c.closed(retryOutcome(c.row.Attempt), reason)
		a.End, a.Discard = end, discard
		if pr, ok := c.ownOpen(); ok {
			a.PR = pr.Number
		}
		return a, true
	}
	t := c.state.Transcript
	limit := c.maxRuntime()
	switch {
	case parkedRun(c.markers, c.row):
		return retry("parked by the usage guard", true, false)
	case c.state.Gone:
		return retry("workplace gone", false, false)
	case c.open.WorkspaceID == "":
		// Claimed, and the tick that claimed it died before it recorded a workplace: no agent
		// was spawned (Spawn follows SetWorkplace). Every tick holds tick.lock, so after max_wait
		// that tick is over.
		if c.age() < c.maxWait() {
			return Action{}, false
		}
		return retry("claimed without a workplace", false, false)
	}
	if _, ok := c.uncertain(); ok {
		// The launch is forgotten first, then whatever started is ended (apply, in that order).
		return retry("start uncertain: no session found", true, true)
	}
	switch {
	case t != nil && t.UnknownCommand != "":
		return retry("unknown command `"+t.UnknownCommand+"`", true, false)
	case c.done() && t != nil && !t.AssistantTurn:
		return retry("session did nothing", true, false)
	case limit > 0 && c.age() >= limit:
		return retry(fmt.Sprintf("timeout (max_runtime %s)", limit), true, false)
	case c.done():
		a := c.base
		a.Kind = KindScheduledFinish
		if pr, ok := c.ownOpen(); ok {
			a.PR = pr.Number
		}
		return a, true
	}
	if _, ok := c.permission(); ok {
		return retry("waiting on a permission for `"+c.pendingPermission().Tool+"`", true, false)
	}
	if _, ok := c.silent(); ok {
		return retry("no activity in max_wait", true, false)
	}
	if _, ok := c.silentForge(); ok {
		return retry("no activity in max_wait (forge stand)", true, false)
	}
	return Action{}, false
}

// prPhase decides a run with pr_at, on its own PR and pr_timeout only.
func (c scheduledCtx) prPhase() (Action, bool) {
	if c.prsWith(func(pr forge.PR) bool { return pr.State == "MERGED" }) {
		return c.closed(runs.Done, ""), true
	}
	pr, open := c.ownOpen()
	switch {
	case open && redChecks(pr):
		a := c.closed(runs.Failed, fmt.Sprintf("PR #%d red", pr.Number))
		a.PR = pr.Number
		return a, true
	case open && pr.Mergeable == "CONFLICTING":
		a := c.closed(runs.Failed, fmt.Sprintf("PR #%d has a merge conflict", pr.Number))
		a.PR = pr.Number
		return a, true
	case open && mergeCandidate(pr) && pr.Roborev != forge.RoborevOpen && (c.in.Brake == nil || !c.in.Brake.Paused):
		a := c.base
		a.Kind, a.PR, a.HeadSHA, a.PRURL = KindScheduledMerge, pr.Number, pr.HeadSHA, pr.URL
		return a, true
	case !open:
		if closed, ok := c.own(func(pr forge.PR) bool { return pr.State == "CLOSED" }); ok {
			return c.closed(runs.Failed, fmt.Sprintf("PR #%d closed without a merge", closed.Number)), true
		}
	}
	if c.in.Now.Sub(c.row.PRAt) < ScheduledPRTimeout {
		return Action{}, false
	}
	if open {
		a := c.closed(runs.Failed, fmt.Sprintf("PR #%d not merged within pr_timeout %s", pr.Number, ScheduledPRTimeout))
		a.PR = pr.Number
		return a, true
	}
	return c.closed(runs.Failed, fmt.Sprintf("no PR merged within pr_timeout %s", ScheduledPRTimeout)), true
}

// closed closes the run with o and the literal reason; apply also deletes its branch.
func (c scheduledCtx) closed(o runs.Outcome, reason string) Action {
	a := c.base
	a.Kind, a.Outcome, a.ReasonArg = KindScheduledClose, o, reason
	return a
}

// maxRuntime is the limit of the agent phase: the max_runtime of the run's record, the host's
// when the record has none or does not read.
func (c scheduledCtx) maxRuntime() time.Duration {
	return scheduledMaxRuntime(c.row.Verdict, time.Duration(c.in.MaxRuntime))
}

// scheduledMaxRuntime is the max_runtime of the record in verdict, or host when it has none.
func scheduledMaxRuntime(verdict string, host time.Duration) time.Duration {
	if rec, err := scheduled.DecodeRecord(verdict); err == nil && rec.MaxRuntime > 0 {
		return rec.MaxRuntime
	}
	return host
}

// ownPR reports whether pr is the run's own: its head is the run's branch, in this repository.
func (c scheduledCtx) ownPR(pr forge.PR) bool {
	return pr.HeadRef == c.row.Branch && !pr.CrossRepo && validBranch(pr.HeadRef)
}

// own returns the first own PR, by number order of the snapshot, that satisfies f.
func (c scheduledCtx) own(f func(forge.PR) bool) (forge.PR, bool) {
	i := slices.IndexFunc(c.prs, func(pr forge.PR) bool { return c.ownPR(pr) && f(pr) })
	if i < 0 {
		return forge.PR{}, false
	}
	return c.prs[i], true
}

// ownOpen returns the run's own open PR.
func (c scheduledCtx) ownOpen() (forge.PR, bool) {
	return c.own(func(pr forge.PR) bool { return pr.State == "OPEN" })
}

// prsWith reports whether an own PR that merged from the run's start on satisfies f.
func (c scheduledCtx) prsWith(f func(forge.PR) bool) bool {
	_, ok := c.own(func(pr forge.PR) bool { return c.endedInRun(pr) && f(pr) })
	return ok
}
