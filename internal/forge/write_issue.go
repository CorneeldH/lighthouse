package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// NewIssue is an issue to create. Milestone is a title; "" sets none.
type NewIssue struct {
	Title, Body string
	Labels      []string
	Milestone   string
}

// IssueEdit is a change to an issue; a nil field stays as it is. A Milestone of "" removes the
// milestone.
type IssueEdit struct {
	Title, Body, Milestone  *string
	AddLabels, RemoveLabels []string
}

// IssueFilter narrows ListIssues. State is open, closed or all; "" is open. Label and
// Milestone (a title) are "" for no filter.
type IssueFilter struct{ State, Label, Milestone string }

// NewPR is a pull request (merge request) to create. Milestone is a title; "" sets none.
// Draft opens it as a draft.
type NewPR struct {
	Title, Body, Head, Base, Milestone string
	Draft                              bool
}

// Change is a pull request or merge request. State is open, closed or merged.
type Change struct {
	Number                   int
	Title, Body, State       string
	Labels                   []string
	Milestone                *Milestone // nil: none
	URL                      string
	CreatedAt                time.Time
	Author, HeadRef, BaseRef string
}

// APIRequest is a raw call to the forge API. Path is relative to the API root; on GitHub
// {owner} and {repo} are filled in. An empty Method is GET without fields and POST with them.
type APIRequest struct {
	Method, Path string
	Fields       []APIField
}

// APIField is one request field. A Typed value is read as a JSON scalar (a number, true, false,
// null) and "@path" as the contents of that file; an untyped value is always text. Fields of a
// GET or DELETE go in the query string, the others in the JSON body.
type APIField struct {
	Name, Value string
	Typed       bool
}

// ErrMilestoneNotFound means no milestone has the given title.
var ErrMilestoneNotFound = errors.New("forge: milestone not found")

// AmbiguousMilestone means more than one milestone has Title; Numbers names them.
type AmbiguousMilestone struct {
	Title   string
	Numbers []int
}

func (e *AmbiguousMilestone) Error() string {
	nums := make([]string, len(e.Numbers))
	for i, n := range e.Numbers {
		nums[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf("forge: milestone title %q matches %d milestones (%s)", e.Title, len(e.Numbers), strings.Join(nums, ", "))
}

// GroupMilestone means the milestone belongs to a group, which the project API cannot edit.
type GroupMilestone struct{ Number int }

func (e *GroupMilestone) Error() string {
	return fmt.Sprintf("forge: milestone %d belongs to a group; change it with the api command", e.Number)
}

// ghMilestone is a milestone in GitHub's REST shape.
type ghMilestone struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Description string `json:"description"`
}

func (m ghMilestone) milestone() Milestone {
	return Milestone(m)
}

// resolveMilestone turns a milestone title into its number, over open and closed milestones.
func (c *GitHub) resolveMilestone(ctx context.Context, title string) (int, error) {
	all, err := c.Milestones(ctx, "all")
	if err != nil {
		return 0, err
	}
	var nums []int
	for _, m := range all {
		if m.Title == title {
			nums = append(nums, m.Number)
		}
	}
	switch len(nums) {
	case 0:
		return 0, fmt.Errorf("%w: %q", ErrMilestoneNotFound, title)
	case 1:
		return nums[0], nil
	}
	return 0, &AmbiguousMilestone{Title: title, Numbers: nums}
}

// CreateIssue creates an issue and returns it as the forge stored it.
func (c *GitHub) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	if err := c.configured(); err != nil {
		return Issue{}, err
	}
	body := map[string]any{"title": in.Title, "body": in.Body}
	if len(in.Labels) > 0 {
		body["labels"] = in.Labels
	}
	if in.Milestone != "" {
		n, err := c.resolveMilestone(ctx, in.Milestone)
		if err != nil {
			return Issue{}, err
		}
		body["milestone"] = n
	}
	var it issueItem
	if _, err := c.do(ctx, http.MethodPost, c.repoURL("/issues"), body, &it); err != nil {
		return Issue{}, err
	}
	return it.issue(), nil
}

// EditIssue applies the edit and returns the issue as it is afterwards. The title, body and
// milestone go in one PATCH, left out when none is set; labels are separate requests.
func (c *GitHub) EditIssue(ctx context.Context, number int, e IssueEdit) (Issue, error) {
	if err := c.configured(); err != nil {
		return Issue{}, err
	}
	patch := map[string]any{}
	if e.Title != nil {
		patch["title"] = *e.Title
	}
	if e.Body != nil {
		patch["body"] = *e.Body
	}
	if e.Milestone != nil {
		if *e.Milestone == "" {
			patch["milestone"] = nil
		} else {
			n, err := c.resolveMilestone(ctx, *e.Milestone)
			if err != nil {
				return Issue{}, err
			}
			patch["milestone"] = n
		}
	}
	base := c.repoURL("/issues/" + strconv.Itoa(number))
	if len(patch) > 0 {
		if _, err := c.do(ctx, http.MethodPatch, base, patch, nil); err != nil {
			return Issue{}, err
		}
	}
	if len(e.AddLabels) > 0 {
		if _, err := c.do(ctx, http.MethodPost, base+"/labels", map[string]any{"labels": e.AddLabels}, nil); err != nil {
			return Issue{}, err
		}
	}
	for _, l := range e.RemoveLabels {
		if _, err := c.do(ctx, http.MethodDelete, base+"/labels/"+neturl.PathEscape(l), nil, nil); err != nil {
			return Issue{}, err
		}
	}
	return c.Issue(ctx, number)
}

// ListIssues lists the issues that match the filter, following every page, without pull requests.
func (c *GitHub) ListIssues(ctx context.Context, f IssueFilter) ([]Issue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	state := f.State
	if state == "" {
		state = "open"
	}
	if !slices.Contains([]string{"open", "closed", "all"}, state) {
		return nil, fmt.Errorf("forge: issue state %q is not open, closed or all", state)
	}
	q := neturl.Values{"state": {state}, "per_page": {"100"}}
	if f.Label != "" {
		q.Set("labels", f.Label)
	}
	if f.Milestone != "" {
		n, err := c.resolveMilestone(ctx, f.Milestone)
		if err != nil {
			return nil, err
		}
		q.Set("milestone", strconv.Itoa(n))
	}
	return c.issuePages(ctx, c.repoURL("/issues?"+q.Encode()))
}

// SearchIssues finds the issues of the repo that match text, without pull requests.
func (c *GitHub) SearchIssues(ctx context.Context, text string) ([]Issue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	q := neturl.Values{"q": {"repo:" + c.Repo + " is:issue " + text}, "per_page": {"100"}}
	var out []Issue
	url := c.BaseURL + "/search/issues?" + q.Encode()
	for url != "" {
		var page struct {
			Items []issueItem `json:"items"`
		}
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			if it.PullRequest == nil {
				out = append(out, it.issue())
			}
		}
		url = next
	}
	return out, nil
}

// issuePages follows the pages of an issues list, without pull requests.
func (c *GitHub) issuePages(ctx context.Context, url string) ([]Issue, error) {
	var out []Issue
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
			out = append(out, it.issue())
		}
		url = next
	}
	return out, nil
}

// ghPull is a pull request in GitHub's REST shape.
type ghPull struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	State     string       `json:"state"`
	HTMLURL   string       `json:"html_url"`
	CreatedAt time.Time    `json:"created_at"`
	MergedAt  *time.Time   `json:"merged_at"`
	Milestone *ghMilestone `json:"milestone"`
	User      *struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Head struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p ghPull) change() Change {
	out := Change{Number: p.Number, Title: p.Title, Body: p.Body, State: p.State, URL: p.HTMLURL,
		CreatedAt: p.CreatedAt, HeadRef: p.Head.Ref, BaseRef: p.Base.Ref}
	if p.MergedAt != nil {
		out.State = "merged"
	}
	if p.User != nil {
		out.Author = p.User.Login
	}
	for _, l := range p.Labels {
		out.Labels = append(out.Labels, l.Name)
	}
	if p.Milestone != nil {
		m := p.Milestone.milestone()
		out.Milestone = &m
	}
	return out
}

// CreateChange opens a pull request. POST pulls takes no milestone, so a milestone is set by a
// second request on the issue side of the pull request. When that second step fails, the pull
// request exists: the change known from the POST comes back together with the error.
func (c *GitHub) CreateChange(ctx context.Context, in NewPR) (Change, error) {
	if err := c.configured(); err != nil {
		return Change{}, err
	}
	ms := 0
	if in.Milestone != "" {
		n, err := c.resolveMilestone(ctx, in.Milestone)
		if err != nil {
			return Change{}, err
		}
		ms = n
	}
	body := map[string]any{"title": in.Title, "body": in.Body, "head": in.Head, "base": in.Base}
	if in.Draft {
		body["draft"] = true
	}
	var p ghPull
	if _, err := c.do(ctx, http.MethodPost, c.repoURL("/pulls"), body, &p); err != nil {
		return Change{}, err
	}
	if ms == 0 {
		return p.change(), nil
	}
	url := c.repoURL("/issues/" + strconv.Itoa(p.Number))
	if _, err := c.do(ctx, http.MethodPatch, url, map[string]any{"milestone": ms}, nil); err != nil {
		return p.change(), fmt.Errorf("PR #%d created (%s), but setting the milestone failed: %w", p.Number, p.HTMLURL, err)
	}
	got, err := c.Change(ctx, p.Number)
	if err != nil {
		return p.change(), fmt.Errorf("PR #%d created (%s) with its milestone, but reading it back failed: %w", p.Number, p.HTMLURL, err)
	}
	return got, nil
}

// Change reads one pull request by number; State is merged for a closed one that was merged.
func (c *GitHub) Change(ctx context.Context, number int) (Change, error) {
	if err := c.configured(); err != nil {
		return Change{}, err
	}
	var p ghPull
	if _, err := c.do(ctx, http.MethodGet, c.repoURL("/pulls/"+strconv.Itoa(number)), nil, &p); err != nil {
		return Change{}, err
	}
	return p.change(), nil
}

// CreateChangeComment comments on a pull request; GitHub keeps those as issue comments.
func (c *GitHub) CreateChangeComment(ctx context.Context, number int, body string) (protocol.Comment, error) {
	return c.CreateComment(ctx, number, body)
}

// Milestones lists the milestones in state open, closed or all, following every page.
func (c *GitHub) Milestones(ctx context.Context, state string) ([]Milestone, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	if state == "" {
		state = "open"
	}
	var out []Milestone
	url := c.repoURL("/milestones?state=" + neturl.QueryEscape(state) + "&per_page=100")
	for url != "" {
		var page []ghMilestone
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, m := range page {
			out = append(out, m.milestone())
		}
		url = next
	}
	return out, nil
}

// MilestoneByNumber reads one milestone.
func (c *GitHub) MilestoneByNumber(ctx context.Context, number int) (Milestone, error) {
	if err := c.configured(); err != nil {
		return Milestone{}, err
	}
	var m ghMilestone
	if _, err := c.do(ctx, http.MethodGet, c.repoURL("/milestones/"+strconv.Itoa(number)), nil, &m); err != nil {
		return Milestone{}, err
	}
	return m.milestone(), nil
}

// CreateMilestone creates an open milestone.
func (c *GitHub) CreateMilestone(ctx context.Context, title, description string) (Milestone, error) {
	if err := c.configured(); err != nil {
		return Milestone{}, err
	}
	var m ghMilestone
	body := map[string]any{"title": title, "description": description}
	if _, err := c.do(ctx, http.MethodPost, c.repoURL("/milestones"), body, &m); err != nil {
		return Milestone{}, err
	}
	return m.milestone(), nil
}

// EditMilestone changes the title and/or description; a nil one stays as it is.
func (c *GitHub) EditMilestone(ctx context.Context, number int, title, description *string) (Milestone, error) {
	if err := c.configured(); err != nil {
		return Milestone{}, err
	}
	patch := map[string]any{}
	if title != nil {
		patch["title"] = *title
	}
	if description != nil {
		patch["description"] = *description
	}
	var m ghMilestone
	if _, err := c.do(ctx, http.MethodPatch, c.repoURL("/milestones/"+strconv.Itoa(number)), patch, &m); err != nil {
		return Milestone{}, err
	}
	return m.milestone(), nil
}

// Labels lists the names of all labels of the repo, following every page.
func (c *GitHub) Labels(ctx context.Context) ([]string, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var out []string
	url := c.repoURL("/labels?per_page=100")
	for url != "" {
		var page []struct {
			Name string `json:"name"`
		}
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, l := range page {
			out = append(out, l.Name)
		}
		url = next
	}
	return out, nil
}

// AddBlocker makes issue blocker block issue number. GitHub wants the database id of the
// blocker, not its number, as a JSON number.
func (c *GitHub) AddBlocker(ctx context.Context, number, blocker int) error {
	it, err := c.readIssue(ctx, blocker)
	if err != nil {
		return err
	}
	if it.ID == 0 {
		return fmt.Errorf("forge: issue %s#%d: no database id", c.Repo, blocker)
	}
	url := c.repoURL("/issues/" + strconv.Itoa(number) + "/dependencies/blocked_by")
	_, err = c.do(ctx, http.MethodPost, url, map[string]any{"issue_id": it.ID}, nil)
	return err
}

// API makes the raw request and returns the response body as it came. A GET that answers a
// JSON list follows the Link rel="next" pages and returns one list of all of them.
func (c *GitHub) API(ctx context.Context, r APIRequest) ([]byte, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	method, fields, err := r.resolve()
	if err != nil {
		return nil, err
	}
	path := strings.NewReplacer("{owner}", c.owner(), "{repo}", c.name()).Replace(strings.TrimPrefix(r.Path, "/"))
	url := c.BaseURL + "/" + path
	var body map[string]any
	if method == http.MethodGet || method == http.MethodDelete {
		url = withQuery(url, fields)
	} else if len(fields) > 0 {
		body = fields
	}
	var out []byte
	var reqBody any
	if body != nil {
		reqBody = body
	}
	next, err := c.do(ctx, method, url, reqBody, &out)
	if err != nil {
		return nil, err
	}
	if method != http.MethodGet || next == "" || !isJSONList(out) {
		return out, nil
	}
	pages := [][]byte{out}
	for next != "" {
		var page []byte
		if next, err = c.do(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return joinLists(pages)
}

// isJSONList is true when data is a JSON array.
func isJSONList(data []byte) bool {
	t := bytes.TrimSpace(data)
	return len(t) > 0 && t[0] == '['
}

// joinLists is the JSON lists of pages as one list. A page that is not a list is an error.
func joinLists(pages [][]byte) ([]byte, error) {
	all := []json.RawMessage{}
	for i, p := range pages {
		var items []json.RawMessage
		if err := json.Unmarshal(p, &items); err != nil {
			return nil, fmt.Errorf("forge api: page %d is not a JSON list: %w", i+1, err)
		}
		all = append(all, items...)
	}
	return json.Marshal(all)
}

// resolve is the method of the request (an empty one is GET, or POST with fields) and its
// fields with their JSON values.
func (r APIRequest) resolve() (string, map[string]any, error) {
	method := strings.ToUpper(r.Method)
	if method == "" {
		method = http.MethodGet
		if len(r.Fields) > 0 {
			method = http.MethodPost
		}
	}
	fields := map[string]any{}
	for _, f := range r.Fields {
		v, err := f.value()
		if err != nil {
			return "", nil, err
		}
		fields[f.Name] = v
	}
	return method, fields, nil
}

// withQuery adds the fields to the query string of url.
func withQuery(url string, fields map[string]any) string {
	if len(fields) == 0 {
		return url
	}
	q := neturl.Values{}
	for k, v := range fields {
		q.Set(k, fmt.Sprint(v))
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return url + sep + q.Encode()
}

// value is the JSON value of a field: text when untyped, else a scalar or file contents.
func (f APIField) value() (any, error) {
	if !f.Typed {
		return f.Value, nil
	}
	switch f.Value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if n, err := strconv.ParseInt(f.Value, 10, 64); err == nil {
		return n, nil
	}
	if path, ok := strings.CutPrefix(f.Value, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("forge api: field %s: %w", f.Name, err)
		}
		return string(data), nil
	}
	return f.Value, nil
}

// repoURL is the URL of a path under /repos/<repo>.
func (c *GitHub) repoURL(path string) string { return c.BaseURL + "/repos/" + c.Repo + path }

func (c *GitHub) owner() string { o, _, _ := strings.Cut(c.Repo, "/"); return o }

func (c *GitHub) name() string { _, n, _ := strings.Cut(c.Repo, "/"); return n }
