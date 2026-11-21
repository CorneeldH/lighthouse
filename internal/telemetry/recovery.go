// This file carries the pure measurement of recovery after a merge: which
// fixes, reverts and bug issues follow a PR within RecoveryWindow. It reads
// through RecoverySource and takes the clock as an argument.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RecoveryWindow is the span after the merge in which repairs count.
const RecoveryWindow = 7 * 24 * time.Hour

// RecoveryStatus is the outcome of a measurement.
type RecoveryStatus string

const (
	// RecoveryOpen: the window has not ended yet.
	RecoveryOpen RecoveryStatus = "open"
	// RecoveryRecovered: the window ended with at least one item.
	RecoveryRecovered RecoveryStatus = "recovered"
	// RecoveryNone: the window ended without an item.
	RecoveryNone RecoveryStatus = "none"
)

// Commit is a default-branch commit with the files it touched.
type Commit struct {
	SHA     string
	Subject string
	At      time.Time
	Files   []string
}

// BugIssue is an issue that may report a defect caused by a PR.
type BugIssue struct {
	Number    int
	Title     string
	Labels    []string
	CreatedAt time.Time
	Body      string
}

// RecoverySource is what MeasureRecovery needs from a repo and its forge.
type RecoverySource interface {
	// MergeCommit finds the squash commit of pr on the default branch.
	MergeCommit(ctx context.Context, pr int) (Commit, error)
	// CommitsAfter lists default-branch commits with At in (after, until], oldest first.
	CommitsAfter(ctx context.Context, after, until time.Time) ([]Commit, error)
	// IssuesMentioning lists issues whose title or body may mention #pr.
	IssuesMentioning(ctx context.Context, pr int) ([]BugIssue, error)
}

// RecoveryItem is one repair found in the window.
type RecoveryItem struct {
	Kind string // "fix", "revert" or "bug_issue"
	Ref  string // commit SHA, or the issue number
	At   time.Time
}

// Recovery is the measurement of one PR.
type Recovery struct {
	PR        int
	MergedAt  time.Time
	WindowEnd time.Time
	Status    RecoveryStatus
	Items     []RecoveryItem
}

// ErrNoMergeCommit means the PR has no squash commit on the default branch.
var ErrNoMergeCommit = errors.New("telemetry: no merge commit found for the PR")

// MeasureRecovery measures the repairs that follow pr within (mergedAt,
// mergedAt+RecoveryWindow]. The status is open while now is before the window
// end, also when items exist; after that it is recovered or none.
func MeasureRecovery(ctx context.Context, src RecoverySource, pr int, mergedAt, now time.Time) (Recovery, error) {
	end := mergedAt.Add(RecoveryWindow)
	rec := Recovery{PR: pr, MergedAt: mergedAt, WindowEnd: end}

	merge, err := src.MergeCommit(ctx, pr)
	if err != nil {
		return Recovery{}, fmt.Errorf("telemetry: merge commit of #%d: %w", pr, err)
	}
	commits, err := src.CommitsAfter(ctx, mergedAt, end)
	if err != nil {
		return Recovery{}, fmt.Errorf("telemetry: commits after #%d: %w", pr, err)
	}
	issues, err := src.IssuesMentioning(ctx, pr)
	if err != nil {
		return Recovery{}, fmt.Errorf("telemetry: issues mentioning #%d: %w", pr, err)
	}

	touched := make(map[string]struct{}, len(merge.Files))
	for _, f := range merge.Files {
		touched[f] = struct{}{}
	}
	for _, c := range commits {
		if c.SHA == merge.SHA || !inWindow(c.At, mergedAt, end) {
			continue
		}
		kind := classifySubject(c.Subject)
		if kind == "" || !overlaps(c.Files, touched) {
			continue
		}
		rec.Items = append(rec.Items, RecoveryItem{Kind: kind, Ref: c.SHA, At: c.At})
	}
	for _, is := range issues {
		if inWindow(is.CreatedAt, mergedAt, end) && hasBugLabel(is.Labels) && mentions(is, pr) {
			rec.Items = append(rec.Items, RecoveryItem{Kind: "bug_issue", Ref: strconv.Itoa(is.Number), At: is.CreatedAt})
		}
	}
	slices.SortFunc(rec.Items, func(a, b RecoveryItem) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.Ref, b.Ref)
	})

	switch {
	case now.Before(end):
		rec.Status = RecoveryOpen
	case len(rec.Items) > 0:
		rec.Status = RecoveryRecovered
	default:
		rec.Status = RecoveryNone
	}
	return rec, nil
}

// inWindow reports whether t is in (start, end].
func inWindow(t, start, end time.Time) bool {
	return t.After(start) && !t.After(end)
}

func overlaps(files []string, set map[string]struct{}) bool {
	for _, f := range files {
		if _, ok := set[f]; ok {
			return true
		}
	}
	return false
}

func hasBugLabel(labels []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return strings.EqualFold(l, "bug") })
}

// mentions reports whether the title or body holds #pr as a whole number.
func mentions(is BugIssue, pr int) bool {
	re := regexp.MustCompile(`#` + strconv.Itoa(pr) + `\b`)
	return re.MatchString(is.Title) || re.MatchString(is.Body)
}

var (
	fixSubject    = regexp.MustCompile(`(?i)^fix(\([^)]*\))?!?:`)
	revertSubject = regexp.MustCompile(`(?i)^revert(\([^)]*\))?!?:|^Revert "`)
)

// classifySubject returns "fix", "revert" or "" for a commit subject.
func classifySubject(s string) string {
	switch {
	case revertSubject.MatchString(s):
		return "revert"
	case fixSubject.MatchString(s):
		return "fix"
	}
	return ""
}
