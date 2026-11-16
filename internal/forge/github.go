// Package forge reads and writes issues, comments, acknowledgements and the board of a forge;
// GitHub is the one implementation so far. The GitHub client also squash-merges a pull request
// at a measured head.
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
	"strings"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// ErrNotConfigured means the client has no repo or no token, so no request was made.
var ErrNotConfigured = errors.New("forge not configured")

// Issue is an issue as the forge returns it; OpenIssues lists only open ones. Pull requests are not issues here.
type Issue struct {
	Number   int
	Title    string
	URL      string // html_url
	Comments int    // comment count from the list
	State    string // open, closed
	// StateReason is why an issue closed: "completed", "not_planned" or "" (open or unknown).
	StateReason string
	Body        string
	Author      string // user.login
	Labels      []string
	UpdatedAt   time.Time
	ClosedAt    time.Time  // zero while open
	Milestone   *Milestone // nil: none
}

// GitHub is a GitHub REST and GraphQL client for one repository.
type GitHub struct {
	Repo, Token, BaseURL string
	HTTP                 *http.Client
	// Board is the [forge.board] of the repo; nil makes every board method fail.
	Board *repoconfig.Board

	mu      sync.Mutex
	nodeIDs map[int]string // issue number → node ID, filled by Issue and OpenIssues
}

// DefaultBaseURL is the GitHub API root.
const DefaultBaseURL = "https://api.github.com"

// New returns a client for repo ("owner/name") with the given token.
func New(repo, token string) *GitHub {
	return &GitHub{
		Repo:    repo,
		Token:   token,
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Kind is "github".
func (c *GitHub) Kind() string { return "github" }

// remember keeps the node ID of an issue for AddToBoard.
func (c *GitHub) remember(it issueItem) {
	if it.NodeID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nodeIDs == nil {
		c.nodeIDs = map[int]string{}
	}
	c.nodeIDs[it.Number] = it.NodeID
}

// nodeID is the node ID of issue number: from the cache, else from one read of the issue.
func (c *GitHub) nodeID(ctx context.Context, number int) (string, error) {
	c.mu.Lock()
	id := c.nodeIDs[number]
	c.mu.Unlock()
	if id != "" {
		return id, nil
	}
	is, err := c.readIssue(ctx, number)
	if err != nil {
		return "", err
	}
	if is.NodeID == "" {
		return "", fmt.Errorf("forge: issue %s#%d: no node ID", c.Repo, number)
	}
	return is.NodeID, nil
}

func (c *GitHub) configured() error {
	if c.Repo == "" || c.Token == "" {
		return ErrNotConfigured
	}
	return nil
}

// OpenIssues lists all open issues of the repo, following every page, without pull requests.
func (c *GitHub) OpenIssues(ctx context.Context) ([]Issue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var out []Issue
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=100", c.BaseURL, c.Repo)
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
			c.remember(it)
			out = append(out, it.issue())
		}
		url = next
	}
	return out, nil
}

// Issue reads one issue by number, whether it is open or closed.
func (c *GitHub) Issue(ctx context.Context, number int) (Issue, error) {
	it, err := c.readIssue(ctx, number)
	if err != nil {
		return Issue{}, err
	}
	return it.issue(), nil
}

// readIssue reads one issue in REST shape and remembers its node ID.
func (c *GitHub) readIssue(ctx context.Context, number int) (issueItem, error) {
	if err := c.configured(); err != nil {
		return issueItem{}, err
	}
	var it issueItem
	url := fmt.Sprintf("%s/repos/%s/issues/%d", c.BaseURL, c.Repo, number)
	if _, err := c.do(ctx, http.MethodGet, url, nil, &it); err != nil {
		return issueItem{}, err
	}
	c.remember(it)
	return it, nil
}

// Comments lists all comments of one issue, following every page.
func (c *GitHub) Comments(ctx context.Context, number int) ([]protocol.Comment, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type item struct {
		ID        int64  `json:"id"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		HTMLURL   string `json:"html_url"`
		User      *struct {
			Login string `json:"login"`
		} `json:"user"`
		Reactions struct {
			Rocket int `json:"rocket"`
		} `json:"reactions"`
	}
	var out []protocol.Comment
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=100", c.BaseURL, c.Repo, number)
	for url != "" {
		var page []item
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			author := ""
			if it.User != nil {
				author = it.User.Login
			}
			out = append(out, protocol.Comment{Body: it.Body, CreatedAt: it.CreatedAt, URL: it.HTMLURL, Rockets: it.Reactions.Rocket, ID: it.ID, Author: author})
		}
		url = next
	}
	return out, nil
}

// do is the only place that makes a request. A non-nil body is sent as JSON; a non-nil into
// receives the decoded JSON response. It returns the URL of the next page, or "" when there is none.
func (c *GitHub) do(ctx context.Context, method, url string, body, into any) (string, error) {
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
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
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
	next := nextPage(resp.Header.Get("Link"))
	if err := c.sameOrigin(next); err != nil {
		return "", err
	}
	return next, nil
}

// statusError describes a non-2xx response, with GitHub's own message when the body has one.
// It never includes the token.
func statusError(method, url string, resp *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	msg := fmt.Sprintf("forge %s %s: status %d", method, url, resp.StatusCode)
	if json.Unmarshal(data, &body) == nil && body.Message != "" {
		msg += ": " + body.Message
	}
	rateLimited := resp.Header.Get("x-ratelimit-remaining") == "0" ||
		resp.Header.Get("retry-after") != "" ||
		strings.Contains(strings.ToLower(body.Message), "rate limit")
	return &httpStatusError{Code: resp.StatusCode, msg: msg, Message: body.Message, RateLimited: rateLimited}
}

// httpStatusError is a non-2xx response; callers read Code with errors.As.
type httpStatusError struct {
	Code int
	msg  string
	// Message is GitHub's own message, "" when the body has none.
	Message string
	// RateLimited is true when the response signals a rate limit: no requests remaining, a
	// retry-after header, or "rate limit" in the message.
	RateLimited bool
}

func (e *httpStatusError) Error() string { return e.msg }

// sameOrigin refuses a next-page URL whose scheme or host differs from BaseURL, so the
// token never goes to another host. Only scheme and host count: GitHub's Link URLs use
// /repositories/<id>/..., not /repos/owner/name/....
func (c *GitHub) sameOrigin(next string) error {
	if next == "" {
		return nil
	}
	base, err := neturl.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("forge base URL: %w", err)
	}
	u, err := neturl.Parse(next)
	if err != nil {
		return fmt.Errorf("forge next page %q: %w", next, err)
	}
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return fmt.Errorf("forge next page %s: not on %s://%s", next, base.Scheme, base.Host)
	}
	return nil
}

// nextPage returns the rel="next" URL of a Link header, or "".
func nextPage(link string) string {
	for _, part := range strings.Split(link, ",") {
		url, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(params, `rel="next"`) {
			continue
		}
		return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(url), "<"), ">")
	}
	return ""
}
