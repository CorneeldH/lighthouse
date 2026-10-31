package forge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type fakeResponse struct {
	status int
	body   string
	link   string
	err    error
}

// fakeTransport answers from a URL map and counts requests. With getOnly it fails the test on any non-GET.
type fakeTransport struct {
	t         *testing.T
	getOnly   bool
	mu        sync.Mutex
	responses map[string]fakeResponse
	requests  []*http.Request
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
	if f.getOnly && r.Method != http.MethodGet {
		f.t.Errorf("method %s: forge client must only GET", r.Method)
		return nil, fmt.Errorf("method %s not allowed", r.Method)
	}
	resp, ok := f.responses[r.URL.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected URL %s", r.URL)
	}
	if resp.err != nil {
		return nil, resp.err
	}
	h := http.Header{}
	if resp.link != "" {
		h.Set("Link", resp.link)
	}
	return &http.Response{
		StatusCode: resp.status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Request:    r,
	}, nil
}

func newTestClient(t *testing.T, responses map[string]fakeResponse) (*Client, *fakeTransport) {
	t.Helper()
	ft := &fakeTransport{t: t, getOnly: true, responses: responses}
	c := New("o/r", "secret-token")
	c.HTTP = &http.Client{Transport: ft}
	return c, ft
}

const (
	issuesURL1   = "https://api.github.com/repos/o/r/issues?state=open&per_page=100"
	issuesURL2   = "https://api.github.com/repos/o/r/issues?state=open&per_page=100&page=2"
	commentsURL1 = "https://api.github.com/repos/o/r/issues/7/comments?per_page=100"
	commentsURL2 = "https://api.github.com/repos/o/r/issues/7/comments?per_page=100&page=2"
)

func nextLink(url string) string { return fmt.Sprintf(`<%s>; rel="next", <%s>; rel="last"`, url, url) }

func issuesJSON(from, count int) string {
	items := make([]string, 0, count)
	for i := from; i < from+count; i++ {
		items = append(items, fmt.Sprintf(`{"number":%d,"title":"t%d","html_url":"https://github.com/o/r/issues/%d","comments":%d}`, i, i, i, i%3))
	}
	return strings.Join(items, ",")
}

func TestOpenIssuesFollowsPages(t *testing.T) {
	page2 := `[` + issuesJSON(101, 1) + `,{"number":999,"title":"pr","html_url":"u","comments":0,"pull_request":{}}]`
	c, ft := newTestClient(t, map[string]fakeResponse{
		issuesURL1: {status: 200, body: `[` + issuesJSON(1, 100) + `]`, link: nextLink(issuesURL2)},
		issuesURL2: {status: 200, body: page2},
	})
	issues, err := c.OpenIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 101 {
		t.Fatalf("got %d issues, want 101 (PR excluded)", len(issues))
	}
	for _, i := range issues {
		if i.Number == 999 {
			t.Errorf("pull request %d must not be listed", i.Number)
		}
	}
	last := issues[100]
	if last.Number != 101 || last.Title != "t101" || last.URL != "https://github.com/o/r/issues/101" || last.Comments != 101%3 {
		t.Errorf("last issue mapped wrong: %+v", last)
	}
	if len(ft.requests) != 2 {
		t.Errorf("got %d requests, want 2", len(ft.requests))
	}
}

func commentsJSON(from, count int, lastRockets int) string {
	items := make([]string, 0, count)
	for i := from; i < from+count; i++ {
		rockets := 0
		if i == from+count-1 {
			rockets = lastRockets
		}
		items = append(items, fmt.Sprintf(`{"body":"b%d","created_at":"2026-01-01T00:00:%02dZ","html_url":"https://github.com/o/r/issues/7#issuecomment-%d","reactions":{"rocket":%d,"+1":5}}`, i, i%60, i, rockets))
	}
	return strings.Join(items, ",")
}

func TestCommentsFollowsPages(t *testing.T) {
	c, _ := newTestClient(t, map[string]fakeResponse{
		commentsURL1: {status: 200, body: `[` + commentsJSON(0, 100, 0) + `]`, link: nextLink(commentsURL2)},
		commentsURL2: {status: 200, body: `[` + commentsJSON(100, 1, 2) + `]`},
	})
	comments, err := c.Comments(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 101 {
		t.Fatalf("got %d comments, want 101", len(comments))
	}
	last := comments[100]
	if last.Rockets != 2 {
		t.Errorf("Rockets = %d, want 2", last.Rockets)
	}
	if last.Body != "b100" || last.CreatedAt != "2026-01-01T00:00:40Z" || last.URL != "https://github.com/o/r/issues/7#issuecomment-100" {
		t.Errorf("last comment mapped wrong: %+v", last)
	}
	if comments[0].Rockets != 0 {
		t.Errorf("first Rockets = %d, want 0", comments[0].Rockets)
	}
}

func TestRequestHeaders(t *testing.T) {
	c, ft := newTestClient(t, map[string]fakeResponse{
		issuesURL1: {status: 200, body: `[]`},
	})
	if _, err := c.OpenIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := ft.requests[0].Header
	want := map[string]string{
		"Authorization":        "Bearer secret-token",
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
}

func TestNewDefaults(t *testing.T) {
	c := New("o/r", "t")
	if c.BaseURL != "https://api.github.com" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.HTTP == nil || c.HTTP.Timeout.Seconds() != 30 {
		t.Errorf("HTTP client = %+v, want timeout 30s", c.HTTP)
	}
}

func TestNotConfigured(t *testing.T) {
	for name, c := range map[string]*Client{"no token": New("o/r", ""), "no repo": New("", "t")} {
		t.Run(name, func(t *testing.T) {
			ft := &fakeTransport{t: t}
			c.HTTP = &http.Client{Transport: ft}
			issues, err := c.OpenIssues(context.Background())
			if !errors.Is(err, ErrNotConfigured) || issues != nil {
				t.Errorf("OpenIssues = %v, %v", issues, err)
			}
			comments, err := c.Comments(context.Background(), 1)
			if !errors.Is(err, ErrNotConfigured) || comments != nil {
				t.Errorf("Comments = %v, %v", comments, err)
			}
			if len(ft.requests) != 0 {
				t.Errorf("got %d requests, want 0", len(ft.requests))
			}
		})
	}
}

func TestForgeErrors(t *testing.T) {
	cases := map[string]fakeResponse{
		"500":          {status: 500, body: `{"message":"boom"}`},
		"401":          {status: 401, body: `{"message":"Bad credentials"}`},
		"invalid json": {status: 200, body: `not json`},
		"transport":    {err: errors.New("connection refused")},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := newTestClient(t, map[string]fakeResponse{issuesURL1: resp, commentsURL1: resp})
			issues, err := c.OpenIssues(context.Background())
			if err == nil || issues != nil {
				t.Errorf("OpenIssues = %v, %v; want nil slice and error", issues, err)
			}
			comments, err := c.Comments(context.Background(), 7)
			if err == nil || comments != nil {
				t.Errorf("Comments = %v, %v; want nil slice and error", comments, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-token") {
				t.Errorf("error leaks the token: %v", err)
			}
		})
	}
}

func TestErrorOnLaterPageDropsPartialList(t *testing.T) {
	c, _ := newTestClient(t, map[string]fakeResponse{
		issuesURL1: {status: 200, body: `[` + issuesJSON(1, 100) + `]`, link: nextLink(issuesURL2)},
		issuesURL2: {status: 502, body: `bad gateway`},
	})
	issues, err := c.OpenIssues(context.Background())
	if err == nil || issues != nil {
		t.Errorf("OpenIssues = %d issues, %v; want nil and error", len(issues), err)
	}
}

func TestReadsOnlyGET(t *testing.T) {
	c, ft := newTestClient(t, map[string]fakeResponse{
		issuesURL1:   {status: 200, body: `[]`},
		commentsURL1: {status: 200, body: `[]`},
	})
	if _, err := c.OpenIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Comments(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(ft.requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(ft.requests))
	}
	// The fake transport fails the test on any method other than GET.
}

func TestNextPageOnOtherHostIsRefused(t *testing.T) {
	evil := "https://evil.example/repositories/1/issues?page=2"
	methods := map[string]func(c *Client) (any, error){
		"OpenIssues": func(c *Client) (any, error) { return c.OpenIssues(context.Background()) },
		"Comments":   func(c *Client) (any, error) { return c.Comments(context.Background(), 7) },
	}
	for name, call := range methods {
		t.Run(name, func(t *testing.T) {
			c, ft := newTestClient(t, map[string]fakeResponse{
				issuesURL1:   {status: 200, body: `[` + issuesJSON(1, 1) + `]`, link: nextLink(evil)},
				commentsURL1: {status: 200, body: `[]`, link: nextLink(evil)},
			})
			got, err := call(c)
			if err == nil {
				t.Fatal("err = nil, want an error for a next page on another host")
			}
			if s, ok := got.([]Issue); ok && s != nil {
				t.Fatalf("issues = %v, want nil", s)
			}
			for _, r := range ft.requests {
				if r.URL.Host == "evil.example" {
					t.Fatalf("request sent to %s", r.URL)
				}
			}
		})
	}
}

func TestNextPageOnSameHostWithOtherPathIsFollowed(t *testing.T) {
	next := "https://api.github.com/repositories/42/issues?state=open&per_page=100&page=2"
	c, _ := newTestClient(t, map[string]fakeResponse{
		issuesURL1: {status: 200, body: `[` + issuesJSON(1, 1) + `]`, link: nextLink(next)},
		next:       {status: 200, body: `[` + issuesJSON(2, 1) + `]`},
	})
	got, err := c.OpenIssues(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v; want 2 issues", got, err)
	}
}

func TestErrorIncludesGitHubMessage(t *testing.T) {
	c, _ := newTestClient(t, map[string]fakeResponse{
		issuesURL1: {status: 401, body: `{"message":"Bad credentials"}`},
	})
	_, err := c.OpenIssues(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("err = %v, want it to contain Bad credentials", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err leaks the token: %v", err)
	}
}
