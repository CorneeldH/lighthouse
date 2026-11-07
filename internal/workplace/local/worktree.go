package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// worktree is one entry of git worktree list; Branch is "" when detached.
type worktree struct{ Path, Branch string }

// parseWorktrees reads git worktree list --porcelain. The first entry is the main checkout.
func parseWorktrees(porcelain string) []worktree {
	var out []worktree
	for line := range strings.Lines(porcelain) {
		line = strings.TrimRight(line, "\n")
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			out = append(out, worktree{Path: path})
			continue
		}
		if ref, ok := strings.CutPrefix(line, "branch "); ok && len(out) > 0 {
			out[len(out)-1].Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
	}
	return out
}

// worktrees lists the worktrees of the repo after a prune, so a worktree whose directory is
// gone no longer shows. The first entry is the main checkout.
func (a *Adapter) worktrees(ctx context.Context) ([]worktree, error) {
	if _, err := a.gitOK(ctx, a.RepoDir, "worktree", "prune"); err != nil {
		return nil, err
	}
	out, err := a.gitOK(ctx, a.RepoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	wts := parseWorktrees(out)
	if len(wts) == 0 {
		return nil, fmt.Errorf("git worktree list in %s: no main checkout", a.RepoDir)
	}
	return wts, nil
}

// resolve reads the record and its worktree. No record, or a vanished worktree whose sessions
// have no marked process: ErrGone. A vanished worktree with marked processes is returned with
// wt == nil. Marked that cannot measure is an error, never ErrGone.
func (a *Adapter) resolve(ctx context.Context, id string) (rec wsRecord, wt *worktree, err error) {
	s, err := a.store()
	if err != nil {
		return wsRecord{}, nil, err
	}
	rec, ok, err := s.workspace(id)
	if err != nil {
		return wsRecord{}, nil, err
	}
	if !ok {
		return wsRecord{}, nil, fmt.Errorf("workspace %s: no record: %w", id, workplace.ErrGone)
	}
	wts, err := a.worktrees(ctx)
	if err != nil {
		return wsRecord{}, nil, err
	}
	// The main checkout is never a workspace of the adapter.
	for _, w := range wts[1:] {
		if w.Path == rec.Path {
			return rec, &w, nil
		}
	}
	sessions, err := s.sessions(id)
	if err != nil {
		return wsRecord{}, nil, err
	}
	for _, sess := range sessions {
		pids, err := a.procs().Marked(sess.SessionID)
		if err != nil {
			return wsRecord{}, nil, fmt.Errorf("workspace %s: processes of %s: %w", id, sess.SessionID, err)
		}
		if len(pids) > 0 {
			// Not gone while the run still has processes: decide must see it alive and stop it.
			return rec, nil, nil
		}
	}
	return wsRecord{}, nil, fmt.Errorf("workspace %s: worktree %s is gone: %w", id, rec.Path, workplace.ErrGone)
}
