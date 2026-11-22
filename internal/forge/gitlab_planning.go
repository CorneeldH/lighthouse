package forge

import (
	"context"
	neturl "net/url"
	"slices"
	"strconv"
	"strings"
)

// Planning reads the open issues of the project with their milestone and open blockers, and
// per active milestone the open issues and the closed ones. Budget, for N open issues in P
// pages and M active milestones in Q pages with R pages of closed issues: P calls for the
// issues, N calls for the links (one per open issue, the only way GitLab's REST API gives
// them), Q for the milestone list and R for the closed issues of the milestones. Open comes
// from the open issues already read. GitLab has no "not planned" close reason: every closed
// issue counts as Done and is in Closed. Any failed call, an expired context included,
// is the error, never a partial answer.
func (g *GitLab) Planning(ctx context.Context) (Planning, error) {
	items, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), neturl.Values{"state": {"opened"}})
	if err != nil {
		return Planning{}, err
	}
	out := Planning{Issues: make([]PlanIssue, 0, len(items)), Milestones: []MilestoneCount{}}
	for _, it := range items {
		is := it.issue()
		links, err := getAll[glLink](ctx, g, g.projectPath("/issues/"+strconv.Itoa(is.Number)+"/links"), nil)
		if err != nil {
			return Planning{}, err
		}
		pi := PlanIssue{Number: is.Number, Title: is.Title, URL: is.URL, Milestone: is.Milestone, Labels: it.Labels}
		if pi.Labels == nil {
			pi.Labels = []string{}
		}
		for _, l := range links {
			if l.LinkType != "is_blocked_by" || l.State != "opened" {
				continue
			}
			repo, _, _ := strings.Cut(l.References.Full, "#")
			pi.BlockedBy = append(pi.BlockedBy, Blocker{Repo: repo, Issue: l.IID, State: "OPEN"})
		}
		out.Issues = append(out.Issues, pi)
	}
	milestones, err := getAll[struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}](ctx, g, g.projectPath("/milestones"), neturl.Values{"state": {"active"}})
	if err != nil {
		return Planning{}, err
	}
	for _, m := range milestones {
		closed, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), neturl.Values{"state": {"closed"}, "milestone": {m.Title}})
		if err != nil {
			return Planning{}, err
		}
		mc := MilestoneCount{
			Milestone: Milestone{Number: m.ID, Title: m.Title, State: "open", Description: m.Description},
			Done:      len(closed), Closed: make([]ClosedIssue, 0, len(closed)),
		}
		for _, c := range closed {
			mc.Closed = append(mc.Closed, ClosedIssue{Number: c.IID, Title: c.Title, URL: c.WebURL})
		}
		slices.SortFunc(mc.Closed, func(a, b ClosedIssue) int { return a.Number - b.Number })
		for _, is := range out.Issues {
			if is.Milestone != nil && is.Milestone.Title == m.Title {
				mc.Open++
			}
		}
		out.Milestones = append(out.Milestones, mc)
	}
	return out, nil
}
