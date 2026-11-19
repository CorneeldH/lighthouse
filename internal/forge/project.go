package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// linkedPRsType is the data_type of the field "Linked pull requests"; it is recognised by that
// type, never by its name.
const linkedPRsType = "linked_pull_requests"

// project is where the board lives on the REST API: base is BaseURL plus
// "/users/<owner>/projectsV2/<n>" (or "/orgs/…"), and the fields are the numeric IDs the items
// route and the item PATCH take. priority is 0 when the config has no priority field.
type project struct {
	base                        string
	status, priority, linkedPRs int64
}

// itemsURL is the first page of the items with the status, priority and linked-PR fields.
func (p project) itemsURL() string {
	fields := strconv.FormatInt(p.status, 10)
	if p.priority != 0 {
		fields += "," + strconv.FormatInt(p.priority, 10)
	}
	fields += "," + strconv.FormatInt(p.linkedPRs, 10)
	return p.base + "/items?per_page=100&fields=" + fields
}

// project finds the project of b: the owner path (user, else organisation), the check of its
// node ID against b.ProjectID, and the numeric IDs of the fields. It is kept once per client;
// an error is not kept.
func (c *GitHub) project(ctx context.Context, b *repoconfig.Board) (project, error) {
	c.mu.Lock()
	p, ok := c.proj, c.projOK
	c.mu.Unlock()
	if ok {
		return p, nil
	}
	p, err := c.findProject(ctx, b)
	if err != nil {
		return project{}, err
	}
	c.mu.Lock()
	c.proj, c.projOK = p, true
	c.mu.Unlock()
	return p, nil
}

func (c *GitHub) findProject(ctx context.Context, b *repoconfig.Board) (project, error) {
	if err := c.configured(); err != nil {
		return project{}, err
	}
	if b.ProjectNumber <= 0 {
		return project{}, errors.New("forge: [forge.board] project_number is not set")
	}
	owner := b.ProjectOwner
	if owner == "" {
		owner, _, _ = strings.Cut(c.Repo, "/")
	}
	var node struct {
		NodeID string `json:"node_id"`
	}
	var base string
	for _, kind := range []string{"users", "orgs"} {
		base = fmt.Sprintf("%s/%s/%s/projectsV2/%d", c.BaseURL, kind, owner, b.ProjectNumber)
		_, err := c.do(ctx, http.MethodGet, base, nil, &node)
		if err == nil {
			break
		}
		var se *httpStatusError
		if !errors.As(err, &se) || se.Code != http.StatusNotFound {
			return project{}, err
		}
		if kind == "orgs" {
			return project{}, fmt.Errorf("forge: project %d not found under users/%s or orgs/%s; set [forge.board] project_owner to the owner of the project: %w", b.ProjectNumber, owner, owner, err)
		}
	}
	if node.NodeID != b.ProjectID {
		return project{}, fmt.Errorf("forge: project %d has node ID %q, but [forge.board] project_id is %q", b.ProjectNumber, node.NodeID, b.ProjectID)
	}
	return c.projectFields(ctx, base, b)
}

// projectFields reads every field of the project at base and picks the three the board needs.
func (c *GitHub) projectFields(ctx context.Context, base string, b *repoconfig.Board) (project, error) {
	type field struct {
		ID       int64  `json:"id"`
		NodeID   string `json:"node_id"`
		DataType string `json:"data_type"`
	}
	var all []field
	for url := base + "/fields?per_page=100"; url != ""; {
		var page []field
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return project{}, err
		}
		all = append(all, page...)
		url = next
	}
	p := project{base: base}
	for _, f := range all {
		switch {
		case f.DataType == linkedPRsType:
			p.linkedPRs = f.ID
		case f.NodeID == b.StatusFieldID:
			p.status = f.ID
		case b.PriorityFieldID != "" && f.NodeID == b.PriorityFieldID:
			p.priority = f.ID
		}
	}
	switch {
	case p.status == 0:
		return project{}, fmt.Errorf("forge: [forge.board] status_field_id %q is not a field of project %d", b.StatusFieldID, b.ProjectNumber)
	case b.PriorityFieldID != "" && p.priority == 0:
		return project{}, fmt.Errorf("forge: [forge.board] priority_field_id %q is not a field of project %d", b.PriorityFieldID, b.ProjectNumber)
	case p.linkedPRs == 0:
		return project{}, fmt.Errorf("forge: project %d has no field of type %s", b.ProjectNumber, linkedPRsType)
	}
	return p, nil
}

// restItem is one project item as GET <base>/items gives it.
type restItem struct {
	ID         int64        `json:"id"`
	NodeID     string       `json:"node_id"`
	UpdatedAt  time.Time    `json:"updated_at"`
	ArchivedAt *time.Time   `json:"archived_at"`
	Content    *restContent `json:"content"`
	Fields     []restField  `json:"fields"`
}

// restContent is the content of a project item: an issue, a pull request (PullRequest set) or
// a draft (no Number).
type restContent struct {
	Number        int       `json:"number"`
	Title         string    `json:"title"`
	State         string    `json:"state"` // open, closed
	UpdatedAt     time.Time `json:"updated_at"`
	RepositoryURL string    `json:"repository_url"`
	Labels        []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest json.RawMessage `json:"pull_request"`
	Summary     struct {
		BlockedBy      int `json:"blocked_by"`
		TotalBlockedBy int `json:"total_blocked_by"`
	} `json:"issue_dependencies_summary"`
}

// issue reports whether the content is an issue.
func (c *restContent) issue() bool {
	return c != nil && c.Number != 0 && (len(c.PullRequest) == 0 || string(c.PullRequest) == "null")
}

// restField is one field value of a project item. Option is the value of a single-select
// field, nil without one; Pulls is the value of the "Linked pull requests" field.
type restField struct {
	ID       int64
	DataType string
	Option   *restOption
	Pulls    []restPull
}

// restOption is a single-select value: the option's ID and its name.
type restOption struct {
	ID   string `json:"id"`
	Name struct {
		Raw string `json:"raw"`
	} `json:"name"`
}

// UnmarshalJSON decodes value by data_type, so a value of the wrong shape fails the page.
func (f *restField) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       int64           `json:"id"`
		DataType string          `json:"data_type"`
		Value    json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*f = restField{ID: raw.ID, DataType: raw.DataType}
	if len(raw.Value) == 0 || string(raw.Value) == "null" {
		return nil
	}
	switch raw.DataType {
	case "single_select":
		return json.Unmarshal(raw.Value, &f.Option)
	case linkedPRsType:
		return json.Unmarshal(raw.Value, &f.Pulls)
	}
	return nil
}

// issueKey names an issue across repositories.
type issueKey struct {
	repo   string
	number int
}

// listItems reads every page of the items of p, archived ones included as REST gives them. A
// complete list replaces the table of numeric IDs by node ID and by issue; a failed one leaves
// the old table.
func (c *GitHub) listItems(ctx context.Context, p project) ([]restItem, error) {
	var all []restItem
	for url := p.itemsURL(); url != ""; {
		var page []restItem
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		url = next
	}
	itemIDs, issueItems := map[string]int64{}, map[issueKey]int64{}
	for _, r := range all {
		itemIDs[r.NodeID] = r.ID
		if r.Content.issue() {
			issueItems[issueKey{repoFromURL(r.Content.RepositoryURL), r.Content.Number}] = r.ID
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.itemIDs, c.issueItems = itemIDs, issueItems
	return all, nil
}

// boardItem maps r without a request: the item, its Status, StatusName and Priority by option
// of b, and for an issue its content and the pull requests of "Linked pull requests". Any
// other content gives only ItemID, ItemUpdatedAt and the fields.
func (r restItem) boardItem(p project, b *repoconfig.Board) (BoardItem, []restPull) {
	it := BoardItem{ItemID: r.NodeID, ItemUpdatedAt: r.UpdatedAt, Archived: r.ArchivedAt != nil}
	values := map[string]selectValue{}
	var pulls []restPull
	for _, f := range r.Fields {
		switch {
		case f.DataType == linkedPRsType:
			pulls = f.Pulls
		case f.Option == nil:
		case f.ID == p.status:
			values[b.StatusFieldID] = selectValue{optionID: f.Option.ID, name: f.Option.Name.Raw}
		case p.priority != 0 && f.ID == p.priority:
			values[b.PriorityFieldID] = selectValue{optionID: f.Option.ID, name: f.Option.Name.Raw}
		}
	}
	ct := r.Content
	if !ct.issue() {
		return named(it, values, b), nil
	}
	it.Repo, it.Issue, it.Title = repoFromURL(ct.RepositoryURL), ct.Number, ct.Title
	it.IssueState, it.IssueUpdatedAt = strings.ToUpper(ct.State), ct.UpdatedAt
	for _, l := range ct.Labels {
		it.Labels = append(it.Labels, l.Name)
	}
	return named(it, values, b), pulls
}
