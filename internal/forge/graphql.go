package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// graphqlError is one entry of the errors list GitHub answers with: its type, such as
// NOT_FOUND, and the path of the field that failed, such as ["repository", "issue"].
type graphqlError struct {
	Type    string `json:"type"`
	Path    []any  `json:"path"`
	Message string `json:"message"`
}

// graphqlErrors is the errors list of a failed query, as an error.
type graphqlErrors []graphqlError

func (e graphqlErrors) Error() string {
	msg := e[0].Message
	if more := len(e) - 1; more > 0 {
		msg += fmt.Sprintf(" (and %d more)", more)
	}
	return "forge graphql: " + msg
}

// graphql posts query with vars to BaseURL/graphql and decodes data into into. GitHub answers
// 200 with an errors list on a failed query; that is a graphqlErrors error here, never empty
// data.
func (c *GitHub) graphql(ctx context.Context, query string, vars map[string]any, into any) error {
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors graphqlErrors   `json:"errors"`
	}
	body := map[string]any{"query": query, "variables": vars}
	if _, err := c.do(ctx, http.MethodPost, c.BaseURL+"/graphql", body, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return resp.Errors
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, into); err != nil {
		return fmt.Errorf("forge graphql: decode data: %w", err)
	}
	return nil
}

// ownerName is the owner and name of Repo for a GraphQL query, after checking the client is
// configured.
func (c *GitHub) ownerName() (owner, name string, err error) {
	if err := c.configured(); err != nil {
		return "", "", err
	}
	owner, name, ok := strings.Cut(c.Repo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	return owner, name, nil
}
