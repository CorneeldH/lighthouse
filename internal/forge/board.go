package forge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
	// BaseRef is the branch the PR merges into.
	BaseRef string `json:"base_ref,omitempty"`
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
	// Archived is an archived item. Only IssueItem gives one: BoardItems lists no archived item.
	Archived bool
}

// EffectiveStatus is the status to decide on: the status_options name of the item's status,
// or, for a board option that status_options does not list, its raw name on the board.
func (it BoardItem) EffectiveStatus() string {
	if strings.HasPrefix(it.Status, UnknownStatusPrefix) {
		return it.StatusName
	}
	return it.Status
}

// OpenPRsWithStatusQuery reads one page of 100 open pull requests with draft, mergeable and
// the state of their checks.
const OpenPRsWithStatusQuery = `query OpenPRsWithStatus($owner: String!, $name: String!, $after: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: 100, after: $after, states: [OPEN]) {
      pageInfo { hasNextPage endCursor }
      nodes { number title url isDraft mergeable statusCheckRollup { state } }
    }
  }
}`

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
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

// BoardItems lists every item of the board of [forge.board], following every page, without
// archived items. A list that cannot be read completely is an error and gives no items: a
// missing blocker or Status value would change what the dispatcher decides.
func (c *GitHub) BoardItems(ctx context.Context) ([]BoardItem, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	b, err := c.board()
	if err != nil {
		return nil, err
	}
	p, err := c.project(ctx, b)
	if err != nil {
		return nil, err
	}
	items, err := c.listItems(ctx, p)
	if err != nil {
		return nil, err
	}
	merged := mergedIssues(items, p, b)
	var out []BoardItem
	for _, r := range items {
		if r.ArchivedAt != nil {
			continue
		}
		it, pulls := r.boardItem(p, b)
		total := 0
		// A closed issue starts or resumes no run, so its blockers are not read.
		if r.issue() && !strings.EqualFold(it.IssueState, "CLOSED") {
			total = r.Content.Summary.TotalBlockedBy
		}
		if err := c.complete(ctx, &it, total, pulls, merged); err != nil {
			return nil, fmt.Errorf("forge: board item %s: %w", it.ItemID, err)
		}
		out = append(out, it)
	}
	return out, nil
}

// mergedIssues is the set of issues listed on the board with a linked pull request that is
// merged. The REST list holds no archived item, so an issue whose item is archived is not in it.
func mergedIssues(items []restItem, p project, b *repoconfig.Board) map[issueKey]bool {
	out := map[issueKey]bool{}
	for _, r := range items {
		it, pulls := r.boardItem(p, b)
		for _, pl := range pulls {
			if !pl.MergedAt.IsZero() {
				out[issueKey{it.Repo, it.Issue}] = true
			}
		}
	}
	return out
}

// complete reads the blockers of it (only when total, the total_blocked_by, is above 0) and
// the PR fields of pulls into it. MergedPR of a blocker is true only for one on the board with
// a merged linked pull request. A 403 or 404 on an item of another repo than c.Repo leaves
// that part empty, as GraphQL did for content the token cannot see; any other error stays one.
func (c *GitHub) complete(ctx context.Context, it *BoardItem, total int, pulls []restPull, merged map[issueKey]bool) error {
	hidden := func(err error) bool { return it.Repo != c.Repo && notVisible(err) }
	if total > 0 {
		blockers, err := c.blockedBy(ctx, it.Repo, it.Issue)
		if err != nil && !hidden(err) {
			return err
		}
		for i := range blockers {
			blockers[i].MergedPR = merged[issueKey{blockers[i].Repo, blockers[i].Issue}]
		}
		it.BlockedBy = blockers
	}
	for _, pl := range pulls {
		pr := pl.pr(true)
		if err := c.enrich(ctx, cmp.Or(pl.repo(), it.Repo), &pr); err != nil {
			if !hidden(err) {
				return err
			}
			pr.Mergeable, pr.Checks = "UNKNOWN", ""
		}
		it.ClosingPRs = append(it.ClosingPRs, pr)
	}
	return nil
}

// notVisible is a 404, or a 403 that is no rate limit: the token cannot see the resource.
func notVisible(err error) bool {
	var se *httpStatusError
	return errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusForbidden && !se.RateLimited)
}

func truncated(connection string, n int) error {
	return fmt.Errorf("%s truncated at %d", connection, n)
}

// BranchPRs lists the pull requests whose head is branch in the repo, open, closed and merged,
// following every page; an open one carries Mergeable and Checks. Closes is false on each: the
// branch says nothing about which issue a PR closes. A PR from a fork with the same branch name
// is not listed.
func (c *GitHub) BranchPRs(ctx context.Context, branch string) ([]PR, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	owner, name, ok := strings.Cut(c.Repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	q := url.Values{"head": {owner + ":" + branch}, "state": {"all"}, "per_page": {"100"}}
	var pulls []restPull
	for next := fmt.Sprintf("%s/repos/%s/pulls?%s", c.BaseURL, c.Repo, q.Encode()); next != ""; {
		var page []restPull
		var err error
		if next, err = c.do(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		pulls = append(pulls, page...)
	}
	out := make([]PR, 0, len(pulls))
	for _, p := range pulls {
		pr := p.pr(false)
		if err := c.enrich(ctx, cmp.Or(p.repo(), c.Repo), &pr); err != nil {
			return nil, fmt.Errorf("forge: branch %s: PR %d: %w", branch, p.Number, err)
		}
		out = append(out, pr)
	}
	return out, nil
}

// PullHead is the head of a pull request or merge request in any state.
type PullHead struct {
	Number int
	State  string // OPEN, CLOSED or MERGED, as PR.State
	// MergedAt is the merge time in RFC 3339, UTC; "" for a PR that is not merged.
	MergedAt string
	HeadRef  string
}

// ClosingHead is a pull request that closes an issue or refers to it.
type ClosingHead struct {
	HeadRef string
	// Repo is the repository the pull request lives in, owner/name.
	Repo string
	Body string
	// Merged is true for a merged pull request.
	Merged bool
	// Closes is true for a pull request that closes the issue; false for one that only refers
	// to it.
	Closes bool
	// CrossRepo is true for a pull request whose head lives in another repository.
	CrossRepo bool
}

// rfc3339 is t in RFC 3339, UTC; "" for the zero time.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// PullHeads lists every pull request of the repo in any state, following every page.
func (c *GitHub) PullHeads(ctx context.Context) ([]PullHead, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var out []PullHead
	for next := fmt.Sprintf("%s/repos/%s/pulls?state=all&per_page=100", c.BaseURL, c.Repo); next != ""; {
		var page []restPull
		var err error
		if next, err = c.do(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		for _, p := range page {
			pr := p.pr(false)
			out = append(out, PullHead{Number: p.Number, State: pr.State, MergedAt: rfc3339(p.MergedAt), HeadRef: p.Head.Ref})
		}
	}
	return out, nil
}

// issueClosingHeadsQuery asks for the pull requests that close an issue and the ones that
// cross-reference it.
const issueClosingHeadsQuery = `query IssueClosingHeads($owner:String!,$name:String!,$nr:Int!){repository(owner:$owner,name:$name){issue(number:$nr){
  closedByPullRequestsReferences(first:50,includeClosedPrs:true){pageInfo{hasNextPage} nodes{number headRefName merged isCrossRepository body repository{nameWithOwner}}}
  timelineItems(itemTypes:[CROSS_REFERENCED_EVENT],first:100){pageInfo{hasNextPage} nodes{... on CrossReferencedEvent{source{... on PullRequest{number headRefName merged isCrossRepository body repository{nameWithOwner}}}}}}}}}`

type closingNode struct {
	Number            int    `json:"number"`
	HeadRefName       string `json:"headRefName"`
	Merged            bool   `json:"merged"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	Body              string `json:"body"`
	Repository        struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// IssueClosingHeads lists the pull requests that close issue number (closedByPullRequestsReferences)
// and the ones that only refer to it (cross-referenced events). A PR in both counts once, as
// closing. A connection that does not fit in one page is an error, never a short list.
func (c *GitHub) IssueClosingHeads(ctx context.Context, number int) ([]ClosingHead, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	owner, name, ok := strings.Cut(c.Repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	var data struct {
		Repository struct {
			Issue struct {
				ClosedBy struct {
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
					Nodes []closingNode `json:"nodes"`
				} `json:"closedByPullRequestsReferences"`
				Timeline struct {
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
					Nodes []struct {
						Source *closingNode `json:"source"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			} `json:"issue"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "name": name, "nr": number}
	if err := c.graphql(ctx, issueClosingHeadsQuery, vars, &data); err != nil {
		return nil, err
	}
	issue := data.Repository.Issue
	if issue.ClosedBy.PageInfo.HasNextPage {
		return nil, truncated("closedByPullRequestsReferences", len(issue.ClosedBy.Nodes))
	}
	if issue.Timeline.PageInfo.HasNextPage {
		return nil, truncated("timelineItems", len(issue.Timeline.Nodes))
	}
	type key struct {
		repo string
		nr   int
	}
	seen := map[key]bool{}
	out := []ClosingHead{}
	add := func(n *closingNode, closes bool) {
		// A source that is no pull request (an issue) has no head branch.
		if n == nil || n.HeadRefName == "" || seen[key{n.Repository.NameWithOwner, n.Number}] {
			return
		}
		seen[key{n.Repository.NameWithOwner, n.Number}] = true
		out = append(out, ClosingHead{HeadRef: n.HeadRefName, Repo: n.Repository.NameWithOwner, Body: n.Body,
			Merged: n.Merged, Closes: closes, CrossRepo: n.IsCrossRepository})
	}
	for i := range issue.ClosedBy.Nodes {
		add(&issue.ClosedBy.Nodes[i], true)
	}
	for _, it := range issue.Timeline.Nodes {
		add(it.Source, false)
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
	p, err := c.project(ctx, b)
	if err != nil {
		return err
	}
	id, err := c.itemNumber(ctx, p, item.ItemID)
	if err != nil {
		return err
	}
	body := map[string]any{"fields": []map[string]any{{"id": p.status, "value": option}}}
	_, err = c.do(ctx, http.MethodPatch, fmt.Sprintf("%s/items/%d", p.base, id), body, nil)
	return err
}

// itemNumber is the numeric ID of the board item with node ID nodeID: from the table, else
// after one read of the list, else an error naming nodeID.
func (c *GitHub) itemNumber(ctx context.Context, p project, nodeID string) (int64, error) {
	lookup := func() (int64, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		id, ok := c.itemIDs[nodeID]
		return id, ok
	}
	if id, ok := lookup(); ok {
		return id, nil
	}
	if _, err := c.listItems(ctx, p); err != nil {
		return 0, err
	}
	if id, ok := lookup(); ok {
		return id, nil
	}
	return 0, fmt.Errorf("forge: board item %s is not on the board of project %d", nodeID, c.Board.ProjectNumber)
}

// IssueItem returns the item of issue number on the board of [forge.board], archived or not;
// ok is false when the issue is not on that board. Status, StatusName, Priority and Labels are
// filled as in BoardItems, blockers and closing PRs are not; an archived item has Archived set.
// A deleted or transferred issue is ErrIssueNotFound.
//
// The REST list of the board holds no archived item, so the numeric item ID comes from the
// in-process table, else one list read, else the item table saved in the CacheStore, which
// keeps an item for 30 days after a list last showed it. An item archived before any list read
// saw it, or gone for longer, reads as not on the board; a client without cache keeps that
// table only for its own process.
func (c *GitHub) IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error) {
	if err := c.configured(); err != nil {
		return BoardItem{}, false, err
	}
	b, err := c.board()
	if err != nil {
		return BoardItem{}, false, err
	}
	p, err := c.project(ctx, b)
	if err != nil {
		return BoardItem{}, false, err
	}
	key := issueKey{c.Repo, number}
	lookup := func() (int64, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		id, listed := c.issueItems[key]
		return id, listed
	}
	id, found := lookup()
	if !found {
		if _, err := c.listItems(ctx, p); err != nil {
			return BoardItem{}, false, err
		}
		if id, found = lookup(); !found {
			id, found = c.savedItemID(ctx, key)
		}
	}
	if !found {
		if _, err := c.readIssue(ctx, number); err != nil {
			var se *httpStatusError
			if errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusGone) {
				return BoardItem{}, false, fmt.Errorf("forge: issue %s#%d: %w", c.Repo, number, ErrIssueNotFound)
			}
			return BoardItem{}, false, err
		}
		return BoardItem{}, false, nil
	}
	var r restItem
	if _, err := c.do(ctx, http.MethodGet, p.itemURL(id), nil, &r); err != nil {
		var se *httpStatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return BoardItem{}, false, nil // the item was deleted from the board
		}
		return BoardItem{}, false, err
	}
	it, _ := r.boardItem(p, b)
	it.Repo, it.Issue = c.Repo, number
	return it, true, nil
}

// AddToBoard adds issue number to the board of [forge.board] and returns the new item, without
// a status. The item stays in the table of numeric IDs until the next complete list.
func (c *GitHub) AddToBoard(ctx context.Context, number int) (BoardItem, error) {
	if err := c.configured(); err != nil {
		return BoardItem{}, err
	}
	b, err := c.board()
	if err != nil {
		return BoardItem{}, err
	}
	owner, name, ok := strings.Cut(c.Repo, "/")
	if !ok || owner == "" || name == "" {
		return BoardItem{}, fmt.Errorf("forge: repo %q is not owner/name", c.Repo)
	}
	p, err := c.project(ctx, b)
	if err != nil {
		return BoardItem{}, err
	}
	var added struct {
		ID     int64  `json:"id"`
		NodeID string `json:"node_id"`
	}
	body := map[string]any{"type": "Issue", "owner": owner, "repo": name, "number": number}
	if _, err := c.do(ctx, http.MethodPost, p.base+"/items", body, &added); err != nil {
		return BoardItem{}, err
	}
	if added.ID == 0 || added.NodeID == "" {
		return BoardItem{}, fmt.Errorf("forge: add %s#%d to board %s: no item in the answer", c.Repo, number, b.ProjectID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.itemIDs == nil {
		c.itemIDs, c.issueItems = map[string]int64{}, map[issueKey]int64{}
	}
	c.itemIDs[added.NodeID] = added.ID
	c.issueItems[issueKey{c.Repo, number}] = added.ID
	return BoardItem{ItemID: added.NodeID, Repo: c.Repo, Issue: number}, nil
}
