package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// restRepo is the repository of a pull request's head or base.
type restRepo struct {
	FullName string `json:"full_name"`
}

// restPull is a pull request in the shape of GET /repos/{repo}/pulls. The list carries no
// mergeable; enrich reads it from the pull request itself.
type restPull struct {
	Number   int       `json:"number"`
	State    string    `json:"state"` // open, closed
	Draft    bool      `json:"draft"`
	MergedAt time.Time `json:"merged_at"` // zero when null
	ClosedAt time.Time `json:"closed_at"` // zero when null
	HTMLURL  string    `json:"html_url"`
	Head     struct {
		SHA  string    `json:"sha"`
		Ref  string    `json:"ref"`
		Repo *restRepo `json:"repo"` // null when the head repository was deleted
	} `json:"head"`
	Base struct {
		Ref  string    `json:"ref"`
		Repo *restRepo `json:"repo"`
	} `json:"base"`
}

// pr maps p without a request. A merged PR is MERGEABLE and a closed one UNKNOWN, as GraphQL
// answered them; an open PR has Mergeable and Checks "" until enrich fills them.
func (p restPull) pr(closes bool) PR {
	out := PR{Number: p.Number, State: strings.ToUpper(p.State), Draft: p.Draft, HeadSHA: p.Head.SHA, HeadRef: p.Head.Ref,
		BaseRef: p.Base.Ref, Closes: closes, URL: p.HTMLURL, MergedAt: p.MergedAt, ClosedAt: p.ClosedAt}
	out.CrossRepo = p.Head.Repo == nil || p.Base.Repo == nil || p.Head.Repo.FullName != p.Base.Repo.FullName
	switch {
	case !p.MergedAt.IsZero():
		out.State, out.Mergeable = "MERGED", "MERGEABLE"
	case out.State == "CLOSED":
		out.Mergeable = "UNKNOWN"
	}
	return out
}

// repo is the repository the pull request merges into, "" when the answer has none.
func (p restPull) repo() string {
	if p.Base.Repo == nil {
		return ""
	}
	return p.Base.Repo.FullName
}

// prKey is one pull request of repo at one head: its checks belong to that head.
type prKey struct {
	repo    string
	number  int
	headSHA string
}

// enrichedPR is what enrich read for one prKey.
type enrichedPR struct{ mergeable, checks string }

// enrich fills Mergeable and Checks of an open pull request of repo; any other PR is left as
// it is, without a request. With EnrichMemo an open PR read before at the same head gets what
// that read gave, without a request; only a read that succeeded is remembered.
func (c *GitHub) enrich(ctx context.Context, repo string, pr *PR) error {
	if pr.State != "OPEN" {
		return nil
	}
	key := prKey{repo: repo, number: pr.Number, headSHA: pr.HeadSHA}
	if c.EnrichMemo {
		c.mu.Lock()
		e, ok := c.enriched[key]
		c.mu.Unlock()
		if ok {
			pr.Mergeable, pr.Checks = e.mergeable, e.checks
			return nil
		}
	}
	var one struct {
		Mergeable *bool `json:"mergeable"`
	}
	if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/pulls/%d", c.BaseURL, repo, pr.Number), nil, &one); err != nil {
		return err
	}
	checks, err := c.checks(ctx, repo, pr.HeadSHA)
	if err != nil {
		return err
	}
	pr.Mergeable, pr.Checks = mergeable(one.Mergeable), checks
	if c.EnrichMemo {
		c.mu.Lock()
		if c.enriched == nil {
			c.enriched = map[prKey]enrichedPR{}
		}
		c.enriched[key] = enrichedPR{mergeable: pr.Mergeable, checks: pr.Checks}
		c.mu.Unlock()
	}
	return nil
}

// mergeable is GitHub's mergeable of one pull request as GraphQL names it; null means GitHub
// is still computing it.
func mergeable(m *bool) string {
	switch {
	case m == nil:
		return "UNKNOWN"
	case *m:
		return "MERGEABLE"
	default:
		return "CONFLICTING"
	}
}

// checkRun is one check run of a commit. App is the slug of the app that made it.
type checkRun struct {
	ID                            int64
	Name, App, Status, Conclusion string
}

// commitStatus is one commit status, from the statuses[] of the combined status.
type commitStatus struct{ State string }

// checkSuite is one check suite of a commit; Runs is its latest_check_runs_count and
// Conclusion "" while it has none.
type checkSuite struct {
	Status     string
	Runs       int
	App        string
	Conclusion string
}

// Conclusions of a check run.
var (
	failedConclusions    = map[string]bool{"failure": true, "timed_out": true, "cancelled": true, "action_required": true, "startup_failure": true}
	succeededConclusions = map[string]bool{"success": true, "neutral": true, "skipped": true}
	// failedSuiteConclusions leaves out cancelled: concurrency cancels a superseded suite,
	// whose runs a newer suite replaces.
	failedSuiteConclusions = map[string]bool{"failure": true, "timed_out": true, "action_required": true, "startup_failure": true}
)

// rollup is the state of the checks of one commit, as statusCheckRollup.state named it: ""
// without runs and statuses, else FAILURE, ERROR, SUCCESS in that order, and PENDING for
// everything else, every unknown value included. Only the newest run per (App, Name) counts:
// a run that a newer one superseded (concurrency cancelled it) no longer decides. Because an
// Actions run is named after its job only, two workflows with a job of the same name share
// one (App, Name); a completed suite that failed is therefore FAILURE too, a cancelled one not.
// SUCCESS also needs every suite done that will still make a run: one with runs, or an
// Actions suite.
func rollup(runs []checkRun, statuses []commitStatus, suites []checkSuite) string {
	runs = newestRuns(runs)
	if len(runs) == 0 && len(statuses) == 0 {
		return ""
	}
	for _, r := range runs {
		if failedConclusions[r.Conclusion] {
			return "FAILURE"
		}
	}
	for _, s := range statuses {
		if s.State == "failure" {
			return "FAILURE"
		}
	}
	for _, s := range suites {
		if s.Status == "completed" && failedSuiteConclusions[s.Conclusion] {
			return "FAILURE"
		}
	}
	for _, s := range statuses {
		if s.State == "error" {
			return "ERROR"
		}
	}
	for _, r := range runs {
		if r.Status != "completed" || !succeededConclusions[r.Conclusion] {
			return "PENDING"
		}
	}
	for _, s := range statuses {
		if s.State != "success" {
			return "PENDING"
		}
	}
	for _, s := range suites {
		if s.Status != "completed" && (s.Runs > 0 || s.App == "github-actions") {
			return "PENDING"
		}
	}
	return "SUCCESS"
}

// newestRuns keeps per (App, Name) only the run with the highest ID, in the order of runs.
func newestRuns(runs []checkRun) []checkRun {
	type key struct{ app, name string }
	newest := map[key]int64{}
	for _, r := range runs {
		k := key{r.App, r.Name}
		if id, ok := newest[k]; !ok || r.ID > id {
			newest[k] = r.ID
		}
	}
	var out []checkRun
	for _, r := range runs {
		if newest[key{r.App, r.Name}] == r.ID {
			out = append(out, r)
		}
	}
	return out
}

// checks reads the check runs, the statuses and the check suites of commit sha in repo, every
// page of each, and rolls them up. A list shorter or longer than its total_count, or one whose
// total_count changes between pages, is an error: the missing item could be the failure.
func (c *GitHub) checks(ctx context.Context, repo, sha string) (string, error) {
	base := fmt.Sprintf("%s/repos/%s/commits/%s", c.BaseURL, repo, url.PathEscape(sha))
	type app struct {
		Slug string `json:"slug"`
	}
	rawRuns, err := counted[struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		App        *app   `json:"app"`
	}](ctx, c, base+"/check-runs?per_page=100", "check_runs")
	if err != nil {
		return "", err
	}
	rawStatuses, err := counted[struct {
		State string `json:"state"`
	}](ctx, c, base+"/status?per_page=100", "statuses")
	if err != nil {
		return "", err
	}
	rawSuites, err := counted[struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Runs       int    `json:"latest_check_runs_count"`
		App        *app   `json:"app"`
	}](ctx, c, base+"/check-suites?per_page=100", "check_suites")
	if err != nil {
		return "", err
	}
	slug := func(a *app) string {
		if a == nil {
			return ""
		}
		return a.Slug
	}
	runs := make([]checkRun, 0, len(rawRuns))
	for _, r := range rawRuns {
		runs = append(runs, checkRun{ID: r.ID, Name: r.Name, App: slug(r.App), Status: r.Status, Conclusion: r.Conclusion})
	}
	statuses := make([]commitStatus, 0, len(rawStatuses))
	for _, s := range rawStatuses {
		statuses = append(statuses, commitStatus{State: s.State})
	}
	suites := make([]checkSuite, 0, len(rawSuites))
	for _, s := range rawSuites {
		suites = append(suites, checkSuite{Status: s.Status, Runs: s.Runs, App: slug(s.App), Conclusion: s.Conclusion})
	}
	return rollup(runs, statuses, suites), nil
}

// counted reads every page of a list that GitHub wraps in an object with total_count and the
// items under key, and checks the number of items against total_count.
func counted[T any](ctx context.Context, c *GitHub, first, key string) ([]T, error) {
	var out []T
	total := -1
	for at := first; at != ""; {
		var page map[string]json.RawMessage
		next, err := c.do(ctx, http.MethodGet, at, nil, &page)
		if err != nil {
			return nil, err
		}
		var n int
		if err := json.Unmarshal(page["total_count"], &n); err != nil {
			return nil, fmt.Errorf("forge GET %s: total_count: %w", at, err)
		}
		var items []T
		if err := json.Unmarshal(page[key], &items); err != nil {
			return nil, fmt.Errorf("forge GET %s: %s: %w", at, key, err)
		}
		if total >= 0 && n != total {
			return nil, fmt.Errorf("forge GET %s: total_count changed from %d to %d between pages", first, total, n)
		}
		total = n
		out = append(out, items...)
		at = next
	}
	if len(out) != total {
		return nil, fmt.Errorf("forge GET %s: read %d of total_count %d", first, len(out), total)
	}
	return out, nil
}
