// This file carries the git side of the recovery measurement: a RecoverySource
// that reads the default branch through the git binary.
package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/roborev"
)

// GitSource implements RecoverySource on a local clone. Issues supplies the
// forge side; without it IssuesMentioning returns nothing.
type GitSource struct {
	Dir    string // the repository
	Branch string // the default branch ref to read, e.g. origin/main
	Issues func(ctx context.Context, pr int) ([]BugIssue, error)
}

// gitLogFormat starts every commit with \x01 so the parser can tell a commit
// from a file name; fields are separated by \x1f. With -z, git ends the format
// and every file name with NUL.
const gitLogFormat = "--format=%x01%H%x1f%cI%x1f%s"

// MergeCommit finds the newest commit on Branch whose subject ends in (#pr).
func (g GitSource) MergeCommit(ctx context.Context, pr int) (Commit, error) {
	commits, err := g.log(ctx)
	if err != nil {
		return Commit{}, err
	}
	re := regexp.MustCompile(`\(#` + strconv.Itoa(pr) + `\)$`)
	// commits are oldest first; the newest match wins.
	for _, c := range slices.Backward(commits) {
		if re.MatchString(c.Subject) {
			return c, nil
		}
	}
	return Commit{}, ErrNoMergeCommit
}

// CommitsAfter lists commits on Branch with committer time in (after, until], oldest first.
func (g GitSource) CommitsAfter(ctx context.Context, after, until time.Time) ([]Commit, error) {
	commits, err := g.log(ctx)
	if err != nil {
		return nil, err
	}
	var out []Commit
	for _, c := range commits {
		if inWindow(c.At, after, until) {
			out = append(out, c)
		}
	}
	return out, nil
}

// IssuesMentioning delegates to Issues.
func (g GitSource) IssuesMentioning(ctx context.Context, pr int) ([]BugIssue, error) {
	if g.Issues == nil {
		return nil, nil
	}
	return g.Issues(ctx, pr)
}

// log returns every commit on Branch with its files, oldest first.
func (g GitSource) log(ctx context.Context) ([]Commit, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", g.Dir, "-c", "core.quotepath=false",
		"log", "--reverse", "-z", "--name-only", gitLogFormat, g.Branch, "--")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("telemetry: git log %s: %w: %s", g.Branch, err, strings.TrimSpace(stderr.String()))
	}
	return parseGitLog(out)
}

// parseGitLog parses the NUL separated output of gitLogFormat with --name-only.
func parseGitLog(out []byte) ([]Commit, error) {
	var commits []Commit
	for _, tok := range strings.Split(string(out), "\x00") {
		tok = strings.TrimPrefix(tok, "\n")
		switch {
		case tok == "":
		case strings.HasPrefix(tok, "\x01"):
			fields := strings.SplitN(tok[1:], "\x1f", 3)
			if len(fields) != 3 {
				return nil, fmt.Errorf("telemetry: unexpected git log record %q", tok)
			}
			at, err := time.Parse(time.RFC3339, fields[1])
			if err != nil {
				return nil, fmt.Errorf("telemetry: commit time %q: %w", fields[1], err)
			}
			commits = append(commits, Commit{SHA: fields[0], At: at, Subject: fields[2]})
		case len(commits) > 0:
			last := &commits[len(commits)-1]
			last.Files = append(last.Files, tok)
		default:
			return nil, fmt.Errorf("telemetry: file name before first commit in git log: %q", tok)
		}
	}
	return commits, nil
}

// CodeLines counts the code lines the squash commit of pr added and removed,
// by the rules of roborev.CountCodeLines. ErrNoMergeCommit when there is none.
func (g GitSource) CodeLines(ctx context.Context, pr int) (int, error) {
	c, err := g.MergeCommit(ctx, pr)
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", g.Dir, "show", "--numstat", "--no-renames", "-z", "--format=", c.SHA)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("telemetry: git show %s: %w: %s", c.SHA, err, strings.TrimSpace(stderr.String()))
	}
	return roborev.CountCodeLines(string(out)), nil
}
