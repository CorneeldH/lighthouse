package dispatch

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/experiment"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/issuestate"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Env is what Apply needs from the outside, per repo where it differs.
type Env struct {
	DB *sql.DB
	// Forge is the forge of a repo: its comments, reactions, merges and board.
	Forge func(repo string) forge.Forge
	// Workplace is the workplace of a repo on a node ("" is self) for a profile.
	Workplace func(repo, node, profile string) workplace.Workplace
	MCPConfig func(profile string) string // the mcp_config of a profile; "" is none
	// ScheduledJob is the job of schedule in repo with the scheduled.Env it runs in (Dir
	// empty); a scheduled start needs it. Its Run runs git in the workplace.
	ScheduledJob func(repo, schedule string) (scheduled.Job, scheduled.Env, error)
	// Remote is where the branch of a scheduled run of repo goes: the forge by an explicit,
	// authenticated URL, never the workplace's origin. Finishing a scheduled run needs it.
	Remote     func(repo string) (scheduled.Remote, error)
	RoundLimit func(repo string) string // what a run does at an adversarial round limit
	// Labels are the [labels] names of a repo: a 🤖 Returned sets one of them (giveBack).
	Labels func(repo string) repoconfig.Labels
	// Experiments are the [[experiment]] declarations of every served repo; a start proposes an
	// assignment from them. nil is none.
	Experiments []experiment.Declared
	// Hash is the draw of a trajectory in [0,1); nil is experiment.Hash. A test fixes it.
	Hash      func(repo string, issue int) float64
	StateDir  string
	MarkerDir string // the resume markers; "" removes none
	// RemoveMarker removes the resume marker of session while its key is key, where the marker
	// came from (self or a node); nil is park.RemoveMarker in MarkerDir.
	RemoveMarker func(session, key string) error
	MaxWait      time.Duration
	Now          func() time.Time
	NewSession   func() string // runs.NewSessionID
	Log          io.Writer     // nil discards
	// Getenv is tick's environment, and TokenEnv the name of the forge token of a repo in it:
	// the pusher gets PATH, HOME, XDG_RUNTIME_DIR and that one token, nothing else.
	Getenv   func(string) string
	TokenEnv func(repo string) string
	// TokenEnvs are the token_env names of every repo the process knows, not only the run's own:
	// the agent of a scheduled run gets all of them unset. nil is none.
	TokenEnvs []string
	// StartPusher starts the workflow pusher detached and returns at once; it does not wait for it.
	StartPusher func(ctx context.Context, spec PusherSpec) error
	// LsRemote is the SHA of refs/heads/<branch> on origin as git sees it in worktree, "" when
	// origin has no such branch; env is the pusher's environment. nil runs git ls-remote.
	LsRemote func(ctx context.Context, worktree, branch string, env []string) (sha string, err error)
	// Fetch fetches sha from origin into worktree without touching a ref or FETCH_HEAD; env is the
	// pusher's environment. nil runs git fetch.
	Fetch func(ctx context.Context, worktree, sha string, env []string) error
	// IsAncestor says whether sha is an ancestor of tip in worktree; env is the pusher's
	// environment. nil runs git merge-base --is-ancestor.
	IsAncestor func(ctx context.Context, worktree, sha, tip string, env []string) (bool, error)
	// ClassifyAnswer reads a user's answer to a pusher question in plain language; nil is no classifier.
	ClassifyAnswer func(ctx context.Context, in AnswerInput) (AnswerVerdict, error)
}

// AnswerInput is what the classifier reads: the pusher's step, the SHA and branch the question
// is about, and the user's answer.
type AnswerInput struct{ Step, SHA, Branch, Answer string }

// AnswerVerdict is what an answer means: "permit", "pushed" or "other".
type AnswerVerdict string

const (
	VerdictPermit AnswerVerdict = "permit" // the user allows the push of the SHA
	VerdictPushed AnswerVerdict = "pushed" // the user says they pushed it themselves
	VerdictOther  AnswerVerdict = "other"  // anything else, in doubt too
)

// PusherSpec is what StartPusher starts: the push row RunID of issue Issue in Repo, for the
// run's Worktree. Permit is the SHA a user's answer allowed, "" for a handoff. Env is the whole
// environment of the pusher process.
type PusherSpec struct {
	Repo     string
	Issue    int
	Worktree string
	RunID    int64
	Permit   string
	Env      []string
	// Merge starts the merge pusher of PR on HeadSHA instead of a push; it has no Worktree.
	Merge   bool
	PR      int
	HeadSHA string
}

// postpone records a deferral of a, for the log line and for the Queue.
func (r *Result) postpone(a Action, reason string) {
	r.Deferred = append(r.Deferred, deferral(a, reason))
	r.DeferredActions = append(r.DeferredActions, Action{Kind: KindDefer, Repo: a.Repo, Issue: a.Issue,
		Schedule: a.Schedule, Slot: a.Slot, Reason: reason})
}

// Result is what one Apply did.
type Result struct {
	Deferred []string // "<repo>#<n>: <reason>"
	// DeferredActions is every Deferred entry as a KindDefer action (Repo, Issue, Schedule,
	// Reason), so the tick can show a deferral that only apply found, such as a busy workplace.
	DeferredActions []Action
	Failed          map[string]error // per repo: the joined errors of its actions; absent = no error
	Started         int              // launches that were accepted or are uncertain
	// NotAccepted is "<repo>#<n>" per launch the workplace refused; its run is closed as
	// start_failed, its Started stays, and a Returned with the reason follows (a usage resume:
	// only after its second attempt).
	NotAccepted []string
}

// runnerForge is the runner field of a 🤖 Started that apply posts.
const runnerForge = "forge"

// Apply runs every action in order. One failing action never stops the rest: the errors are
// joined, and kept per repo in Result.Failed.
func Apply(ctx context.Context, env Env, actions []Action) (Result, error) {
	ap := applier{env: env, log: env.Log}
	if ap.log == nil {
		ap.log = io.Discard
	}
	res := Result{Failed: map[string]error{}}
	var errs []error
	for _, a := range actions {
		err := ap.apply(ctx, a, &res)
		if err == nil {
			continue
		}
		err = fmt.Errorf("%s %s: %w", subject(a), a.Kind, err)
		res.Failed[a.Repo] = errors.Join(res.Failed[a.Repo], err)
		errs = append(errs, err)
	}
	return res, errors.Join(errs...)
}

type applier struct {
	env Env
	log io.Writer
}

func (ap applier) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(ap.log, format+"\n", args...)
}

// oneLine flattens s for a log line or a comment field.
func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ")
}

func (ap applier) apply(ctx context.Context, a Action, res *Result) error {
	switch a.Kind {
	case KindStart, KindResume:
		return ap.start(ctx, a, res)
	case KindScheduledStart:
		return ap.startScheduled(ctx, a, res)
	case KindScheduledFinish:
		return ap.finishScheduled(ctx, a)
	case KindScheduledMerge:
		return ap.mergeScheduled(ctx, a, res)
	case KindScheduledClose:
		return ap.closeScheduled(ctx, a)
	case KindReturn:
		// The Returned first, then the label and To do, then the run: a failed give-back leaves
		// the run open, settle closes it on its own Returned, and finishReturn finishes the rest.
		body := protocol.RenderReturned(reasonText(a), a.SessionID, "", a.Language)
		if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		if err := ap.giveBack(ctx, a, ReturnLabelKey(a.Reason, a.Blocked)); err != nil {
			return err
		}
		if a.RunID == 0 {
			return nil
		}
		if err := ap.release(ctx, a); err != nil {
			return err
		}
		return ap.closeRun(ctx, a.RunID, runs.Returned, reasonText(a))
	case KindFinishReturn:
		return ap.giveBack(ctx, a, ReturnLabelKey(a.Reason, a.Blocked))
	case KindDone:
		note := protocol.Heading("note_done", a.Language)
		body := protocol.RenderDone(a.SessionID, a.PRURL, note, a.Language)
		if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		if err := ap.release(ctx, a); err != nil {
			return err
		}
		return ap.closeRun(ctx, a.RunID, runs.Done, "")
	case KindRepost:
		// A stranded handoff or question: post the body again and leave the run open. A failed
		// post is a deferral, so the next tick gathers and reposts; it is no action error.
		if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, a.Body); err != nil {
			res.postpone(a, fmt.Sprintf("repost run %d: %s", a.RunID, oneLine(err.Error())))
		}
		return nil
	case KindPush:
		return ap.push(ctx, a, res)
	case KindPushRecover:
		return ap.pushRecover(ctx, a, res)
	case KindMerge:
		return ap.merge(ctx, a, res)
	case KindClose:
		if err := ap.release(ctx, a); err != nil {
			return err
		}
		if err := ap.closeRun(ctx, a.RunID, a.Outcome, closeReason(a)); err != nil {
			return err
		}
		ap.recordQuestion(ctx, a)
		return nil
	case KindNoteDone:
		ap.noteDone(ctx, a)
		return nil
	case KindRocket:
		return ap.env.Forge(a.Repo).Acknowledge(ctx, a.Issue, a.CommentID)
	case KindDefer:
		res.postpone(a, a.Reason)
		return nil
	case KindPark:
		// The comment first: a failed label or board change leaves a Parked that the next tick
		// sees, and holds without posting a second one.
		body := protocol.RenderParked(a.Blockers, a.Language)
		if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		return ap.hold(ctx, a)
	case KindHold:
		return ap.hold(ctx, a)
	case KindUnpark:
		return ap.unpark(ctx, a)
	case KindToDo:
		return ap.setStatus(ctx, a, issuestate.ToDo)
	case KindDropMarker:
		ap.removeMarker(a)
		return nil
	}
	return fmt.Errorf("unknown action kind %q", a.Kind)
}

// merge squash-merges PR a.PR on head a.HeadSHA, posts the 🤖 Done and ends the run as done.
// A head that moved since the measurement is deferred, so the next tick gathers and measures
// again. A merge GitHub refuses is noted on the open run, so decide plans a fix-pr for that
// head instead of merging again; the run stays open. Any other failure is an action error.
func (ap applier) merge(ctx context.Context, a Action, res *Result) error {
	err := ap.env.Forge(a.Repo).MergePR(ctx, a.PR, a.HeadSHA)
	var refused *forge.MergeRefused
	switch {
	case errors.Is(err, forge.ErrHeadMoved):
		res.postpone(a, fmt.Sprintf("merge of PR #%d skipped: head moved", a.PR))
		return nil
	case errors.As(err, &refused):
		if err := runs.NoteMergeFailed(ctx, ap.env.DB, a.RunID, a.HeadSHA, refused.Message); err != nil {
			return err
		}
		ap.logf("%s#%d: merge of PR #%d refused: %s", a.Repo, a.Issue, a.PR, oneLine(refused.Message))
		return nil
	case err != nil:
		return fmt.Errorf("merge: %w", err)
	}
	note := protocol.Heading("note_merged", a.Language)
	body := protocol.RenderDone(a.SessionID, a.PRURL, note, a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return fmt.Errorf("post: %w", err)
	}
	if err := ap.release(ctx, a); err != nil {
		return err
	}
	return ap.closeRun(ctx, a.RunID, runs.Done, "")
}

// setStatus moves the board item of a to status.
func (ap applier) setStatus(ctx context.Context, a Action, status string) error {
	item := forge.BoardItem{ItemID: a.ItemID, Repo: a.Repo, Issue: a.Issue}
	if err := ap.env.Forge(a.Repo).SetStatus(ctx, item, status); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

// hold parks issue a.Issue after its 🤖 Parked: the blocked_issue label, then the board status
// To do. The run and its workplace stay; the tick unparks the issue once no blocker is active.
func (ap applier) hold(ctx context.Context, a Action) error {
	if _, err := issuestate.Apply(ctx, ap.env.Forge(a.Repo), a.Issue, "blocked_issue", ap.env.Labels(a.Repo)); err != nil {
		return fmt.Errorf("label: %w", err)
	}
	return ap.setStatus(ctx, a, issuestate.ToDo)
}

// unpark takes the blocked_issue label off issue a.Issue when it is there (the unblock lane may
// have taken it off already), then moves the item to Controller, where the next tick resumes its
// last Handoff. The label goes first: a failed board change leaves a To do item without the
// label, which the tick unparks again.
func (ap applier) unpark(ctx context.Context, a Action) error {
	f := ap.env.Forge(a.Repo)
	if name := ap.env.Labels(a.Repo).BlockedIssue; name != "" {
		is, err := f.Issue(ctx, a.Issue)
		if err != nil {
			return fmt.Errorf("label: %w", err)
		}
		if slices.Contains(is.Labels, name) {
			if _, err := f.EditIssue(ctx, a.Issue, forge.IssueEdit{RemoveLabels: []string{name}}); err != nil {
				return fmt.Errorf("label: %w", err)
			}
		}
	}
	return ap.setStatus(ctx, a, statusController)
}

// giveBack gives issue a.Issue back to a human after its 🤖 Returned: the blocking label of key
// and the board status To do, with the Priority as it is (issuestate.Return). A closed issue
// gets neither: the forge already moved it to Done.
func (ap applier) giveBack(ctx context.Context, a Action, key string) error {
	f := ap.env.Forge(a.Repo)
	is, err := f.Issue(ctx, a.Issue)
	if err != nil {
		return fmt.Errorf("give back: %w", err)
	}
	if is.State == "closed" {
		return nil
	}
	if _, err := issuestate.Return(ctx, f, a.Issue, key, ap.env.Labels(a.Repo), true); err != nil {
		return fmt.Errorf("give back: %w", err)
	}
	return nil
}

// reasonText is the heading of a.Reason in a.Language, followed by ": <argument>" when the
// reason has one.
func reasonText(a Action) string {
	text := protocol.Heading(a.Reason, a.Language)
	if a.ReasonArg != "" {
		text += ": " + a.ReasonArg
	}
	return text
}

func deferral(a Action, reason string) string {
	return fmt.Sprintf("%s: %s", subject(a), reason)
}

// subject names what an action is about in a log line or error: "<repo>#<issue>", or for a
// scheduled run, which has no issue, "<repo> scheduled:<schedule>:<slot>".
func subject(a Action) string {
	if a.Schedule != "" {
		return a.Repo + " " + scheduled.TriggerKey(a.Schedule, a.Slot)
	}
	return fmt.Sprintf("%s#%d", a.Repo, a.Issue)
}

// closeReason is the reason a KindClose records: the heading text when the action names a
// reason, else the literal cause (a start_failed).
func closeReason(a Action) string {
	if a.Reason != "" {
		return reasonText(a)
	}
	return a.ReasonArg
}

// release stops the session of run a.RunID (a.Stop), ends only its claude (a.End) or forgets its
// workspace (a.Discard), in the workplace of the run's node and profile. Stop and End take the claude session id, which the workplace finds the
// process by, never the forge key; a run the workplace never adopted (no forge key) has nothing
// to stop, a run without a session id nothing to stop or end. A row without a workspace has
// nothing to forget. None of these is an error. A run no longer open has no row to read, and
// nothing is stopped.
func (ap applier) release(ctx context.Context, a Action) error {
	if !a.Stop && !a.End && !a.Discard {
		return nil
	}
	open, err := runs.Open(ctx, ap.env.DB, a.Repo)
	if err != nil {
		return err
	}
	for _, r := range open {
		if r.ID != a.RunID {
			continue
		}
		w := ap.env.Workplace(a.Repo, r.Node, r.Profile)
		switch {
		case a.Stop && r.WorkplaceSession != "" && r.SessionID != "":
			if err := w.Stop(ctx, r.WorkspaceID, []string{r.SessionID}); err != nil {
				return fmt.Errorf("stop run %d: %w", r.ID, err)
			}
		case a.End && r.SessionID != "":
			if err := w.Kill(ctx, r.WorkspaceID, []string{r.SessionID}); err != nil {
				return fmt.Errorf("end claude of run %d: %w", r.ID, err)
			}
		case a.Discard && r.WorkspaceID != "":
			if err := w.Discard(ctx, r.WorkspaceID); err != nil {
				return fmt.Errorf("discard run %d: %w", r.ID, err)
			}
		}
		return nil
	}
	return nil
}

// propose is the assignment proposed for the trajectory of a start: the merged experiment of all
// served repos (standard for a repo that does not declare it), the variants it has used so far, and the draw of the trajectory. A note (a
// conflict between declarations) is logged. Take decides: a trajectory that has an assignment
// keeps it.
func (ap applier) propose(ctx context.Context, a Action, now time.Time) (assignment, experimentID string, err error) {
	env := ap.env
	exp, repos, note := experiment.Merge(env.Experiments)
	if !repos[a.Repo] {
		exp = nil // a repo that does not declare the experiment does not take part
	}
	count := 0
	if exp != nil {
		if count, err = experiment.VariantCount(ctx, env.DB, exp.ID, exp.Component, exp.Start); err != nil {
			return "", "", err
		}
	}
	hash := env.Hash
	if hash == nil {
		hash = experiment.Hash
	}
	d := experiment.DecideWith(hash, exp, note, a.Repo, a.Issue, count, now)
	if d.Note != "" {
		ap.logf("%s#%d: %s", a.Repo, a.Issue, d.Note)
	}
	return d.Assignment, d.ExperimentID, nil
}

// closeRun closes the run; a run another process already closed is no error.
func (ap applier) closeRun(ctx context.Context, runID int64, o runs.Outcome, reason string) error {
	err := runs.Release(ctx, ap.env.DB, runID, o, reason, ap.env.Now())
	if errors.Is(err, runs.ErrNotOpen) {
		ap.logf("run %d: already closed", runID)
		return nil
	}
	return err
}

// start runs a start or resume in the decided order: prepare and the busy check, the claim,
// the instructions, the 🤖 Started or Resumed, the 🚀, and last the launch. A failure between
// the claim and the launch closes the run as start_failed, so no open run is left without a
// workplace. A first fix-pr start that stops before the claim hands the issue back
// (returnFixPR).
func (ap applier) start(ctx context.Context, a Action, res *Result) error {
	env := ap.env
	w := env.Workplace(a.Repo, a.Node, a.Profile)
	now := env.Now()
	ws, err := w.Prepare(ctx, a.Branch)
	// A workspace with own work goes through the busy check and the claim like any other, so
	// the hand-back spends the trigger key and no later tick starts it again.
	var own *workplace.OwnWorkError
	if errors.As(err, &own) {
		ws, err = own.Workspace, nil
		// A resume continues the session that made this work in this workspace: it is never
		// handed back for it, whatever origin did meanwhile (#356).
		if a.Kind == KindResume {
			own = nil
		}
	}
	if err != nil {
		return ap.returnFixPR(ctx, a, fmt.Errorf("prepare: %w", err))
	}
	busy, earlier, err := ap.busy(ctx, w, ws, a, now)
	if err != nil {
		return ap.returnFixPR(ctx, a, err)
	}
	if busy != "" {
		err := ap.returnFixPR(ctx, a, nil)
		res.postpone(a, busy)
		return err
	}

	session := a.SessionID
	var relocErr error
	if a.Kind == KindStart {
		session = env.NewSession()
	}
	if a.Kind == KindStart {
		var command string
		if command, relocErr = ap.relocate(ctx, w, ws, a); relocErr == nil && command != a.Command {
			a.Prompt = strings.Replace(a.Prompt, a.Command, command, 1)
			a.Command = command
		}
	}
	profile, mcpConfig := a.Profile, env.MCPConfig(a.Profile)
	// runs knows no trigger repair (0008_runs_schedule.sql): its row takes answer, and its key
	// repair:<id> tells it.
	trigger := a.Trigger
	if trigger == triggerRepair {
		trigger = triggerAnswer
	}
	claim := runs.Claim{
		Repo: a.Repo, Issue: a.Issue, SessionID: session, Mode: a.Mode, Trigger: trigger,
		TriggerKey: a.TriggerKey, Command: a.Command, Prompt: a.Prompt, Profile: profile,
		Node: a.Node, Branch: a.Branch, Now: now,
	}
	// Only the start of an issue proposes an assignment; Take keeps the one the trajectory has.
	if a.Kind == KindStart {
		if claim.Assignment, claim.ExperimentID, err = ap.propose(ctx, a, now); err != nil {
			return err
		}
	}
	run, err := runs.Take(ctx, env.DB, claim)
	if errors.Is(err, runs.ErrClaimed) || errors.Is(err, runs.ErrTriggerSpent) {
		ap.logf("%s#%d: %s not claimed: %v", a.Repo, a.Issue, a.TriggerKey, err)
		if errors.Is(err, runs.ErrClaimed) {
			return ap.returnFixPR(ctx, a, nil)
		}
		return nil
	}
	if err != nil {
		return err
	}
	ap.recordAnswer(ctx, a, run.ID)
	// After the claim, so the retry rule sees a start_failed run with the path in its reason.
	if relocErr != nil {
		return ap.startFailed(ctx, run.ID, "handoff path", relocErr)
	}
	if own != nil {
		return ap.returnOwnWork(ctx, a, run.ID, session, own)
	}
	// While a claude of an earlier run of the issue still lives in the workspace, forge refuses
	// the handoff: a resume reuses its session (#206), and a run handed back while waiting on a
	// permission keeps its session alive, which the new handoff replaces (#263). Only after the
	// claim, so a process that lost it kills nothing.
	if a.Kind == KindResume && !slices.Contains(earlier, session) {
		earlier = append(earlier, session)
	}
	if len(earlier) > 0 {
		if err := w.Kill(ctx, ws.ID, earlier); err != nil {
			return ap.startFailed(ctx, run.ID, "end earlier claude", err)
		}
	}

	file, err := WriteInstructions(env.StateDir, session, env.RoundLimit(a.Repo))
	if err != nil {
		return ap.startFailed(ctx, run.ID, "instructions", err)
	}
	if a.Trigger != triggerUsage {
		if err := ap.announce(ctx, a, run.ID, session, ws, now); err != nil {
			return err
		}
	}

	// The line comes from the run, which holds what Take settled, never from the claim.
	prompt := a.Prompt
	if run.Assignment != "" && run.Assignment != "standard" {
		prompt += "\nassignment: " + run.Assignment
	}
	spec := workplace.LaunchSpec{
		Repo: a.Repo, Issue: a.Issue, SessionID: session, Mode: a.Mode, Profile: profile,
		Prompt: prompt, Native: a.Native, InstructionsFile: file, MCPConfig: mcpConfig,
	}
	outcome, reason, err := workplace.Launch(ctx, env.DB, w, run.ID, a.Branch, spec, now)
	switch outcome {
	case workplace.Accepted:
		// An error is a session key the run could not record: SessionKeys then misses the agent.
		res.Started++
		ap.resumedMarker(a)
		return err
	case workplace.Uncertain:
		// The error is diagnostic only; AdoptSessions finds its session and Decide settles the open run.
		res.Started++
		ap.resumedMarker(a)
		if err != nil {
			ap.logf("%s#%d: launch of run %d uncertain: %s", a.Repo, a.Issue, run.ID, oneLine(err.Error()))
		}
	case workplace.NotAccepted:
		// An error means the run could not be closed: an open run without an agent.
		if err != nil {
			return err
		}
		ap.logf("%s#%d: launch of run %d not accepted: %s", a.Repo, a.Issue, run.ID, oneLine(reason))
		// The second attempt of a usage resume posts no header: a Returned now would outlast its agent.
		res.NotAccepted = append(res.NotAccepted, fmt.Sprintf("%s#%d", a.Repo, a.Issue))
		if a.Trigger != triggerUsage || a.Attempt > 1 {
			return ap.returnStartFailed(ctx, a, run.ID, session, reason)
		}
	}
	return nil
}

// pusherEnv is the environment of the pusher: PATH, HOME, XDG_RUNTIME_DIR and the forge token
// under its own name, each only when tick has it and each once (a token_env may name one of the
// others). The workflow token and every other variable of tick stay behind.
func (ap applier) pusherEnv(repo string) []string {
	return ap.scrubbedEnv(repo, true)
}

// scrubbedEnv is pusherEnv with the forge token only when token is set.
func (ap applier) scrubbedEnv(repo string, token bool) []string {
	if ap.env.Getenv == nil {
		return nil
	}
	names := []string{"PATH", "HOME", "XDG_RUNTIME_DIR"}
	if token && ap.env.TokenEnv != nil {
		if k := ap.env.TokenEnv(repo); !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	var out []string
	for _, k := range names {
		if v := ap.env.Getenv(k); k != "" && v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// push claims the push row of a KindPush, puts the 🚀 on the comment that asked and starts the pusher. The
// row has no session and carries the branch, so the PR of that branch settles it. A start that
// fails closes the row start_failed; a trigger key another run spent starts nothing. The merge
// pusher (trigger merge) is started the same way, with the PR and head instead of a worktree;
// runs knows no trigger merge, so its row takes handoff, and its key merge:<pr>:<head> tells it.
// A push by hand (trigger pushed) claims its row as answer, with the 🚀 on the Vraag.
func (ap applier) push(ctx context.Context, a Action, res *Result) error {
	env := ap.env
	trigger := "handoff"
	if a.Trigger == triggerPushAnswer || a.Trigger == triggerPushed {
		// runs knows no trigger pushed: the key pushed:<id> tells it from an answer.
		trigger = "answer"
	}
	run, err := runs.Take(ctx, env.DB, runs.Claim{
		Repo: a.Repo, Issue: a.Issue, Mode: "start", Trigger: trigger, TriggerKey: a.TriggerKey,
		Command: cmdPush, Branch: a.Branch, Now: env.Now(),
	})
	if errors.Is(err, runs.ErrClaimed) || errors.Is(err, runs.ErrTriggerSpent) {
		ap.logf("%s#%d: %s not claimed: %v", a.Repo, a.Issue, a.TriggerKey, err)
		return nil
	}
	if err != nil {
		return err
	}
	ap.recordAnswer(ctx, a, run.ID)
	if a.CommentID > 0 {
		if err := env.Forge(a.Repo).Acknowledge(ctx, a.Issue, a.CommentID); err != nil {
			ap.logf("%s#%d: rocket on comment %d: %v", a.Repo, a.Issue, a.CommentID, err)
		}
	}
	return ap.startPusher(ctx, a, run, res)
}

// startPusher starts the pusher on the claimed push row run, with a.PermitSHA as its permit. A
// start that fails closes the row start_failed.
func (ap applier) startPusher(ctx context.Context, a Action, run runs.Run, res *Result) error {
	env := ap.env
	if env.StartPusher == nil {
		return ap.startFailed(ctx, run.ID, "start pusher", errors.New("no way to start the pusher"))
	}
	spec := PusherSpec{
		Repo: a.Repo, Issue: a.Issue, Worktree: a.Worktree, RunID: run.ID, Permit: a.PermitSHA,
		Env: ap.pusherEnv(a.Repo),
	}
	if a.Trigger == triggerMerge {
		spec.Merge, spec.PR, spec.HeadSHA = true, a.PR, a.HeadSHA
	}
	if err := env.StartPusher(ctx, spec); err != nil {
		return ap.startFailed(ctx, run.ID, "start pusher", err)
	}
	res.Started++
	return nil
}

// pushRecover handles a user's answer other than `push` to a pusher 🤖 Vraag. It asks origin
// first (originContains); under a closed brake an answer origin does not satisfy is postponed
// without a claim. Then it takes the trigger key, like push. When origin has the Vraag's SHA the
// push has happened already, so it posts the 🤖 Handoff with the Vraag's after: command and closes
// the row as handoff at once: no pusher starts, and an open row would block handoff() and read as
// a silent pusher. Without an after: command it returns the issue. Any other state goes to
// classifyFlow. A failed post closes the row as returned (recoverPostFailed).
func (ap applier) pushRecover(ctx context.Context, a Action, res *Result) error {
	env := ap.env
	state, gitErr := ap.originContains(ctx, a)
	if a.Braked != "" && state != onOrigin {
		// A closed brake keeps the answer for a later tick: no claim, no post, no classifier.
		res.postpone(a, fmt.Sprintf("push answer waits on the brake: %s", a.Braked))
		return nil
	}
	run, err := runs.Take(ctx, env.DB, runs.Claim{
		Repo: a.Repo, Issue: a.Issue, Mode: "start", Trigger: "answer", TriggerKey: a.TriggerKey,
		Command: cmdPush, Branch: a.Branch, Now: env.Now(),
	})
	if errors.Is(err, runs.ErrClaimed) || errors.Is(err, runs.ErrTriggerSpent) {
		ap.logf("%s#%d: %s not claimed: %v", a.Repo, a.Issue, a.TriggerKey, err)
		return nil
	}
	if err != nil {
		return err
	}
	ap.recordAnswer(ctx, a, run.ID)
	if a.CommentID > 0 {
		if err := env.Forge(a.Repo).Acknowledge(ctx, a.Issue, a.CommentID); err != nil {
			ap.logf("%s#%d: rocket on comment %d: %v", a.Repo, a.Issue, a.CommentID, err)
		}
	}
	if state != onOrigin {
		return ap.classifyFlow(ctx, a, run, gitErr, res)
	}
	if a.After == "" {
		return ap.recoverReturn(ctx, a, run.ID, "reason_pusher_no_after", pushCommand(a.Worktree))
	}
	body := protocol.RenderPushed(a.Issue, a.Worktree, a.Branch, "", "", a.After, a.Lane, a.Language)
	if _, err := env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return ap.recoverPostFailed(ctx, run.ID, "post handoff", err)
	}
	return ap.closeRun(ctx, run.ID, runs.Handoff, "")
}

// originState says whether origin has the SHA of a pusher question.
type originState int

const (
	notOnOrigin originState = iota // origin lacks the SHA, or git could not tell
	onOrigin                       // origin has Branch on the SHA, or a branch that contains it
)

// originContains asks whether origin has a.PermitSHA on a.Branch: ls-remote equal to it, or a
// tip that contains it (fetched without touching a ref or FETCH_HEAD, then merge-base). An error
// is returned with notOnOrigin.
func (ap applier) originContains(ctx context.Context, a Action) (originState, error) {
	env := ap.env
	lsRemote := env.LsRemote
	if lsRemote == nil {
		lsRemote = gitLsRemote
	}
	fetch := env.Fetch
	if fetch == nil {
		fetch = gitFetch
	}
	isAncestor := env.IsAncestor
	if isAncestor == nil {
		isAncestor = gitIsAncestor
	}
	// No prompt: git asks for credentials on the terminal, and the tick has none to answer.
	gitEnv := append(ap.scrubbedEnv(a.Repo, false), "GIT_TERMINAL_PROMPT=0")
	tip, err := lsRemote(ctx, a.Worktree, a.Branch, gitEnv)
	switch {
	case err != nil:
		return notOnOrigin, err
	case tip == "":
		return notOnOrigin, nil
	case tip == a.PermitSHA:
		return onOrigin, nil
	}
	if err := fetch(ctx, a.Worktree, tip, gitEnv); err != nil {
		return notOnOrigin, err
	}
	ok, err := isAncestor(ctx, a.Worktree, a.PermitSHA, tip, gitEnv)
	if err != nil || !ok {
		return notOnOrigin, err
	}
	return onOrigin, nil
}

// classifyFlow reads the answer to a pusher question that origin does not satisfy in plain
// language; gitErr is the git failure of originContains, nil when origin simply lacks the SHA.
// A permit notes itself on the claimed row (pushTargetOf reads it on a retry) and starts the
// pusher for exactly the Vraag's SHA; a note that does not write starts nothing. Any other
// verdict, a failing or missing classifier included, posts one reply that says what works and
// closes the row as question: the reply is the open Vraag for the next answer.
func (ap applier) classifyFlow(ctx context.Context, a Action, run runs.Run, gitErr error, res *Result) error {
	if gitErr != nil {
		ap.logf("%s#%d: %s: git could not tell whether origin has %s: %v", a.Repo, a.Issue, a.TriggerKey, a.PermitSHA, gitErr)
	}
	verdict := VerdictOther
	readable := false
	if classify := ap.env.ClassifyAnswer; classify != nil {
		v, err := classify(ctx, AnswerInput{Step: a.Step, SHA: a.PermitSHA, Branch: a.Branch, Answer: a.AnswerText})
		switch {
		case err != nil:
			ap.logf("%s#%d: %s: classifier: %v", a.Repo, a.Issue, a.TriggerKey, err)
		case v == VerdictPermit || v == VerdictPushed || v == VerdictOther:
			verdict, readable = v, true
		default:
			ap.logf("%s#%d: %s: classifier: unknown verdict %q", a.Repo, a.Issue, a.TriggerKey, v)
		}
	}
	if verdict == VerdictPermit {
		if err := runs.SetVerdict(ctx, ap.env.DB, run.ID, permitVerdict); err != nil {
			return ap.startFailed(ctx, run.ID, "note permit", err)
		}
		return ap.startPusher(ctx, a, run, res)
	}
	h := func(key string) string { return protocol.Heading(key, a.Language) }
	var line []string
	switch {
	case !readable:
		line = append(line, h("reply_push_unreadable"))
	case verdict == VerdictOther:
		line = append(line, h("reply_push_not_permit"))
	case gitErr == nil: // pushed, and origin lacks the SHA
		line = append(line, strings.NewReplacer("{sha}", a.PermitSHA, "{branch}", a.Branch).Replace(h("reply_push_not_on_origin")))
	}
	if gitErr != nil {
		line = append(line, h("reply_push_could_not_check"))
	}
	body := protocol.RenderPushReply(protocol.PushReply{
		SHA: a.PermitSHA, Worktree: a.Worktree, Branch: a.Branch, After: a.After, Lane: a.Lane, Step: a.Step,
		Question: a.QuestionURL, Line: strings.Join(line, " "), Command: pushCommand(a.Worktree),
	}, a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return ap.recoverPostFailed(ctx, run.ID, "post reply", err)
	}
	return ap.closeRun(ctx, run.ID, runs.Question, "")
}

// recoverReturn posts the 🤖 Returned of a push recovery that could not hand on, gives the issue
// back and closes the claimed row as returned.
func (ap applier) recoverReturn(ctx context.Context, a Action, runID int64, reason, arg string) error {
	ra := a
	ra.Reason, ra.ReasonArg = reason, arg
	text := reasonText(ra)
	body := protocol.RenderReturned(text, "", "", a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return ap.recoverPostFailed(ctx, runID, "post returned", err)
	}
	if err := ap.giveBack(ctx, ra, ReturnLabelKey(reason, false)); err != nil {
		return errors.Join(err, ap.closeRun(ctx, runID, runs.Returned, text))
	}
	return ap.closeRun(ctx, runID, runs.Returned, text)
}

// recoverPostFailed closes the row of a push recovery whose post failed as returned, never as
// start_failed: retry reads a start_failed push row as a push to try again, and this answer
// permitted no push. The trigger key stays spent; the idle net picks the silent issue up.
func (ap applier) recoverPostFailed(ctx context.Context, runID int64, step string, cause error) error {
	return ap.failedAs(ctx, runID, runs.Returned, step, cause)
}

// lsRemoteTimeout bounds one git ls-remote: a remote that hangs must not hold the tick.
const lsRemoteTimeout = 30 * time.Second

// gitLsRemote is Env.LsRemote in production: git ls-remote origin refs/heads/<branch> in worktree,
// within lsRemoteTimeout.
func gitLsRemote(ctx context.Context, worktree, branch string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", worktree, "ls-remote", "origin", "refs/heads/"+branch)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git ls-remote: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git ls-remote: %w", err)
	}
	sha, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	return sha, nil
}

// gitFetch is Env.Fetch in production: git fetch --no-write-fetch-head origin <sha>, within
// lsRemoteTimeout.
func gitFetch(ctx context.Context, worktree, sha string, env []string) error {
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", worktree, "fetch", "--no-write-fetch-head", "origin", sha)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitIsAncestor is Env.IsAncestor in production: git merge-base --is-ancestor <sha> <tip>, where
// exit code 1 is a plain no and any other failure an error.
func gitIsAncestor(ctx context.Context, worktree, sha, tip string, env []string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", worktree, "merge-base", "--is-ancestor", sha, tip)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base: %w: %s", err, strings.TrimSpace(string(out)))
}

// relocate finds the path of the start command in the worktree of ws. On self it looks on this
// host; on a node the node looks in its own worktree (workplace.Relocator, #327). A workplace on a
// node without one keeps the command as decided. Every kenn-forge adapter is a Relocator, but on
// self it cannot answer, so the node decides, not the type.
func (ap applier) relocate(ctx context.Context, w workplace.Workplace, ws workplace.Workspace, a Action) (string, error) {
	if a.Node == "" {
		return Relocate(a.Command, ws.Path)
	}
	if r, ok := w.(workplace.Relocator); ok {
		return r.Relocate(ctx, ws.ID, a.Command)
	}
	return a.Command, nil
}

// returnFixPR posts a 🤖 Returned without a run for a first fix-pr start that stops before its
// claim, and returns cause, joined with a failed post. The reason is the one decide gave the
// start (a.Reason with a.ReasonArg), "PR red" when it gave none. The run that found the PR
// already closed as a handoff, so without it the issue would sit silent on that PR. A
// trigger key another run already spent needs nothing; a second attempt comes from retry, which
// plans it every tick and hands the issue back itself; any other start posts nothing.
func (ap applier) returnFixPR(ctx context.Context, a Action, cause error) error {
	if a.Trigger != triggerFixPR || a.Attempt != 1 {
		return cause
	}
	ra := a
	ra.Reason = cmp.Or(a.Reason, "reason_pr_red")
	body := protocol.RenderReturned(reasonText(ra), "", "", a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return errors.Join(cause, fmt.Errorf("post returned: %w", err))
	}
	// No retry follows this Returned: the issue goes to To do.
	return errors.Join(cause, ap.giveBack(ctx, ra, ReturnLabelKey(ra.Reason, false)))
}

// returnOwnWork hands back a claimed run whose workspace holds own work origin lacks (#269):
// a start there would run silently on an old HEAD. The Returned names the workspace and the
// number of own commits; the run closes as returned, so its trigger is spent. A failed post
// closes it as start_failed instead, and retry tries once more.
func (ap applier) returnOwnWork(ctx context.Context, a Action, runID int64, session string, own *workplace.OwnWorkError) error {
	ra := a
	ra.Reason, ra.ReasonArg = "reason_own_work", own.Error()
	reason := reasonText(ra)
	body := protocol.RenderReturned(reason, session, "", a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return ap.startFailed(ctx, runID, "post", err)
	}
	ap.logf("%s#%d: run %d returned: %s", a.Repo, a.Issue, runID, reason)
	// The run closes also when the give-back fails: it has no agent, and settle would discard
	// the workspace that holds the own work. finishReturn finishes the give-back.
	return errors.Join(ap.giveBack(ctx, ra, ReturnLabelKey(ra.Reason, false)),
		ap.closeRun(ctx, runID, runs.Returned, reason))
}

// returnStartFailed posts the 🤖 Returned of a refused launch under its Started, with the
// session of the run, so decide sees it and posts none after a second refusal. A failed post
// logs one line and is no action error: the run is closed, and decide posts the Returned of a
// second refusal itself.
//
// Only after the second attempt (a.Attempt > 1) does the issue go to To do with its label: after
// the first, retry plans the second attempt, which runs only under Controller, so the issue
// stays there without a label. A failed give-back is the action's error; finishReturn finishes it.
func (ap applier) returnStartFailed(ctx context.Context, a Action, runID int64, session, reason string) error {
	ra := a
	ra.Reason, ra.ReasonArg = "reason_start_failed", reason
	body := protocol.RenderReturned(reasonText(ra), session, "", a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		ap.logf("%s#%d: post Returned of run %d: %v", a.Repo, a.Issue, runID, err)
		return nil
	}
	if a.Attempt <= 1 {
		return nil
	}
	return ap.giveBack(ctx, ra, ReturnLabelKey(ra.Reason, false))
}

// announce posts the 🤖 Started or Resumed and the 🚀; a failed post closes the run as
// start_failed.
func (ap applier) announce(ctx context.Context, a Action, runID int64, session string, ws workplace.Workspace, now time.Time) error {
	poster := ap.env.Forge(a.Repo)
	if _, err := poster.CreateComment(ctx, a.Issue, startBody(a, session, ws, now)); err != nil {
		return ap.startFailed(ctx, runID, "post", err)
	}
	if a.CommentID > 0 {
		if err := poster.Acknowledge(ctx, a.Issue, a.CommentID); err != nil {
			ap.logf("%s#%d: rocket on comment %d: %v", a.Repo, a.Issue, a.CommentID, err)
		}
	}
	return nil
}

// resumedMarker removes the marker a usage resume answered, once the launch is accepted or
// uncertain; any other start leaves markers alone.
func (ap applier) resumedMarker(a Action) {
	if a.Trigger == triggerUsage {
		ap.removeMarker(a)
	}
}

// removeMarker removes the marker of a.SessionID while its key is a.TriggerKey: through
// Env.RemoveMarker, which knows the node the marker came from, else in MarkerDir. A failure logs
// one line and is no action error: the next tick drops the marker again.
func (ap applier) removeMarker(a Action) {
	remove := ap.env.RemoveMarker
	if remove == nil {
		if ap.env.MarkerDir == "" {
			return
		}
		remove = func(session, key string) error { return park.RemoveMarker(ap.env.MarkerDir, session, key) }
	}
	if err := remove(a.SessionID, a.TriggerKey); err != nil {
		ap.logf("%s#%d: remove marker of session %s: %v", a.Repo, a.Issue, a.SessionID, err)
	}
}

// busy returns why the workplace cannot take the run, or "" when it can, and the session ids of
// the claudes of the issue's closed runs in the workspace (workplace.Earlier). A node is like
// self: Kill ends an earlier claude there through serve (#327).
func (ap applier) busy(ctx context.Context, w workplace.Workplace, ws workplace.Workspace, a Action, now time.Time) (string, []string, error) {
	sessions, err := w.Sessions(ctx, ws.ID)
	if err != nil {
		return "", nil, fmt.Errorf("sessions: %w", err)
	}
	keys, err := runs.SessionKeys(ctx, ap.env.DB, a.Repo, a.Issue)
	if err != nil {
		return "", nil, err
	}
	open, err := runs.Open(ctx, ap.env.DB, a.Repo)
	if err != nil {
		return "", nil, err
	}
	openSessions := map[string]bool{}
	for _, r := range open {
		if r.Issue == a.Issue && r.SessionID != "" {
			openSessions[r.SessionID] = true
		}
	}
	own := workplace.Own(keys)
	s, busy := workplace.Busy(sessions, own, openSessions, nil, now, ap.env.MaxWait)
	if !busy {
		return "", workplace.Earlier(sessions, own, openSessions), nil
	}
	return workplace.BusyReason(s), nil, nil
}

// startFailed closes a claimed run as start_failed with reason "<step>: <cause>". The release
// ignores a cancelled ctx: the cancel may be what made the step fail.
func (ap applier) startFailed(ctx context.Context, runID int64, step string, cause error) error {
	return ap.failedAs(ctx, runID, runs.StartFailed, step, cause)
}

// failedAs closes runID with outcome o and the failed step as its reason, and returns that
// failure, joined with a failed release. The release ignores a cancelled ctx.
func (ap applier) failedAs(ctx context.Context, runID int64, o runs.Outcome, step string, cause error) error {
	err := fmt.Errorf("%s: %w", step, cause)
	if rerr := runs.Release(context.WithoutCancel(ctx), ap.env.DB, runID, o, err.Error(), ap.env.Now()); rerr != nil {
		return errors.Join(err, fmt.Errorf("release run %d: %w", runID, rerr))
	}
	return err
}

// startBody is the 🤖 Started of a start, or the 🤖 Resumed of a resume.
func startBody(a Action, session string, ws workplace.Workspace, now time.Time) string {
	if a.Kind == KindResume {
		return protocol.RenderResumed(session, a.CommentID, a.Language)
	}
	skip := ""
	if a.Skip != "" {
		skip = protocol.Heading(a.Skip, a.Language)
	}
	return protocol.RenderStarted(protocol.Started{
		Session: session, Worktree: ws.Path, Command: a.Command, Start: now.UTC().Format(time.RFC3339),
		Branch: a.Branch, Skip: skip, Runner: runnerForge, Workspace: ws.ID,
	}, a.Language)
}
