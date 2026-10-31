package forge

import (
	"context"
	"fmt"
	"net/http"

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
