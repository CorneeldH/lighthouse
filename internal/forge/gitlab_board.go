package forge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// closedWindow is how far back BoardItems reads closed issues, by updated_at. A closed issue
// with an open run outside it reaches the dispatcher through IssueItem (dispatch.Prepare).
const closedWindow = 14 * 24 * time.Hour

// gitlabGQLMRFields is the selection of a closing merge request, the fields of gitlabGQLMR.
const gitlabGQLMRFields = `iid state diffHeadSha sourceBranch headPipeline { status } detailedMergeStatus mergedAt sourceProjectId targetProjectId`

// workItemSelection is one issue work item as BoardItems and IssueItem read it: the status
// widget, the labels, the blockers (with the state of their closing merge requests) and the
// merge requests that close it. Every nested list asks pageInfo, so a list longer than its
// first: n is an error instead of a silent cut.
const workItemSelection = `id iid title state updatedAt
      widgets {
        type
        ... on WorkItemWidgetStatus { status { name } }
        ... on WorkItemWidgetLabels { labels(first: 100) { pageInfo { hasNextPage } nodes { title } } }
        ... on WorkItemWidgetLinkedItems {
          linkedItems(filter: BLOCKED_BY, first: 20) {
            pageInfo { hasNextPage }
            nodes {
              workItem {
                iid state namespace { fullPath }
                widgets {
                  type
                  ... on WorkItemWidgetDevelopment {
                    closingMergeRequests(first: 10) { pageInfo { hasNextPage } nodes { mergeRequest { state } } }
                  }
                }
              }
            }
          }
        }
        ... on WorkItemWidgetDevelopment {
          closingMergeRequests(first: 20) {
            pageInfo { hasNextPage }
            nodes { mergeRequest { ` + gitlabGQLMRFields + ` } }
          }
        }
      }`

// gitlabBoardItemsQuery reads one page of 100 issue work items of one state ("opened" or
// "closed"), updated after $updatedAfter when it is set. One query has no OR over state, so
// BoardItems runs it twice. The text, with workItemSelection in place:
//
//	query LighthouseBoardItems($project: ID!, $state: IssuableState!, $updatedAfter: Time, $after: String) {
//	  project(fullPath: $project) {
//	    workItems(types: [ISSUE], state: $state, updatedAfter: $updatedAfter, first: 100, after: $after) {
//	      pageInfo { hasNextPage endCursor }
//	      nodes { <workItemSelection> }
//	    }
//	  }
//	}
const gitlabBoardItemsQuery = `query LighthouseBoardItems($project: ID!, $state: IssuableState!, $updatedAfter: Time, $after: String) {
  project(fullPath: $project) {
    workItems(types: [ISSUE], state: $state, updatedAfter: $updatedAfter, first: 100, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { ` + workItemSelection + ` }
    }
  }
}`

// gitlabIssueItemQuery reads the work item of one issue by iid:
//
//	query LighthouseIssueItem($project: ID!, $iid: String!) {
//	  project(fullPath: $project) {
//	    workItems(types: [ISSUE], iid: $iid, first: 1) { nodes { <workItemSelection> } }
//	  }
//	}
const gitlabIssueItemQuery = `query LighthouseIssueItem($project: ID!, $iid: String!) {
  project(fullPath: $project) {
    workItems(types: [ISSUE], iid: $iid, first: 1) { nodes { ` + workItemSelection + ` } }
  }
}`

// gitlabWorkItemIDQuery reads the global ID (gid://gitlab/WorkItem/<id>) of one issue by iid:
// workItemUpdate takes that ID, not the iid that is BoardItem.ItemID.
const gitlabWorkItemIDQuery = `query LighthouseWorkItemID($project: ID!, $iid: String!) {
  project(fullPath: $project) {
    workItems(types: [ISSUE], iid: $iid, first: 1) { nodes { id } }
  }
}`

// gitlabSetStatusMutation sets the built-in status of one work item by status name, which
// GitLab matches case-insensitively against the lifecycle of the top-level group.
const gitlabSetStatusMutation = `mutation LighthouseSetStatus($id: WorkItemID!, $status: String!) {
  workItemUpdate(input: {id: $id, statusWidget: {name: $status}}) {
    errors
    workItem { id }
  }
}`

// gitlabGQLMR is a merge request in GitLab's GraphQL shape, exactly gitlabGQLMRFields. Enum
// values (pipeline and merge status) are upper case there; rest translates them.
type gitlabGQLMR struct {
	IID          string `json:"iid"`
	State        string `json:"state"`
	DiffHeadSHA  string `json:"diffHeadSha"`
	SourceBranch string `json:"sourceBranch"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"headPipeline"`
	DetailedMergeStatus string     `json:"detailedMergeStatus"`
	MergedAt            *time.Time `json:"mergedAt"`
	SourceProjectID     int64      `json:"sourceProjectId"`
	TargetProjectID     int64      `json:"targetProjectId"`
}

// rest translates m to the REST shape mrToPR reads. GraphQL has no has_conflicts in this
// selection: the merge status CONFLICT says it.
func (m gitlabGQLMR) rest() (glMR, error) {
	iid, err := strconv.Atoi(m.IID)
	if err != nil {
		return glMR{}, fmt.Errorf("merge request iid %q: %w", m.IID, err)
	}
	status := strings.ToLower(m.DetailedMergeStatus)
	out := glMR{IID: iid, State: m.State, SHA: m.DiffHeadSHA, SourceBranch: m.SourceBranch,
		DetailedMergeStatus: status, HasConflicts: status == "conflict", MergedAt: m.MergedAt,
		SourceProjectID: m.SourceProjectID, TargetProjectID: m.TargetProjectID}
	if p := m.HeadPipeline; p != nil {
		out.HeadPipeline = &struct {
			Status string `json:"status"`
		}{Status: strings.ToLower(p.Status)}
	}
	return out, nil
}

// gqlConnection is a nested list with its pageInfo.
type gqlConnection[T any] struct {
	PageInfo pageInfo `json:"pageInfo"`
	Nodes    []T      `json:"nodes"`
}

// gqlWidget is one widget of a work item; type says which of the fields it fills.
type gqlWidget struct {
	Type   string `json:"type"`
	Status *struct {
		Name string `json:"name"`
	} `json:"status"`
	Labels *gqlConnection[struct {
		Title string `json:"title"`
	}] `json:"labels"`
	LinkedItems *gqlConnection[struct {
		WorkItem *gqlLinkedIssue `json:"workItem"`
	}] `json:"linkedItems"`
	ClosingMergeRequests *gqlConnection[struct {
		MergeRequest *gitlabGQLMR `json:"mergeRequest"`
	}] `json:"closingMergeRequests"`
}

// gqlLinkedIssue is a blocker: an issue work item, possibly of another project.
type gqlLinkedIssue struct {
	IID       string `json:"iid"`
	State     string `json:"state"`
	Namespace struct {
		FullPath string `json:"fullPath"`
	} `json:"namespace"`
	Widgets []gqlWidget `json:"widgets"`
}

// gqlWorkItem is an issue work item as workItemSelection reads it.
type gqlWorkItem struct {
	ID        string      `json:"id"`
	IID       string      `json:"iid"`
	Title     string      `json:"title"`
	State     string      `json:"state"`
	UpdatedAt time.Time   `json:"updatedAt"`
	Widgets   []gqlWidget `json:"widgets"`
}

// gqlWorkItems is the project.workItems answer of every query here.
type gqlWorkItems struct {
	Project *struct {
		WorkItems gqlConnection[gqlWorkItem] `json:"workItems"`
	} `json:"project"`
}

// board is the [forge.board] of the repo, or an error when there is none.
func (g *GitLab) board() (*repoconfig.Board, error) {
	if g.Board == nil {
		return nil, fmt.Errorf("forge: %s: missing [forge.board]", g.Repo)
	}
	return g.Board, nil
}

// boardItem translates a work item to a BoardItem of this project. ItemID is the iid.
func (g *GitLab) boardItem(w gqlWorkItem, b *repoconfig.Board) (BoardItem, error) {
	iid, err := strconv.Atoi(w.IID)
	if err != nil {
		return BoardItem{}, fmt.Errorf("forge: work item %s: iid %q: %w", w.ID, w.IID, err)
	}
	it := BoardItem{ItemID: w.IID, ItemUpdatedAt: w.UpdatedAt, Repo: g.Repo, Issue: iid, Title: w.Title,
		IssueState: w.State, IssueUpdatedAt: w.UpdatedAt}
	for _, wd := range w.Widgets {
		if err := g.applyWidget(&it, wd, b); err != nil {
			return BoardItem{}, fmt.Errorf("forge: issue %s#%d: %w", g.Repo, iid, err)
		}
	}
	return it, nil
}

// applyWidget fills the fields of it that widget wd carries.
func (g *GitLab) applyWidget(it *BoardItem, wd gqlWidget, b *repoconfig.Board) error {
	switch wd.Type {
	case "STATUS":
		if wd.Status != nil && wd.Status.Name != "" {
			it.Status = statusName(b.StatusOptions, wd.Status.Name)
		}
	case "LABELS":
		if wd.Labels == nil {
			return nil
		}
		if wd.Labels.PageInfo.HasNextPage {
			return truncated("labels", len(wd.Labels.Nodes))
		}
		for _, l := range wd.Labels.Nodes {
			it.Labels = append(it.Labels, l.Title)
			if p := labelPriority(b.PriorityOptions, l.Title); p != "" {
				it.Priority = p
			}
		}
	case "LINKED_ITEMS":
		if wd.LinkedItems == nil {
			return nil
		}
		if wd.LinkedItems.PageInfo.HasNextPage {
			return truncated("linkedItems", len(wd.LinkedItems.Nodes))
		}
		for _, n := range wd.LinkedItems.Nodes {
			if n.WorkItem == nil {
				continue
			}
			bl, err := blocker(*n.WorkItem)
			if err != nil {
				return err
			}
			it.BlockedBy = append(it.BlockedBy, bl)
		}
	case "DEVELOPMENT":
		if wd.ClosingMergeRequests == nil {
			return nil
		}
		if wd.ClosingMergeRequests.PageInfo.HasNextPage {
			return truncated("closingMergeRequests", len(wd.ClosingMergeRequests.Nodes))
		}
		for _, n := range wd.ClosingMergeRequests.Nodes {
			if n.MergeRequest == nil {
				continue
			}
			m, err := n.MergeRequest.rest()
			if err != nil {
				return err
			}
			p := mrToPR(m)
			p.Closes = true
			it.ClosingPRs = append(it.ClosingPRs, p)
		}
	}
	return nil
}

// blocker translates a linked blocking work item; MergedPR is a merged closing merge request.
func blocker(w gqlLinkedIssue) (Blocker, error) {
	iid, err := strconv.Atoi(w.IID)
	if err != nil {
		return Blocker{}, fmt.Errorf("blocker iid %q: %w", w.IID, err)
	}
	bl := Blocker{Repo: w.Namespace.FullPath, Issue: iid, State: w.State}
	for _, wd := range w.Widgets {
		if wd.Type != "DEVELOPMENT" || wd.ClosingMergeRequests == nil {
			continue
		}
		if wd.ClosingMergeRequests.PageInfo.HasNextPage {
			return Blocker{}, truncated(fmt.Sprintf("blocker %s#%d closingMergeRequests", bl.Repo, iid), len(wd.ClosingMergeRequests.Nodes))
		}
		for _, n := range wd.ClosingMergeRequests.Nodes {
			bl.MergedPR = bl.MergedPR || (n.MergeRequest != nil && n.MergeRequest.State == "merged")
		}
	}
	return bl, nil
}

// statusName is the lighthouse name of GitLab status raw, matched case-insensitively as GitLab
// matches names; "unknown:<raw>" for a status that status_options does not map.
func statusName(options map[string]string, raw string) string {
	for name, gl := range options {
		if strings.EqualFold(gl, raw) {
			return name
		}
	}
	return UnknownStatusPrefix + raw
}

// labelPriority is the lighthouse name of priority label label, "" for a label that
// priority_options does not list.
func labelPriority(options map[string]string, label string) string {
	for name, l := range options {
		if l == label {
			return name
		}
	}
	return ""
}

// BoardItems lists the issues of the project: every open issue, and the closed issues updated
// within closedWindow. That is two paginated queries of 100 work items, a fixed number of pages
// per tick and no call per issue. A nested list GitLab cut off is an error and gives no items.
func (g *GitLab) BoardItems(ctx context.Context) ([]BoardItem, error) {
	b, err := g.board()
	if err != nil {
		return nil, err
	}
	out, err := g.workItems(ctx, b, map[string]any{"state": "opened"})
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-closedWindow).UTC().Format(time.RFC3339)
	closed, err := g.workItems(ctx, b, map[string]any{"state": "closed", "updatedAfter": since})
	if err != nil {
		return nil, err
	}
	return append(out, closed...), nil
}

// workItems reads every page of gitlabBoardItemsQuery with filter as its variables.
func (g *GitLab) workItems(ctx context.Context, b *repoconfig.Board, filter map[string]any) ([]BoardItem, error) {
	var out []BoardItem
	var after any // null on the first page
	for {
		vars := map[string]any{"project": g.Repo, "after": after}
		for k, v := range filter {
			vars[k] = v
		}
		var data gqlWorkItems
		if err := g.graphql(ctx, gitlabBoardItemsQuery, vars, &data); err != nil {
			return nil, err
		}
		if data.Project == nil {
			return nil, fmt.Errorf("forge: project %s not found", g.Repo)
		}
		page := data.Project.WorkItems
		for _, w := range page.Nodes {
			it, err := g.boardItem(w, b)
			if err != nil {
				return nil, err
			}
			out = append(out, it)
		}
		if !page.PageInfo.HasNextPage {
			return out, nil
		}
		after = page.PageInfo.EndCursor
	}
}

// IssueItem returns the item of issue number. Every issue is on the GitLab board, so ok is
// true for an issue that exists; one that does not is an error.
func (g *GitLab) IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error) {
	b, err := g.board()
	if err != nil {
		return BoardItem{}, false, err
	}
	w, err := g.workItem(ctx, gitlabIssueItemQuery, number)
	if err != nil {
		return BoardItem{}, false, err
	}
	it, err := g.boardItem(w, b)
	return it, err == nil, err
}

// workItem reads the work item of issue number with query, which selects by $iid.
func (g *GitLab) workItem(ctx context.Context, query string, number int) (gqlWorkItem, error) {
	var data gqlWorkItems
	if err := g.graphql(ctx, query, map[string]any{"project": g.Repo, "iid": strconv.Itoa(number)}, &data); err != nil {
		return gqlWorkItem{}, err
	}
	if data.Project == nil || len(data.Project.WorkItems.Nodes) == 0 {
		return gqlWorkItem{}, fmt.Errorf("forge: issue %s#%d not found", g.Repo, number)
	}
	return data.Project.WorkItems.Nodes[0], nil
}

// AddToBoard writes nothing: every issue is on the GitLab board. It returns IssueItem.
func (g *GitLab) AddToBoard(ctx context.Context, number int) (BoardItem, error) {
	it, _, err := g.IssueItem(ctx, number)
	return it, err
}

// SetStatus sets the built-in status of item to the GitLab status that status_options maps
// status to. A name it does not list is an error before any request. The mutation needs the
// global work item ID, so it costs one query on the iid first. GitLab refuses a name outside
// the lifecycle of the top-level group; that error names the status and where to add it.
func (g *GitLab) SetStatus(ctx context.Context, item BoardItem, status string) error {
	b, err := g.board()
	if err != nil {
		return err
	}
	name, ok := b.StatusOptions[status]
	if !ok {
		return fmt.Errorf("status %q is not in [forge.board] status_options", status)
	}
	iid, err := strconv.Atoi(item.ItemID)
	if err != nil || iid <= 0 {
		return fmt.Errorf("forge: %s#%d: no board item to set the status of", item.Repo, item.Issue)
	}
	w, err := g.workItem(ctx, gitlabWorkItemIDQuery, iid)
	if err != nil {
		return err
	}
	var data struct {
		WorkItemUpdate *struct {
			Errors []string `json:"errors"`
		} `json:"workItemUpdate"`
	}
	err = g.graphql(ctx, gitlabSetStatusMutation, map[string]any{"id": w.ID, "status": name}, &data)
	if err == nil && data.WorkItemUpdate != nil && len(data.WorkItemUpdate.Errors) > 0 {
		err = &gitlabGraphQLError{Message: strings.Join(data.WorkItemUpdate.Errors, "; ")}
	}
	if ge := (*gitlabGraphQLError)(nil); errors.As(err, &ge) {
		return fmt.Errorf("forge: set status of %s#%d to %q (GitLab status %q): %w; a status missing from the "+
			"lifecycle is added on the top-level group under Settings → Work items → Statuses", g.Repo, iid, status, name, err)
	}
	if err != nil {
		return err
	}
	if data.WorkItemUpdate == nil {
		return fmt.Errorf("forge: set status of %s#%d: no workItemUpdate in the answer", g.Repo, iid)
	}
	return nil
}
