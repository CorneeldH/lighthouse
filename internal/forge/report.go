package forge

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"slices"
	"strings"
	"time"
)

// PRSummary is a pull request as a report lists it.
type PRSummary struct {
	Number    int
	Title     string
	URL       string
	Draft     bool
	Mergeable string    // MERGEABLE, CONFLICTING, UNKNOWN; "" for a merged PR
	Checks    string    // statusCheckRollup.state, "" without checks
	MergedAt  time.Time // zero while not merged
}

// searchLimit is the most results one GitHub search returns.
const searchLimit = 1000

// searchPage is one page of GET /search/issues.
type searchPage struct {
	TotalCount        int         `json:"total_count"`
	IncompleteResults bool        `json:"incomplete_results"`
	Items             []issueItem `json:"items"`
}

// searchItem is one search result: the issue shape, plus when a pull request merged.
type searchItem struct {
	issueItem
	MergedAt time.Time
}

// searchPeriod searches issues of the repo with the qualifier "<qualifier>:<since>..<until>"
// next to kind (for example "is:pr is:merged"), following every page. The range is inclusive on
// both ends, so the caller filters again. It fails on more than 1000 results or an incomplete
// result, never truncates.
func (c *GitHub) searchPeriod(ctx context.Context, kind, qualifier string, since, until time.Time) ([]searchItem, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	q := fmt.Sprintf("repo:%s %s %s:%s..%s", c.Repo, kind, qualifier, since.Format(time.RFC3339), until.Format(time.RFC3339))
	vals := neturl.Values{"q": {q}, "per_page": {"100"}}
	url := c.BaseURL + "/search/issues?" + vals.Encode()
	var out []searchItem
	for url != "" {
		var page searchPage
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		if page.TotalCount > searchLimit {
			return nil, fmt.Errorf("forge search %q: %d results, more than the %d a search returns", q, page.TotalCount, searchLimit)
		}
		if page.IncompleteResults {
			return nil, fmt.Errorf("forge search %q: incomplete results", q)
		}
		for _, it := range page.Items {
			si := searchItem{issueItem: it}
			if it.PullRequest != nil {
				var pr struct {
					MergedAt *time.Time `json:"merged_at"`
				}
				if err := json.Unmarshal(it.PullRequest, &pr); err != nil {
					return nil, fmt.Errorf("forge search %q: pull_request of #%d: %w", q, it.Number, err)
				}
				if pr.MergedAt != nil {
					si.MergedAt = *pr.MergedAt
				}
			}
			out = append(out, si)
		}
		url = next
	}
	return out, nil
}

// inPeriod is true for at in [since, until).
func inPeriod(at, since, until time.Time) bool {
	return !at.IsZero() && !at.Before(since) && at.Before(until)
}

// MergedPRs lists the pull requests that merged in [since, until), oldest merge first.
func (c *GitHub) MergedPRs(ctx context.Context, since, until time.Time) ([]PRSummary, error) {
	items, err := c.searchPeriod(ctx, "is:pr is:merged", "merged", since, until)
	if err != nil {
		return nil, err
	}
	var out []PRSummary
	for _, it := range items {
		if it.PullRequest == nil || !inPeriod(it.MergedAt, since, until) {
			continue
		}
		out = append(out, PRSummary{Number: it.Number, Title: it.Title, URL: it.HTMLURL, Draft: it.Draft, MergedAt: it.MergedAt})
	}
	slices.SortFunc(out, func(a, b PRSummary) int {
		return cmp.Or(a.MergedAt.Compare(b.MergedAt), cmp.Compare(a.Number, b.Number))
	})
	return out, nil
}

// ClosedIssues lists the issues closed as done in [since, until), oldest close first. An issue
// closed for another reason ("not_planned", "duplicate") is left out; only "completed" or no reason
// counts as done.
func (c *GitHub) ClosedIssues(ctx context.Context, since, until time.Time) ([]Issue, error) {
	items, err := c.searchPeriod(ctx, "is:issue is:closed", "closed", since, until)
	if err != nil {
		return nil, err
	}
	var out []Issue
	for _, it := range items {
		is := it.issue()
		if it.PullRequest != nil || (is.StateReason != "" && is.StateReason != "completed") || !inPeriod(is.ClosedAt, since, until) {
			continue
		}
		out = append(out, is)
	}
	slices.SortFunc(out, func(a, b Issue) int {
		return cmp.Or(a.ClosedAt.Compare(b.ClosedAt), cmp.Compare(a.Number, b.Number))
	})
	return out, nil
}

// MilestonedIssue is an issue that was put in a milestone inside a period.
type MilestonedIssue struct {
	Issue     Issue
	Milestone Milestone
	At        time.Time
}

// issueEvent is one entry of GET .../issues/{n}/events; a milestoned event names the milestone
// by title only.
type issueEvent struct {
	Event     string    `json:"event"`
	CreatedAt time.Time `json:"created_at"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

// milestonedAt is the time of the last milestoned event in [since, until) that names title, or
// the zero time when there is none.
func milestonedAt(events []issueEvent, title string, since, until time.Time) time.Time {
	var at time.Time
	for _, ev := range events {
		if ev.Event == "milestoned" && ev.Milestone != nil && ev.Milestone.Title == title &&
			inPeriod(ev.CreatedAt, since, until) && ev.CreatedAt.After(at) {
			at = ev.CreatedAt
		}
	}
	return at
}

// issueEvents lists the events of one issue, following every page.
func (c *GitHub) issueEvents(ctx context.Context, number int) ([]issueEvent, error) {
	var out []issueEvent
	url := fmt.Sprintf("%s/repos/%s/issues/%d/events?per_page=100", c.BaseURL, c.Repo, number)
	for url != "" {
		var page []issueEvent
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		url = next
	}
	return out, nil
}

// MilestonedIssues lists the open issues that were put in their current milestone in
// [since, until), by milestone title and then number. The candidates are the open issues with a
// milestone updated since since; the events of each decide. A failed events call fails the method.
func (c *GitHub) MilestonedIssues(ctx context.Context, since, until time.Time) ([]MilestonedIssue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var candidates []Issue
	vals := neturl.Values{"state": {"open"}, "milestone": {"*"}, "since": {since.Format(time.RFC3339)}, "per_page": {"100"}}
	url := fmt.Sprintf("%s/repos/%s/issues?%s", c.BaseURL, c.Repo, vals.Encode())
	for url != "" {
		var page []issueItem
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			if it.PullRequest == nil && it.Milestone != nil {
				candidates = append(candidates, it.issue())
			}
		}
		url = next
	}
	var out []MilestonedIssue
	for _, is := range candidates {
		events, err := c.issueEvents(ctx, is.Number)
		if err != nil {
			return nil, err
		}
		if at := milestonedAt(events, is.Milestone.Title, since, until); !at.IsZero() {
			out = append(out, MilestonedIssue{Issue: is, Milestone: *is.Milestone, At: at})
		}
	}
	slices.SortFunc(out, func(a, b MilestonedIssue) int {
		return cmp.Or(cmp.Compare(a.Milestone.Title, b.Milestone.Title), cmp.Compare(a.Issue.Number, b.Issue.Number))
	})
	return out, nil
}

// openPRNode is one pull request of OpenPRsWithStatusQuery.
type openPRNode struct {
	Number            int    `json:"number"`
	Title             string `json:"title"`
	URL               string `json:"url"`
	IsDraft           bool   `json:"isDraft"`
	Mergeable         string `json:"mergeable"`
	StatusCheckRollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

// OpenPRsWithStatus lists the open pull requests with draft, mergeable and checks, by number,
// following every page. MergedAt is zero.
func (c *GitHub) OpenPRsWithStatus(ctx context.Context) ([]PRSummary, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	owner, name, ok := strings.Cut(c.Repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	var out []PRSummary
	after := ""
	for {
		var data struct {
			Repository *struct {
				PullRequests struct {
					PageInfo pageInfo     `json:"pageInfo"`
					Nodes    []openPRNode `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": owner, "name": name, "after": nil}
		if after != "" {
			vars["after"] = after
		}
		if err := c.graphql(ctx, OpenPRsWithStatusQuery, vars, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil {
			return nil, fmt.Errorf("forge: repository %s not found", c.Repo)
		}
		prs := data.Repository.PullRequests
		for _, n := range prs.Nodes {
			p := PRSummary{Number: n.Number, Title: n.Title, URL: n.URL, Draft: n.IsDraft, Mergeable: n.Mergeable}
			if n.StatusCheckRollup != nil {
				p.Checks = n.StatusCheckRollup.State
			}
			out = append(out, p)
		}
		if !prs.PageInfo.HasNextPage {
			break
		}
		if prs.PageInfo.EndCursor == "" || prs.PageInfo.EndCursor == after {
			return nil, fmt.Errorf("forge: open pull requests: page %q has a next page but no new cursor", after)
		}
		after = prs.PageInfo.EndCursor
	}
	slices.SortFunc(out, func(a, b PRSummary) int { return cmp.Compare(a.Number, b.Number) })
	return out, nil
}
