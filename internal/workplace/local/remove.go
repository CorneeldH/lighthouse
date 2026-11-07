package local

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

var safeSession = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// Remove deletes the worktree and every record of the workspace; the local branch stays.
// A workspace without a record is already removed. A vanished or adopted worktree is never
// deleted from disk (an adopted one is the user's): only its records go, after the processes
// of its sessions ended; when that fails, the error is returned and the records stay. A
// worktree with changes is ErrDirty; any other refusal of git is a plain error.
func (a *Adapter) Remove(ctx context.Context, workspaceID string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	s, err := a.store()
	if err != nil {
		return err
	}
	rec, wt, err := a.resolve(ctx, workspaceID)
	if errors.Is(err, workplace.ErrGone) {
		return s.dropWorkspace(workspaceID) // no record, or a vanished worktree without processes
	}
	if err != nil {
		return err
	}
	// Cleanup skips a session that is done, yet its dev server may still carry the mark. The
	// session records are the only way to find it, so end the processes before they go.
	sids, err := a.sessionIDs(s, workspaceID)
	if err != nil {
		return err
	}
	if wt == nil || rec.Adopted {
		if err := a.end(ctx, workspaceID, sids); err != nil {
			return fmt.Errorf("remove %s: %w", workspaceID, err)
		}
		return s.dropWorkspace(workspaceID)
	}
	dirty, err := workplace.Dirty(ctx, a.Run, rec.Path)
	if err != nil {
		return fmt.Errorf("remove %s: %w", workspaceID, err)
	}
	if dirty {
		return fmt.Errorf("remove %s: worktree %s has changes: %w", workspaceID, rec.Path, workplace.ErrDirty)
	}
	// After the Dirty check, so a dirty worktree signals nothing; before the removal, so a
	// process that cannot be ended leaves the worktree and the records in place.
	if err := a.end(ctx, workspaceID, sids); err != nil {
		return fmt.Errorf("remove %s: %w", workspaceID, err)
	}
	if _, err := a.gitOK(ctx, a.RepoDir, "worktree", "remove", rec.Path); err != nil {
		return fmt.Errorf("remove %s: %w", workspaceID, err)
	}
	return s.dropWorkspace(workspaceID)
}

func (a *Adapter) sessionIDs(s store, workspaceID string) ([]string, error) {
	recs, err := s.sessions(workspaceID)
	if err != nil {
		return nil, err
	}
	sids := make([]string, 0, len(recs))
	for _, r := range recs {
		sids = append(sids, r.SessionID)
	}
	return sids, nil
}

// Discard forgets the launches whose agent never appeared: the session records without a pid
// file, after ending the processes that carry their mark. When the worktree is gone and no process is left, every record of the workspace goes.
// A vanished worktree whose run still has processes keeps its records. Nothing to forget is
// no error.
func (a *Adapter) Discard(ctx context.Context, workspaceID string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	s, err := a.store()
	if err != nil {
		return err
	}
	_, wt, err := a.resolve(ctx, workspaceID)
	if errors.Is(err, workplace.ErrGone) {
		return s.dropWorkspace(workspaceID)
	}
	if err != nil {
		return err
	}
	if wt == nil {
		return nil
	}
	recs, err := s.sessions(workspaceID)
	if err != nil {
		return err
	}
	for _, r := range recs {
		_, _, ok, err := s.pid(workspaceID, r.Key)
		if err != nil {
			return err
		}
		if !ok {
			// The record is the only way to find processes with its mark: end them first.
			if err := a.end(ctx, workspaceID, []string{r.SessionID}); err != nil {
				return fmt.Errorf("discard %s: %w", workspaceID, err)
			}
			if err := s.dropSession(workspaceID, r.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

// Transcript reads the transcript of session sessionID from the project directory of the
// worktree under Home, as workplace.ReadTranscript does. A session id outside
// [A-Za-z0-9-] is refused before anything is read.
func (a *Adapter) Transcript(ctx context.Context, workspaceID, sessionID string) (workplace.Transcript, error) {
	if err := checkLocal(workspaceID); err != nil {
		return workplace.Transcript{}, err
	}
	if !safeSession.MatchString(sessionID) {
		return workplace.Transcript{}, fmt.Errorf("transcript in workspace %s: unsafe session id %q", workspaceID, sessionID)
	}
	rec, _, err := a.resolve(ctx, workspaceID)
	if err != nil {
		return workplace.Transcript{}, err
	}
	dir, err := workplace.HostTranscriptDir(a.Home, workspaceID, rec.Path)
	if err != nil {
		return workplace.Transcript{}, err
	}
	return workplace.ReadTranscript(dir, sessionID)
}
