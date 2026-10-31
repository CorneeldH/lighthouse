package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// graphql posts query with vars to BaseURL/graphql and decodes data into into. GitHub answers
// 200 with an errors list on a failed query; that is an error here, never empty data.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, into any) error {
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	body := map[string]any{"query": query, "variables": vars}
	if _, err := c.do(ctx, http.MethodPost, c.BaseURL+"/graphql", body, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		msg := resp.Errors[0].Message
		if more := len(resp.Errors) - 1; more > 0 {
			msg += fmt.Sprintf(" (and %d more)", more)
		}
		return fmt.Errorf("forge graphql: %s", msg)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, into); err != nil {
		return fmt.Errorf("forge graphql: decode data: %w", err)
	}
	return nil
}
