package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// CreateComment posts a comment on issue number and returns it as the forge stored it.
func (c *Client) CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error) {
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

// AddRocket adds a 🚀 reaction to a comment. GitHub answers 200 when the reaction already
// existed and 201 when it is new; both are success.
func (c *Client) AddRocket(ctx context.Context, commentID int64) error {
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
func (c *Client) MergePR(ctx context.Context, number int, sha string) error {
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
