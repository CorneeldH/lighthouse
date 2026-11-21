// This file carries the forge side of the recovery measurement: one search
// per run, filtered per PR in memory.
package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// IssueSearcher is the part of a forge that ForgeIssues uses.
type IssueSearcher interface {
	SearchIssues(ctx context.Context, text string) ([]forge.Issue, error)
}

// ForgeIssues returns an issue source for GitSource.Issues. The search API
// allows few requests a minute, so the first call does the one search of the
// run, "label:bug created:>=<since>" (since is the oldest merge time of the
// PRs to measure, as a UTC date), and every call filters that result on #<pr>.
// A search error is kept: every call returns it.
func ForgeIssues(f IssueSearcher, since time.Time) func(ctx context.Context, pr int) ([]BugIssue, error) {
	var (
		once   sync.Once
		issues []forge.Issue
		err    error
	)
	return func(ctx context.Context, pr int) ([]BugIssue, error) {
		once.Do(func() {
			issues, err = f.SearchIssues(ctx, "label:bug created:>="+since.UTC().Format("2006-01-02"))
		})
		if err != nil {
			return nil, fmt.Errorf("telemetry: search bug issues: %w", err)
		}
		var out []BugIssue
		for _, is := range issues {
			b := BugIssue{Number: is.Number, Title: is.Title, Labels: is.Labels, CreatedAt: is.CreatedAt, Body: is.Body}
			if mentions(b, pr) {
				out = append(out, b)
			}
		}
		return out, nil
	}
}
