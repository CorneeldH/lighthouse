package forge

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"slices"
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
// closed as "not_planned" is left out; one without a state reason counts as done.
func (c *GitHub) ClosedIssues(ctx context.Context, since, until time.Time) ([]Issue, error) {
	items, err := c.searchPeriod(ctx, "is:issue is:closed", "closed", since, until)
	if err != nil {
		return nil, err
	}
	var out []Issue
	for _, it := range items {
		is := it.issue()
		if it.PullRequest != nil || is.StateReason == "not_planned" || !inPeriod(is.ClosedAt, since, until) {
			continue
		}
		out = append(out, is)
	}
	slices.SortFunc(out, func(a, b Issue) int {
		return cmp.Or(a.ClosedAt.Compare(b.ClosedAt), cmp.Compare(a.Number, b.Number))
	})
	return out, nil
}
