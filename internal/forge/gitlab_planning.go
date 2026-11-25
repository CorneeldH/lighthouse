package forge

import (
	"context"
	neturl "net/url"
	"slices"
	"strconv"
	"strings"
)

// PrioInputs reads the open issues of the project with their milestone, and the active
// milestones with their description: one call per page of each, no links and no closed issues.
// A milestone's Number is its global id, as in Planning.
func (g *GitLab) PrioInputs(ctx context.Context) (PrioInputs, error) {
	items, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), neturl.Values{"state": {"opened"}})
	if err != nil {
		return PrioInputs{}, err
	}
	out := PrioInputs{Issues: make(map[int]int, len(items)), Milestones: []Milestone{}}
	for _, it := range items {
		is := it.issue()
		out.Issues[is.Number] = 0
		if is.Milestone != nil {
			out.Issues[is.Number] = is.Milestone.Number
		}
	}
	milestones, err := getAll[glPlanMilestone](ctx, g, g.projectPath("/milestones"), neturl.Values{"state": {"active"}})
	if err != nil {
		return PrioInputs{}, err
	}
	for _, m := range milestones {
		out.Milestones = append(out.Milestones, Milestone{Number: m.ID, Title: m.Title, State: "open", Description: m.Description, URL: m.WebURL})
	}
	return out, nil
}

// glPlanMilestone is an active milestone as Planning and PrioInputs read it.
type glPlanMilestone struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	WebURL      string `json:"web_url"`
}

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
	milestones, err := getAll[glPlanMilestone](ctx, g, g.projectPath("/milestones"), neturl.Values{"state": {"active"}})
	if err != nil {
		return Planning{}, err
	}
	for _, m := range milestones {
		closed, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), neturl.Values{"state": {"closed"}, "milestone": {m.Title}})
		if err != nil {
			return Planning{}, err
		}
		mc := MilestoneCount{
			Milestone: Milestone{Number: m.ID, Title: m.Title, State: "open", Description: m.Description, URL: m.WebURL},
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
