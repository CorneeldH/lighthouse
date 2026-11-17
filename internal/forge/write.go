package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// CreateComment posts a comment on issue number and returns it as the forge stored it.
func (c *GitHub) CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error) {
	if err := c.configured(); err != nil {
		return protocol.Comment{}, err
	}
	var out struct {
		ID        int64  `json:"id"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		Body      string `json:"body"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.BaseURL, c.Repo, number)
	if _, err := c.do(ctx, http.MethodPost, url, map[string]string{"body": body}, &out); err != nil {
		return protocol.Comment{}, err
	}
	return protocol.Comment{ID: out.ID, Author: out.User.Login, Body: out.Body, CreatedAt: out.CreatedAt, URL: out.HTMLURL}, nil
}

// Acknowledge adds a 🚀 reaction to a comment. GitHub answers 200 when the reaction already
// existed and 201 when it is new; both are success. number is not used: GitHub addresses a
// comment by its ID alone.
func (c *GitHub) Acknowledge(ctx context.Context, _ int, commentID int64) error {
	if err := c.configured(); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/issues/comments/%d/reactions", c.BaseURL, c.Repo, commentID)
	_, err := c.do(ctx, http.MethodPost, url, map[string]string{"content": "rocket"}, nil)
	return err
}

// ErrHeadMoved means the head (or base) of the PR changed since the sha was measured.
var ErrHeadMoved = errors.New("forge: head moved")

// MergeRefused is a merge GitHub declined for a reason that retrying will not fix.
type MergeRefused struct{ Message string }

func (e *MergeRefused) Error() string { return "forge: merge refused: " + e.Message }

// MergePR squash-merges pull request number, only when its head is still sha.
func (c *GitHub) MergePR(ctx context.Context, number int, sha string) error {
	if err := c.configured(); err != nil {
		return err
	}
	var out struct {
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
	}
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/merge", c.BaseURL, c.Repo, number)
	_, err := c.do(ctx, http.MethodPut, url, map[string]string{"sha": sha, "merge_method": "squash"}, &out)
	if err == nil {
		if !out.Merged {
			return &MergeRefused{Message: out.Message}
		}
		return nil
	}
	var se *httpStatusError
	if !errors.As(err, &se) {
		return err
	}
	switch {
	case se.Code == http.StatusConflict:
		return ErrHeadMoved
	case se.Code == http.StatusMethodNotAllowed && strings.Contains(se.Message, "Base branch was modified"):
		return ErrHeadMoved
	case se.Code == http.StatusMethodNotAllowed:
		return &MergeRefused{Message: se.Message}
	case se.Code == http.StatusForbidden && !se.RateLimited:
		return &MergeRefused{Message: se.Message}
	}
	return err
}

// Item is an issue or a pull request found by marker, open or closed. Body is complete.
type Item struct {
	Number  int
	IsPR    bool
	State   string // "open" or "closed", as the issues list names it; a merged PR is "closed"
	Body    string
	HeadRef string // of a PR; "" for an issue
	// Author is the login that opened the issue or pull request; "" when the forge names none.
	Author string
}

// UpdateIssueBody replaces the body of issue number.
func (c *GitHub) UpdateIssueBody(ctx context.Context, number int, body string) error {
	return c.patch(ctx, "issues", number, map[string]string{"body": body})
}

// CloseIssue comments on issue number when comment is not empty, then closes it.
func (c *GitHub) CloseIssue(ctx context.Context, number int, comment string) error {
	return c.closeWithComment(ctx, "issues", number, comment)
}

// UpdatePRBody replaces the body of pull request number.
func (c *GitHub) UpdatePRBody(ctx context.Context, number int, body string) error {
	return c.patch(ctx, "pulls", number, map[string]string{"body": body})
}

// ClosePR comments on pull request number when comment is not empty, then closes it unmerged.
func (c *GitHub) ClosePR(ctx context.Context, number int, comment string) error {
	return c.closeWithComment(ctx, "pulls", number, comment)
}

// closeWithComment comments first, so a failed close leaves the reason on the item.
func (c *GitHub) closeWithComment(ctx context.Context, kind string, number int, comment string) error {
	if err := c.configured(); err != nil {
		return err
	}
	if comment != "" {
		if _, err := c.CreateComment(ctx, number, comment); err != nil {
			return err
		}
	}
	return c.patch(ctx, kind, number, map[string]string{"state": "closed"})
}

// patch sends PATCH /repos/{repo}/{kind}/{number}; kind is "issues" or "pulls".
func (c *GitHub) patch(ctx context.Context, kind string, number int, fields map[string]string) error {
	if err := c.configured(); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/%s/%d", c.BaseURL, c.Repo, kind, number)
	_, err := c.do(ctx, http.MethodPatch, url, fields, nil)
	return err
}

// prItem is a pull request in GitHub's REST shape.
type prItem struct {
	Number   int        `json:"number"`
	State    string     `json:"state"`
	Draft    bool       `json:"draft"`
	HTMLURL  string     `json:"html_url"`
	Body     string     `json:"body"`
	MergedAt *time.Time `json:"merged_at"`
	ClosedAt *time.Time `json:"closed_at"`
	Head     struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// CreatePR opens a pull request. Checks and Mergeable stay empty: a new PR has neither yet.
func (c *GitHub) CreatePR(ctx context.Context, pr NewPR) (PR, error) {
	if err := c.configured(); err != nil {
		return PR{}, err
	}
	var out prItem
	url := fmt.Sprintf("%s/repos/%s/pulls", c.BaseURL, c.Repo)
	req := map[string]any{"title": pr.Title, "body": pr.Body, "head": pr.Head, "base": pr.Base, "draft": pr.Draft}
	if _, err := c.do(ctx, http.MethodPost, url, req, &out); err != nil {
		return PR{}, err
	}
	state := strings.ToUpper(out.State)
	if out.MergedAt != nil {
		state = "MERGED"
	}
	got := PR{Number: out.Number, State: state, Draft: out.Draft, HeadSHA: out.Head.SHA, HeadRef: out.Head.Ref, URL: out.HTMLURL}
	if out.MergedAt != nil {
		got.MergedAt = *out.MergedAt
	}
	if out.ClosedAt != nil {
		got.ClosedAt = *out.ClosedAt
	}
	return got, nil
}

// FindByMarker lists the issues and pull requests, open and closed, whose body contains marker.
// The issues list does not carry a PR's head, so each PR hit costs one more request.
func (c *GitHub) FindByMarker(ctx context.Context, marker string) ([]Item, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	if marker == "" {
		return nil, errors.New("forge: FindByMarker needs a marker")
	}
	out := []Item{}
	url := fmt.Sprintf("%s/repos/%s/issues?state=all&per_page=100", c.BaseURL, c.Repo)
	for url != "" {
		var page []issueItem
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			if !strings.Contains(it.Body, marker) {
				continue
			}
			item := Item{Number: it.Number, IsPR: it.PullRequest != nil, State: it.State, Body: it.Body}
			if it.User != nil {
				item.Author = it.User.Login
			}
			if item.IsPR {
				var pr prItem
				prURL := fmt.Sprintf("%s/repos/%s/pulls/%d", c.BaseURL, c.Repo, it.Number)
				if _, err := c.do(ctx, http.MethodGet, prURL, nil, &pr); err != nil {
					return nil, err
				}
				item.HeadRef = pr.Head.Ref
			}
			out = append(out, item)
		}
		url = next
	}
	return out, nil
}
