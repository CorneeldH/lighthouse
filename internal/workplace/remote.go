package workplace

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// remoteGit runs one git command in dir and returns trimmed stdout and the exit code. A
// command that could not run or timed out is an error, never a negative answer.
func remoteGit(ctx context.Context, run Runner, timeout time.Duration, dir string, args ...string) (string, int, error) {
	out, code, err := run(ctx, timeout, "git", append([]string{"-C", dir}, args...)...)
	if err != nil {
		return "", code, fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(out), code, nil
}

func remoteGitOK(ctx context.Context, run Runner, timeout time.Duration, dir string, args ...string) (string, error) {
	out, code, err := remoteGit(ctx, run, timeout, dir, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", strings.Join(args, " "), code)
	}
	return out, nil
}

// FollowRemote moves the workspace onto origin/<branch> when that branch exists, without
// ever dropping commits of its own. A found workspace with own work (commits origin lacks,
// or a dirty work tree) stays where it is and gives an *OwnWorkError. timeout applies to
// every git command.
func FollowRemote(ctx context.Context, run Runner, timeout time.Duration, ws Workspace, created bool) error {
	gitOK := func(args ...string) (string, error) {
		return remoteGitOK(ctx, run, timeout, ws.Path, args...)
	}
	ref := "refs/heads/" + ws.Branch
	remote := "origin/" + ws.Branch
	out, err := gitOK("ls-remote", "origin", ref)
	if err != nil {
		return err
	}
	if out == "" {
		return nil
	}
	if _, err := gitOK("fetch", "origin", "--", ws.Branch); err != nil {
		return err
	}
	head, err := gitOK("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	want, err := gitOK("rev-parse", remote)
	if err != nil {
		return err
	}
	if head == want {
		return nil
	}
	if created {
		// Forge puts a new workspace on an existing remote branch at origin/HEAD, not at the
		// branch (measured in paia, controller-forge-verification.md row (i)).
		return checkoutRemote(ctx, run, timeout, ws, remote, want)
	}
	// A found workspace can hold work of an earlier run: never checkout -B over it.
	cur, err := gitOK("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if cur == ws.Branch {
		_, code, err := remoteGit(ctx, run, timeout, ws.Path, "merge-base", "--is-ancestor", "HEAD", remote)
		if err != nil {
			return err
		}
		switch code {
		case 0:
			_, err := gitOK("merge", "--ff-only", remote)
			return err
		case 1:
		default:
			return fmt.Errorf("git merge-base --is-ancestor: exit code %d", code)
		}
	}
	// On another branch (main, say) a fast-forward would move that branch, and after a
	// rebase and force-push of the branch HEAD is no ancestor: only switch when HEAD holds
	// nothing origin lacks. Otherwise the run must not start silently on the old HEAD.
	own, err := ownCommits(ctx, run, timeout, ws.Path, remote)
	if err != nil {
		return err
	}
	// Untracked files count, as in Dirty: checkout -B refuses one the branch tracks.
	status, err := gitOK("status", "--porcelain")
	if err != nil {
		return err
	}
	if own > 0 || status != "" {
		return &OwnWorkError{Workspace: ws, Commits: own, Dirty: status != ""}
	}
	return checkoutRemote(ctx, run, timeout, ws, remote, want)
}

// ownCommits counts the commits of HEAD that are on no origin ref and have no patch-equal
// commit on remote: a rebase gives the commits of an earlier run new SHAs on origin, and
// those are no own work. A merge commit git cherry does not list counts as own.
func ownCommits(ctx context.Context, run Runner, timeout time.Duration, dir, remote string) (int, error) {
	out, err := remoteGitOK(ctx, run, timeout, dir, "rev-list", "HEAD", "--not", "--remotes=origin")
	if err != nil || out == "" {
		return 0, err
	}
	cherry, err := remoteGitOK(ctx, run, timeout, dir, "cherry", remote, "HEAD")
	if err != nil {
		return 0, err
	}
	onRemote := map[string]bool{}
	for line := range strings.Lines(cherry) {
		if sha, ok := strings.CutPrefix(strings.TrimSpace(line), "- "); ok {
			onRemote[sha] = true
		}
	}
	own := 0
	for sha := range strings.Lines(out) {
		if !onRemote[strings.TrimSpace(sha)] {
			own++
		}
	}
	return own, nil
}

func checkoutRemote(ctx context.Context, run Runner, timeout time.Duration, ws Workspace, remote, want string) error {
	if _, err := remoteGitOK(ctx, run, timeout, ws.Path, "checkout", "-B", ws.Branch, "--track", remote); err != nil {
		return err
	}
	head, err := remoteGitOK(ctx, run, timeout, ws.Path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != want {
		return fmt.Errorf("workspace %s: HEAD %s is not %s after checkout", ws.ID, head, remote)
	}
	return nil
}
