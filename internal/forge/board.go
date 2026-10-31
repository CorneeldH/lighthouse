package forge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PR is a pull request as the snapshot holds it.
type PR struct {
	Number    int    `json:"number"`
	State     string `json:"state"` // OPEN, CLOSED, MERGED
	Draft     bool   `json:"draft"`
	Mergeable string `json:"mergeable"` // MERGEABLE, CONFLICTING, UNKNOWN
	HeadSHA   string `json:"head_sha"`
	HeadRef   string `json:"head_ref"`
	Checks    string `json:"checks"` // statusCheckRollup.state, "" without checks
	Closes    bool   `json:"closes"` // in closedByPullRequestsReferences
}

// Blocker is an issue that blocks a board item.
type Blocker struct {
	Repo     string `json:"repo"` // nameWithOwner
	Issue    int    `json:"issue"`
	State    string `json:"state"`     // OPEN, CLOSED
	MergedPR bool   `json:"merged_pr"` // a closing PR of the blocker is merged
}

// BoardItem is one item of a Projects v2 board.
type BoardItem struct {
	ItemID         string
	ItemUpdatedAt  time.Time
	SingleSelect   map[string]string // field ID → option ID
	Repo           string            // nameWithOwner; "" when the content is no issue
	Issue          int
	Title          string
	IssueState     string
	IssueUpdatedAt time.Time
	BlockedBy      []Blocker
	ClosingPRs     []PR
}

// prFields are the pull request fields both queries read into a PR.
const prFields = `number state isDraft mergeable headRefOid headRefName statusCheckRollup { state }`

// BoardQuery reads one page of 100 project items. Every nested connection asks pageInfo, so a
// list longer than its first: n is an error instead of a silent cut.
const BoardQuery = `query Board($project: ID!, $after: String) {
  node(id: $project) {
    ... on ProjectV2 {
      items(first: 100, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          updatedAt
          fieldValues(first: 50) {
            pageInfo { hasNextPage }
            nodes {
              ... on ProjectV2ItemFieldSingleSelectValue { optionId field { ... on ProjectV2SingleSelectField { id } } }
            }
          }
          content {
            ... on Issue {
              number title state updatedAt
              repository { nameWithOwner }
              blockedBy(first: 50) {
                pageInfo { hasNextPage }
                nodes {
                  number state
                  repository { nameWithOwner }
                  closedByPullRequestsReferences(first: 10, includeClosedPrs: true) {
                    pageInfo { hasNextPage }
                    nodes { merged }
                  }
                }
              }
              closedByPullRequestsReferences(first: 20, includeClosedPrs: true) {
                pageInfo { hasNextPage }
                nodes { ` + prFields + ` }
              }
            }
          }
        }
      }
    }
  }
}`

// BranchPRsQuery reads the pull requests whose head is one branch, in every state.
const BranchPRsQuery = `query BranchPRs($owner: String!, $name: String!, $branch: String!) {
  repository(owner: $owner, name: $name) {
    pullRequests(headRefName: $branch, first: 20, states: [OPEN, CLOSED, MERGED]) {
      pageInfo { hasNextPage }
      nodes { ` + prFields + ` }
    }
  }
}`

// SetStatusMutation sets one single-select field of one project item.
const SetStatusMutation = `mutation SetStatus($project: ID!, $item: ID!, $field: ID!, $option: String!) {
  updateProjectV2ItemFieldValue(input: {projectId: $project, itemId: $item, fieldId: $field, value: {singleSelectOptionId: $option}}) {
    projectV2Item { id }
  }
}`

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type prNode struct {
	Number            int    `json:"number"`
	State             string `json:"state"`
	IsDraft           bool   `json:"isDraft"`
	Mergeable         string `json:"mergeable"`
	HeadRefOid        string `json:"headRefOid"`
	HeadRefName       string `json:"headRefName"`
	StatusCheckRollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

func (n prNode) pr(closes bool) PR {
	p := PR{Number: n.Number, State: n.State, Draft: n.IsDraft, Mergeable: n.Mergeable,
		HeadSHA: n.HeadRefOid, HeadRef: n.HeadRefName, Closes: closes}
	if n.StatusCheckRollup != nil {
		p.Checks = n.StatusCheckRollup.State
	}
	return p
}

type repoRef struct {
	NameWithOwner string `json:"nameWithOwner"`
}

type itemNode struct {
	ID          string    `json:"id"`
	UpdatedAt   time.Time `json:"updatedAt"`
	FieldValues struct {
		PageInfo pageInfo `json:"pageInfo"`
		Nodes    []struct {
			OptionID string `json:"optionId"`
			Field    struct {
				ID string `json:"id"`
			} `json:"field"`
		} `json:"nodes"`
	} `json:"fieldValues"`
	Content *struct {
		Number     int       `json:"number"`
		Title      string    `json:"title"`
		State      string    `json:"state"`
		UpdatedAt  time.Time `json:"updatedAt"`
		Repository repoRef   `json:"repository"`
		BlockedBy  struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
				Number     int     `json:"number"`
				State      string  `json:"state"`
				Repository repoRef `json:"repository"`
				ClosedBy   struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []struct {
						Merged bool `json:"merged"`
					} `json:"nodes"`
				} `json:"closedByPullRequestsReferences"`
			} `json:"nodes"`
		} `json:"blockedBy"`
		ClosedBy struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []prNode `json:"nodes"`
		} `json:"closedByPullRequestsReferences"`
	} `json:"content"`
}

// BoardItems lists every item of a Projects v2 board, following every page. A nested list
// that GitHub cut off is an error and gives no items: a missing blocker PR or Status value
// would change what the dispatcher decides.
func (c *Client) BoardItems(ctx context.Context, projectID string) ([]BoardItem, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var out []BoardItem
	var after any // null on the first page
	for {
		var data struct {
			Node *struct {
				Items *struct {
					PageInfo pageInfo   `json:"pageInfo"`
					Nodes    []itemNode `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		}
		if err := c.graphql(ctx, BoardQuery, map[string]any{"project": projectID, "after": after}, &data); err != nil {
			return nil, err
		}
		if data.Node == nil || data.Node.Items == nil {
			return nil, fmt.Errorf("forge: board %s: not a ProjectV2", projectID)
		}
		for _, n := range data.Node.Items.Nodes {
			it, err := n.item()
			if err != nil {
				return nil, fmt.Errorf("forge: board item %s: %w", n.ID, err)
			}
			out = append(out, it)
		}
		if !data.Node.Items.PageInfo.HasNextPage {
			return out, nil
		}
		// An empty or repeated cursor would ask the same page again, for ever.
		next := data.Node.Items.PageInfo.EndCursor
		if next == "" || next == after {
			return nil, fmt.Errorf("forge: board %s: pagination stuck at endCursor %q", projectID, next)
		}
		after = next
	}
}

// item maps one node, refusing any nested connection with more pages.
func (n itemNode) item() (BoardItem, error) {
	it := BoardItem{ItemID: n.ID, ItemUpdatedAt: n.UpdatedAt, SingleSelect: map[string]string{}}
	if n.FieldValues.PageInfo.HasNextPage {
		return BoardItem{}, truncated("fieldValues", len(n.FieldValues.Nodes))
	}
	for _, v := range n.FieldValues.Nodes {
		if v.OptionID != "" && v.Field.ID != "" {
			it.SingleSelect[v.Field.ID] = v.OptionID
		}
	}
	ct := n.Content
	if ct == nil || ct.Number == 0 {
		return it, nil // a draft issue, a pull request, or content the token cannot see
	}
	it.Repo, it.Issue, it.Title = ct.Repository.NameWithOwner, ct.Number, ct.Title
	it.IssueState, it.IssueUpdatedAt = ct.State, ct.UpdatedAt
	if ct.BlockedBy.PageInfo.HasNextPage {
		return BoardItem{}, truncated("blockedBy", len(ct.BlockedBy.Nodes))
	}
	for _, b := range ct.BlockedBy.Nodes {
		if b.ClosedBy.PageInfo.HasNextPage {
			return BoardItem{}, truncated(fmt.Sprintf("blocker %s#%d closedByPullRequestsReferences", b.Repository.NameWithOwner, b.Number), len(b.ClosedBy.Nodes))
		}
		bl := Blocker{Repo: b.Repository.NameWithOwner, Issue: b.Number, State: b.State}
		for _, p := range b.ClosedBy.Nodes {
			bl.MergedPR = bl.MergedPR || p.Merged
		}
		it.BlockedBy = append(it.BlockedBy, bl)
	}
	if ct.ClosedBy.PageInfo.HasNextPage {
		return BoardItem{}, truncated("closedByPullRequestsReferences", len(ct.ClosedBy.Nodes))
	}
	for _, p := range ct.ClosedBy.Nodes {
		it.ClosingPRs = append(it.ClosingPRs, p.pr(true))
	}
	return it, nil
}

func truncated(connection string, n int) error {
	return fmt.Errorf("%s truncated at %d", connection, n)
}

// BranchPRs lists the pull requests whose head is branch, open, closed and merged. Closes is
// false on each: the branch says nothing about which issue a PR closes.
func (c *Client) BranchPRs(ctx context.Context, branch string) ([]PR, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	owner, name, ok := strings.Cut(c.Repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	var data struct {
		Repository *struct {
			PullRequests struct {
				PageInfo pageInfo `json:"pageInfo"`
				Nodes    []prNode `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "name": name, "branch": branch}
	if err := c.graphql(ctx, BranchPRsQuery, vars, &data); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, fmt.Errorf("forge: repository %s not found", c.Repo)
	}
	prs := data.Repository.PullRequests
	if prs.PageInfo.HasNextPage {
		return nil, fmt.Errorf("forge: branch %s: %w", branch, truncated("pullRequests", len(prs.Nodes)))
	}
	out := make([]PR, 0, len(prs.Nodes))
	for _, p := range prs.Nodes {
		out = append(out, p.pr(false))
	}
	return out, nil
}

// SetStatus sets the single-select field fieldID of a board item to optionID.
func (c *Client) SetStatus(ctx context.Context, projectID, itemID, fieldID, optionID string) error {
	if err := c.configured(); err != nil {
		return err
	}
	vars := map[string]any{"project": projectID, "item": itemID, "field": fieldID, "option": optionID}
	return c.graphql(ctx, SetStatusMutation, vars, nil)
}
