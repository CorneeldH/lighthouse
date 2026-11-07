package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// excludeLine keeps the worktrees of the adapter out of git status in the main checkout.
const excludeLine = "/.claude/worktrees/"

// Prepare gives the worktree of branch: the one git already has for it, or a new one under
// <RepoDir>/.claude/worktrees/<id>. A branch in the main checkout is an error: the adapter
// never works in the checkout of the user.
func (a *Adapter) Prepare(ctx context.Context, branch string) (workplace.Workspace, error) {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return workplace.Workspace{}, fmt.Errorf("local workplace: unusable branch name %q", branch)
	}
	s, err := a.store()
	if err != nil {
		return workplace.Workspace{}, err
	}
	wts, err := a.worktrees(ctx)
	if err != nil {
		return workplace.Workspace{}, err
	}
	if wts[0].Branch == branch {
		return workplace.Workspace{}, fmt.Errorf("branch %s is checked out in the main checkout %s", branch, a.RepoDir)
	}
	var ws workplace.Workspace
	created := false
	if wt, ok := onBranch(wts, branch); ok {
		ws, err = a.keep(s, wt)
	} else {
		ws, created, err = a.add(ctx, s, branch)
	}
	if err != nil {
		return workplace.Workspace{}, err
	}
	if err := workplace.FollowRemote(ctx, a.Run, gitTimeout, ws, created); err != nil {
		return workplace.Workspace{}, fmt.Errorf("workspace %s: %w", ws.ID, err)
	}
	return ws, nil
}

// onBranch finds the worktree that has branch checked out, the main checkout left out.
func onBranch(wts []worktree, branch string) (worktree, bool) {
	for _, w := range wts[1:] {
		if w.Branch == branch {
			return w, true
		}
	}
	return worktree{}, false
}

// keep returns the record of a worktree git already has. A worktree no record holds was not
// made by the adapter: it gets a fresh ID and adopted true.
func (a *Adapter) keep(s store, wt worktree) (workplace.Workspace, error) {
	recs, err := s.workspaces()
	if err != nil {
		return workplace.Workspace{}, err
	}
	for _, r := range recs {
		if r.Path == wt.Path {
			return workplace.Workspace{ID: r.ID, Path: r.Path, Branch: wt.Branch}, nil
		}
	}
	r := wsRecord{ID: newID(wt.Branch), Path: wt.Path, Branch: wt.Branch, Adopted: true, CreatedAt: a.now()}
	if err := s.putWorkspace(r); err != nil {
		return workplace.Workspace{}, err
	}
	return workplace.Workspace{ID: r.ID, Path: r.Path, Branch: r.Branch}, nil
}

// add makes a worktree for branch under .claude/worktrees/<id>. created is true when the
// branch is new here; an existing local branch can hold commits of its own and is never reset.
func (a *Adapter) add(ctx context.Context, s store, branch string) (workplace.Workspace, bool, error) {
	if _, err := a.gitOK(ctx, a.RepoDir, "fetch", "origin"); err != nil {
		return workplace.Workspace{}, false, err
	}
	id := newID(branch)
	path := filepath.Join(a.RepoDir, ".claude", "worktrees", id)
	// A record on this path is left over from a vanished worktree.
	recs, err := s.workspaces()
	if err != nil {
		return workplace.Workspace{}, false, err
	}
	for _, r := range recs {
		if r.Path == path {
			if err := s.dropWorkspace(r.ID); err != nil {
				return workplace.Workspace{}, false, err
			}
		}
	}
	if err := a.excludeWorktrees(ctx); err != nil {
		return workplace.Workspace{}, false, err
	}
	local, err := a.hasRef(ctx, "refs/heads/"+branch)
	if err != nil {
		return workplace.Workspace{}, false, err
	}
	remote, err := a.hasRef(ctx, "refs/remotes/origin/"+branch)
	if err != nil {
		return workplace.Workspace{}, false, err
	}
	var args []string
	switch {
	case local:
		args = []string{"worktree", "add", path, branch}
	case remote:
		args = []string{"worktree", "add", path, "-b", branch, "--track", "origin/" + branch}
	default:
		// Without --no-track origin/<default> would become the upstream, and a bare push fails.
		args = []string{"worktree", "add", "--no-track", path, "-b", branch, "origin/" + a.DefaultBranch}
	}
	if _, err := a.gitOK(ctx, a.RepoDir, args...); err != nil {
		return workplace.Workspace{}, false, err
	}
	// Keep the path as git gives it: that is what later lookups compare with.
	wts, err := a.worktrees(ctx)
	if err != nil {
		return workplace.Workspace{}, false, err
	}
	wt, ok := onBranch(wts, branch)
	if !ok {
		return workplace.Workspace{}, false, fmt.Errorf("git worktree add %s: branch %s not in the worktree list", path, branch)
	}
	r := wsRecord{ID: id, Path: wt.Path, Branch: branch, CreatedAt: a.now()}
	if err := s.putWorkspace(r); err != nil {
		return workplace.Workspace{}, false, err
	}
	return workplace.Workspace{ID: id, Path: wt.Path, Branch: branch}, !local, nil
}

// hasRef reports whether ref exists in the main checkout.
func (a *Adapter) hasRef(ctx context.Context, ref string) (bool, error) {
	_, code, err := a.git(ctx, a.RepoDir, "show-ref", "--verify", "--quiet", ref)
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("git show-ref %s: exit code %d", ref, code)
}

// excludeWorktrees puts excludeLine in <git-common-dir>/info/exclude once, so a repo without
// it in .gitignore keeps a clean git status in the main checkout. The file is git's, not the
// adapter's: it is appended to, never replaced, and keeps its mode.
func (a *Adapter) excludeWorktrees(ctx context.Context) error {
	common, err := a.gitOK(ctx, a.RepoDir, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(a.RepoDir, common)
	}
	path := filepath.Join(common, "info", "exclude")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for line := range strings.Lines(string(b)) {
		if strings.TrimSpace(line) == excludeLine {
			return nil
		}
	}
	add := excludeLine + "\n"
	if len(b) > 0 && b[len(b)-1] != '\n' {
		add = "\n" + add
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(add); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
