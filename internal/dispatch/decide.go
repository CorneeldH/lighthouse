package dispatch

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
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
}

// RepoInput is what Decide knows about one repo.
type RepoInput struct {
	Snapshot      Snapshot                  `json:"snapshot"`
	Controller    repoconfig.Controller     `json:"controller"`
	Language      string                    `json:"language"`
	DefaultBranch string                    `json:"default_branch"`
	Installed     map[string]bool           `json:"installed"`
	History       []runs.Row                `json:"history"`
	Stands        map[int64]workplace.Stand `json:"stands"` // per open run id; missing = not measurable
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
)

// Action is one thing apply does. Reason and Skip are heading keys, never posted text; only a
// KindDefer carries a literal Reason (night or blocker), which apply reports without a lookup.
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
}

// Decide is the pure decision of one tick: per repo, per item, first the orphan rule, then
// the settlement of the item's open runs, then the 🚀 repair, then at most one start or
// resume (next). The result is ordered: the actions without a start or resume by repo and
// issue, then the defers, then the starts and resumes by priority, trigger time, repo and
// issue.
func Decide(in Input) []Action {
	var ps []planned
	for _, repo := range in.Repos {
		for _, it := range repo.Snapshot.Items {
			ps = append(ps, decideIssue(in, repo, it)...)
		}
	}
	slices.SortStableFunc(ps, comparePlanned)
	out := make([]Action, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Action)
	}
	return out
}

// planned is an action with what orders it; priority and at count for a start or resume.
type planned struct {
	Action
	priority int
	at       time.Time
}

// group places an action in the output: 0 without a start or resume, 1 a defer, 2 a start
// or resume.
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
// issue back: a start next to that Returned would contradict it.
func decideIssue(in Input, repo RepoInput, it Item) []planned {
	hs := headers(it)
	base := Action{Repo: repo.Snapshot.Repo, Issue: it.Issue, Language: repo.Language}
	var out []planned
	a, orphaned := orphan(repo.History, hs, base)
	if orphaned {
		out = append(out, planned{Action: a})
	}
	free := !orphaned
	for _, row := range repo.History {
		if !row.Open || row.Issue != it.Issue {
			continue
		}
		a, closes := settle(repo, it, row, hs, base)
		if closes {
			out = append(out, planned{Action: a})
		}
		free = free && closes
	}
	for _, a := range repair(repo.History, it, base) {
		out = append(out, planned{Action: a})
	}
	if free && it.Status == statusController && it.State == "OPEN" {
		c := issueCtx{repo: repo, it: it, hs: hs, base: base, now: in.Now, night: in.Night}
		if p, ok := c.next(); ok {
			out = append(out, p)
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

// settle decides whether the open run row ends in this tick. Only comments from row's start
// on count; a Done or Question also needs row's session, a Handoff carries none. A Done
// closes, then a Handoff that does not end the chain, then a Question; else a green PR with
// a session that is Done gives the done action. Every action it returns closes row.
func settle(repo RepoInput, it Item, row runs.Row, hs []parsed, base Action) (Action, bool) {
	var done, question bool
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
		case protocol.KindHandoff:
			handoff = &hs[i]
		}
	}
	a := base
	a.RunID = row.ID
	switch {
	case done:
		a.Kind, a.Outcome = KindClose, runs.Done
		return a, true
	case handoff != nil && !endsChain(repo, it.Issue, handoff.Header):
		a.Kind, a.Outcome = KindClose, runs.Handoff
		return a, true
	case question:
		a.Kind, a.Outcome = KindClose, runs.Question
		return a, true
	}
	pr, green := greenPR(it.PRs)
	if stand, measured := repo.Stands[row.ID]; green && measured && stand == workplace.Done {
		a.Kind, a.SessionID, a.PRURL = KindDone, row.SessionID, pr.URL
		return a, true
	}
	return Action{}, false
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
	cmdStartIssue    = "/lighthouse-start-issue"
	modeStart        = "start"
	modeResume       = "resume"
	triggerHandoff   = "handoff"
	triggerAnswer    = "answer"
	triggerClose     = "closecheck"
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

// next returns the one start, resume, return or defer the item gets in this tick: the first
// rule that gives an action wins.
func (c issueCtx) next() (planned, bool) {
	for _, rule := range []func() (planned, bool){c.retry, c.answer, c.closeCheck, c.newIssue, c.handoff} {
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
// from the trigger key; a handoff trigger is judged again for Native and Skip.
func (c issueCtx) secondAttempt(row runs.Row) planned {
	a := c.base
	a.Kind, a.Mode = KindStart, row.Mode
	if row.Mode == modeResume {
		a.Kind, a.SessionID = KindResume, row.SessionID
	}
	a.Trigger, a.TriggerKey, a.Attempt = row.Trigger, row.TriggerKey, 2
	a.Command, a.Prompt, a.Branch = row.Command, row.Prompt, row.Branch
	at := c.it.ItemUpdatedAt
	if id, ok := triggerComment(row.TriggerKey); ok {
		a.CommentID = id
		if com, found := c.comment(id); found {
			at = parseTime(com.CreatedAt)
			if strings.HasPrefix(row.TriggerKey, triggerHandoff+":") {
				a.Native, a.Skip = c.handoffFlags(com)
			}
		}
	}
	return c.planned(a, at)
}

// handoffFlags judges the handoff in com again: Native and the skip key, or neither when it
// does not read.
func (c issueCtx) handoffFlags(com Comment) (bool, string) {
	h, _ := protocol.ParseHeader(com.Body)
	ho, err := protocol.ParseHandoff(h)
	if err != nil {
		return false, ""
	}
	j, skip := judgeHandoff(c.repo, c.it.Issue, ho)
	return j.Native, skip
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
// names a branch git must not see; waits for the night or an open blocker; else starts.
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
	if Executing(j.Command) && c.blocked() {
		return c.deferred("blocker"), true
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

func (c issueCtx) blocked() bool {
	for _, b := range c.it.BlockedBy {
		if b.State == "OPEN" {
			return true
		}
	}
	return false
}

// lastTurn returns the last protocol comment that is a turn: a Rebased is a status note and
// does not hide a Handoff or Question.
func lastTurn(hs []parsed) *parsed {
	for i := len(hs) - 1; i >= 0; i-- {
		if hs[i].Header.Kind != protocol.KindRebased {
			return &hs[i]
		}
	}
	return nil
}

// spent reports whether a run of the repo already has this trigger key.
func (c issueCtx) spent(key string) bool {
	for _, row := range c.repo.History {
		if row.TriggerKey == key {
			return true
		}
	}
	return false
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

func (c issueCtx) deferred(reason string) planned {
	a := c.base
	a.Kind, a.Reason = KindDefer, reason
	return planned{Action: a}
}

// planned orders a start or resume by the item's priority and the time of its trigger.
func (c issueCtx) planned(a Action, at time.Time) planned {
	return planned{Action: a, priority: priorityRank(c.it.Priority), at: at}
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
