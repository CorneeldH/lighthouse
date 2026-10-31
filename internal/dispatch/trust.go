// Package dispatch turns forge reads into the per-repo snapshot a tick decides on.
package dispatch

import (
	"context"
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// Trust decides whether a comment author may steer the dispatcher: its repository role is
// in the allowed set ([dispatch] answer_permissions). It asks once per login and keeps the
// answer, so one Trust lives for one repo in one tick. It is not safe for concurrent use.
type Trust struct {
	client  *forge.Client
	allowed map[string]bool
	cache   map[string]bool
}

// NewTrust returns a Trust for the repo of c that trusts the roles in allowed.
func NewTrust(c *forge.Client, allowed []string) *Trust {
	t := &Trust{client: c, allowed: map[string]bool{}, cache: map[string]bool{}}
	for _, role := range allowed {
		t.allowed[role] = true
	}
	return t
}

// Trusted reports whether login has an allowed role. An empty login (a deleted user) and a
// login the repo does not know are not trusted; neither is an error. A failed request is.
func (t *Trust) Trusted(ctx context.Context, login string) (bool, error) {
	if login == "" {
		return false, nil
	}
	if ok, found := t.cache[login]; found {
		return ok, nil
	}
	role, err := t.client.RoleName(ctx, login)
	if err != nil {
		return false, fmt.Errorf("dispatch: role of %s: %w", login, err)
	}
	ok := t.allowed[role]
	t.cache[login] = ok
	return ok, nil
}
