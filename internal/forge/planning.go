package forge

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// PlanIssue is one open issue as the planning view reads it.
type PlanIssue struct {
	Number int
	Title  string
	URL    string
	// Milestone is nil for an issue without one. Its State is "open" or "closed": an open
	// issue can sit in a closed milestone.
	Milestone *Milestone
	// BlockedBy holds only the open blockers; a closed blocker blocks nothing.
	BlockedBy []Blocker
	// Labels are the label names of the issue; never nil.
	Labels []string
}

// ClosedIssue is one issue that counts as done in a milestone.
type ClosedIssue struct {
	Number     int
	Title, URL string
}

// MilestoneCount is an open milestone with the size of its work, counted over issues only.
type MilestoneCount struct {
	Milestone
	// Open is the number of open issues. Done is the number of issues closed as completed;
	// pull requests and issues closed as not planned count in neither.
	Open, Done int
	// Closed are the issues behind Done, by ascending number; never nil, so len(Closed) == Done.
	// On GitHub only issues closed as completed are in it; GitLab cannot tell a close reason, so
	// there it holds every closed issue.
	Closed []ClosedIssue
}

// Planning is what the prioritisation view reads of one repository.
type Planning struct {
	Issues []PlanIssue
	// Milestones are all open milestones, also one without issues; never nil.
	Milestones []MilestoneCount
}

// PrioInputs is what the priority weight of #445 needs of one repository besides the blockers.
type PrioInputs struct {
	// Issues maps every open issue (no pull request) to the number of its milestone; 0 is none.
	Issues map[int]int
	// Milestones are the open milestones, with their description; never nil.
	Milestones []Milestone
}

// PrioInputs reads the open issue list and the open milestones over REST, the same pages as
// Planning, so a repeated read is answered by 304s. It reads no closed issues of a milestone and
// no blocked_by list.
func (c *GitHub) PrioInputs(ctx context.Context) (PrioInputs, error) {
	if err := c.configured(); err != nil {
		return PrioInputs{}, err
	}
	out := PrioInputs{Issues: map[int]int{}, Milestones: []Milestone{}}
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&sort=created&direction=asc&per_page=100", c.BaseURL, c.Repo)
	for url != "" {
		var page []issueItem
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return PrioInputs{}, err
		}
		for _, it := range page {
			if it.PullRequest != nil {
				continue
			}
			is := it.issue()
			out.Issues[is.Number] = 0
			if is.Milestone != nil {
				out.Issues[is.Number] = is.Milestone.Number
			}
		}
		url = next
	}
	milestones, err := c.Milestones(ctx, "open")
	if err != nil {
		return PrioInputs{}, err
	}
	out.Milestones = append(out.Milestones, milestones...)
	return out, nil
}

// Planning reads the open issues of the repo with their milestone and open blockers, and per
// open milestone the number of open issues and the issues closed as completed. It reads REST
// only: the open issue list, the open milestones, the closed issues of each of them, and the
// blocked_by list of each issue that has blockers, so a repeated read is answered by 304s.
func (c *GitHub) Planning(ctx context.Context) (Planning, error) {
	if err := c.configured(); err != nil {
		return Planning{}, err
	}
	issues, open, err := c.planIssues(ctx)
	if err != nil {
		return Planning{}, err
	}
	milestones, err := c.Milestones(ctx, "open")
	if err != nil {
		return Planning{}, err
	}
	counts := []MilestoneCount{}
	for _, m := range milestones {
		closed, err := c.ClosedInMilestone(ctx, m, time.Time{})
		if err != nil {
			return Planning{}, fmt.Errorf("forge: milestone %q of %s: %w", m.Title, c.Repo, err)
		}
		m.OpenIssues, m.ClosedIssues = 0, 0 // they count pull requests too; Open and Done below do not
		done := []ClosedIssue{}
		for _, is := range closed {
			if is.StateReason == "" || is.StateReason == "completed" { // old issues carry no reason: done, as in report.go
				done = append(done, ClosedIssue{Number: is.Number, Title: is.Title, URL: is.URL})
			}
		}
		slices.SortFunc(done, func(a, b ClosedIssue) int { return a.Number - b.Number })
		counts = append(counts, MilestoneCount{Milestone: m, Open: open[m.Number], Done: len(done), Closed: done})
	}
	// REST lists milestones by due date; the view wants a stable order.
	slices.SortFunc(counts, func(a, b MilestoneCount) int { return a.Number - b.Number })
	return Planning{Issues: issues, Milestones: counts}, nil
}

// planIssues lists the open issues, oldest first and without pull requests, and counts them per
// milestone number. An issue is asked for its blockers unless its dependency summary says none.
func (c *GitHub) planIssues(ctx context.Context) ([]PlanIssue, map[int]int, error) {
	out := []PlanIssue{}
	open := map[int]int{}
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&sort=created&direction=asc&per_page=100", c.BaseURL, c.Repo)
	for url != "" {
		var page []issueItem
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, nil, err
		}
		for _, it := range page {
			if it.PullRequest != nil {
				continue
			}
			is := it.issue()
			pi := PlanIssue{Number: is.Number, Title: is.Title, URL: is.URL, Milestone: is.Milestone, Labels: is.Labels}
			if pi.Labels == nil {
				pi.Labels = []string{}
			}
			if is.Milestone != nil {
				open[is.Milestone.Number]++
			}
			if it.DependenciesSummary == nil || it.DependenciesSummary.TotalBlockedBy > 0 {
				blockers, err := c.blockedBy(ctx, c.Repo, it.Number)
				if err != nil {
					return nil, nil, err
				}
				for _, b := range blockers {
					if b.State == "OPEN" {
						pi.BlockedBy = append(pi.BlockedBy, b)
					}
				}
			}
			out = append(out, pi)
		}
		url = next
	}
	return out, open, nil
}

// nextCursor is the cursor to ask next, or done after the last page. An empty or repeated
// cursor would ask the same page again, for ever, so it is an error.
func nextCursor(what string, p pageInfo, after any) (next string, done bool, err error) {
	if !p.HasNextPage {
		return "", true, nil
	}
	if p.EndCursor == "" || p.EndCursor == after {
		return "", false, fmt.Errorf("forge: %s: pagination stuck at endCursor %q", what, p.EndCursor)
	}
	return p.EndCursor, false, nil
}
