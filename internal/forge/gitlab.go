package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// GitLab is a GitLab REST client for one project. Pull requests are merge requests, the board
// is the project, and a status is the built-in issue status.
type GitLab struct {
	// Repo is the project path, "group/sub/project".
	Repo, Token, BaseURL string
	HTTP                 *http.Client
	// Board is the [forge.board] of the repo; nil makes every board method fail.
	Board *repoconfig.Board

	mu      sync.Mutex
	userIDs map[string]int64 // username → user ID, filled by Role
}

// DefaultGitLabHost is the host of a repo without [forge] host.
const DefaultGitLabHost = "gitlab.com"

// newGitLab returns the client for cfg, whose Kind is "gitlab".
func newGitLab(cfg Config) *GitLab {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		host := cfg.Host
		if host == "" {
			host = DefaultGitLabHost
		}
		base = "https://" + host + "/api/v4"
	}
	g := &GitLab{Repo: cfg.Repo, Token: cfg.Token, BaseURL: base, Board: cfg.Board,
		HTTP: &http.Client{Timeout: 30 * time.Second}}
	if cfg.HTTP != nil {
		g.HTTP = cfg.HTTP
	}
	return g
}

// Kind is "gitlab".
func (g *GitLab) Kind() string { return "gitlab" }

func (g *GitLab) configured() error {
	if g.Repo == "" || g.Token == "" {
		return ErrNotConfigured
	}
	return nil
}

// project is the project path as one URL path segment: every "/" is %2F.
func (g *GitLab) project() string { return neturl.PathEscape(g.Repo) }

// projectPath is the path under the API root of sub, which starts with "/" or is empty.
func (g *GitLab) projectPath(sub string) string { return "/projects/" + g.project() + sub }

// do makes a request to path under the API root. A non-nil body is sent as JSON; a non-nil
// into receives the decoded JSON response. It returns the x-next-page header, "" on the last
// page. The next page is a number, never a URL, so the token cannot go to another host.
func (g *GitLab) do(ctx context.Context, method, path string, q neturl.Values, body, into any) (string, error) {
	url := g.BaseURL + path
	if len(q) > 0 {
		url += "?" + q.Encode()
	}
	return g.send(ctx, method, url, body, into)
}

// send is the only place that makes a request. url is under the host of BaseURL: the API root,
// or the GraphQL endpoint next to it.
func (g *GitLab) send(ctx context.Context, method, url string, body, into any) (string, error) {
	if err := g.configured(); err != nil {
		return "", err
	}
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("forge %s %s: encode: %w", method, url, err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return "", fmt.Errorf("forge request %s: %w", url, err)
	}
	req.Header.Set("PRIVATE-TOKEN", g.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.sameHostClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("forge %s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", statusError(method, url, resp)
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			return "", fmt.Errorf("forge %s %s: decode: %w", method, url, err)
		}
	}
	return resp.Header.Get("x-next-page"), nil
}

// sameHostClient is a copy of g.HTTP that refuses a redirect to another scheme or host: the
// PRIVATE-TOKEN header is custom, so net/http would forward it. The caller's client is not
// changed, and its own CheckRedirect still runs.
func (g *GitLab) sameHostClient() *http.Client {
	c := *g.HTTP
	inner := c.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		base, err := neturl.Parse(g.BaseURL)
		if err != nil {
			return fmt.Errorf("forge base URL: %w", err)
		}
		if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
			return fmt.Errorf("forge redirect to %s://%s refused: not on %s://%s", req.URL.Scheme, req.URL.Host, base.Scheme, base.Host)
		}
		if inner != nil {
			return inner(req, via)
		}
		if len(via) >= 10 {
			return errors.New("forge: stopped after 10 redirects")
		}
		return nil
	}
	return &c
}

// get reads one page of path under the API root into out and returns the next page number.
func (g *GitLab) get(ctx context.Context, path string, q neturl.Values, out any) (next string, err error) {
	return g.do(ctx, http.MethodGet, path, q, nil, out)
}

// getAll reads every page of the list at path, 100 records a page, following x-next-page.
func getAll[T any](ctx context.Context, g *GitLab, path string, q neturl.Values) ([]T, error) {
	q = cloneValues(q)
	q.Set("per_page", "100")
	var out []T
	for page := ""; ; {
		if page != "" {
			q.Set("page", page)
		}
		var items []T
		next, err := g.get(ctx, path, q, &items)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == "" {
			return out, nil
		}
		page = next
	}
}

func cloneValues(q neturl.Values) neturl.Values {
	out := neturl.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// glIssue is an issue in GitLab's REST shape.
type glIssue struct {
	IID       int        `json:"iid"`
	Title     string     `json:"title"`
	WebURL    string     `json:"web_url"`
	Notes     int        `json:"user_notes_count"`
	State     string     `json:"state"`
	Desc      string     `json:"description"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Author    *struct {
		Username string `json:"username"`
	} `json:"author"`
	Labels    []string `json:"labels"`
	Milestone *struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		State       string `json:"state"`
		Description string `json:"description"`
	} `json:"milestone"`
}

func (it glIssue) issue() Issue {
	out := Issue{Number: it.IID, Title: it.Title, URL: it.WebURL, Comments: it.Notes, Body: it.Desc,
		Labels: it.Labels, UpdatedAt: it.UpdatedAt, State: it.State}
	if it.State == "opened" {
		out.State = "open"
	}
	if it.ClosedAt != nil {
		out.ClosedAt = *it.ClosedAt
	}
	if it.Author != nil {
		out.Author = it.Author.Username
	}
	if m := it.Milestone; m != nil {
		// Milestone.Number is the global ID: the iid repeats across project and group milestones.
		state := m.State
		if state == "active" {
			state = "open"
		}
		out.Milestone = &Milestone{Number: m.ID, Title: m.Title, State: state, Description: m.Description}
	}
	return out
}

// Issue reads one issue by iid, whether it is open or closed.
func (g *GitLab) Issue(ctx context.Context, number int) (Issue, error) {
	var it glIssue
	if _, err := g.get(ctx, g.projectPath("/issues/"+strconv.Itoa(number)), nil, &it); err != nil {
		return Issue{}, err
	}
	return it.issue(), nil
}

// OpenIssues lists all open issues of the project, following every page.
func (g *GitLab) OpenIssues(ctx context.Context) ([]Issue, error) {
	items, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), neturl.Values{"state": {"opened"}})
	if err != nil {
		return nil, err
	}
	return issues(items), nil
}

func issues(items []glIssue) []Issue {
	out := make([]Issue, 0, len(items))
	for _, it := range items {
		out = append(out, it.issue())
	}
	return out
}

// ClosedInMilestone lists the closed issues of milestone m (filtered on m.Title) that closed
// at or after since. updated_after narrows the list on the server; closed_at decides.
func (g *GitLab) ClosedInMilestone(ctx context.Context, m Milestone, since time.Time) ([]Issue, error) {
	q := neturl.Values{"milestone": {m.Title}, "state": {"closed"}, "updated_after": {since.UTC().Format(time.RFC3339)}}
	items, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), q)
	if err != nil {
		return nil, err
	}
	var out []Issue
	for _, is := range issues(items) {
		if !is.ClosedAt.Before(since) {
			out = append(out, is)
		}
	}
	return out, nil
}

// Login returns the username of the token's user.
func (g *GitLab) Login(ctx context.Context) (string, error) {
	var u struct {
		Username string `json:"username"`
	}
	if _, err := g.get(ctx, "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Username, nil
}

// accessRoles translates a GitLab access level to the role words of [dispatch] answer_permissions.
var accessRoles = map[int]string{10: "read", 15: "triage", 20: "triage", 30: "write", 40: "maintain", 50: "admin"}

// Role returns the role of user in the project, inherited membership included; "" for a user
// that is no member or that GitLab does not know. The user ID is cached per client, and a
// client lives one tick.
func (g *GitLab) Role(ctx context.Context, user string) (string, error) {
	id, ok, err := g.userID(ctx, user)
	if err != nil || !ok {
		return "", err
	}
	var m struct {
		AccessLevel int `json:"access_level"`
	}
	if _, err := g.get(ctx, g.projectPath("/members/all/"+strconv.FormatInt(id, 10)), nil, &m); err != nil {
		var se *httpStatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return accessRoles[m.AccessLevel], nil
}

// userID looks up the ID of username, once per client.
func (g *GitLab) userID(ctx context.Context, username string) (int64, bool, error) {
	g.mu.Lock()
	id, ok := g.userIDs[username]
	g.mu.Unlock()
	if ok {
		return id, true, nil
	}
	var users []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if _, err := g.get(ctx, "/users", neturl.Values{"username": {username}}, &users); err != nil {
		return 0, false, err
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, username) {
			g.mu.Lock()
			if g.userIDs == nil {
				g.userIDs = map[string]int64{}
			}
			g.userIDs[username] = u.ID
			g.mu.Unlock()
			return u.ID, true, nil
		}
	}
	return 0, false, nil
}

// glNote is a note in GitLab's REST shape.
type glNote struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	System    bool   `json:"system"`
	Author    *struct {
		Username string `json:"username"`
	} `json:"author"`
}

// comment turns a note of issue number into a comment with rockets rocket awards. A note has
// no URL of its own: it is built from the API root, which is <web root>/api/v4.
func (g *GitLab) comment(n glNote, number, rockets int) protocol.Comment {
	c := protocol.Comment{ID: n.ID, Body: n.Body, CreatedAt: n.CreatedAt, Rockets: rockets}
	if n.Author != nil {
		c.Author = n.Author.Username
	}
	if web, ok := strings.CutSuffix(g.BaseURL, "/api/v4"); ok {
		c.URL = fmt.Sprintf("%s/%s/-/issues/%d#note_%d", web, g.Repo, number, n.ID)
	}
	return c
}

// Comments lists the notes of one issue that are no system note, oldest first. The notes list
// carries no award emoji, so every note costs one more call to count its rockets: the calls
// per Comments are bounded by the notes of that one issue.
func (g *GitLab) Comments(ctx context.Context, number int) ([]protocol.Comment, error) {
	base := g.projectPath("/issues/" + strconv.Itoa(number) + "/notes")
	notes, err := getAll[glNote](ctx, g, base, neturl.Values{"sort": {"asc"}, "order_by": {"created_at"}})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Comment, 0, len(notes))
	for _, n := range notes {
		if n.System {
			continue
		}
		awards, err := getAll[struct {
			Name string `json:"name"`
		}](ctx, g, base+"/"+strconv.FormatInt(n.ID, 10)+"/award_emoji", nil)
		if err != nil {
			return nil, err
		}
		rockets := 0
		for _, a := range awards {
			if a.Name == "rocket" {
				rockets++
			}
		}
		out = append(out, g.comment(n, number, rockets))
	}
	return out, nil
}

// CreateComment adds a note to issue number and returns it.
func (g *GitLab) CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error) {
	var n glNote
	path := g.projectPath("/issues/" + strconv.Itoa(number) + "/notes")
	if _, err := g.do(ctx, http.MethodPost, path, nil, map[string]string{"body": body}, &n); err != nil {
		return protocol.Comment{}, err
	}
	return g.comment(n, number, 0), nil
}

// Planning is not built for GitLab yet.
func (c *GitLab) Planning(context.Context) (Planning, error) {
	return Planning{}, errors.New("forge: Planning is not supported on gitlab yet")
}
