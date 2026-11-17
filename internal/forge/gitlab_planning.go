package forge

import (
	"context"
	neturl "net/url"
	"strconv"
	"strings"
)

// Planning reads the open issues of the project with their milestone and open blockers, and
// per active milestone the number of open and closed issues. Budget, for N open issues in P
// pages and M active milestones in Q pages: P calls for the issues, N calls for the links
// (one per open issue, the only way GitLab's REST API gives them), Q for the milestone list
// and M for issues_statistics. No closed issue is listed. GitLab has no "not planned" close
// reason: every closed issue counts as Done. Any failed call, an expired context included,
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
		pi := PlanIssue{Number: is.Number, Title: is.Title, URL: is.URL, Milestone: is.Milestone}
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
		var st struct {
			Statistics struct {
				Counts struct {
					Opened int `json:"opened"`
					Closed int `json:"closed"`
				} `json:"counts"`
			} `json:"statistics"`
		}
		if _, err := g.get(ctx, g.projectPath("/issues_statistics"), neturl.Values{"milestone": {m.Title}}, &st); err != nil {
			return Planning{}, err
		}
		out.Milestones = append(out.Milestones, MilestoneCount{
			Milestone: Milestone{Number: m.ID, Title: m.Title, State: "open", Description: m.Description},
			Open:      st.Statistics.Counts.Opened, Done: st.Statistics.Counts.Closed,
		})
	}
	return out, nil
}
