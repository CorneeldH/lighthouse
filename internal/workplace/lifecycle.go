package workplace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

// Reasons a run is returned by CheckOpen.
const (
	ReasonUncertain = "start uncertain: no session found"
	ReasonTimeout   = "timeout (max_runtime)"
)

// Limits are the two upper bounds on a run.
type Limits struct{ MaxWait, MaxRuntime time.Duration }

// Outcome is what Launch made of a start.
type Outcome int

// The outcomes of Launch.
const (
	// Accepted: the forge started an agent and the run holds its session key.
	Accepted Outcome = iota
	// Uncertain: an agent may be running; the run stays open without a session.
	Uncertain
	// NotAccepted: no agent started and the run is closed as start_failed.
	NotAccepted
)

// Launch starts the agent of a claimed run (after runs.Take). It never spawns
// twice: an uncertain spawn leaves the row open for CheckOpen to settle.
//
// With Uncertain the error is diagnostic only (the cause of the doubt): the row
// stays open and the caller must never spawn again.
func Launch(ctx context.Context, db *sql.DB, w Workplace, runID int64, branch string, l LaunchSpec, now time.Time) (Outcome, error) {
	ws, err := w.Prepare(ctx, branch)
	if err != nil {
		return NotAccepted, releaseFailed(ctx, db, runID, "prepare: "+err.Error(), now)
	}
	if err := runs.SetWorkplace(ctx, db, runID, ws.ID, branch); err != nil {
		// Nothing was spawned, and a row without workspace_id would never close.
		cause := fmt.Errorf("record workplace of run %d: %w", runID, err)
		return NotAccepted, errors.Join(cause, releaseFailed(ctx, db, runID, "record workplace: "+err.Error(), now))
	}
	s, err := w.Spawn(ctx, ws, l)
	switch {
	case errors.Is(err, ErrNotAccepted):
		return NotAccepted, releaseFailed(ctx, db, runID, err.Error(), now)
	case err != nil:
		// ErrUncertain, or any other failure: an agent may be running.
		return Uncertain, fmt.Errorf("spawn run %d: %w", runID, err)
	}
	if err := runs.SetSession(ctx, db, runID, s.Key); err != nil {
		return Accepted, fmt.Errorf("record session of run %d: %w", runID, err)
	}
	return Accepted, nil
}

func releaseFailed(ctx context.Context, db *sql.DB, runID int64, reason string, now time.Time) error {
	if err := runs.Release(ctx, db, runID, runs.StartFailed, reason, now); err != nil {
		return fmt.Errorf("release run %d: %w", runID, err)
	}
	return nil
}

// Closed is a run that CheckOpen returned.
type Closed struct {
	RunID  int64
	Issue  int
	Reason string
}

// CheckOpen settles the open runs of repo that have a workspace: it looks up
// the session of an uncertain start, and stops a run past MaxRuntime. A run it
// cannot measure or stop stays open; the errors are joined. A run it returns
// as uncertain has its launch discarded; a failed Discard is joined, and the
// run is still returned.
func CheckOpen(ctx context.Context, db *sql.DB, w Workplace, repo string, lim Limits, now time.Time) ([]Closed, error) {
	open, err := runs.Open(ctx, db, repo)
	if err != nil {
		return nil, err
	}
	var closed []Closed
	var errs []error
	for _, run := range open {
		if run.WorkspaceID == "" {
			continue
		}
		age := now.Sub(run.StartedAt)
		if run.WorkplaceSession == "" {
			sessions, err := w.Sessions(ctx, run.WorkspaceID)
			if err != nil {
				errs = append(errs, fmt.Errorf("sessions of run %d: %w", run.ID, err))
				continue
			}
			taken, err := runs.SessionKeys(ctx, db, repo, run.Issue)
			if err != nil {
				errs = append(errs, fmt.Errorf("session keys of run %d: %w", run.ID, err))
				continue
			}
			if key, ok := findSession(sessions, run.SessionID, taken); ok {
				if err := runs.SetSession(ctx, db, run.ID, key); err != nil {
					errs = append(errs, fmt.Errorf("record session of run %d: %w", run.ID, err))
					continue
				}
			} else if age >= lim.MaxWait {
				if err := runs.Release(ctx, db, run.ID, runs.Returned, ReasonUncertain, now); err != nil {
					errs = append(errs, fmt.Errorf("release run %d: %w", run.ID, err))
					continue
				}
				closed = append(closed, Closed{RunID: run.ID, Issue: run.Issue, Reason: ReasonUncertain})
				// The row is closed: a late target must not start its agent any more.
				if err := w.Discard(ctx, run.WorkspaceID); err != nil {
					errs = append(errs, fmt.Errorf("discard launch of run %d: %w", run.ID, err))
				}
				continue
			}
		}
		if age < lim.MaxRuntime {
			continue
		}
		if err := w.Stop(ctx, run.WorkspaceID, []string{run.SessionID}); err != nil {
			errs = append(errs, fmt.Errorf("stop run %d: %w", run.ID, err))
			continue
		}
		if err := runs.Release(ctx, db, run.ID, runs.Returned, ReasonTimeout, now); err != nil {
			errs = append(errs, fmt.Errorf("release run %d: %w", run.ID, err))
			continue
		}
		closed = append(closed, Closed{RunID: run.ID, Issue: run.Issue, Reason: ReasonTimeout})
	}
	return closed, errors.Join(errs...)
}

// findSession looks up the session of an uncertain start by its session_id. A
// key that a row already holds is skipped: a resume run shares the session_id
// of the earlier run, whose forge session may still be listed.
func findSession(sessions []Session, sessionID string, taken map[string]bool) (key string, ok bool) {
	for _, s := range sessions {
		if s.SessionID == sessionID && !taken[s.Key] {
			return s.Key, true
		}
	}
	return "", false
}

// Candidate is a workspace that may be removed. InController and MergedHead
// come from the board and the PR.
type Candidate struct {
	Repo         string
	Issue        int
	InController bool
	MergedHead   string
	Workspace    Workspace
}

// Cleanup removes the workspace of a candidate when nothing is lost by it. A
// workspace it must keep, or cannot measure, is recorded as a leftover; it is
// never removed on an unmeasurable check. A running run is never touched.
//
// The order: in the controller or an open run (nothing), the workspace path,
// the sessions (a workspace forge no longer knows counts as removed), a live
// foreign session (keep), stop the own sessions, dirty, unpushed, remove.
func Cleanup(ctx context.Context, db *sql.DB, w Workplace, run Runner, c Candidate, now time.Time) (removed bool, err error) {
	if c.Workspace.ID == "" {
		return false, errors.New("cleanup: candidate has no workspace id")
	}
	if c.InController {
		return false, nil
	}
	open, err := runs.Open(ctx, db, c.Repo)
	if err != nil {
		return false, err
	}
	for _, r := range open {
		if r.Issue == c.Issue {
			return false, nil
		}
	}
	keep := func(reason string) (bool, error) {
		l := Leftover{Repo: c.Repo, Issue: c.Issue, WorkspaceID: c.Workspace.ID, Path: c.Workspace.Path, Reason: reason}
		return false, RecordLeftover(ctx, db, l, now)
	}
	// git -C "" or a relative path would measure the working directory instead.
	if !filepath.IsAbs(c.Workspace.Path) {
		return keep("not measurable: no workspace path")
	}

	sessions, err := w.Sessions(ctx, c.Workspace.ID)
	if errors.Is(err, ErrGone) {
		// Forge no longer knows the workspace: removed, as after a 404 on Remove.
		if err := w.Discard(ctx, c.Workspace.ID); err != nil {
			return keep("not measurable: discard: " + err.Error())
		}
		if err := ClearLeftover(ctx, db, c.Repo, c.Workspace.ID); err != nil {
			return true, err
		}
		return true, nil
	}
	if err != nil {
		return keep("not measurable: sessions: " + err.Error())
	}
	own, err := runs.SessionKeys(ctx, db, c.Repo, c.Issue)
	if err != nil {
		return keep("not measurable: session keys: " + err.Error())
	}
	if s, ok := ForeignLive(sessions, own); ok {
		return keep("foreign session " + s.Key)
	}

	// Stop ends the whole systemd unit of the worktree, so it runs only after
	// ForeignLive found no foreign session there. It runs before the git
	// checks: an own agent that still works cannot commit after them.
	var ids []string
	for _, s := range sessions {
		if !own[s.Key] {
			continue
		}
		if s.SessionID == "" {
			if s.Stand() == Done {
				continue
			}
			// Stop finds the agent by session id; without one it cannot be stopped.
			return keep("not measurable: own session " + s.Key + " has no session id")
		}
		ids = append(ids, s.SessionID)
	}
	if len(ids) > 0 {
		if err := w.Stop(ctx, c.Workspace.ID, ids); err != nil {
			return keep("not measurable: stop: " + err.Error())
		}
	}

	dirty, err := Dirty(ctx, run, c.Workspace.Path)
	if err != nil {
		return keep("not measurable: " + err.Error())
	}
	if dirty {
		return keep("uncommitted changes")
	}
	unpushed, err := Unpushed(ctx, run, c.Workspace.Path, c.MergedHead)
	if err != nil {
		return keep("not measurable: " + err.Error())
	}
	if unpushed {
		return keep("unpushed commits")
	}
	if err := w.Remove(ctx, c.Workspace.ID); err != nil {
		if errors.Is(err, ErrDirty) {
			return keep("forge refused: dirty worktree (409)")
		}
		if _, kerr := keep("not measurable: remove: " + err.Error()); kerr != nil {
			return false, kerr
		}
		return false, fmt.Errorf("remove workspace %s: %w", c.Workspace.ID, err)
	}
	if err := ClearLeftover(ctx, db, c.Repo, c.Workspace.ID); err != nil {
		return true, err
	}
	return true, nil
}
