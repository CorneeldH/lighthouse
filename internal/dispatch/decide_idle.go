package dispatch

import (
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// idleTickLimit is the number of evaluations in a row, the current one counted, that find a
// Controller issue without a next step before the issue goes back with reason_no_rule.
const idleTickLimit = 3

// idle is the safety net of next: an issue in Controller for which no other rule gave an action
// and that waits for nothing the tick knows of. It waits for something when it has an open run,
// a needs_* label or an active blocker, a pause (pausedRun), an open PR or an end handoff that
// awaits its PR, a Question without an answer (the pusher's too), or when the last protocol
// comment is the tick's own Returned: that issue was given back already, and giving it back
// again would loop. Otherwise it is a KindIdle mark, the tick counts it in RepoInput.IdleTicks,
// and the evaluation that reaches idleTickLimit gives it back naming the last protocol comment.
func (c issueCtx) idle() (planned, bool) {
	if hasOpenRun(c.repo.History, c.it.Issue) || c.needsHuman() || c.active() || c.pausedRun() ||
		c.waitsForPR() || c.ownReturned() || c.openQuestion() {
		return planned{}, false
	}
	if c.repo.IdleTicks[c.it.Issue]+1 >= idleTickLimit {
		return c.returned("reason_no_rule", c.lastProtocolLine()), true
	}
	a := c.base
	a.Kind = KindIdle
	return planned{Action: a}, true
}

// pausedRun reports whether the issue may have a run the usage guard parked: a marker or an
// unreadable marker file of a session whose last run is on this issue, or markers of a node that
// could not be read this tick.
func (c issueCtx) pausedRun() bool {
	if c.repo.MarkersIncomplete {
		return true
	}
	onIssue := func(session string) bool {
		row, ok := lastOfSession(c.repo.History, session)
		return ok && row.Issue == c.it.Issue
	}
	for _, m := range c.repo.Markers {
		if onIssue(m.SessionID) {
			return true
		}
	}
	return slices.ContainsFunc(c.repo.Unreadable, onIssue)
}

// waitsForPR reports whether a PR of the issue is open, or the last turn is a Handoff that
// awaits its PR (awaitsPR).
func (c issueCtx) waitsForPR() bool {
	if slices.ContainsFunc(c.it.PRs, func(pr forge.PR) bool { return pr.State == "OPEN" }) {
		return true
	}
	t := lastTurn(c.hs)
	return t != nil && t.Header.Kind == protocol.KindHandoff && c.awaitsPR(t.Time)
}

// ownReturned reports whether the last protocol comment is a Returned of the token login.
func (c issueCtx) ownReturned() bool {
	p := lastProtocol(c.hs)
	return p != nil && p.Header.Kind == protocol.KindReturned && p.Comment.Author == c.repo.Snapshot.TokenLogin
}

// openQuestion reports whether the last turn is a Question nobody answered yet.
func (c issueCtx) openQuestion() bool {
	q := lastTurn(c.hs)
	if q == nil || q.Header.Kind != protocol.KindQuestion {
		return false
	}
	_, answered := c.answerTo(q.Comment.ID)
	return !answered
}

// lastProtocolLine is the first line of the last protocol comment, or note_no_protocol_comment
// when the issue has none.
func (c issueCtx) lastProtocolLine() string {
	p := lastProtocol(c.hs)
	if p == nil {
		return protocol.Heading("note_no_protocol_comment", c.repo.Language)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(p.Comment.Body), "\n")
	return strings.TrimSpace(first)
}
