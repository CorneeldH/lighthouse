package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"

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

// defaultBranch is the default branch of the repository, fetched once per client.
func (c *GitHub) defaultBranch(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.base != "" {
		return c.base, nil
	}
	var out struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s", c.BaseURL, c.Repo), nil, &out); err != nil {
		return "", err
	}
	if out.DefaultBranch == "" {
		return "", fmt.Errorf("forge: repo %s has no default branch", c.Repo)
	}
	c.base = out.DefaultBranch
	return c.base, nil
}

// CreatePR opens a pull request from branch into the default branch of the repository.
func (c *GitHub) CreatePR(ctx context.Context, branch, title, body string) (PR, error) {
	if err := c.configured(); err != nil {
		return PR{}, err
	}
	base, err := c.defaultBranch(ctx)
	if err != nil {
		return PR{}, err
	}
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
		Head    struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	url := fmt.Sprintf("%s/repos/%s/pulls", c.BaseURL, c.Repo)
	req := map[string]string{"head": branch, "base": base, "title": title, "body": body}
	if _, err := c.do(ctx, http.MethodPost, url, req, &out); err != nil {
		return PR{}, err
	}
	return PR{Number: out.Number, State: "OPEN", Draft: out.Draft, HeadSHA: out.Head.SHA, HeadRef: out.Head.Ref, URL: out.HTMLURL}, nil
}

// ClosePR closes pull request number without merging it.
func (c *GitHub) ClosePR(ctx context.Context, number int) error {
	if err := c.configured(); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/pulls/%d", c.BaseURL, c.Repo, number)
	_, err := c.do(ctx, http.MethodPatch, url, map[string]string{"state": "closed"}, nil)
	return err
}

// DeleteBranch deletes branch from the repository. A branch that is already gone (404, or 422
// "Reference does not exist") is not an error.
func (c *GitHub) DeleteBranch(ctx context.Context, branch string) error {
	if err := c.configured(); err != nil {
		return err
	}
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = neturl.PathEscape(s)
	}
	url := fmt.Sprintf("%s/repos/%s/git/refs/heads/%s", c.BaseURL, c.Repo, strings.Join(segs, "/"))
	_, err := c.do(ctx, http.MethodDelete, url, nil, nil)
	var se *httpStatusError
	if errors.As(err, &se) {
		if se.Code == http.StatusNotFound ||
			se.Code == http.StatusUnprocessableEntity && strings.Contains(se.Message, "Reference does not exist") {
			return nil
		}
	}
	return err
}
