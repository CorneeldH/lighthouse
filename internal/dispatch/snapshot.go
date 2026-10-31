package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// SnapshotVersion is the version of the snapshot JSON; ReadSnapshot refuses any other.
const SnapshotVersion = 1

// Snapshot is what one tick knows about one repo: its board items in Controller or On hold,
// with their PRs and trusted comments, and its open runs.
type Snapshot struct {
	Version    int            `json:"version"`
	Repo       string         `json:"repo"`
	TokenLogin string         `json:"token_login"`
	GatheredAt time.Time      `json:"gathered_at"`
	Items      []Item         `json:"items"` // sorted by issue
	OpenRuns   []runs.OpenRun `json:"open_runs"`
}

// Item is one board item of the repo with Status Controller or On hold.
type Item struct {
	Issue          int             `json:"issue"`
	Title          string          `json:"title"`
	State          string          `json:"state"`
	ItemID         string          `json:"item_id"`
	Status         string          `json:"status"`   // "Controller" or "On hold"
	Priority       string          `json:"priority"` // option name, "" when unset
	ItemUpdatedAt  time.Time       `json:"item_updated_at"`
	IssueUpdatedAt time.Time       `json:"issue_updated_at"`
	BlockedBy      []forge.Blocker `json:"blocked_by"`
	PRs            []forge.PR      `json:"prs"`      // closing PRs plus PRs on the open run's branch, by number
	Comments       []Comment       `json:"comments"` // trusted authors only, oldest first
}

// Comment is an issue comment by a trusted author.
type Comment struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
	Rocket    bool   `json:"rocket"` // a 🚀 from the token login
}

// ProtocolComments returns the comments in the shape protocol.Classify reads. Only the
// token's 🚀 counts, so Rockets is 1 when Rocket is set and 0 otherwise.
func (i Item) ProtocolComments() []protocol.Comment {
	out := make([]protocol.Comment, 0, len(i.Comments))
	for _, c := range i.Comments {
		pc := protocol.Comment{ID: c.ID, Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt, URL: c.URL}
		if c.Rocket {
			pc.Rockets = 1
		}
		out = append(out, pc)
	}
	return out
}

// ReadSnapshot reads a snapshot file, refusing one with another Version.
func ReadSnapshot(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("dispatch: read snapshot: %w", err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("dispatch: decode snapshot %s: %w", path, err)
	}
	if s.Version != SnapshotVersion {
		return Snapshot{}, fmt.Errorf("dispatch: snapshot %s has version %d, want %d", path, s.Version, SnapshotVersion)
	}
	return s, nil
}
