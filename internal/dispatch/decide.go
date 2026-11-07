package dispatch

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Input is everything one tick decides on.
type Input struct {
	Now   time.Time   `json:"now"`
	Night string      `json:"night"`
	Repos []RepoInput `json:"repos"`
	// MaxWait and MaxRuntime are host.toml [dispatch] max_wait and max_runtime; zero turns
	// the time rules that use them off.
	MaxWait    Duration `json:"max_wait,omitempty"`
	MaxRuntime Duration `json:"max_runtime,omitempty"`
	Brake      *Brake   `json:"brake,omitempty"` // nil: no brake
}

// RepoInput is what Decide knows about one repo.
type RepoInput struct {
	Snapshot      Snapshot                  `json:"snapshot"`
	Controller    repoconfig.Controller     `json:"controller"`
	Language      string                    `json:"language"`
	DefaultBranch string                    `json:"default_branch"`
	Installed     map[string]bool           `json:"installed"`
	History       []runs.Row                `json:"history"`
	Stands        map[int64]workplace.Stand `json:"stands"`         // per open run id; missing = not measurable
	Runs          map[int64]RunState        `json:"runs,omitempty"` // per open run id; missing = not measured
	// Markers are the controller markers whose session's last run is in this repo, resumable
	// or not.
	Markers []park.Marker `json:"markers"`
}

// Kind is what an action does.
type Kind string

const (
	KindStart  Kind = "start"
	KindResume Kind = "resume"
	KindReturn Kind = "return"
	KindDone   Kind = "done"
	KindClose  Kind = "close"
	KindRocket Kind = "rocket"
	KindDefer  Kind = "defer"
	KindPark   Kind = "park"
	KindHold   Kind = "hold"
	KindUnpark Kind = "unpark"
	// KindDropMarker removes the resume marker of SessionID while its key is TriggerKey.
	KindDropMarker Kind = "drop_marker"
)

// Action is one thing apply does. Reason and Skip are heading keys, never posted text; only a
// KindDefer carries a literal Reason (night), which apply reports without a lookup. A park,
// hold or unpark carries the board ItemID; a park also the Blockers its 🤖 Parked names.
type Action struct {
	Kind       Kind         `json:"kind"`
	Repo       string       `json:"repo"`
	Issue      int          `json:"issue"`
	RunID      int64        `json:"run_id,omitempty"`
	Outcome    runs.Outcome `json:"outcome,omitempty"`
	Trigger    string       `json:"trigger,omitempty"`
	TriggerKey string       `json:"trigger_key,omitempty"`
	Attempt    int          `json:"attempt,omitempty"`
	Mode       string       `json:"mode,omitempty"`
	SessionID  string       `json:"session_id,omitempty"` // resume: the session; start: "" (apply makes one)
	Command    string       `json:"command,omitempty"`
	Prompt     string       `json:"prompt,omitempty"`
	Branch     string       `json:"branch,omitempty"`
	Native     bool         `json:"native,omitempty"`
	Skip       string       `json:"skip,omitempty"`       // a headings key
	CommentID  int64        `json:"comment_id,omitempty"` // 🚀 target; for resume also the answer id of 🤖 Resumed
	Reason     string       `json:"reason,omitempty"`     // a headings key; for KindDefer literal
	ReasonArg  string       `json:"reason_arg,omitempty"` // the argument of Reason, literal
	PRURL      string       `json:"pr_url,omitempty"`
	Language   string       `json:"language,omitempty"`
	Stop       bool         `json:"stop,omitempty"`     // stop the session of run RunID
	End        bool         `json:"end,omitempty"`      // end only the claude of run RunID
	Discard    bool         `json:"discard,omitempty"`  // forget the launch of run RunID
	ItemID     string       `json:"item_id,omitempty"`  // park, hold, unpark: the board item
	Blockers   []string     `json:"blockers,omitempty"` // park: "<repo>#<n> (<status>)" per active blocker
}

// Decide is the pure decision of one tick: per repo, per item, first the orphan rule, then
// the settlement of the item's open runs, then the 🚀 repair, then at most one start,
// resume, park or hold (a fix-pr start, else next), or for an item in On hold at most one
// unpark; per repo also the markers that will never resume. After the brake, a run closed for
// a fix-pr start that did not survive (a closed gate, a defer, a pause) is handed back as
// "PR red" instead. The result is ordered: the actions without a start or resume (park, hold
// and unpark among them) by repo and issue, then the defers, then the starts and resumes by
// priority, trigger time, repo and issue.
func Decide(in Input) []Action {
	var ps []planned
	for _, repo := range in.Repos {
		for _, it := range repo.Snapshot.Items {
			ps = append(ps, decideIssue(in, repo, it)...)
		}
		for _, a := range dropMarkers(repo) {
			ps = append(ps, planned{Action: a})
		}
	}
	slices.SortStableFunc(ps, comparePlanned)
	if in.Brake != nil {
		ps = in.Brake.apply(ps)
	}
	ps = returnUnstartedFixPR(ps)
	slices.SortStableFunc(ps, comparePlanned)
	out := make([]Action, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Action)
	}
	return out
}

// returnUnstartedFixPR hands back every run settle closed for a fix-pr start (a handoff close
// whose ReasonArg is the fixpr: key) when ps holds no fix-pr start for its issue: decideIssue
// did not plan one, or the brake deferred or dropped it. The run then closes as Returned "PR
// red" with its own session, as before fix-pr existed: a handoff without a start would leave
// the issue silent on a red PR.
func returnUnstartedFixPR(ps []planned) []planned {
	type issueKey struct {
		repo  string
		issue int
	}
	started := map[issueKey]bool{}
	for _, p := range ps {
		if p.Kind == KindStart && p.Trigger == triggerFixPR {
			started[issueKey{p.Repo, p.Issue}] = true
		}
	}
	for i, p := range ps {
		if p.Kind != KindClose || p.Outcome != runs.Handoff || !strings.HasPrefix(p.ReasonArg, triggerFixPR+":") ||
			started[issueKey{p.Repo, p.Issue}] {
			continue
		}
		a := p.Action
		a.Kind, a.Reason, a.Outcome, a.ReasonArg = KindReturn, "reason_pr_red", "", ""
		ps[i] = planned{Action: a}
	}
	return ps
}

// planned is an action with what orders it; priority and at count for a start or resume.
type planned struct {
	Action
	priority int
	at       time.Time
	software bool // the repo profile is "software"
	parked   bool // a usage resume: its run already holds a slot
}

// group places an action in the output: 0 without a start or resume (a park, hold or unpark
// too), 1 a defer, 2 a start or resume.
func (p planned) group() int {
	switch p.Kind {
	case KindStart, KindResume:
		return 2
	case KindDefer:
		return 1
	}
	return 0
}

func comparePlanned(a, b planned) int {
	if c := cmp.Compare(a.group(), b.group()); c != 0 {
		return c
	}
	if a.group() == 2 {
		if c := cmp.Compare(a.priority, b.priority); c != 0 {
			return c
		}
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
	}
	return cmp.Or(strings.Compare(a.Repo, b.Repo), cmp.Compare(a.Issue, b.Issue))
}

// decideIssue gives the actions of one item. The start rules run only for an open issue in
// Controller whose open runs all close in this tick, and not when the orphan rule hands the
// issue back: a start next to that Returned would contradict it. The same holds for the
// unpark rule of an open issue in On hold. When settle closed a run for a fix-pr start and no
// blocker is active, that start is the item's one start in this tick and next does not run;
// in every other case Decide hands the run back (returnUnstartedFixPR).
func decideIssue(in Input, repo RepoInput, it Item) []planned {
	hs := headers(it)
	base := Action{Repo: repo.Snapshot.Repo, Issue: it.Issue, Language: repo.Language}
	var out []planned
	a, orphaned := orphan(repo.History, hs, base)
	if orphaned {
		out = append(out, planned{Action: a})
	}
	free := !orphaned
	var fix *fixPRStart
	for _, row := range repo.History {
		if !row.Open || row.Issue != it.Issue {
			continue
		}
		a, closes, f := settle(in, repo, it, row, hs, base)
		if closes {
			out = append(out, planned{Action: a})
			if f != nil {
				fix = f
			}
		}
		free = free && closes
	}
	for _, a := range repair(repo.History, it, base) {
		out = append(out, planned{Action: a})
	}
	if !free || it.State != "OPEN" {
		return out
	}
	c := issueCtx{repo: repo, it: it, hs: hs, base: base, now: in.Now, night: in.Night}
	switch it.Status {
	case statusController:
		if fix != nil && !c.active() {
			a := c.fresh(triggerFixPR, fix.key, cmdFixPR+" "+strconv.Itoa(fix.pr.Number), fix.pr.HeadRef)
			out = append(out, c.planned(a, c.now))
		} else if p, ok := c.next(); ok {
			out = append(out, p)
		}
	case statusOnHold:
		if c.ownParked() && !c.active() {
			out = append(out, planned{Action: c.board(KindUnpark)})
		}
	}
	return out
}

// parsed is a protocol comment with its header and its time.
type parsed struct {
	Comment Comment
	Header  protocol.Header
	Time    time.Time
}

// headers returns the protocol comments of it, oldest first as the snapshot keeps them. An
// unreadable CreatedAt is the zero time.
func headers(it Item) []parsed {
	var out []parsed
	for _, c := range it.Comments {
		h, ok := protocol.ParseHeader(c.Body)
		if !ok {
			continue
		}
		out = append(out, parsed{Comment: c, Header: h, Time: parseTime(c.CreatedAt)})
	}
	return out
}

// parseTime reads an RFC 3339 time; an unreadable one is the zero time.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// orphan returns a return with reason_state_lost when the last protocol comment (Rebased
// does not count) is a Started or Resumed whose session no run in the history has: the
// database lost the run. A row without a session makes no session known.
func orphan(history []runs.Row, hs []parsed, base Action) (Action, bool) {
	var last *parsed
	for i := range hs {
		if hs[i].Header.Kind != protocol.KindRebased {
			last = &hs[i]
		}
	}
	if last == nil || (last.Header.Kind != protocol.KindStarted && last.Header.Kind != protocol.KindResumed) {
		return Action{}, false
	}
	for _, row := range history {
		if row.SessionID != "" && row.SessionID == last.Header.Session {
			return Action{}, false
		}
	}
	a := base
	a.Kind, a.Reason, a.SessionID = KindReturn, "reason_state_lost", last.Header.Session
	return a, true
}

// settle decides whether the open run row ends in this tick: the first rule of the run-outcome
// table that holds wins. Only comments from row's start on count; a Done, Question or Returned
// also needs row's session, a Handoff carries none. A Done closes, then a Handoff that does
// not end the chain, then a Question, then the run's own Returned (posted by an earlier tick
// whose stop failed); after those the measurement rules of runCtx.outcome. Every action it
// returns closes row. The third result, only from prRed, is the fix-pr start the close stands
// for; settle starts nothing itself.
func settle(in Input, repo RepoInput, it Item, row runs.Row, hs []parsed, base Action) (Action, bool, *fixPRStart) {
	var done, question, returned bool
	var handoff *parsed
	for i, p := range hs {
		if p.Time.Before(row.StartedAt) {
			continue
		}
		ofRun := p.Header.Session != "" && p.Header.Session == row.SessionID
		switch p.Header.Kind {
		case protocol.KindDone:
			done = done || ofRun
		case protocol.KindQuestion:
			question = question || ofRun
		case protocol.KindReturned:
			returned = returned || ofRun
		case protocol.KindHandoff:
			handoff = &hs[i]
		}
	}
	c := newRunCtx(in, repo, it, row, base)
	c.handoff = handoff != nil
	switch {
	case done:
		return c.closed(runs.Done), true, nil
	case handoff != nil && !endsChain(repo, it.Issue, handoff.Header):
		return c.closed(runs.Handoff), true, nil
	case question:
		return c.closed(runs.Question), true, nil
	case returned:
		a := c.closed(runs.Returned)
		if c.open.WorkplaceSession == "" {
			a.Discard = true
		} else {
			// A gone workspace has no session to stop: Stop would fail every tick.
			a.Stop = !c.done() && !c.state.Gone
		}
		return a, true, nil
	}
	return c.outcome()
}

// fixPRStart is the fix-pr start settle hands to decideIssue: the red PR and its trigger key.
type fixPRStart struct {
	pr  forge.PR
	key string
}

// runCtx is what the measurement rules of one open run read.
type runCtx struct {
	in       Input
	it       Item
	row      runs.Row
	base     Action
	open     runs.OpenRun // the snapshot's open run of row; zero when it has none
	state    RunState
	measured bool // the run is in RepoInput.Runs
	stand    workplace.Stand
	hasStand bool
	handoff  bool          // a Handoff stands from row's start on
	markers  []park.Marker // the repo's resume markers
	history  []runs.Row    // the repo's runs, open and closed
	trunk    string        // the repo's default branch
}

func newRunCtx(in Input, repo RepoInput, it Item, row runs.Row, base Action) runCtx {
	c := runCtx{in: in, it: it, row: row, base: base, markers: repo.Markers, history: repo.History,
		trunk: repo.DefaultBranch}
	for _, o := range repo.Snapshot.OpenRuns {
		if o.ID == row.ID {
			c.open = o
		}
	}
	c.state, c.measured = repo.Runs[row.ID]
	c.stand, c.hasStand = repo.Stands[row.ID]
	return c
}

// outcome returns the action of the first measurement rule that holds, in the order of the
// run-outcome table, with the fix-pr start of prRed.
func (c runCtx) outcome() (Action, bool, *fixPRStart) {
	rules := []func() (Action, bool, *fixPRStart){
		noStart(c.parked), noStart(c.merged), noStart(c.closedIssue), noStart(c.green), noStart(c.gone),
		noStart(c.unknownCommand), noStart(c.didNothing), noStart(c.prClosed), noStart(c.prDraft),
		c.prRed, noStart(c.noHandoff), noStart(c.uncertain), noStart(c.permission), noStart(c.silent),
		noStart(c.silentForge), noStart(c.timeout),
	}
	for _, rule := range rules {
		if a, ok, fix := rule(); ok {
			return a, true, fix
		}
	}
	return Action{}, false, nil
}

// noStart lifts a measurement rule that never starts fix-pr into outcome's rule shape.
func noStart(rule func() (Action, bool)) func() (Action, bool, *fixPRStart) {
	return func() (Action, bool, *fixPRStart) {
		a, ok := rule()
		return a, ok, nil
	}
}

// done reports whether the run's session measured Done. A forge stand done is not Done while
// the transcript shows work after the Stop that set it: background work still running, for
// which claude ended its turn, or a turn claude took on the notification of such work (#270).
// The silence and runtime rules still hold for such a run.
func (c runCtx) done() bool {
	t := c.state.Transcript
	return c.hasStand && c.stand == workplace.Done && (t == nil || t.Background == 0 && !t.TurnOpen)
}

// closed closes row with o. A run that ends as done, handoff or question is over, but its
// claude lives on in the workplace, also with a forge stand done: closed ends it, or a resume
// of its session fails at forge.
func (c runCtx) closed(o runs.Outcome) Action {
	a := c.base
	a.Kind, a.RunID, a.Outcome = KindClose, c.row.ID, o
	a.End = o == runs.Done || o == runs.Handoff || o == runs.Question
	return a
}

// returned hands the issue back and closes row; the session footer is row's, so a later tick
// recognises the Returned as the run's own.
func (c runCtx) returned(reason, arg string) Action {
	a := c.base
	a.Kind, a.RunID, a.SessionID, a.Reason, a.ReasonArg = KindReturn, c.row.ID, c.row.SessionID, reason, arg
	return a
}

func (c runCtx) parked() (Action, bool) {
	return c.closed(runs.Parked), parkedRun(c.markers, c.row)
}

// parkedRun reports whether the usage guard parked row: a marker of row's session written
// at or after row's start. An older marker belongs to an earlier parking: a resume reuses the
// session, and its row must not close on the marker the resume answered.
func parkedRun(markers []park.Marker, row runs.Row) bool {
	for _, m := range markers {
		created, _ := m.Created()
		if row.SessionID != "" && m.SessionID == row.SessionID && !created.Before(row.StartedAt) {
			return true
		}
	}
	return false
}

// endedInRun reports whether pr merged or closed from the run's start on: a PR of an earlier
// run does not count.
func (c runCtx) endedInRun(pr forge.PR) bool {
	switch pr.State {
	case "MERGED":
		return !pr.MergedAt.Before(c.row.StartedAt)
	case "CLOSED":
		return !pr.ClosedAt.Before(c.row.StartedAt)
	}
	return false
}

// anyPR reports whether a PR that counts for the run satisfies f: an open one, or one that
// ended in the run.
func (c runCtx) anyPR(f func(forge.PR) bool) bool {
	for _, pr := range c.it.PRs {
		if (pr.State == "OPEN" || c.endedInRun(pr)) && f(pr) {
			return true
		}
	}
	return false
}

func (c runCtx) merged() (Action, bool) {
	return c.closed(runs.Done), c.anyPR(func(pr forge.PR) bool { return pr.State == "MERGED" })
}

// closedIssue closes a run whose session is Done on an issue that is closed: a close-check run
// that closed the issue leaves no Handoff, Question or PR of its own.
func (c runCtx) closedIssue() (Action, bool) {
	return c.closed(runs.Done), c.done() && c.it.State == "CLOSED"
}

func (c runCtx) green() (Action, bool) {
	pr, green := greenPR(c.it.PRs)
	if !green || !c.done() {
		return Action{}, false
	}
	a := c.base
	a.Kind, a.RunID, a.SessionID, a.PRURL, a.End = KindDone, c.row.ID, c.row.SessionID, pr.URL, true
	return a, true
}

func (c runCtx) gone() (Action, bool) {
	return c.returned("reason_workplace_gone", ""), c.state.Gone
}

// unknownCommand closes a run whose session failed to start on its command; the retry rule
// starts it again or hands the issue back with the cause.
func (c runCtx) unknownCommand() (Action, bool) {
	t := c.state.Transcript
	if t == nil || t.UnknownCommand == "" {
		return Action{}, false
	}
	a := c.closed(runs.StartFailed)
	a.ReasonArg, a.Stop = "unknown command `"+t.UnknownCommand+"`", !c.done()
	return a, true
}

// didNothing closes a run whose session is Done without one assistant turn.
func (c runCtx) didNothing() (Action, bool) {
	t := c.state.Transcript
	if !c.done() || t == nil || t.AssistantTurn {
		return Action{}, false
	}
	a := c.closed(runs.StartFailed)
	a.ReasonArg = "session did nothing"
	return a, true
}

// prClosed hands back a run whose PR closed unmerged. A run that also has an open PR replaced
// the closed one and works on.
func (c runCtx) prClosed() (Action, bool) {
	a := c.returned("reason_pr_closed", "")
	a.Stop = !c.done()
	closed := c.anyPR(func(pr forge.PR) bool { return pr.State == "CLOSED" })
	open := c.anyPR(func(pr forge.PR) bool { return pr.State == "OPEN" })
	return a, closed && !open
}

func (c runCtx) prDraft() (Action, bool) {
	a := c.returned("reason_pr_draft", "")
	a.Stop = !c.done()
	return a, c.anyPR(func(pr forge.PR) bool { return pr.State == "OPEN" && pr.Draft })
}

// prRed settles a Done run with an open, red PR. Only the run's own PR (ownPR) gets a fix-pr
// start: anyone can link a PR to a public issue with "Closes #N", and a fix-pr session reads
// the PR's text and logs. Any other red PR hands the issue back with "PR red". For the own PR,
// at the ceiling of maxFixPRRuns fix-pr runs it hands the issue back with the ceiling reason;
// on a head SHA a fix-pr run already had, with "PR red"; the ceiling wins when both hold. Else
// it closes the run as a handoff carrying the fix-pr key and the run's session, and returns
// the fix-pr start for decideIssue.
func (c runCtx) prRed() (Action, bool, *fixPRStart) {
	red := func(pr forge.PR) bool {
		return pr.State == "OPEN" && (pr.Checks == "FAILURE" || pr.Checks == "ERROR")
	}
	if !c.done() || !slices.ContainsFunc(c.it.PRs, red) {
		return Action{}, false, nil
	}
	i := slices.IndexFunc(c.it.PRs, func(pr forge.PR) bool { return red(pr) && c.ownPR(pr) })
	if i < 0 {
		return c.returned("reason_pr_red", ""), true, nil
	}
	pr := c.it.PRs[i]
	key := fixPRKey(pr.Number, pr.HeadSHA)
	switch {
	case fixPRRuns(c.history, c.it.Issue, pr.Number) >= maxFixPRRuns:
		return c.returned("reason_pr_red_ceiling", ""), true, nil
	case spentKey(c.history, key):
		return c.returned("reason_pr_red", ""), true, nil
	}
	a := c.closed(runs.Handoff)
	a.ReasonArg, a.SessionID = key, c.row.SessionID
	return a, true, &fixPRStart{pr: pr, key: key}
}

// ownPR reports whether pr is the run's own PR, the one a fix-pr run may work on: its head is
// the run's branch, a name safe to pass to git, not the default branch, in this repository and
// not in a fork.
func (c runCtx) ownPR(pr forge.PR) bool {
	return pr.HeadRef == c.row.Branch && validBranch(pr.HeadRef) && pr.HeadRef != c.trunk && !pr.CrossRepo
}

// fixPRKey is the trigger key of a fix-pr start: one per PR and head SHA, so a red state on
// the same SHA never starts twice.
func fixPRKey(pr int, sha string) string {
	return triggerFixPR + ":" + strconv.Itoa(pr) + ":" + sha
}

// fixPRRuns counts the runs of issue that ran /lighthouse-fix-pr on pr, an open one included,
// a failed start not: when the third fix-pr run closes, the count is already 3.
func fixPRRuns(history []runs.Row, issue, pr int) int {
	command := cmdFixPR + " " + strconv.Itoa(pr)
	n := 0
	for _, row := range history {
		if row.Issue == issue && row.Command == command && row.Outcome != runs.StartFailed {
			n++
		}
	}
	return n
}

// noHandoff hands back a session that is Done without a PR, Handoff or Question: the Question
// and a Handoff that does not end the chain closed the run before this rule.
func (c runCtx) noHandoff() (Action, bool) {
	anyPR := c.anyPR(func(forge.PR) bool { return true })
	return c.returned("reason_no_handoff", ""), c.done() && !c.handoff && !anyPR
}

// The time rules. Each is off while its limit is zero.

func (c runCtx) maxWait() time.Duration { return time.Duration(c.in.MaxWait) }

func (c runCtx) age() time.Duration { return c.in.Now.Sub(c.row.StartedAt) }

// uncertain forgets a launch for which no session was found within max_wait. Only a measured
// run counts: a run nothing could be measured of (a forge outage) may still be running.
func (c runCtx) uncertain() (Action, bool) {
	if c.maxWait() <= 0 || !c.measured || c.open.WorkplaceSession != "" || c.age() < c.maxWait() {
		return Action{}, false
	}
	a := c.returned("reason_start_uncertain", "")
	a.Discard = true
	return a, true
}

// permission hands back a run whose prompt has waited max_wait. The session keeps waiting.
func (c runCtx) permission() (Action, bool) {
	p := c.pendingPermission()
	if c.maxWait() <= 0 || p == nil || c.in.Now.Sub(p.At) < c.maxWait() {
		return Action{}, false
	}
	return c.returned("reason_permission", "`"+p.Tool+"` `"+p.Path+"`"), true
}

// pendingPermission is the run's permission prompt while it waits for an answer, else nil:
// only activity of the prompt's own thread that is newer than the prompt means it was answered.
// A session waiting on a prompt writes nothing, so while one is pending the silence rules hold
// off and the permission rule decides at max_wait. A note from before the run's start is one of
// an earlier run on the same session (a resume): it does not count.
func (c runCtx) pendingPermission() *Permission {
	p := c.state.Permission
	if p == nil || p.At.Before(c.row.StartedAt) || c.threadActivity(p.Thread).After(p.At) {
		return nil
	}
	return p
}

// threadActivity is the newest event of thread; the transcript's newest event when the
// thread has none, and the forge time of the session without a transcript.
func (c runCtx) threadActivity(thread string) time.Time {
	t := c.state.Transcript
	if t == nil {
		return c.state.SessionAt
	}
	if at, ok := t.Threads[thread]; ok {
		return at
	}
	return t.LastEvent
}

// silent stops a session whose transcripts, subagents included, had no event in max_wait, unless
// it waits on a permission prompt. Silence counts from the run's start at the earliest: a resume
// reuses the session, and its transcript, of an earlier run.
func (c runCtx) silent() (Action, bool) {
	t := c.state.Transcript
	if c.maxWait() <= 0 || c.done() || t == nil || c.pendingPermission() != nil {
		return Action{}, false
	}
	if c.in.Now.Sub(latest(t.LastEvent, c.row.StartedAt)) < c.maxWait() {
		return Action{}, false
	}
	a := c.returned("reason_silent", "")
	a.Stop = true
	return a, true
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// silentForge falls back on the forge state when the transcript is not measurable: a waiting
// session whose forge time is max_wait old. Without a forge time nothing says how long it waits.
// Like silent, it counts from the run's start at the earliest: a resume reuses the session, and
// its forge time, of an earlier run.
func (c runCtx) silentForge() (Action, bool) {
	if c.maxWait() <= 0 || c.state.Transcript != nil || !c.hasStand || c.stand != workplace.Waiting || c.pendingPermission() != nil {
		return Action{}, false
	}
	if c.state.SessionAt.IsZero() {
		return Action{}, false
	}
	if c.in.Now.Sub(latest(c.state.SessionAt, c.row.StartedAt)) < c.maxWait() {
		return Action{}, false
	}
	a := c.returned("reason_silent_forge", "")
	a.Stop = true
	return a, true
}

func (c runCtx) timeout() (Action, bool) {
	limit := time.Duration(c.in.MaxRuntime)
	if limit <= 0 || c.age() < limit {
		return Action{}, false
	}
	a := c.returned("reason_timeout", "")
	a.Stop = true
	return a, true
}

// lastOfSession returns the last row of the history with session, on any issue: the run
// state says to which issue a marker belongs.
func lastOfSession(history []runs.Row, session string) (runs.Row, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].SessionID == session {
			return history[i], true
		}
	}
	return runs.Row{}, false
}

// dropMarkers gives a drop_marker for every marker of repo that will never resume: its last
// run is closed and not resumable, or parked under a key a run already spent (usageResume never
// fires for it), or its issue left the snapshot or is closed. An open last run, or none, gives
// nothing; On hold keeps the marker for when the issue comes back.
func dropMarkers(repo RepoInput) []Action {
	var out []Action
	for _, m := range repo.Markers {
		last, ok := lastOfSession(repo.History, m.SessionID)
		if !ok || last.Open {
			continue
		}
		i := slices.IndexFunc(repo.Snapshot.Items, func(it Item) bool { return it.Issue == last.Issue })
		gone := i < 0 || repo.Snapshot.Items[i].State == "CLOSED"
		stuck := last.Outcome == runs.Parked && spentKey(repo.History, m.Key())
		if !gone && !stuck && runs.Resumable(last, m.Key()) {
			continue
		}
		out = append(out, Action{
			Kind: KindDropMarker, Repo: repo.Snapshot.Repo, Issue: last.Issue, SessionID: m.SessionID,
			TriggerKey: m.Key(), Language: repo.Language,
		})
	}
	return out
}

// endsChain reports whether a handoff names the end command for issue: the run then waits
// for green and the handoff does not close it. An unreadable handoff, or one for another
// issue, does not end the chain.
func endsChain(repo RepoInput, issue int, h protocol.Header) bool {
	ho, err := protocol.ParseHandoff(h)
	if err != nil || ho.Issue != issue {
		return false
	}
	j, _ := judgeHandoff(repo, issue, ho)
	return j.Verdict == VerdictEnd
}

// judgeHandoff skips a review the config turned off, then judges the next command of ho.
// The second result is the skip key, "" when nothing is skipped.
func judgeHandoff(repo RepoInput, issue int, ho protocol.Handoff) (Judgement, string) {
	command, skip := ReviewSkip(ho.Next, repo.Controller)
	return Judge(command, issue, ho.Lane, repo.Installed), skip
}

// greenPR returns the first PR that is open, not a draft, mergeable and with green checks.
func greenPR(prs []forge.PR) (forge.PR, bool) {
	for _, pr := range prs {
		if pr.State == "OPEN" && !pr.Draft && pr.Mergeable == "MERGEABLE" && pr.Checks == "SUCCESS" {
			return pr, true
		}
	}
	return forge.PR{}, false
}

// repair puts back the 🚀 on the comment of a spent handoff or answer trigger: one rocket
// per comment that is in the snapshot without one.
func repair(history []runs.Row, it Item, base Action) []Action {
	rocket := make(map[int64]bool, len(it.Comments))
	for _, c := range it.Comments {
		rocket[c.ID] = c.Rocket
	}
	var out []Action
	emitted := map[int64]bool{}
	for _, row := range history {
		id, ok := triggerComment(row.TriggerKey)
		if !ok || emitted[id] {
			continue
		}
		if has, present := rocket[id]; !present || has {
			continue
		}
		emitted[id] = true
		a := base
		a.Kind, a.CommentID = KindRocket, id
		out = append(out, a)
	}
	return out
}

// triggerComment returns the comment id of a handoff:<id> or answer:<id> trigger key.
func triggerComment(key string) (int64, bool) {
	for _, prefix := range []string{"handoff:", "answer:"} {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			id, err := strconv.ParseInt(rest, 10, 64)
			return id, err == nil
		}
	}
	return 0, false
}

const (
	statusController = "Controller"
	statusOnHold     = "On hold"
	cmdStartIssue    = "/lighthouse-start-issue"
	modeStart        = "start"
	modeResume       = "resume"
	triggerHandoff   = "handoff"
	triggerAnswer    = "answer"
	triggerClose     = "closecheck"
	triggerFixPR     = "fixpr"
	cmdFixPR         = "/lighthouse-fix-pr"
	maxFixPRRuns     = 3
)

// issueCtx is what the start rules of one item read.
type issueCtx struct {
	repo  RepoInput
	it    Item
	hs    []parsed
	base  Action
	now   time.Time
	night string
}

// next returns the one start, resume, return, defer, park or hold the item gets in this tick:
// the first rule that gives an action wins.
func (c issueCtx) next() (planned, bool) {
	for _, rule := range []func() (planned, bool){c.retry, c.usageResume, c.answer, c.closeCheck, c.newIssue, c.handoff} {
		if p, ok := rule(); ok {
			return p, true
		}
	}
	return planned{}, false
}

// retry is the second attempt of a trigger key of the issue whose runs all failed to start,
// whatever 🚀 or Started the issue shows. After one failure the trigger starts again from
// its row; after two the issue goes back, once per session of the second row.
func (c issueCtx) retry() (planned, bool) {
	var keys []string
	byKey := map[string][]runs.Row{}
	for _, row := range c.repo.History {
		if row.Issue != c.it.Issue {
			continue
		}
		if _, seen := byKey[row.TriggerKey]; !seen {
			keys = append(keys, row.TriggerKey)
		}
		byKey[row.TriggerKey] = append(byKey[row.TriggerKey], row)
	}
	for _, key := range keys {
		rows := byKey[key]
		if !allStartFailed(rows) {
			continue
		}
		if len(rows) == 1 {
			return c.secondAttempt(rows[0]), true
		}
		if a, ok := c.startFailed(rows[1]); ok {
			return planned{Action: a}, true
		}
	}
	return planned{}, false
}

func allStartFailed(rows []runs.Row) bool {
	for _, row := range rows {
		if row.Outcome != runs.StartFailed {
			return false
		}
	}
	return true
}

// secondAttempt starts row's trigger again with what the row keeps. The comment id comes
// from the trigger key. A handoff trigger is native when its command is the interactive one
// (Judge starts that only on the native lane), even when the comment is gone; the skip key
// comes from judging the comment again.
func (c issueCtx) secondAttempt(row runs.Row) planned {
	a := c.base
	a.Kind, a.Mode = KindStart, row.Mode
	if row.Mode == modeResume {
		a.Kind, a.SessionID = KindResume, row.SessionID
	}
	a.Trigger, a.TriggerKey, a.Attempt = row.Trigger, row.TriggerKey, 2
	a.Command, a.Prompt, a.Branch = row.Command, row.Prompt, row.Branch
	handoff := strings.HasPrefix(row.TriggerKey, triggerHandoff+":")
	if handoff {
		fields := strings.Fields(row.Command)
		a.Native = len(fields) > 0 && fields[0] == cmdInteractive
	}
	at := c.it.ItemUpdatedAt
	if id, ok := triggerComment(row.TriggerKey); ok {
		a.CommentID = id
		if com, found := c.comment(id); found {
			at = parseTime(com.CreatedAt)
			if handoff {
				a.Skip = c.handoffSkip(com)
			}
		}
	}
	return c.planned(a, at)
}

// handoffSkip judges the handoff in com again for its skip key, "" when it does not read.
func (c issueCtx) handoffSkip(com Comment) string {
	h, _ := protocol.ParseHeader(com.Body)
	ho, err := protocol.ParseHandoff(h)
	if err != nil {
		return ""
	}
	_, skip := judgeHandoff(c.repo, c.it.Issue, ho)
	return skip
}

// startFailed hands the issue back after the second failed start, unless a Returned of that
// run's session already stands.
func (c issueCtx) startFailed(row runs.Row) (Action, bool) {
	for _, p := range c.hs {
		if p.Header.Kind == protocol.KindReturned && p.Header.Session == row.SessionID {
			return Action{}, false
		}
	}
	a := c.base
	a.Kind, a.Reason, a.ReasonArg, a.SessionID = KindReturn, "reason_start_failed", row.Reason, row.SessionID
	return a, true
}

// usageResume resumes a run the usage guard parked, once per parking: a marker whose
// session's last run is on this issue and closed as parked, whose key no run has, and whose
// reset has passed. A pause marker does not wait for its reset: while the pause holds, the
// brake drops the resume. The second attempt after a failed start is retry's.
func (c issueCtx) usageResume() (planned, bool) {
	for _, m := range c.repo.Markers {
		run, ok := lastOfSession(c.repo.History, m.SessionID)
		// Resumable also holds for a first resume that failed to start; that one is retry's,
		// so only a run closed as parked resumes here.
		if !ok || run.Issue != c.it.Issue || !runs.Resumable(run, m.Key()) || run.Outcome != runs.Parked ||
			c.spent(m.Key()) {
			continue
		}
		at, _ := m.Reset()
		if m.Pause {
			at = c.now
		} else if at.After(c.now) {
			continue
		}
		a := c.base
		a.Kind, a.Mode, a.Trigger, a.TriggerKey, a.Attempt = KindResume, modeResume, triggerUsage, m.Key(), 1
		a.SessionID, a.Command, a.Branch = m.SessionID, run.Command, run.Branch
		a.Prompt = protocol.Heading("prompt_usage_resume", c.repo.Language)
		return c.planned(a, at), true
	}
	return planned{}, false
}

// answer resumes the session of the last turn when that is a Question with a session and an
// answer follows it. A session no run of this issue has resumes nothing: a Question carrying
// another issue's session never resumes that run.
func (c issueCtx) answer() (planned, bool) {
	q := lastTurn(c.hs)
	if q == nil || q.Header.Kind != protocol.KindQuestion || q.Header.Session == "" {
		return planned{}, false
	}
	ans, ok := c.answerTo(q.Comment.ID)
	if !ok {
		return planned{}, false
	}
	key := triggerAnswer + ":" + strconv.FormatInt(ans.ID, 10)
	if c.spent(key) {
		return planned{}, false
	}
	run, ok := lastRunOf(c.repo.History, c.it.Issue, q.Header.Session)
	if !ok {
		return planned{}, false
	}
	a := c.base
	a.Kind, a.Mode, a.Trigger, a.TriggerKey, a.Attempt = KindResume, modeResume, triggerAnswer, key, 1
	a.SessionID, a.CommentID = q.Header.Session, ans.ID
	a.Command, a.Branch = run.Command, run.Branch
	a.Prompt = protocol.AnswerPrompt(q.Comment.Body, ans.Body)
	return c.planned(a, parseTime(ans.CreatedAt)), true
}

// answerTo returns the first comment after the one with id that can answer it: no 🚀, no
// protocol header, no artefact (protocol.IsAnswer).
func (c issueCtx) answerTo(id int64) (Comment, bool) {
	after := false
	for _, com := range c.it.Comments {
		if after && !com.Rocket && protocol.IsAnswer(com.Body) {
			return com, true
		}
		after = after || com.ID == id
	}
	return Comment{}, false
}

// lastRunOf returns the last row of the history on issue with session.
func lastRunOf(history []runs.Row, issue int, session string) (runs.Row, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Issue == issue && history[i].SessionID == session {
			return history[i], true
		}
	}
	return runs.Row{}, false
}

// closeCheck starts the end command after a PR merged that does not close the issue, once
// per PR, when the issue was under the controller (a Started) before the merge.
func (c issueCtx) closeCheck() (planned, bool) {
	for _, pr := range c.it.PRs {
		if pr.Closes || pr.State != "MERGED" || !c.startedBefore(pr.MergedAt) {
			continue
		}
		key := triggerClose + ":" + strconv.Itoa(pr.Number)
		if c.spent(key) {
			continue
		}
		a := c.fresh(triggerClose, key, cmdClose+" "+strconv.Itoa(c.it.Issue), pr.HeadRef)
		return c.planned(a, c.it.ItemUpdatedAt), true
	}
	return planned{}, false
}

func (c issueCtx) startedBefore(t time.Time) bool {
	for _, p := range c.hs {
		if p.Header.Kind == protocol.KindStarted && p.Time.Before(t) {
			return true
		}
	}
	return false
}

// newIssue starts an issue without any protocol comment on its own branch.
func (c issueCtx) newIssue() (planned, bool) {
	key := "start:" + strconv.Itoa(c.it.Issue)
	if len(c.hs) > 0 || c.spent(key) {
		return planned{}, false
	}
	a := c.fresh(triggerHandoff, key, cmdStartIssue+" "+strconv.Itoa(c.it.Issue), c.issueBranch())
	return c.planned(a, c.it.ItemUpdatedAt), true
}

// handoff acts on the last turn when that is a Handoff without 🚀 whose trigger has no run:
// it goes back when it does not read, names another issue, is not for a headless run or
// names a branch git must not see; waits for the night; parks an executing command with an
// active blocker, or only holds it when its own Parked already stands; else starts.
func (c issueCtx) handoff() (planned, bool) {
	p := lastTurn(c.hs)
	if p == nil || p.Header.Kind != protocol.KindHandoff || p.Comment.Rocket {
		return planned{}, false
	}
	key := triggerHandoff + ":" + strconv.FormatInt(p.Comment.ID, 10)
	if c.spent(key) {
		return planned{}, false
	}
	ho, err := protocol.ParseHandoff(p.Header)
	if err != nil {
		return c.returned("reason_bad_handoff", strings.TrimPrefix(err.Error(), protocol.ErrHandoff.Error()+": ")), true
	}
	if ho.Issue != c.it.Issue {
		return c.returned("reason_not_allowed", ho.Next), true
	}
	j, skip := judgeHandoff(c.repo, c.it.Issue, ho)
	switch j.Verdict {
	case VerdictEnd:
		return planned{}, false
	case VerdictBrainstorm:
		return c.returned("reason_needs_brainstorm", ""), true
	case VerdictInteractive:
		return c.returned("reason_interactive", ""), true
	case VerdictRejected:
		return c.returned("reason_not_allowed", j.Arg), true
	case VerdictSkillMissing:
		return c.returned("reason_skill_missing", j.Arg), true
	}
	if !validBranch(ho.Branch) {
		return c.returned("reason_bad_handoff", "branch "+ho.Branch), true
	}
	if ho.When == "night" && !InNight(c.night, c.now) {
		return c.deferred("night"), true
	}
	if Executing(j.Command) && c.active() {
		if c.ownParked() {
			return planned{Action: c.board(KindHold)}, true
		}
		a := c.board(KindPark)
		a.Blockers = c.activeBlockers()
		return planned{Action: a}, true
	}
	branch := ho.Branch
	if branch == c.repo.DefaultBranch {
		branch = c.issueBranch()
	}
	a := c.fresh(triggerHandoff, key, j.Command, branch)
	a.CommentID, a.Native, a.Skip = p.Comment.ID, j.Native, skip
	return c.planned(a, p.Time), true
}

// branchName is the shape of a handoff branch: the branch reaches git as an argument.
var branchName = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// validBranch reports whether branch is safe to pass to git: a plain ref name that does not
// start with - (an option) and holds no .. (a range).
func validBranch(branch string) bool {
	return branchName.MatchString(branch) && !strings.HasPrefix(branch, "-") && !strings.Contains(branch, "..")
}

// active reports whether a blocker of the item is still active: open and without a merged
// PR. A blocker closed without a merge has lapsed.
func (c issueCtx) active() bool {
	return len(c.activeBlockers()) > 0
}

// activeBlockers names the active blockers in BlockedBy order, as "<repo>#<n> (<status>)",
// or "<repo>#<n>" for a blocker that is not on the board.
func (c issueCtx) activeBlockers() []string {
	var out []string
	for _, b := range c.it.BlockedBy {
		if b.State != "OPEN" || b.MergedPR {
			continue
		}
		name := b.Repo + "#" + strconv.Itoa(b.Issue)
		if b.Status != "" {
			name += " (" + b.Status + ")"
		}
		out = append(out, name)
	}
	return out
}

// ownParked reports whether the last protocol comment (Rebased does not count) is a Parked
// by the token login: the tick parked the item itself.
func (c issueCtx) ownParked() bool {
	for i := len(c.hs) - 1; i >= 0; i-- {
		p := c.hs[i]
		if p.Header.Kind == protocol.KindRebased {
			continue
		}
		return p.Header.Kind == protocol.KindParked && p.Comment.Author == c.repo.Snapshot.TokenLogin
	}
	return false
}

// lastTurn returns the last protocol comment that is a turn: a Rebased or a Parked is a
// status note and does not hide a Handoff or Question.
func lastTurn(hs []parsed) *parsed {
	for i := len(hs) - 1; i >= 0; i-- {
		if k := hs[i].Header.Kind; k != protocol.KindRebased && k != protocol.KindParked {
			return &hs[i]
		}
	}
	return nil
}

// spent reports whether a run of the repo already has this trigger key.
func (c issueCtx) spent(key string) bool { return spentKey(c.repo.History, key) }

func spentKey(history []runs.Row, key string) bool {
	return slices.ContainsFunc(history, func(row runs.Row) bool { return row.TriggerKey == key })
}

func (c issueCtx) comment(id int64) (Comment, bool) {
	for _, com := range c.it.Comments {
		if com.ID == id {
			return com, true
		}
	}
	return Comment{}, false
}

// fresh is a first start of command on branch, with the controller-run prompt.
func (c issueCtx) fresh(trigger, key, command, branch string) Action {
	a := c.base
	a.Kind, a.Mode, a.Trigger, a.TriggerKey, a.Attempt = KindStart, modeStart, trigger, key, 1
	a.Command, a.Branch = command, branch
	a.Prompt = command + " (issue #" + strconv.Itoa(c.it.Issue) + ", controller-run)"
	return a
}

// issueBranch is issue-<N>-<slug of the title>, or issue-<N> when the title gives no slug.
func (c issueCtx) issueBranch() string {
	branch := "issue-" + strconv.Itoa(c.it.Issue)
	if slug := Slug(c.it.Title); slug != "" {
		branch += "-" + slug
	}
	return branch
}

func (c issueCtx) returned(reason, arg string) planned {
	a := c.base
	a.Kind, a.Reason, a.ReasonArg = KindReturn, reason, arg
	return planned{Action: a}
}

// board is a park, hold or unpark of the item's board item.
func (c issueCtx) board(kind Kind) Action {
	a := c.base
	a.Kind, a.ItemID = kind, c.it.ItemID
	return a
}

func (c issueCtx) deferred(reason string) planned {
	a := c.base
	a.Kind, a.Reason = KindDefer, reason
	return planned{Action: a}
}

// planned orders a start or resume by the item's priority and the time of its trigger.
func (c issueCtx) planned(a Action, at time.Time) planned {
	return planned{
		Action:   a,
		priority: priorityRank(c.it.Priority),
		at:       at,
		software: c.repo.Controller.Profile == ProfileSoftware,
		parked:   a.Trigger == triggerUsage,
	}
}

// priorityRank orders P0, P1, P2, then no or an unknown priority.
func priorityRank(priority string) int {
	switch priority {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	}
	return 3
}
