package report

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// Source is the part of forge.Forge a report reads.
type Source interface {
	MergedPRs(ctx context.Context, since, until time.Time) ([]forge.PRSummary, error)
	ClosedIssues(ctx context.Context, since, until time.Time) ([]forge.Issue, error)
	MilestonedIssues(ctx context.Context, since, until time.Time) ([]forge.MilestonedIssue, error)
	OpenPRsWithStatus(ctx context.Context) ([]forge.PRSummary, error)
}

// Report is the data of one period report.
type Report struct {
	Period  Period
	Repo    string
	At      time.Time
	Merged  []forge.PRSummary // by MergedAt
	Closed  []forge.Issue     // by ClosedAt
	OpenPRs []forge.PRSummary // by number
	Roadmap []RoadmapGroup    // by milestone title
}

// RoadmapGroup is the issues that got one milestone in the period.
type RoadmapGroup struct {
	Milestone forge.Milestone
	Issues    []forge.Issue // by number
}

// Status is the state of an open pull request; the first matching one wins.
type Status string

const (
	StatusDraft    Status = "draft"
	StatusConflict Status = "conflict"
	StatusRed      Status = "red"
	StatusBusy     Status = "busy"
	StatusGreen    Status = "green"
	StatusNoChecks Status = "no-checks"
)

// PRStatus classifies an open pull request: draft, then conflict, then the checks.
func PRStatus(pr forge.PRSummary) Status {
	switch {
	case pr.Draft:
		return StatusDraft
	case pr.Mergeable == "CONFLICTING":
		return StatusConflict
	}
	switch pr.Checks {
	case "FAILURE", "ERROR":
		return StatusRed
	case "PENDING", "EXPECTED":
		return StatusBusy
	case "SUCCESS":
		return StatusGreen
	}
	return StatusNoChecks
}

// Build reads the four sources one after another. The first failing source
// fails the build with an empty Report: there is no half report.
func Build(ctx context.Context, src Source, repo string, period Period, now time.Time) (Report, error) {
	merged, err := src.MergedPRs(ctx, period.Since, period.Until)
	if err != nil {
		return Report{}, fmt.Errorf("report: merged: %w", err)
	}
	closed, err := src.ClosedIssues(ctx, period.Since, period.Until)
	if err != nil {
		return Report{}, fmt.Errorf("report: closed: %w", err)
	}
	milestoned, err := src.MilestonedIssues(ctx, period.Since, period.Until)
	if err != nil {
		return Report{}, fmt.Errorf("report: milestoned: %w", err)
	}
	open, err := src.OpenPRsWithStatus(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("report: open: %w", err)
	}

	sort.SliceStable(merged, func(i, j int) bool { return merged[i].MergedAt.Before(merged[j].MergedAt) })
	sort.SliceStable(closed, func(i, j int) bool { return closed[i].ClosedAt.Before(closed[j].ClosedAt) })
	sort.SliceStable(open, func(i, j int) bool { return open[i].Number < open[j].Number })

	return Report{
		Period:  period,
		Repo:    repo,
		At:      now,
		Merged:  merged,
		Closed:  closed,
		OpenPRs: open,
		Roadmap: groupRoadmap(milestoned),
	}, nil
}

// groupRoadmap groups issues by milestone number, groups by title, issues by number.
func groupRoadmap(in []forge.MilestonedIssue) []RoadmapGroup {
	var groups []RoadmapGroup
	index := map[int]int{}
	for _, mi := range in {
		i, ok := index[mi.Milestone.Number]
		if !ok {
			i = len(groups)
			index[mi.Milestone.Number] = i
			groups = append(groups, RoadmapGroup{Milestone: mi.Milestone})
		}
		groups[i].Issues = append(groups[i].Issues, mi.Issue)
	}
	for _, g := range groups {
		sort.SliceStable(g.Issues, func(i, j int) bool { return g.Issues[i].Number < g.Issues[j].Number })
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Milestone.Title < groups[j].Milestone.Title })
	return groups
}
