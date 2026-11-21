package forge

import (
	"context"
	"fmt"
	"slices"
	"strings"
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

// planIssuesQuery reads one page of 100 open issues. blockedBy asks pageInfo, so a list longer
// than its first: 20 is an error instead of a silent cut.
const planIssuesQuery = `query Planning($owner: String!, $name: String!, $after: String) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, after: $after, states: OPEN, orderBy: {field: CREATED_AT, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title url
        labels(first: 50) { pageInfo { hasNextPage } nodes { name } }
        milestone { number title state description }
        blockedBy(first: 20) {
          pageInfo { hasNextPage }
          nodes { number state repository { nameWithOwner } }
        }
      }
    }
  }
}`

// planMilestonesQuery reads one page of 100 open milestones, each with its closed issues
// (first: 100, pageInfo asked, so a longer list is an error); Go keeps the completed ones.
const planMilestonesQuery = `query PlanMilestones($owner: String!, $name: String!, $after: String) {
  repository(owner: $owner, name: $name) {
    milestones(first: 100, after: $after, states: OPEN, orderBy: {field: NUMBER, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state description
        issues(first: 100, states: CLOSED) {
          pageInfo { hasNextPage }
          nodes { number title url stateReason }
        }
      }
    }
  }
}`

// planCountsChunk is the number of milestones one PlanCounts query counts: one search alias
// each, a size GitHub accepts at one point per query.
const planCountsChunk = 50

type planMilestoneNode struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Description string `json:"description"`
	Issues      struct {
		PageInfo pageInfo `json:"pageInfo"`
		Nodes    []struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			URL         string `json:"url"`
			StateReason string `json:"stateReason"`
		} `json:"nodes"`
	} `json:"issues"`
}

// closed is the completed issues of the node by ascending number, never nil. A list cut at
// its first: 100 is an error naming the milestone.
func (n planMilestoneNode) closed(repo string) ([]ClosedIssue, error) {
	if n.Issues.PageInfo.HasNextPage {
		return nil, fmt.Errorf("forge: milestone %q of %s: %w", n.Title, repo, truncated("closed issues", len(n.Issues.Nodes)))
	}
	out := []ClosedIssue{}
	for _, i := range n.Issues.Nodes {
		if i.StateReason == "COMPLETED" {
			out = append(out, ClosedIssue{Number: i.Number, Title: i.Title, URL: i.URL})
		}
	}
	slices.SortFunc(out, func(a, b ClosedIssue) int { return a.Number - b.Number })
	return out, nil
}

func (n planMilestoneNode) milestone() Milestone {
	return Milestone{Number: n.Number, Title: n.Title, State: strings.ToLower(n.State), Description: n.Description}
}

// Planning reads the open issues of the repo with their milestone and open blockers, and per
// open milestone the number of open issues and the issues closed as completed. GitHub prices
// it at about one point per query: pages of issues, pages of milestones, and one counts query
// per 50 milestones; the priorities document holds the measurement. The labels of each issue
// and the closed issues of each milestone ride along as nested connections, so the query cost
// rises with the number of issues and milestones. Any truncated connection or stuck
// cursor is an error, never a partial answer.
func (c *GitHub) Planning(ctx context.Context) (Planning, error) {
	owner, name, err := c.ownerName()
	if err != nil {
		return Planning{}, err
	}
	issues, err := c.planIssues(ctx, owner, name)
	if err != nil {
		return Planning{}, err
	}
	milestones, err := c.planMilestones(ctx, owner, name)
	if err != nil {
		return Planning{}, err
	}
	return Planning{Issues: issues, Milestones: milestones}, nil
}

func (c *GitHub) planIssues(ctx context.Context, owner, name string) ([]PlanIssue, error) {
	type node struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
		Labels struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"labels"`
		Milestone *planMilestoneNode `json:"milestone"`
		BlockedBy struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
				Number     int    `json:"number"`
				State      string `json:"state"`
				Repository struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
			} `json:"nodes"`
		} `json:"blockedBy"`
	}
	out := []PlanIssue{}
	var after any // null on the first page
	for {
		var data struct {
			Repository *struct {
				Issues struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []node   `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		}
		if err := c.graphql(ctx, planIssuesQuery, map[string]any{"owner": owner, "name": name, "after": after}, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil {
			return nil, fmt.Errorf("forge: repository %s not found", c.Repo)
		}
		for _, n := range data.Repository.Issues.Nodes {
			if n.BlockedBy.PageInfo.HasNextPage {
				return nil, fmt.Errorf("forge: issue %s#%d: %w", c.Repo, n.Number, truncated("blockedBy", len(n.BlockedBy.Nodes)))
			}
			if n.Labels.PageInfo.HasNextPage {
				return nil, fmt.Errorf("forge: issue %s#%d: %w", c.Repo, n.Number, truncated("labels", len(n.Labels.Nodes)))
			}
			is := PlanIssue{Number: n.Number, Title: n.Title, URL: n.URL, Labels: []string{}}
			for _, l := range n.Labels.Nodes {
				is.Labels = append(is.Labels, l.Name)
			}
			if n.Milestone != nil {
				m := n.Milestone.milestone()
				is.Milestone = &m
			}
			for _, b := range n.BlockedBy.Nodes {
				if b.State == "OPEN" {
					is.BlockedBy = append(is.BlockedBy, Blocker{Repo: b.Repository.NameWithOwner, Issue: b.Number, State: b.State})
				}
			}
			out = append(out, is)
		}
		next, done, err := nextCursor("issues of "+c.Repo, data.Repository.Issues.PageInfo, after)
		if err != nil || done {
			return out, err
		}
		after = next
	}
}

func (c *GitHub) planMilestones(ctx context.Context, owner, name string) ([]MilestoneCount, error) {
	out := []MilestoneCount{}
	var after any
	for {
		var data struct {
			Repository *struct {
				Milestones struct {
					PageInfo pageInfo            `json:"pageInfo"`
					Nodes    []planMilestoneNode `json:"nodes"`
				} `json:"milestones"`
			} `json:"repository"`
		}
		if err := c.graphql(ctx, planMilestonesQuery, map[string]any{"owner": owner, "name": name, "after": after}, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil {
			return nil, fmt.Errorf("forge: repository %s not found", c.Repo)
		}
		for _, n := range data.Repository.Milestones.Nodes {
			closed, err := n.closed(c.Repo)
			if err != nil {
				return nil, err
			}
			out = append(out, MilestoneCount{Milestone: n.milestone(), Done: len(closed), Closed: closed})
		}
		next, done, err := nextCursor("milestones of "+c.Repo, data.Repository.Milestones.PageInfo, after)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		after = next
	}
	for start := 0; start < len(out); start += planCountsChunk {
		if err := c.planCounts(ctx, out[start:min(start+planCountsChunk, len(out))]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// planCounts fills Open of ms with one query of one search alias per milestone; Done comes
// from the closed issues, so the blocks and the percentage cannot drift apart.
// The search is restricted to is:issue: without it GitHub counts pull requests too.
func (c *GitHub) planCounts(ctx context.Context, ms []MilestoneCount) error {
	var decl, body strings.Builder
	vars := map[string]any{}
	for i, m := range ms {
		// GitHub documents no escape inside a quoted qualifier and none was verified, so a
		// title that would need one fails visibly instead of counting another milestone.
		if strings.ContainsAny(m.Title, `"\`) {
			return fmt.Errorf("forge: milestone %q of %s: a quote or backslash in the title cannot be searched", m.Title, c.Repo)
		}
		prefix := fmt.Sprintf(`repo:%s milestone:"%s" is:issue`, c.Repo, m.Title)
		v := fmt.Sprintf("q%d", i)
		vars[v] = prefix + " is:open"
		fmt.Fprintf(&decl, "$%s: String!, ", v)
		fmt.Fprintf(&body, "m%d: search(query: $%s, type: ISSUE) { issueCount } ", i, v)
	}
	query := "query PlanCounts(" + strings.TrimSuffix(decl.String(), ", ") + ") { " + body.String() + "}"
	var data map[string]struct {
		IssueCount int `json:"issueCount"`
	}
	if err := c.graphql(ctx, query, vars, &data); err != nil {
		return err
	}
	for i := range ms {
		open, ok := data[fmt.Sprintf("m%d", i)]
		if !ok {
			return fmt.Errorf("forge: milestone %q of %s: counts missing from the answer", ms[i].Title, c.Repo)
		}
		ms[i].Open = open.IssueCount
	}
	return nil
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
