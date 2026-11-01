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
	Body        string          `json:"body"`
	NodeID      string          `json:"node_id"`
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
	out := Issue{Number: it.Number, Title: it.Title, URL: it.HTMLURL, Comments: it.Comments, State: it.State,
		Body: it.Body, NodeID: it.NodeID, UpdatedAt: it.UpdatedAt}
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

// ClosedInMilestone lists the closed issues of a milestone that closed at or after since,
// following every page, without pull requests.
func (c *Client) ClosedInMilestone(ctx context.Context, milestone int, since time.Time) ([]Issue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var out []Issue
	url := fmt.Sprintf("%s/repos/%s/issues?state=closed&milestone=%d&per_page=100", c.BaseURL, c.Repo, milestone)
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
func (c *Client) BlockedBy(ctx context.Context, number int) ([]Blocker, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type item struct {
		Number        int    `json:"number"`
		State         string `json:"state"`
		RepositoryURL string `json:"repository_url"`
	}
	var out []Blocker
	url := fmt.Sprintf("%s/repos/%s/issues/%d/dependencies/blocked_by?per_page=100", c.BaseURL, c.Repo, number)
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
func (c *Client) OpenPRs(ctx context.Context) ([]OpenPR, error) {
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
