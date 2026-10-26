// Package forge reads issues and comments from GitHub. It only ever issues GET requests.
package forge

import (
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

// Client is a read-only GitHub REST client.
type Client struct {
	Repo, Token, BaseURL string
	HTTP                 *http.Client
}

// New returns a client for repo ("owner/name") with the given token.
func New(repo, token string) *Client {
	return &Client{
		Repo:    repo,
		Token:   token,
		BaseURL: "https://api.github.com",
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
		next, err := c.get(ctx, url, &page)
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
		next, err := c.get(ctx, url, &page)
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

// get is the only place that makes a request. It decodes the JSON body into into and
// returns the URL of the next page, or "" when there is none.
func (c *Client) get(ctx context.Context, url string, into any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("forge request %s: %w", url, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("forge GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", statusError(url, resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return "", fmt.Errorf("forge GET %s: decode: %w", url, err)
	}
	next := nextPage(resp.Header.Get("Link"))
	if err := c.sameOrigin(next); err != nil {
		return "", err
	}
	return next, nil
}

// statusError describes a non-2xx response, with GitHub's own message when the body has one.
// It never includes the token.
func statusError(url string, resp *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(data, &body) == nil && body.Message != "" {
		return fmt.Errorf("forge GET %s: status %d: %s", url, resp.StatusCode, body.Message)
	}
	return fmt.Errorf("forge GET %s: status %d", url, resp.StatusCode)
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
