package workplace

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const gitTimeout = 30 * time.Second

// runGit runs git and turns a non-zero exit into an error: an unmeasurable
// worktree is never reported as clean.
func runGit(ctx context.Context, run Runner, args ...string) (string, error) {
	out, code, err := run(ctx, gitTimeout, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[len(args)-1], err)
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", strings.Join(args, " "), code)
	}
	return out, nil
}

// Dirty reports uncommitted changes, untracked files included.
// --no-optional-locks keeps status from refreshing the index of a worktree
// in which the user may be committing.
func Dirty(ctx context.Context, run Runner, path string) (bool, error) {
	out, err := runGit(ctx, run, "--no-optional-locks", "-C", path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Unpushed reports commits that exist on no origin ref. A branch that was never
// pushed counts. mergedHead is the head SHA of the issue's merged PR, or ""
// when unknown: every PR is squash-merged and GitHub deletes the head branch,
// so after fetch --prune no commit of the branch is on a remote ref any more.
// Without this exception every finished workspace would stay.
func Unpushed(ctx context.Context, run Runner, path, mergedHead string) (bool, error) {
	if mergedHead != "" {
		merged, err := inMergedPR(ctx, run, path, mergedHead)
		if err != nil {
			return false, err
		}
		if merged {
			return false, nil
		}
	}
	out, err := runGit(ctx, run, "-C", path, "rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	if err != nil {
		return false, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return false, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	return n > 0, nil
}

// inMergedPR reports whether HEAD is mergedHead or one of its ancestors. Those commits are in
// the merged PR, also when the PR got commits from elsewhere after the last one of this
// workspace. A mergedHead this clone lacks is fetched first: GitHub serves it through
// refs/pull/N/head. When that fails the answer is an error, never a guess.
func inMergedPR(ctx context.Context, run Runner, path, mergedHead string) (bool, error) {
	head, err := runGit(ctx, run, "-C", path, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(head) == mergedHead {
		return true, nil
	}
	_, code, err := run(ctx, gitTimeout, "git", "-C", path, "cat-file", "-e", mergedHead+"^{commit}")
	if err != nil {
		return false, fmt.Errorf("git cat-file: %w", err)
	}
	if code != 0 {
		if _, err := runGit(ctx, run, "-C", path, "fetch", "--no-tags", "origin", mergedHead); err != nil {
			return false, fmt.Errorf("merged PR head %s: %w", mergedHead, err)
		}
	}
	_, code, err = run(ctx, gitTimeout, "git", "-C", path, "merge-base", "--is-ancestor", "HEAD", mergedHead)
	if err != nil {
		return false, fmt.Errorf("git merge-base: %w", err)
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("git merge-base --is-ancestor: exit code %d", code)
	}
}
