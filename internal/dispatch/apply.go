package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Poster is the forge side apply writes to: one repo's comments and reactions.
type Poster interface {
	CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
	AddRocket(ctx context.Context, commentID int64) error
}

// Board is the board side apply writes to: the Status of one repo's board items, by option
// name.
type Board interface {
	SetStatus(ctx context.Context, itemID, status string) error
}

// Env is what Apply needs from the outside, per repo where it differs.
type Env struct {
	DB         *sql.DB
	Poster     func(repo string) Poster
	Board      func(repo string) Board
	Workplace  func(repo string) workplace.Workplace
	Profile    func(repo string) (name, mcpConfig string)
	RoundLimit func(repo string) string // what a run does at an adversarial round limit
	StateDir   string
	MarkerDir  string // the resume markers; "" removes none
	MaxWait    time.Duration
	Now        func() time.Time
	NewSession func() string // runs.NewSessionID
	Log        io.Writer     // nil discards
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
		err = fmt.Errorf("%s#%d %s: %w", a.Repo, a.Issue, a.Kind, err)
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
	case KindReturn:
		body := protocol.RenderReturned(reasonText(a), a.SessionID, "", a.Language)
		if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
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
		if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		return ap.closeRun(ctx, a.RunID, runs.Done, "")
	case KindClose:
		if err := ap.release(ctx, a); err != nil {
			return err
		}
		return ap.closeRun(ctx, a.RunID, a.Outcome, closeReason(a))
	case KindRocket:
		return ap.env.Poster(a.Repo).AddRocket(ctx, a.CommentID)
	case KindDefer:
		res.Deferred = append(res.Deferred, deferral(a, a.Reason))
		return nil
	case KindPark:
		// The comment first: a failed board change leaves a Parked that the next tick sees,
		// and holds without posting a second one.
		body := protocol.RenderParked(a.Blockers, a.Language)
		if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
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

// setStatus moves the board item of a to status.
func (ap applier) setStatus(ctx context.Context, a Action, status string) error {
	if err := ap.env.Board(a.Repo).SetStatus(ctx, a.ItemID, status); err != nil {
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
	return fmt.Sprintf("%s#%d: %s", a.Repo, a.Issue, reason)
}

// closeReason is the reason a KindClose records: the heading text when the action names a
// reason, else the literal cause (a start_failed).
func closeReason(a Action) string {
	if a.Reason != "" {
		return reasonText(a)
	}
	return a.ReasonArg
}

// release stops the session of run a.RunID (a.Stop) or forgets its workspace (a.Discard). Stop
// takes the claude session id, which the workplace finds the process by, never the forge key; a
// run the workplace never adopted (no forge key) or without a session id has nothing to stop. A
// row without a workspace has nothing to forget. Neither is an error. A run no longer open has
// no row to read, and nothing is stopped.
func (ap applier) release(ctx context.Context, a Action) error {
	if !a.Stop && !a.Discard {
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
		w := ap.env.Workplace(a.Repo)
		switch {
		case a.Stop && r.WorkplaceSession != "" && r.SessionID != "":
			if err := w.Stop(ctx, r.WorkspaceID, []string{r.SessionID}); err != nil {
				return fmt.Errorf("stop run %d: %w", r.ID, err)
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
	w := env.Workplace(a.Repo)
	now := env.Now()
	ws, err := w.Prepare(ctx, a.Branch)
	if err != nil {
		return ap.returnFixPR(ctx, a, fmt.Errorf("prepare: %w", err))
	}
	busy, err := ap.busy(ctx, w, ws, a, now)
	if err != nil {
		return ap.returnFixPR(ctx, a, err)
	}
	if busy != "" {
		err := ap.returnFixPR(ctx, a, nil)
		res.Deferred = append(res.Deferred, deferral(a, busy))
		return err
	}

	session := a.SessionID
	if a.Kind == KindStart {
		session = env.NewSession()
	}
	profile, mcpConfig := env.Profile(a.Repo)
	run, err := runs.Take(ctx, env.DB, runs.Claim{
		Repo: a.Repo, Issue: a.Issue, SessionID: session, Mode: a.Mode, Trigger: a.Trigger,
		TriggerKey: a.TriggerKey, Command: a.Command, Prompt: a.Prompt, Profile: profile,
		Branch: a.Branch, Now: now,
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

// returnFixPR posts a 🤖 Returned "PR red" without a run for a first fix-pr start that stops
// before its claim, and returns cause, joined with a failed post. The run that found the red
// PR already closed as a handoff, so without it the issue would sit silent on a red PR. A
// trigger key another run already spent needs nothing; a second attempt comes from retry, which
// plans it every tick and hands the issue back itself; any other start posts nothing.
func (ap applier) returnFixPR(ctx context.Context, a Action, cause error) error {
	if a.Trigger != triggerFixPR || a.Attempt != 1 {
		return cause
	}
	body := protocol.RenderReturned(protocol.Heading("reason_pr_red", a.Language), "", "", a.Language)
	if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		return errors.Join(cause, fmt.Errorf("post returned: %w", err))
	}
	return cause
}

// returnStartFailed posts the 🤖 Returned of a refused launch under its Started, with the
// session of the run, so decide sees it and posts none after a second refusal. A failed post
// logs one line and is no action error: the run is closed, and decide posts the Returned of a
// second refusal itself.
func (ap applier) returnStartFailed(ctx context.Context, a Action, runID int64, session, reason string) {
	ra := a
	ra.Reason, ra.ReasonArg = "reason_start_failed", reason
	body := protocol.RenderReturned(reasonText(ra), session, "", a.Language)
	if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
		ap.logf("%s#%d: post Returned of run %d: %v", a.Repo, a.Issue, runID, err)
	}
}

// announce posts the 🤖 Started or Resumed and the 🚀; a failed post closes the run as
// start_failed.
func (ap applier) announce(ctx context.Context, a Action, runID int64, session string, ws workplace.Workspace, now time.Time) error {
	poster := ap.env.Poster(a.Repo)
	if _, err := poster.CreateComment(ctx, a.Issue, startBody(a, session, ws, now)); err != nil {
		return ap.startFailed(ctx, runID, "post", err)
	}
	if a.CommentID > 0 {
		if err := poster.AddRocket(ctx, a.CommentID); err != nil {
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

// removeMarker removes the marker of a.SessionID while its key is a.TriggerKey. A failure
// logs one line and is no action error: the next tick drops the marker again.
func (ap applier) removeMarker(a Action) {
	if ap.env.MarkerDir == "" {
		return
	}
	if err := park.RemoveMarker(ap.env.MarkerDir, a.SessionID, a.TriggerKey); err != nil {
		ap.logf("%s#%d: remove marker of session %s: %v", a.Repo, a.Issue, a.SessionID, err)
	}
}

// busy returns why the workplace cannot take the run, or "" when it can.
func (ap applier) busy(ctx context.Context, w workplace.Workplace, ws workplace.Workspace, a Action, now time.Time) (string, error) {
	sessions, err := w.Sessions(ctx, ws.ID)
	if err != nil {
		return "", fmt.Errorf("sessions: %w", err)
	}
	keys, err := runs.SessionKeys(ctx, ap.env.DB, a.Repo, a.Issue)
	if err != nil {
		return "", err
	}
	open, err := runs.Open(ctx, ap.env.DB, a.Repo)
	if err != nil {
		return "", err
	}
	openSessions := map[string]bool{}
	for _, r := range open {
		if r.Issue == a.Issue && r.SessionID != "" {
			openSessions[r.SessionID] = true
		}
	}
	s, busy := workplace.Busy(sessions, workplace.Own(keys), openSessions, nil, now, ap.env.MaxWait)
	if !busy {
		return "", nil
	}
	return workplace.BusyReason(s), nil
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
