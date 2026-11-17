package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// PR is a pull request as the snapshot holds it.
type PR struct {
	Number    int    `json:"number"`
	State     string `json:"state"` // OPEN, CLOSED, MERGED
	Draft     bool   `json:"draft"`
	Mergeable string `json:"mergeable"` // MERGEABLE, CONFLICTING, UNKNOWN
	HeadSHA   string `json:"head_sha"`
	HeadRef   string `json:"head_ref"`
	// CrossRepo is true for a PR whose head lives in another repository, such as a fork.
	CrossRepo bool   `json:"cross_repo,omitempty"`
	Checks    string `json:"checks"` // statusCheckRollup.state, "" without checks
	Closes    bool   `json:"closes"` // in closedByPullRequestsReferences
	URL       string `json:"url"`
	// Roborev is "" without a roborev review, RoborevProcessed or RoborevOpen.
	Roborev string `json:"roborev,omitempty"`
	// PushedSHA is the SHA of the newest pusher comment on the issue whose branch is HeadRef:
	// the workflow pusher pushed it, so the merge of this PR goes to the pusher (dispatch sets it).
	PushedSHA string `json:"pushed_sha,omitempty"`
	// MergedAt is the zero time for a PR that is not merged.
	MergedAt time.Time `json:"merged_at"`
	// ClosedAt is the zero time for an open PR; a merged PR has one too.
	ClosedAt time.Time `json:"closed_at"`
}

// Roborev states of a PR.
const (
	RoborevProcessed = "processed"
	RoborevOpen      = "open"
)

// Blocker is an issue that blocks a board item.
type Blocker struct {
	Repo     string `json:"repo"` // nameWithOwner
	Issue    int    `json:"issue"`
	State    string `json:"state"`     // OPEN, CLOSED
	MergedPR bool   `json:"merged_pr"` // a closing PR of the blocker is merged
	// Status is the Status option name of the blocker on the same board, "" when it is not on it.
	Status string `json:"status,omitempty"`
}

// UnknownStatusPrefix starts the Status of an item whose option [forge.board] status_options does not
// list: "unknown:<option ID>". Such a column (In progress, Done) is not "no status".
const UnknownStatusPrefix = "unknown:"

// BoardItem is one item of a Projects v2 board.
type BoardItem struct {
	ItemID        string
	ItemUpdatedAt time.Time
	// Status is the option name of the Status field; "" without a status, and
	// "unknown:<option ID>" for an option that status_options does not list.
	Status string
	// StatusName is the raw name of the item's status on the board, whether status_options
	// lists it or not: the option name on GitHub, the status name on GitLab; "" without one.
	StatusName string
	// Priority is the option name of the Priority field; "" without a priority or for an
	// option that priority_options does not list.
	Priority       string
	Repo           string // nameWithOwner; "" when the content is no issue
	Issue          int
	Title          string
	IssueState     string
	IssueUpdatedAt time.Time
	Labels         []string // label names of the issue
	BlockedBy      []Blocker
	ClosingPRs     []PR
	// Archived is an archived item. Only IssueItem reads it: BoardItems lists no archived item.
	Archived bool
}

// prFields are the pull request fields both queries read into a PR.
const prFields = `number state isDraft mergeable headRefOid headRefName isCrossRepository mergedAt closedAt url statusCheckRollup { state }`

// fieldValuesSelection is the single-select values of a project item, shared by the board and
// the one-issue query so both fill BoardItem.Status, StatusName and Priority the same way.
const fieldValuesSelection = `fieldValues(first: 50) {
            pageInfo { hasNextPage }
            nodes {
              ... on ProjectV2ItemFieldSingleSelectValue { optionId name field { ... on ProjectV2SingleSelectField { id } } }
            }
          }`

// BoardQuery reads one page of 100 project items. Every nested connection asks pageInfo, so a
// list longer than its first: n is an error instead of a silent cut. GitHub prices a page by
// the product of the nested limits; blockedBy and the closing PRs of a blocker dominate it,
// so their limits stay small (measured: first: 50 × 10 cost 53 points per page).
const BoardQuery = `query Board($project: ID!, $after: String) {
  node(id: $project) {
    ... on ProjectV2 {
      items(first: 100, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          updatedAt
          ` + fieldValuesSelection + `
          content {
            ... on Issue {
              number title state updatedAt
              repository { nameWithOwner }
              labels(first: 20) { nodes { name } pageInfo { hasNextPage } }
              blockedBy(first: 10) {
                pageInfo { hasNextPage }
                nodes {
                  number state
                  repository { nameWithOwner }
                  closedByPullRequestsReferences(first: 5, includeClosedPrs: true) {
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

// IssueItemsQuery reads the project items of one issue, with their single-select values.
// projectItems includes archived items, so it selects isArchived.
const IssueItemsQuery = `query IssueItems($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      projectItems(first: 20) {
        pageInfo { hasNextPage }
        nodes {
          id
          updatedAt
          isArchived
          project { id }
          ` + fieldValuesSelection + `
        }
      }
    }
  }
}`

// AddToBoardMutation adds an issue, by its node ID, to a project.
const AddToBoardMutation = `mutation AddToBoard($project: ID!, $content: ID!) { addProjectV2ItemById(input: {projectId: $project, contentId: $content}) { item { id } } }`

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
	Number            int       `json:"number"`
	State             string    `json:"state"`
	IsDraft           bool      `json:"isDraft"`
	Mergeable         string    `json:"mergeable"`
	HeadRefOid        string    `json:"headRefOid"`
	HeadRefName       string    `json:"headRefName"`
	IsCrossRepository bool      `json:"isCrossRepository"`
	MergedAt          time.Time `json:"mergedAt"`
	ClosedAt          time.Time `json:"closedAt"`
	URL               string    `json:"url"`
	StatusCheckRollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

func (n prNode) pr(closes bool) PR {
	p := PR{Number: n.Number, State: n.State, Draft: n.IsDraft, Mergeable: n.Mergeable,
		HeadSHA: n.HeadRefOid, HeadRef: n.HeadRefName, CrossRepo: n.IsCrossRepository, Closes: closes, URL: n.URL, MergedAt: n.MergedAt, ClosedAt: n.ClosedAt}
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
	IsArchived  bool      `json:"isArchived"` // only IssueItemsQuery selects it
	FieldValues struct {
		PageInfo pageInfo `json:"pageInfo"`
		Nodes    []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
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
		Labels     struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"labels"`
		BlockedBy struct {
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

// board is the [forge.board] of the repo, or an error when there is none.
func (c *GitHub) board() (*repoconfig.Board, error) {
	if c.Board == nil {
		return nil, fmt.Errorf("forge: %s: missing [forge.board]", c.Repo)
	}
	return c.Board, nil
}

// selectValue is one single-select value of a project item: the option's ID and its name.
type selectValue struct{ optionID, name string }

// named sets Status, StatusName and Priority of it from its single-select values, by option
// name of b.
func named(it BoardItem, values map[string]selectValue, b *repoconfig.Board) BoardItem {
	if v := values[b.StatusFieldID]; v.optionID != "" {
		it.Status = optionName(b.StatusOptions, v.optionID)
		if it.Status == "" {
			it.Status = UnknownStatusPrefix + v.optionID
		}
		it.StatusName = v.name
	}
	it.Priority = optionName(b.PriorityOptions, values[b.PriorityFieldID].optionID)
	return it
}

// optionName is the name of option id in options; "" for an unknown or empty id.
func optionName(options map[string]string, id string) string {
	if id == "" {
		return ""
	}
	for name, oid := range options {
		if oid == id {
			return name
		}
	}
	return ""
}

// BoardItems lists every item of the board of [forge.board] project_id, following every page.
// A nested list that GitHub cut off is an error and gives no items: a missing blocker PR or
// Status value would change what the dispatcher decides.
func (c *GitHub) BoardItems(ctx context.Context) ([]BoardItem, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	b, err := c.board()
	if err != nil {
		return nil, err
	}
	return c.boardItems(ctx, b)
}

// boardItems reads the items of project b.ProjectID.
func (c *GitHub) boardItems(ctx context.Context, b *repoconfig.Board) ([]BoardItem, error) {
	projectID := b.ProjectID
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
			it, values, err := n.item()
			if err != nil {
				return nil, fmt.Errorf("forge: board item %s: %w", n.ID, err)
			}
			out = append(out, named(it, values, b))
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

// item maps one node, refusing any nested connection with more pages. values are its
// single-select values per field ID.
func (n itemNode) item() (it BoardItem, values map[string]selectValue, err error) {
	it = BoardItem{ItemID: n.ID, ItemUpdatedAt: n.UpdatedAt, Archived: n.IsArchived}
	if n.FieldValues.PageInfo.HasNextPage {
		return BoardItem{}, nil, truncated("fieldValues", len(n.FieldValues.Nodes))
	}
	values = map[string]selectValue{}
	for _, v := range n.FieldValues.Nodes {
		if v.OptionID != "" && v.Field.ID != "" {
			values[v.Field.ID] = selectValue{optionID: v.OptionID, name: v.Name}
		}
	}
	ct := n.Content
	if ct == nil || ct.Number == 0 {
		return it, values, nil // a draft issue, a pull request, or content the token cannot see
	}
	it.Repo, it.Issue, it.Title = ct.Repository.NameWithOwner, ct.Number, ct.Title
	it.IssueState, it.IssueUpdatedAt = ct.State, ct.UpdatedAt
	if ct.Labels.PageInfo.HasNextPage {
		return BoardItem{}, nil, truncated("labels", len(ct.Labels.Nodes))
	}
	for _, l := range ct.Labels.Nodes {
		it.Labels = append(it.Labels, l.Name)
	}
	if ct.BlockedBy.PageInfo.HasNextPage {
		return BoardItem{}, nil, truncated("blockedBy", len(ct.BlockedBy.Nodes))
	}
	for _, b := range ct.BlockedBy.Nodes {
		if b.ClosedBy.PageInfo.HasNextPage {
			return BoardItem{}, nil, truncated(fmt.Sprintf("blocker %s#%d closedByPullRequestsReferences", b.Repository.NameWithOwner, b.Number), len(b.ClosedBy.Nodes))
		}
		bl := Blocker{Repo: b.Repository.NameWithOwner, Issue: b.Number, State: b.State}
		for _, p := range b.ClosedBy.Nodes {
			bl.MergedPR = bl.MergedPR || p.Merged
		}
		it.BlockedBy = append(it.BlockedBy, bl)
	}
	if ct.ClosedBy.PageInfo.HasNextPage {
		return BoardItem{}, nil, truncated("closedByPullRequestsReferences", len(ct.ClosedBy.Nodes))
	}
	for _, p := range ct.ClosedBy.Nodes {
		it.ClosingPRs = append(it.ClosingPRs, p.pr(true))
	}
	return it, values, nil
}

func truncated(connection string, n int) error {
	return fmt.Errorf("%s truncated at %d", connection, n)
}

// BranchPRs lists the pull requests whose head is branch, open, closed and merged. Closes is
// false on each: the branch says nothing about which issue a PR closes.
func (c *GitHub) BranchPRs(ctx context.Context, branch string) ([]PR, error) {
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

// SetStatus moves item to status, an option name of [forge.board] status_options. A name it
// does not list, or an item without an ItemID, is an error before any request.
func (c *GitHub) SetStatus(ctx context.Context, item BoardItem, status string) error {
	if err := c.configured(); err != nil {
		return err
	}
	b, err := c.board()
	if err != nil {
		return err
	}
	option, ok := b.StatusOptions[status]
	if !ok {
		return fmt.Errorf("status %q is not in [forge.board] status_options", status)
	}
	if item.ItemID == "" {
		return fmt.Errorf("forge: %s#%d: no board item to set the status of", item.Repo, item.Issue)
	}
	return c.setOption(ctx, b.ProjectID, item.ItemID, b.StatusFieldID, option)
}

// setOption sets the single-select field fieldID of a board item to optionID.
func (c *GitHub) setOption(ctx context.Context, projectID, itemID, fieldID, optionID string) error {
	vars := map[string]any{"project": projectID, "item": itemID, "field": fieldID, "option": optionID}
	return c.graphql(ctx, SetStatusMutation, vars, nil)
}

// IssueItem returns the item of issue number on the board of [forge.board]; ok is false when
// the issue is not on that board. Status, StatusName and Priority are filled as in BoardItems; a cut list
// is an error. An archived item counts as an item, with Archived set.
func (c *GitHub) IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error) {
	if err := c.configured(); err != nil {
		return BoardItem{}, false, err
	}
	b, err := c.board()
	if err != nil {
		return BoardItem{}, false, err
	}
	owner, name, found := strings.Cut(c.Repo, "/")
	if !found || owner == "" || name == "" {
		return BoardItem{}, false, fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	var data struct {
		Repository *struct {
			Issue *struct {
				ProjectItems struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []struct {
						itemNode
						Project struct {
							ID string `json:"id"`
						} `json:"project"`
					} `json:"nodes"`
				} `json:"projectItems"`
			} `json:"issue"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "name": name, "number": number}
	if err := c.graphql(ctx, IssueItemsQuery, vars, &data); err != nil {
		// GitHub answers a deleted or transferred issue with issue null and a NOT_FOUND error
		// on repository.issue; any other error stays what it is.
		var ge graphqlErrors
		if errors.As(err, &ge) && ge.notFound("repository", "issue") {
			return BoardItem{}, false, fmt.Errorf("forge: issue %s#%d: %w", c.Repo, number, ErrIssueNotFound)
		}
		return BoardItem{}, false, err
	}
	if data.Repository == nil || data.Repository.Issue == nil {
		return BoardItem{}, false, fmt.Errorf("forge: issue %s#%d: %w", c.Repo, number, ErrIssueNotFound)
	}
	items := data.Repository.Issue.ProjectItems
	if items.PageInfo.HasNextPage {
		return BoardItem{}, false, fmt.Errorf("forge: issue %s#%d: %w", c.Repo, number, truncated("projectItems", len(items.Nodes)))
	}
	for _, n := range items.Nodes {
		if n.Project.ID != b.ProjectID {
			continue
		}
		it, values, err := n.item()
		if err != nil {
			return BoardItem{}, false, fmt.Errorf("forge: board item %s: %w", n.ID, err)
		}
		it.Repo, it.Issue = c.Repo, number // the query selects no content: the issue is the one asked for
		return named(it, values, b), true, nil
	}
	return BoardItem{}, false, nil
}

// AddToBoard adds issue number to the board of [forge.board] and returns the new item, without
// a status. The issue's node ID comes from the cache Issue and OpenIssues fill, else from one
// read of the issue.
func (c *GitHub) AddToBoard(ctx context.Context, number int) (BoardItem, error) {
	if err := c.configured(); err != nil {
		return BoardItem{}, err
	}
	b, err := c.board()
	if err != nil {
		return BoardItem{}, err
	}
	contentID, err := c.nodeID(ctx, number)
	if err != nil {
		return BoardItem{}, err
	}
	itemID, err := c.addToBoard(ctx, b.ProjectID, contentID)
	if err != nil {
		return BoardItem{}, err
	}
	return BoardItem{ItemID: itemID, Repo: c.Repo, Issue: number}, nil
}

// addToBoard adds the issue with node ID contentID to project projectID and returns the ID of
// the new project item.
func (c *GitHub) addToBoard(ctx context.Context, projectID, contentID string) (string, error) {
	var data struct {
		AddProjectV2ItemByID *struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	if err := c.graphql(ctx, AddToBoardMutation, map[string]any{"project": projectID, "content": contentID}, &data); err != nil {
		return "", err
	}
	if data.AddProjectV2ItemByID == nil || data.AddProjectV2ItemByID.Item.ID == "" {
		return "", fmt.Errorf("forge: add %s to board %s: no item in the answer", contentID, projectID)
	}
	return data.AddProjectV2ItemByID.Item.ID, nil
}
