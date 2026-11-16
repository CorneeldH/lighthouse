package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// gitlabGraphQLError is an error GitLab's GraphQL API answered: a top-level errors list, or the
// errors of a mutation payload. A request that failed on HTTP is no such error.
type gitlabGraphQLError struct{ Message string }

func (e *gitlabGraphQLError) Error() string { return "forge graphql: " + e.Message }

// graphqlURL is the GraphQL endpoint next to the REST root: <web root>/api/graphql.
func (g *GitLab) graphqlURL() (string, error) {
	root, ok := strings.CutSuffix(g.BaseURL, "/api/v4")
	if !ok {
		return "", fmt.Errorf("forge: GitLab API root %q does not end in /api/v4: no GraphQL endpoint", g.BaseURL)
	}
	return root + "/api/graphql", nil
}

// graphql posts query with vars to the GraphQL endpoint and decodes data into out. GitLab
// answers 200 with an errors list on a failed operation; that is a *gitlabGraphQLError, never
// empty data. The request goes through send, so the token stays on the host of BaseURL.
func (g *GitLab) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	url, err := g.graphqlURL()
	if err != nil {
		return err
	}
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := g.send(ctx, http.MethodPost, url, map[string]any{"query": query, "variables": vars}, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		msg := resp.Errors[0].Message
		if more := len(resp.Errors) - 1; more > 0 {
			msg += fmt.Sprintf(" (and %d more)", more)
		}
		return &gitlabGraphQLError{Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("forge graphql: decode data: %w", err)
	}
	return nil
}
