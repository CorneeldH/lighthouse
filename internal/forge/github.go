// Package forge reads and writes GitHub issues, comments, reactions and the Projects v2 board. It never merges.
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
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// ErrNotConfigured means the client has no repo or no token, so no request was made.
var ErrNotConfigured = errors.New("forge not configured")

// Issue is an open issue as listed by the forge. Pull requests are not issues here.
type Issue struct {
	Number   int
	Title    string
	URL      string // html_url
	Comments int    // comment count from the list
}

// Client is a GitHub REST client.
type Client struct {
	Repo, Token, BaseURL string
	HTTP                 *http.Client
}

// DefaultBaseURL is the GitHub API root.
const DefaultBaseURL = "https://api.github.com"

// New returns a client for repo ("owner/name") with the given token.
func New(repo, token string) *Client {
	return &Client{
		Repo:    repo,
		Token:   token,
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) configured() error {
	if c.Repo == "" || c.Token == "" {
		return ErrNotConfigured
	}
	return nil
}

// OpenIssues lists all open issues of the repo, following every page, without pull requests.
func (c *Client) OpenIssues(ctx context.Context) ([]Issue, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type item struct {
		Number      int             `json:"number"`
		Title       string          `json:"title"`
		HTMLURL     string          `json:"html_url"`
		Comments    int             `json:"comments"`
		PullRequest json.RawMessage `json:"pull_request"`
	}
	var out []Issue
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=100", c.BaseURL, c.Repo)
	for url != "" {
		var page []item
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			if it.PullRequest != nil {
				continue
			}
			out = append(out, Issue{Number: it.Number, Title: it.Title, URL: it.HTMLURL, Comments: it.Comments})
		}
		url = next
	}
	return out, nil
}

// Comments lists all comments of one issue, following every page.
func (c *Client) Comments(ctx context.Context, number int) ([]protocol.Comment, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type item struct {
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		HTMLURL   string `json:"html_url"`
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
			out = append(out, protocol.Comment{Body: it.Body, CreatedAt: it.CreatedAt, URL: it.HTMLURL, Rockets: it.Reactions.Rocket})
		}
		url = next
	}
	return out, nil
}

// do is the only place that makes a request. A non-nil body is sent as JSON; a non-nil into
// receives the decoded JSON response. It returns the URL of the next page, or "" when there is none.
func (c *Client) do(ctx context.Context, method, url string, body, into any) (string, error) {
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
	if json.Unmarshal(data, &body) == nil && body.Message != "" {
		return fmt.Errorf("forge %s %s: status %d: %s", method, url, resp.StatusCode, body.Message)
	}
	return fmt.Errorf("forge %s %s: status %d", method, url, resp.StatusCode)
}

// sameOrigin refuses a next-page URL whose scheme or host differs from BaseURL, so the
// token never goes to another host. Only scheme and host count: GitHub's Link URLs use
// /repositories/<id>/..., not /repos/owner/name/....
func (c *Client) sameOrigin(next string) error {
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
