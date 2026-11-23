// Package telemetry holds the data model logic for run and session telemetry.
// This file carries the pure rule that links an interactive session to an
// issue trajectory; it uses no file, database or clock.
package telemetry

import (
	"regexp"
	"strconv"
)

// Link bases and reasons, as stored in the database.
const (
	BasisBranch     = "branch"
	BasisTranscript = "transcript"

	ReasonMainOnly      = "main_only"
	ReasonNoIssueBranch = "no_issue_branch"
	ReasonNoIssueNumber = "no_issue_number"
)

var issueBranchRe = regexp.MustCompile(`^issue-(\d+)(-|$)`)

// IssueFromBranch extracts the issue number from a branch named
// issue-<N> or issue-<N>-<slug>. It reports false for any other branch
// and for a number below 1.
func IssueFromBranch(branch string) (int, bool) {
	m := issueBranchRe.FindStringSubmatch(branch)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// Line is one transcript line: its branch and the issue number the writer
// extracted from it (0 means none).
type Line struct {
	Branch string
	Issue  int
}

// LinkResult is the outcome of Link. With Issue > 0, Basis is "branch" or
// "transcript" and Reason is empty; otherwise Reason says why there is no
// trajectory. Branch is the branch of the last line of the winning issue,
// and empty for basis "transcript".
type LinkResult struct {
	Issue  int
	Basis  string
	Branch string
	Reason string
}

// tally counts the lines of one issue.
type tally struct {
	count  int
	last   int // index of the last occurrence
	branch string
}

func (t *tally) add(pos int, branch string) {
	t.count++
	t.last = pos
	t.branch = branch
}

// winner picks the issue with the most lines; a tie goes to the issue that
// occurs last. It reports false for an empty map.
func winner(tallies map[int]*tally) (int, *tally, bool) {
	best, bestIssue := (*tally)(nil), 0
	for issue, t := range tallies {
		if best == nil || t.count > best.count || (t.count == best.count && t.last > best.last) {
			best, bestIssue = t, issue
		}
	}
	return bestIssue, best, best != nil
}

// Link links a session to an issue from its transcript lines.
//
//  1. Lines on defaultBranch or with an empty branch do not count for the
//     branch tally.
//  2. The remaining lines are counted per issue number taken from the branch
//     name; the issue with most lines wins (tie: last occurring), basis
//     "branch".
//  3. Without an issue branch, the issue number most often found in the
//     transcript wins (all lines count; tie: last seen), basis "transcript".
//  4. Otherwise there is no trajectory, with a reason.
func Link(defaultBranch string, lines []Line) LinkResult {
	byBranch := map[int]*tally{}
	byNumber := map[int]*tally{}
	var sawDefault, sawOther bool

	for i, l := range lines {
		if l.Issue > 0 {
			t := byNumber[l.Issue]
			if t == nil {
				t = &tally{}
				byNumber[l.Issue] = t
			}
			t.add(i, "")
		}
		if l.Branch == "" {
			continue
		}
		if l.Branch == defaultBranch {
			sawDefault = true
			continue
		}
		sawOther = true
		if n, ok := IssueFromBranch(l.Branch); ok {
			t := byBranch[n]
			if t == nil {
				t = &tally{}
				byBranch[n] = t
			}
			t.add(i, l.Branch)
		}
	}

	if issue, t, ok := winner(byBranch); ok {
		return LinkResult{Issue: issue, Basis: BasisBranch, Branch: t.branch}
	}
	if issue, _, ok := winner(byNumber); ok {
		return LinkResult{Issue: issue, Basis: BasisTranscript}
	}
	switch {
	case sawOther:
		return LinkResult{Reason: ReasonNoIssueBranch}
	case sawDefault:
		return LinkResult{Reason: ReasonMainOnly}
	default:
		return LinkResult{Reason: ReasonNoIssueNumber}
	}
}
