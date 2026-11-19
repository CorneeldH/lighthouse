package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Milestone is the milestone of an issue.
type Milestone struct {
	Number                    int
	Title, State, Description string // State is "open" or "closed"
	// OpenIssues and ClosedIssues count the issues of the milestone by state; both are zero where
	// the forge does not say.
	OpenIssues, ClosedIssues int
}

// OpenPR is an open pull request as the pulls list returns it.
type OpenPR struct {
	Number               int
	Title, Body, HeadRef string
}

// issueItem is an issue in GitHub's REST shape.
type issueItem struct {
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	HTMLURL     string          `json:"html_url"`
	Comments    int             `json:"comments"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	Draft       bool            `json:"draft"`
	Body        string          `json:"body"`
	ID          int64           `json:"id"`
	NodeID      string          `json:"node_id"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	ClosedAt    *time.Time      `json:"closed_at"`
	PullRequest json.RawMessage `json:"pull_request"`
	User        *struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		State       string `json:"state"`
		Description string `json:"description"`
	} `json:"milestone"`
}

func (it issueItem) issue() Issue {
	out := Issue{Number: it.Number, Title: it.Title, URL: it.HTMLURL, Comments: it.Comments, State: it.State, StateReason: it.StateReason,
		Body: it.Body, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt}
	if it.ClosedAt != nil {
		out.ClosedAt = *it.ClosedAt
	}
	if it.User != nil {
		out.Author = it.User.Login
	}
	for _, l := range it.Labels {
		out.Labels = append(out.Labels, l.Name)
	}
	if m := it.Milestone; m != nil {
		out.Milestone = &Milestone{Number: m.Number, Title: m.Title, State: m.State, Description: m.Description}
	}
	return out
}

// ClosedInMilestone lists the closed issues of milestone m (by m.Number) that closed at or
// after since, following every page, without pull requests.
func (c *GitHub) ClosedInMilestone(ctx context.Context, m Milestone, since time.Time) ([]Issue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var out []Issue
	url := fmt.Sprintf("%s/repos/%s/issues?state=closed&milestone=%d&per_page=100", c.BaseURL, c.Repo, m.Number)
	for url != "" {
		var page []issueItem
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			if it.PullRequest != nil {
				continue
			}
			if is := it.issue(); !is.ClosedAt.Before(since) {
				out = append(out, is)
			}
		}
		url = next
	}
	return out, nil
}

// BlockedBy lists the issues that block issue number. MergedPR stays false: REST does not say.
func (c *GitHub) BlockedBy(ctx context.Context, number int) ([]Blocker, error) {
	return c.dependencies(ctx, number, "blocked_by")
}

// Blocking lists the issues that issue number blocks, with their own state. MergedPR stays
// false. Every page is followed; a failing page is an error.
func (c *GitHub) Blocking(ctx context.Context, number int) ([]Blocker, error) {
	return c.dependencies(ctx, number, "blocking")
}

// dependencies reads one dependency list of issue number, "blocked_by" or "blocking", following
// every page.
func (c *GitHub) dependencies(ctx context.Context, number int, kind string) ([]Blocker, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type item struct {
		Number        int    `json:"number"`
		State         string `json:"state"`
		RepositoryURL string `json:"repository_url"`
	}
	var out []Blocker
	url := fmt.Sprintf("%s/repos/%s/issues/%d/dependencies/%s?per_page=100", c.BaseURL, c.Repo, number, kind)
	for url != "" {
		var page []item
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			out = append(out, Blocker{Repo: repoFromURL(it.RepositoryURL), Issue: it.Number, State: strings.ToUpper(it.State)})
		}
		url = next
	}
	return out, nil
}

// repoFromURL turns ".../repos/owner/name" into "owner/name".
func repoFromURL(u string) string {
	_, rest, ok := strings.Cut(u, "/repos/")
	if !ok {
		return ""
	}
	return rest
}

// OpenPRs lists all open pull requests of the repo, following every page.
func (c *GitHub) OpenPRs(ctx context.Context) ([]OpenPR, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type item struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	var out []OpenPR
	url := fmt.Sprintf("%s/repos/%s/pulls?state=open&per_page=100", c.BaseURL, c.Repo)
	for url != "" {
		var page []item
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			out = append(out, OpenPR{Number: it.Number, Title: it.Title, Body: it.Body, HeadRef: it.Head.Ref})
		}
		url = next
	}
	return out, nil
}

// issueChangesQuery reads one page of the pull requests that close issue $nr and of those whose
// cross-reference events its timeline shows. Each connection pages on its own cursor and is
// left out (@include) once its last page is read.
const issueChangesQuery = `query IssueChanges($owner: String!, $name: String!, $nr: Int!,
  $closing: Boolean!, $closingAfter: String, $refs: Boolean!, $refsAfter: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $nr) {
      closedByPullRequestsReferences(first: 100, after: $closingAfter, includeClosedPrs: true) @include(if: $closing) {
        pageInfo { hasNextPage endCursor }
        nodes { number state headRefName isCrossRepository body repository { nameWithOwner } }
      }
      timelineItems(itemTypes: [CROSS_REFERENCED_EVENT], first: 100, after: $refsAfter) @include(if: $refs) {
        pageInfo { hasNextPage endCursor }
        nodes { ... on CrossReferencedEvent { source { ... on PullRequest {
          number state headRefName isCrossRepository body repository { nameWithOwner }
        } } } }
      }
    }
  }
}`

// ghIssueChange is a pull request node of issueChangesQuery.
type ghIssueChange struct {
	Number      int    `json:"number"`
	State       string `json:"state"`
	HeadRefName string `json:"headRefName"`
	CrossRepo   bool   `json:"isCrossRepository"`
	Body        string `json:"body"`
	Repository  struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func (n ghIssueChange) change() IssueChange {
	return IssueChange{Number: n.Number, State: n.State, HeadRef: n.HeadRefName, CrossRepo: n.CrossRepo,
		Repo: n.Repository.NameWithOwner, Body: n.Body}
}

// IssueChanges lists the pull requests that close issue number (closedByPullRequestsReferences)
// and those that mention it (CROSS_REFERENCED_EVENT), each once. Both connections are followed
// to their last page; a failing page is an error.
func (c *GitHub) IssueChanges(ctx context.Context, number int) ([]IssueChange, error) {
	owner, name, err := c.ownerName()
	if err != nil {
		return nil, err
	}
	var closing, mentioning []IssueChange
	vars := map[string]any{"owner": owner, "name": name, "nr": number,
		"closing": true, "closingAfter": nil, "refs": true, "refsAfter": nil}
	what := fmt.Sprintf("pull requests of %s#%d", c.Repo, number)
	for vars["closing"] == true || vars["refs"] == true {
		var data struct {
			Repository *struct {
				Issue *struct {
					ClosedBy *struct {
						PageInfo pageInfo        `json:"pageInfo"`
						Nodes    []ghIssueChange `json:"nodes"`
					} `json:"closedByPullRequestsReferences"`
					Timeline *struct {
						PageInfo pageInfo `json:"pageInfo"`
						Nodes    []struct {
							Source ghIssueChange `json:"source"`
						} `json:"nodes"`
					} `json:"timelineItems"`
				} `json:"issue"`
			} `json:"repository"`
		}
		if err := c.graphql(ctx, issueChangesQuery, vars, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil || data.Repository.Issue == nil {
			return nil, fmt.Errorf("forge: issue %s#%d not found", c.Repo, number)
		}
		is := data.Repository.Issue
		if vars["closing"] == true {
			if is.ClosedBy == nil {
				return nil, fmt.Errorf("forge: %s: closedByPullRequestsReferences missing from the answer", what)
			}
			for _, n := range is.ClosedBy.Nodes {
				closing = append(closing, n.change())
			}
			if err := advance(vars, "closing", what, is.ClosedBy.PageInfo); err != nil {
				return nil, err
			}
		}
		if vars["refs"] == true {
			if is.Timeline == nil {
				return nil, fmt.Errorf("forge: %s: timelineItems missing from the answer", what)
			}
			for _, n := range is.Timeline.Nodes {
				if n.Source.Number != 0 { // the source of a cross-reference can be an issue
					mentioning = append(mentioning, n.Source.change())
				}
			}
			if err := advance(vars, "refs", what, is.Timeline.PageInfo); err != nil {
				return nil, err
			}
		}
	}
	return issueChanges(closing, mentioning), nil
}

// advance moves the connection that vars[key] includes to its next page, at vars[key+"After"],
// or leaves it out once its last page is read.
func advance(vars map[string]any, key, what string, p pageInfo) error {
	next, done, err := nextCursor(what+" ("+key+")", p, vars[key+"After"])
	if err != nil {
		return err
	}
	vars[key], vars[key+"After"] = !done, next
	return nil
}

// issueCloserQuery reads the closer of the last ClosedEvent of issue $nr.
const issueCloserQuery = `query IssueCloser($owner: String!, $name: String!, $nr: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $nr) {
      timelineItems(itemTypes: [CLOSED_EVENT], last: 1) {
        nodes { ... on ClosedEvent { closer { ... on PullRequest { number repository { nameWithOwner } } } } }
      }
    }
  }
}`

// Closer is the number of the pull request that closed issue number the last time, from the
// closer of its last ClosedEvent; 0 for a close by hand or by a commit, an issue never closed,
// or a pull request in another repository (its number would name another one here). The
// repository compares without regard to case: GitHub gives the canonical spelling,
// lighthouse.toml what the user typed.
func (c *GitHub) Closer(ctx context.Context, number int) (int, error) {
	owner, name, err := c.ownerName()
	if err != nil {
		return 0, err
	}
	var data struct {
		Repository *struct {
			Issue *struct {
				TimelineItems struct {
					Nodes []struct {
						Closer *struct {
							Number     int `json:"number"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
						} `json:"closer"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, issueCloserQuery, map[string]any{"owner": owner, "name": name, "nr": number}, &data); err != nil {
		return 0, err
	}
	if data.Repository == nil || data.Repository.Issue == nil {
		return 0, fmt.Errorf("forge: issue %s#%d not found", c.Repo, number)
	}
	nodes := data.Repository.Issue.TimelineItems.Nodes
	if len(nodes) == 0 || nodes[len(nodes)-1].Closer == nil {
		return 0, nil
	}
	closer := nodes[len(nodes)-1].Closer
	if !strings.EqualFold(closer.Repository.NameWithOwner, c.Repo) {
		return 0, nil
	}
	return closer.Number, nil
}
