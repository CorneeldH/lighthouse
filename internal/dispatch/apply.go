package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Poster is the forge side apply writes to: one repo's comments and reactions.
type Poster interface {
	CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
	AddRocket(ctx context.Context, commentID int64) error
}

// Env is what Apply needs from the outside, per repo where it differs.
type Env struct {
	DB         *sql.DB
	Poster     func(repo string) Poster
	Workplace  func(repo string) workplace.Workplace
	Profile    func(repo string) (name, mcpConfig string)
	RoundLimit func(repo string) string // what a run does at an adversarial round limit
	StateDir   string
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

func (ap applier) apply(ctx context.Context, a Action, res *Result) error {
	switch a.Kind {
	case KindStart, KindResume:
		return ap.start(ctx, a, res)
	case KindReturn:
		body := protocol.RenderReturned(reasonText(a), a.SessionID, "", a.Language)
		if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		return nil
	case KindDone:
		note := protocol.Heading("note_done", a.Language)
		body := protocol.RenderDone(a.SessionID, a.PRURL, note, a.Language)
		if _, err := ap.env.Poster(a.Repo).CreateComment(ctx, a.Issue, body); err != nil {
			return fmt.Errorf("post: %w", err)
		}
		return ap.closeRun(ctx, a.RunID, runs.Done)
	case KindClose:
		return ap.closeRun(ctx, a.RunID, a.Outcome)
	case KindRocket:
		return ap.env.Poster(a.Repo).AddRocket(ctx, a.CommentID)
	case KindDefer:
		res.Deferred = append(res.Deferred, deferral(a, a.Reason))
		return nil
	}
	return fmt.Errorf("unknown action kind %q", a.Kind)
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

// closeRun closes the run; a run another process already closed is no error.
func (ap applier) closeRun(ctx context.Context, runID int64, o runs.Outcome) error {
	err := runs.Release(ctx, ap.env.DB, runID, o, "", ap.env.Now())
	if errors.Is(err, runs.ErrNotOpen) {
		ap.logf("run %d: already closed", runID)
		return nil
	}
	return err
}

// start runs a start or resume in the decided order: prepare and the busy check, the claim,
// the instructions, the 🤖 Started or Resumed, the 🚀, and last the launch. A failure between
// the claim and the launch closes the run as start_failed, so no open run is left without a
// workplace.
func (ap applier) start(ctx context.Context, a Action, res *Result) error {
	env := ap.env
	w := env.Workplace(a.Repo)
	now := env.Now()
	ws, err := w.Prepare(ctx, a.Branch)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	busy, err := ap.busy(ctx, w, ws, a, now)
	if err != nil {
		return err
	}
	if busy != "" {
		res.Deferred = append(res.Deferred, deferral(a, busy))
		return nil
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
		return nil
	}
	if err != nil {
		return err
	}

	file, err := WriteInstructions(env.StateDir, session, env.RoundLimit(a.Repo))
	if err != nil {
		return ap.startFailed(ctx, run.ID, "instructions", err)
	}
	poster := env.Poster(a.Repo)
	if _, err := poster.CreateComment(ctx, a.Issue, startBody(a, session, ws, now)); err != nil {
		return ap.startFailed(ctx, run.ID, "post", err)
	}
	if a.CommentID > 0 {
		if err := poster.AddRocket(ctx, a.CommentID); err != nil {
			ap.logf("%s#%d: rocket on comment %d: %v", a.Repo, a.Issue, a.CommentID, err)
		}
	}

	spec := workplace.LaunchSpec{
		Repo: a.Repo, Issue: a.Issue, SessionID: session, Mode: a.Mode, Profile: profile,
		Prompt: a.Prompt, Native: a.Native, InstructionsFile: file, MCPConfig: mcpConfig,
	}
	outcome, err := workplace.Launch(ctx, env.DB, w, run.ID, a.Branch, spec, now)
	switch outcome {
	case workplace.Accepted, workplace.Uncertain:
		res.Started++
		if err != nil {
			ap.logf("%s#%d: launch of run %d: %v", a.Repo, a.Issue, run.ID, err)
		}
	case workplace.NotAccepted:
		ap.logf("%s#%d: launch of run %d not accepted: %v", a.Repo, a.Issue, run.ID, err)
	}
	return nil
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

// startFailed closes a claimed run as start_failed with reason "<step>: <cause>".
func (ap applier) startFailed(ctx context.Context, runID int64, step string, cause error) error {
	err := fmt.Errorf("%s: %w", step, cause)
	if rerr := runs.Release(ctx, ap.env.DB, runID, runs.StartFailed, err.Error(), ap.env.Now()); rerr != nil {
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
