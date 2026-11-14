// Package bashwrites is the Go port of the former pack/core/lib/bash_writes.py: what a Bash call
// wrote, found from the git state, and the gates that judge it. This part holds the git readers,
// the roots of a command and the parsers.
package bashwrites

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// GitError is a failed git read: a non-zero exit, a timeout, or a process that does not start.
// Code is -1 on a timeout or a start failure (Python None); Msg is the stderr text of git, or the
// fixed text `git read past the deadline` on a timeout.
type GitError struct {
	Root    string
	Code    int
	Timeout bool
	Msg     string
}

func (e *GitError) Error() string { return e.Msg }

// minGitTimeout keeps an already elapsed deadline from giving git no time at all.
const minGitTimeout = 100 * time.Millisecond

// Git runs `git --no-optional-locks -C root <args>` and returns stdout. Every non-zero exit,
// timeout or start failure is a *GitError. The timeout is what remains of deadline, at least 100 ms.
func Git(root string, deadline time.Time, args ...string) ([]byte, error) {
	timeout := max(minGitTimeout, time.Until(deadline))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", root}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, &GitError{Root: root, Code: -1, Timeout: true, Msg: "git read past the deadline"}
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, &GitError{Root: root, Code: exit.ExitCode(), Msg: strings.TrimSpace(decodeReplace(stderr.Bytes()))}
		}
		return nil, &GitError{Root: root, Code: -1, Msg: err.Error()}
	}
	return stdout.Bytes(), nil
}

// decodeReplace is Python's `bytes.decode(errors="replace")`: every invalid byte becomes U+FFFD.
func decodeReplace(b []byte) string { return string([]rune(string(b))) }

// HeadOf is the current HEAD sha, or "unborn" in a repo without commits: `rev-parse --verify -q
// HEAD` failing with exactly code 1 and `symbolic-ref -q HEAD` succeeding. Every other failure is
// a broken `.git` and is returned.
func HeadOf(root string, deadline time.Time) (string, error) {
	out, err := Git(root, deadline, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		if ge, ok := err.(*GitError); ok && ge.Code == 1 {
			if _, err := Git(root, deadline, "symbolic-ref", "-q", "HEAD"); err != nil {
				return "", err
			}
			return "unborn", nil
		}
		return "", err
	}
	return strings.TrimSpace(decodeReplace(out)), nil
}

// ReadReflog is `git reflog --date=unix --format='%H%x1f%gd%x1f%gs'`, parsed. A line that does not
// parse is a *GitError, so the caller reports it as COULD NOT CHECK.
func ReadReflog(root string, deadline time.Time) ([]ReflogEntry, error) {
	data, err := Git(root, deadline, "reflog", "--date=unix", "--format=%H%x1f%gd%x1f%gs")
	if err != nil {
		return nil, err
	}
	entries, err := ParseReflog(decodeReplace(data))
	if err != nil {
		return nil, &GitError{Root: root, Code: -1, Msg: err.Error()}
	}
	return entries, nil
}

// DiffCommit is what sha changed, repo-relative. An ordinary commit uses --root (a root commit
// also gives a diff); a merge uses the combined diff (-c), which names only paths that differ from
// every parent.
func DiffCommit(root, sha string, merge bool, deadline time.Time) (changed, deleted map[string]bool, err error) {
	mode := "--root"
	if merge {
		mode = "-c"
	}
	data, err := Git(root, deadline, "diff-tree", "-z", "-r", "--no-commit-id", "--name-status", "--no-renames", mode, sha)
	if err != nil {
		return nil, nil, err
	}
	changed, deleted = ParseDiffTree(data)
	return changed, deleted, nil
}
