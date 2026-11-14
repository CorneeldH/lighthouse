package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Forge is one repository on a forge: its issues, comments, acknowledgements, pull requests
// and board. A status is an option name of [forge.board] status_options, never an ID.
type Forge interface {
	// Kind is "github" or "gitlab".
	Kind() string
	Login(ctx context.Context) (string, error)
	// Role is the repository role of user; "" for a user the repo does not know.
	Role(ctx context.Context, user string) (string, error)
	Issue(ctx context.Context, number int) (Issue, error)
	OpenIssues(ctx context.Context) ([]Issue, error)
	ClosedInMilestone(ctx context.Context, m Milestone, since time.Time) ([]Issue, error)
	BlockedBy(ctx context.Context, number int) ([]Blocker, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
	CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
	// Acknowledge marks comment commentID on issue number as seen by the token's user.
	Acknowledge(ctx context.Context, number int, commentID int64) error
	// Acknowledgers lists the logins that marked comment commentID on issue number as seen.
	Acknowledgers(ctx context.Context, number int, commentID int64) ([]string, error)
	OpenPRs(ctx context.Context) ([]OpenPR, error)
	BranchPRs(ctx context.Context, branch string) ([]PR, error)
	BoardItems(ctx context.Context) ([]BoardItem, error)
	IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error)
	AddToBoard(ctx context.Context, number int) (BoardItem, error)
	SetStatus(ctx context.Context, item BoardItem, status string) error
}

var _ Forge = (*GitHub)(nil)

// Config is what Open needs to reach one repository.
type Config struct {
	Kind, Repo, Host, Token string
	// BaseURL is the API root; "" is the default of Kind. Trailing slashes are dropped.
	BaseURL string
	// Board is the [forge.board] of the repo; nil makes every board method fail.
	Board *repoconfig.Board
	// HTTP is the client for every request; nil is one with a 30 s timeout.
	HTTP *http.Client
}

// Open returns the forge of cfg.Kind ("" is github).
func Open(cfg Config) (Forge, error) {
	switch cfg.Kind {
	case "", "github":
		c := New(cfg.Repo, cfg.Token)
		if cfg.BaseURL != "" {
			c.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
		}
		if cfg.HTTP != nil {
			c.HTTP = cfg.HTTP
		}
		c.Board = cfg.Board
		return c, nil
	case "gitlab":
		return nil, errors.New("forge: GitLab client not implemented yet")
	default:
		return nil, fmt.Errorf("forge: kind %q is not github or gitlab", cfg.Kind)
	}
}
