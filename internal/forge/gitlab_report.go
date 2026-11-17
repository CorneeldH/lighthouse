package forge

import (
	"context"
	"time"
)

// OpenPRsWithStatus is not supported on GitLab yet.
func (g *GitLab) OpenPRsWithStatus(context.Context) ([]PRSummary, error) {
	return nil, ErrReportUnsupported
}

// MergedPRs is not supported on GitLab yet.
func (g *GitLab) MergedPRs(context.Context, time.Time, time.Time) ([]PRSummary, error) {
	return nil, ErrReportUnsupported
}

// ClosedIssues is not supported on GitLab yet.
func (g *GitLab) ClosedIssues(context.Context, time.Time, time.Time) ([]Issue, error) {
	return nil, ErrReportUnsupported
}

// MilestonedIssues is not supported on GitLab yet.
func (g *GitLab) MilestonedIssues(context.Context, time.Time, time.Time) ([]MilestonedIssue, error) {
	return nil, ErrReportUnsupported
}
