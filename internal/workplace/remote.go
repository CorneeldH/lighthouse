package workplace

import (
	"context"
	"fmt"
	"strconv"
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
// ever dropping commits of its own. timeout applies to every git command.
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
	// On another branch (main, say) a fast-forward would move that branch: only switch
	// when HEAD holds nothing that is not on origin.
	n, err := gitOK("rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	if err != nil {
		return err
	}
	own, err := strconv.Atoi(n)
	if err != nil {
		return fmt.Errorf("git rev-list --count: unreadable answer %q", n)
	}
	if own > 0 {
		return nil
	}
	return checkoutRemote(ctx, run, timeout, ws, remote, want)
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
