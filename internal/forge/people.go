package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
)

// Login returns the login of the token's user.
func (c *Client) Login(ctx context.Context) (string, error) {
	if err := c.configured(); err != nil {
		return "", err
	}
	var u struct {
		Login string `json:"login"`
	}
	if _, err := c.do(ctx, http.MethodGet, c.BaseURL+"/user", nil, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// RocketLogins lists the logins that put a 🚀 on one comment, following every page.
func (c *Client) RocketLogins(ctx context.Context, commentID int64) ([]string, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	type reaction struct {
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	var out []string
	url := fmt.Sprintf("%s/repos/%s/issues/comments/%d/reactions?content=rocket&per_page=100", c.BaseURL, c.Repo, commentID)
	for url != "" {
		var page []reaction
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if r.User != nil {
				out = append(out, r.User.Login)
			}
		}
		url = next
	}
	return out, nil
}

// RoleName returns the repository role of user ("admin", "maintain", "write", "triage", "read",
// or a custom role). An unknown login gives "" and no error.
func (c *Client) RoleName(ctx context.Context, user string) (string, error) {
	if err := c.configured(); err != nil {
		return "", err
	}
	var p struct {
		RoleName string `json:"role_name"`
	}
	url := fmt.Sprintf("%s/repos/%s/collaborators/%s/permission", c.BaseURL, c.Repo, neturl.PathEscape(user))
	if _, err := c.do(ctx, http.MethodGet, url, nil, &p); err != nil {
		var se *httpStatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return p.RoleName, nil
}
