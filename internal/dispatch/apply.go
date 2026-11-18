package dispatch

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
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
	StateDir   string
	MarkerDir  string // the resume markers; "" removes none
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
	TokenEnvs func() []string
	// StartPusher starts the workflow pusher detached and returns at once; it does not wait for it.
	StartPusher func(ctx context.Context, spec PusherSpec) error
}

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

// Result is what one Apply did.
type Result struct {
	Deferred []string         // "<repo>#<n>: <reason>"
	Failed   map[string]error // per repo: the joined errors of its actions; absent = no error
	Started  int              // launches that were accepted or are uncertain
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
		body := protocol.RenderReturned(reasonText(a), a.SessionID, "", a.Language)
		if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		if a.RunID == 0 {
			return nil
		}
		if err := ap.release(ctx, a); err != nil {
			return err
		}
		return ap.closeRun(ctx, a.RunID, runs.Returned, reasonText(a))
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
	case KindPush:
		return ap.push(ctx, a, res)
	case KindMerge:
		return ap.merge(ctx, a, res)
	case KindClose:
		if err := ap.release(ctx, a); err != nil {
			return err
		}
		return ap.closeRun(ctx, a.RunID, a.Outcome, closeReason(a))
	case KindRocket:
		return ap.env.Forge(a.Repo).Acknowledge(ctx, a.Issue, a.CommentID)
	case KindDefer:
		res.Deferred = append(res.Deferred, deferral(a, a.Reason))
		return nil
	case KindPark:
		// The comment first: a failed board change leaves a Parked that the next tick sees,
		// and holds without posting a second one.
		body := protocol.RenderParked(a.Blockers, a.Language)
		if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		return ap.setStatus(ctx, a, statusOnHold)
	case KindHold:
		return ap.setStatus(ctx, a, statusOnHold)
	case KindUnpark:
		return ap.setStatus(ctx, a, statusController)
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
		res.Deferred = append(res.Deferred, deferral(a, fmt.Sprintf("merge of PR #%d skipped: head moved", a.PR)))
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
		res.Deferred = append(res.Deferred, deferral(a, busy))
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
	run, err := runs.Take(ctx, env.DB, runs.Claim{
		Repo: a.Repo, Issue: a.Issue, SessionID: session, Mode: a.Mode, Trigger: a.Trigger,
		TriggerKey: a.TriggerKey, Command: a.Command, Prompt: a.Prompt, Profile: profile,
		Node: a.Node, Branch: a.Branch, Now: now,
	})
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

	spec := workplace.LaunchSpec{
		Repo: a.Repo, Issue: a.Issue, SessionID: session, Mode: a.Mode, Profile: profile,
		Prompt: a.Prompt, Native: a.Native, InstructionsFile: file, MCPConfig: mcpConfig,
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
		if a.Trigger != triggerUsage || a.Attempt > 1 {
			ap.returnStartFailed(ctx, a, run.ID, session, reason)
		}
		res.NotAccepted = append(res.NotAccepted, fmt.Sprintf("%s#%d", a.Repo, a.Issue))
	}
	return nil
}

// pusherEnv is the environment of the pusher: PATH, HOME, XDG_RUNTIME_DIR and the forge token
// under its own name, each only when tick has it and each once (a token_env may name one of the
// others). The workflow token and every other variable of tick stay behind.
func (ap applier) pusherEnv(repo string) []string {
	if ap.env.Getenv == nil {
		return nil
	}
	names := []string{"PATH", "HOME", "XDG_RUNTIME_DIR"}
	if ap.env.TokenEnv != nil {
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
func (ap applier) push(ctx context.Context, a Action, res *Result) error {
	env := ap.env
	trigger := "handoff"
	if a.Trigger == triggerPushAnswer {
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
	if a.CommentID > 0 {
		if err := env.Forge(a.Repo).Acknowledge(ctx, a.Issue, a.CommentID); err != nil {
			ap.logf("%s#%d: rocket on comment %d: %v", a.Repo, a.Issue, a.CommentID, err)
		}
	}
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
	err = env.StartPusher(ctx, spec)
	if err != nil {
		return ap.startFailed(ctx, run.ID, "start pusher", err)
	}
	res.Started++
	return nil
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
	return cause
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
	return ap.closeRun(ctx, runID, runs.Returned, reason)
}

// returnStartFailed posts the 🤖 Returned of a refused launch under its Started, with the
// session of the run, so decide sees it and posts none after a second refusal. A failed post
// logs one line and is no action error: the run is closed, and decide posts the Returned of a
// second refusal itself.
func (ap applier) returnStartFailed(ctx context.Context, a Action, runID int64, session, reason string) {
	ra := a
	ra.Reason, ra.ReasonArg = "reason_start_failed", reason
	body := protocol.RenderReturned(reasonText(ra), session, "", a.Language)
	if _, err := ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		ap.logf("%s#%d: post Returned of run %d: %v", a.Repo, a.Issue, runID, err)
	}
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
	err := fmt.Errorf("%s: %w", step, cause)
	if rerr := runs.Release(context.WithoutCancel(ctx), ap.env.DB, runID, runs.StartFailed, err.Error(), ap.env.Now()); rerr != nil {
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
