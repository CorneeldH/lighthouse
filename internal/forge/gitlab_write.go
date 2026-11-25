package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// glMilestone is a milestone in GitLab's REST shape. ID is the global id, IID the number inside
// its project or group: two milestones can share an IID, never an ID. A group milestone has a
// GroupID.
type glMilestone struct {
	ID          int    `json:"id"`
	IID         int    `json:"iid"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Description string `json:"description"`
	GroupID     *int   `json:"group_id"`
	WebURL      string `json:"web_url"`
}

// milestone translates to a Milestone; Number is the global id.
func (m glMilestone) milestone() Milestone {
	state := m.State
	if state == "active" {
		state = "open"
	}
	return Milestone{Number: m.ID, Title: m.Title, State: state, Description: m.Description, URL: m.WebURL}
}

// change translates a merge request to a Change.
func (m glMR) change() Change {
	out := Change{Number: m.IID, Title: m.Title, Body: m.Description, State: m.State, Labels: m.Labels, URL: m.WebURL,
		CreatedAt: m.CreatedAt, HeadRef: m.SourceBranch, BaseRef: m.TargetBranch, Draft: m.Draft}
	// Only conflicts and an unsettled check count, as on GitHub: fix-pr reads the CI and the
	// draft flag apart.
	switch {
	case m.HasConflicts, m.DetailedMergeStatus == "conflict", m.DetailedMergeStatus == "need_rebase":
		out.Mergeable = "conflicting"
	case slices.Contains([]string{"unchecked", "checking", "preparing", "approvals_syncing"}, m.DetailedMergeStatus):
		out.Mergeable = "unknown"
	default:
		out.Mergeable = "mergeable"
	}
	switch m.State {
	case "opened", "locked": // locked while it merges
		out.State = "open"
	}
	if m.MergedAt != nil {
		out.MergedAt = *m.MergedAt
	}
	if m.Author != nil {
		out.Author = m.Author.Username
	}
	if m.Milestone != nil {
		ms := m.Milestone.milestone()
		out.Milestone = &ms
	}
	return out
}

// glMilestones lists the milestones of the project and its ancestor groups in state active,
// closed or "" for both.
func (g *GitLab) glMilestones(ctx context.Context, state string) ([]glMilestone, error) {
	q := neturl.Values{"include_ancestors": {"true"}}
	if state != "" {
		q.Set("state", state)
	}
	return getAll[glMilestone](ctx, g, g.projectPath("/milestones"), q)
}

// resolveMilestone turns a milestone title into its global id, over open and closed milestones
// of the project and its groups.
func (g *GitLab) resolveMilestone(ctx context.Context, title string) (int, error) {
	all, err := g.glMilestones(ctx, "")
	if err != nil {
		return 0, err
	}
	var ids []int
	for _, m := range all {
		if m.Title == title {
			ids = append(ids, m.ID)
		}
	}
	switch len(ids) {
	case 0:
		return 0, fmt.Errorf("%w: %q", ErrMilestoneNotFound, title)
	case 1:
		return ids[0], nil
	}
	return 0, &AmbiguousMilestone{Title: title, Numbers: ids}
}

// labelList is the labels as GitLab takes them: one comma-separated string.
func labelList(labels []string) string { return strings.Join(labels, ",") }

// CreateIssue creates an issue and returns it as the forge stored it.
func (g *GitLab) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	if err := g.configured(); err != nil {
		return Issue{}, err
	}
	body := map[string]any{"title": in.Title, "description": in.Body}
	if len(in.Labels) > 0 {
		body["labels"] = labelList(in.Labels)
	}
	if in.Milestone != "" {
		id, err := g.resolveMilestone(ctx, in.Milestone)
		if err != nil {
			return Issue{}, err
		}
		body["milestone_id"] = id
	}
	var it glIssue
	if _, err := g.do(ctx, http.MethodPost, g.projectPath("/issues"), nil, body, &it); err != nil {
		return Issue{}, err
	}
	return it.issue(), nil
}

// EditIssue applies the edit in one PUT and returns the issue as it is afterwards; an edit
// that changes nothing only reads the issue.
func (g *GitLab) EditIssue(ctx context.Context, number int, e IssueEdit) (Issue, error) {
	if err := g.configured(); err != nil {
		return Issue{}, err
	}
	put := map[string]any{}
	if e.Title != nil {
		put["title"] = *e.Title
	}
	if e.Body != nil {
		put["description"] = *e.Body
	}
	if len(e.AddLabels) > 0 {
		put["add_labels"] = labelList(e.AddLabels)
	}
	if len(e.RemoveLabels) > 0 {
		put["remove_labels"] = labelList(e.RemoveLabels)
	}
	if e.Milestone != nil {
		id := 0 // 0 removes the milestone
		if *e.Milestone != "" {
			var err error
			if id, err = g.resolveMilestone(ctx, *e.Milestone); err != nil {
				return Issue{}, err
			}
		}
		put["milestone_id"] = id
	}
	if len(put) == 0 {
		return g.Issue(ctx, number)
	}
	var it glIssue
	if _, err := g.do(ctx, http.MethodPut, g.projectPath("/issues/"+strconv.Itoa(number)), nil, put, &it); err != nil {
		return Issue{}, err
	}
	return it.issue(), nil
}

// ListIssues lists the issues that match the filter, following every page. The milestone is
// matched by title on the server.
func (g *GitLab) ListIssues(ctx context.Context, f IssueFilter) ([]Issue, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	state := f.State
	switch state {
	case "", "open":
		state = "opened"
	case "closed", "all":
	default:
		return nil, fmt.Errorf("forge: issue state %q is not open, closed or all", f.State)
	}
	q := neturl.Values{"state": {state}}
	if f.Label != "" {
		q.Set("labels", f.Label)
	}
	if f.Milestone != "" {
		q.Set("milestone", f.Milestone)
	}
	items, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), q)
	if err != nil {
		return nil, err
	}
	return issues(items), nil
}

// SearchIssues finds the issues of the project, open and closed, whose title or description
// match text.
func (g *GitLab) SearchIssues(ctx context.Context, text string) ([]Issue, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	items, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), neturl.Values{"search": {text}, "in": {"title,description"}})
	if err != nil {
		return nil, err
	}
	return issues(items), nil
}

// CreateChange opens a merge request, with its milestone in the same request.
func (g *GitLab) CreateChange(ctx context.Context, in NewPR) (Change, error) {
	if err := g.configured(); err != nil {
		return Change{}, err
	}
	title := in.Title
	if in.Draft && !strings.HasPrefix(title, "Draft:") {
		title = "Draft: " + title
	}
	body := map[string]any{"title": title, "description": in.Body, "source_branch": in.Head, "target_branch": in.Base}
	if in.Milestone != "" {
		id, err := g.resolveMilestone(ctx, in.Milestone)
		if err != nil {
			return Change{}, err
		}
		body["milestone_id"] = id
	}
	var m glMR
	if _, err := g.do(ctx, http.MethodPost, g.projectPath("/merge_requests"), nil, body, &m); err != nil {
		return Change{}, err
	}
	return m.change(), nil
}

// Change reads one merge request by iid; State is open, closed or merged.
func (g *GitLab) Change(ctx context.Context, number int) (Change, error) {
	if err := g.configured(); err != nil {
		return Change{}, err
	}
	var m glMR
	if _, err := g.get(ctx, g.projectPath("/merge_requests/"+strconv.Itoa(number)), nil, &m); err != nil {
		return Change{}, err
	}
	return m.change(), nil
}

// ListChanges lists the merge requests in state open, merged, closed (closed without a merge) or
// all; "" is open. Every page is followed; a failing page is an error.
func (g *GitLab) ListChanges(ctx context.Context, state string) ([]Change, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	state, err := changeState(state)
	if err != nil {
		return nil, err
	}
	if state == "open" {
		state = "opened"
	}
	mrs, err := getAll[glMR](ctx, g, g.projectPath("/merge_requests"), neturl.Values{"state": {state}})
	if err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(mrs))
	for _, m := range mrs {
		out = append(out, m.change())
	}
	return out, nil
}

// ChangeComments lists the notes of merge request number, without system notes, over every page.
func (g *GitLab) ChangeComments(ctx context.Context, number int) ([]protocol.Comment, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	notes, err := getAll[glNote](ctx, g, g.projectPath("/merge_requests/"+strconv.Itoa(number)+"/notes"),
		neturl.Values{"sort": {"asc"}, "order_by": {"created_at"}})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Comment, 0, len(notes))
	for _, n := range notes {
		if !n.System {
			out = append(out, g.noteComment(n, "merge_requests", number, 0))
		}
	}
	return out, nil
}

// CreateChangeComment adds a note to merge request number and returns it.
func (g *GitLab) CreateChangeComment(ctx context.Context, number int, body string) (protocol.Comment, error) {
	if err := g.configured(); err != nil {
		return protocol.Comment{}, err
	}
	var n glNote
	path := g.projectPath("/merge_requests/" + strconv.Itoa(number) + "/notes")
	if _, err := g.do(ctx, http.MethodPost, path, nil, map[string]string{"body": body}, &n); err != nil {
		return protocol.Comment{}, err
	}
	return g.noteComment(n, "merge_requests", number, 0), nil
}

// Milestones lists the milestones of the project and its ancestor groups in state open, closed
// or all, following every page. Number is the global id.
func (g *GitLab) Milestones(ctx context.Context, state string) ([]Milestone, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	switch state {
	case "", "open":
		state = "active"
	case "all":
		state = ""
	case "closed":
	default:
		return nil, fmt.Errorf("forge: milestone state %q is not open, closed or all", state)
	}
	all, err := g.glMilestones(ctx, state)
	if err != nil {
		return nil, err
	}
	out := make([]Milestone, len(all))
	for i, m := range all {
		if out[i], err = g.countedMilestone(ctx, m); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MilestoneByNumber reads the milestone with global id number, of the project or a group.
func (g *GitLab) MilestoneByNumber(ctx context.Context, number int) (Milestone, error) {
	if err := g.configured(); err != nil {
		return Milestone{}, err
	}
	m, err := g.findMilestone(ctx, number)
	if err != nil {
		return Milestone{}, err
	}
	return g.countedMilestone(ctx, m)
}

// countedMilestone is m with its issue counts, read from issues_statistics by the milestone
// title, never by a numeric milestone_id: that endpoint takes the title.
func (g *GitLab) countedMilestone(ctx context.Context, m glMilestone) (Milestone, error) {
	var st struct {
		Statistics struct {
			Counts struct {
				Opened int `json:"opened"`
				Closed int `json:"closed"`
			} `json:"counts"`
		} `json:"statistics"`
	}
	if _, err := g.get(ctx, g.projectPath("/issues_statistics"), neturl.Values{"milestone": {m.Title}}, &st); err != nil {
		return Milestone{}, err
	}
	out := m.milestone()
	out.OpenIssues, out.ClosedIssues = st.Statistics.Counts.Opened, st.Statistics.Counts.Closed
	return out, nil
}

// findMilestone finds the milestone with global id number among the milestones the project sees.
func (g *GitLab) findMilestone(ctx context.Context, number int) (glMilestone, error) {
	all, err := g.glMilestones(ctx, "")
	if err != nil {
		return glMilestone{}, err
	}
	i := slices.IndexFunc(all, func(m glMilestone) bool { return m.ID == number })
	if i < 0 {
		return glMilestone{}, fmt.Errorf("%w: %d", ErrMilestoneNotFound, number)
	}
	return all[i], nil
}

// CreateMilestone creates an open milestone in the project.
func (g *GitLab) CreateMilestone(ctx context.Context, title, description string) (Milestone, error) {
	if err := g.configured(); err != nil {
		return Milestone{}, err
	}
	var m glMilestone
	body := map[string]any{"title": title, "description": description}
	if _, err := g.do(ctx, http.MethodPost, g.projectPath("/milestones"), nil, body, &m); err != nil {
		return Milestone{}, err
	}
	return m.milestone(), nil
}

// EditMilestone changes the title, description and/or state (open or closed) of a project
// milestone; a nil one stays as it is; the path takes the global id. A group milestone is refused with *GroupMilestone: the project API cannot edit it.
func (g *GitLab) EditMilestone(ctx context.Context, number int, title, description, state *string) (Milestone, error) {
	if err := g.configured(); err != nil {
		return Milestone{}, err
	}
	if err := milestoneState(state); err != nil {
		return Milestone{}, err
	}
	cur, err := g.findMilestone(ctx, number)
	if err != nil {
		return Milestone{}, err
	}
	if cur.GroupID != nil {
		return Milestone{}, &GroupMilestone{Number: number}
	}
	put := map[string]any{}
	if title != nil {
		put["title"] = *title
	}
	if description != nil {
		put["description"] = *description
	}
	if state != nil {
		put["state_event"] = map[string]string{"open": "activate", "closed": "close"}[*state]
	}
	var m glMilestone
	if _, err := g.do(ctx, http.MethodPut, g.projectPath("/milestones/"+strconv.Itoa(cur.ID)), nil, put, &m); err != nil {
		return Milestone{}, err
	}
	return m.milestone(), nil
}

// Labels lists the names of all labels of the project, following every page.
func (g *GitLab) Labels(ctx context.Context) ([]string, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	ls, err := getAll[struct {
		Name string `json:"name"`
	}](ctx, g, g.projectPath("/labels"), nil)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out, nil
}

// CreateLabel creates the label name; GitLab wants the colour with a leading "#". A 409 means the
// label exists already and is no error.
func (g *GitLab) CreateLabel(ctx context.Context, name, color string) error {
	if err := g.configured(); err != nil {
		return err
	}
	_, err := g.do(ctx, http.MethodPost, g.projectPath("/labels"), nil, map[string]any{"name": name, "color": "#" + color}, nil)
	var se *httpStatusError
	if errors.As(err, &se) && se.Code == http.StatusConflict {
		return nil
	}
	return err
}

// DeleteLabel deletes the label name; GitLab removes it from every issue too. A 404 means the
// label does not exist and is no error.
func (g *GitLab) DeleteLabel(ctx context.Context, name string) error {
	if err := g.configured(); err != nil {
		return err
	}
	_, err := g.do(ctx, http.MethodDelete, g.projectPath("/labels/"+neturl.PathEscape(name)), nil, nil, nil)
	var se *httpStatusError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil
	}
	return err
}

// CommitKnown reports whether GitLab knows the commit sha. A 404 or 422 is false, nil.
func (g *GitLab) CommitKnown(ctx context.Context, sha string) (bool, error) {
	if err := g.configured(); err != nil {
		return false, err
	}
	_, err := g.do(ctx, http.MethodGet, g.projectPath("/repository/commits/"+neturl.PathEscape(sha)), nil, nil, nil)
	return commitKnown(err)
}

// BranchContains reports whether branch holds sha: the merge base of the two is sha itself.
// A branch or sha GitLab does not know (404, or a 400 for a ref it cannot resolve) is false, nil.
func (g *GitLab) BranchContains(ctx context.Context, branch, sha string) (bool, error) {
	if err := g.configured(); err != nil {
		return false, err
	}
	q := neturl.Values{"refs[]": {sha, branch}}
	var base struct {
		ID string `json:"id"`
	}
	_, err := g.do(ctx, http.MethodGet, g.projectPath("/repository/merge_base"), q, nil, &base)
	var se *httpStatusError
	if errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusBadRequest) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return base.ID != "" && (base.ID == sha || (len(sha) >= 7 && strings.HasPrefix(base.ID, sha))), nil
}

// AddBlocker makes issue blocker block issue number: a link on number of type is_blocked_by.
// A plan without blocking links answers 403, which comes back with GitLab's message.
func (g *GitLab) AddBlocker(ctx context.Context, number, blocker int) error {
	if err := g.configured(); err != nil {
		return err
	}
	body := map[string]any{"target_project_id": g.Repo, "target_issue_iid": strconv.Itoa(blocker), "link_type": "is_blocked_by"}
	_, err := g.do(ctx, http.MethodPost, g.projectPath("/issues/"+strconv.Itoa(number)+"/links"), nil, body, nil)
	return err
}

// API makes the raw request and returns the response body as it came. Path is relative to the
// API root, so "projects/1/labels" and "/api/v4/projects/1/labels" are the same request, and
// "api/graphql" is the GraphQL endpoint of the host. An absolute URL or a path with ".." is
// refused: the token only goes to the host of BaseURL. A GET that answers a JSON list follows
// x-next-page and returns one list of all the pages.
func (g *GitLab) API(ctx context.Context, r APIRequest) ([]byte, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	method, fields, err := r.resolve()
	if err != nil {
		return nil, err
	}
	path := strings.TrimPrefix(r.Path, "/")
	if strings.Contains(path, "://") || slices.Contains(strings.Split(strings.SplitN(path, "?", 2)[0], "/"), "..") {
		return nil, fmt.Errorf("forge api: path %q must be relative to the API root", r.Path)
	}
	var url string
	if rest, ok := strings.CutPrefix(path, "api/graphql"); ok && (rest == "" || rest[0] == '/' || rest[0] == '?') {
		root, err := g.graphqlURL()
		if err != nil {
			return nil, err
		}
		url = root + rest
	} else {
		url = g.BaseURL + "/" + strings.TrimPrefix(path, "api/v4/")
	}
	var body any
	if method == http.MethodGet || method == http.MethodDelete {
		url = withQuery(url, fields)
	} else if len(fields) > 0 {
		body = fields
	}
	var out []byte
	next, err := g.send(ctx, method, url, body, &out)
	if err != nil {
		return nil, err
	}
	if method != http.MethodGet || next == "" || !isJSONList(out) {
		return out, nil
	}
	pages := [][]byte{out}
	for next != "" {
		pageURL, err := withPage(url, next)
		if err != nil {
			return nil, err
		}
		var page []byte
		if next, err = g.send(ctx, http.MethodGet, pageURL, nil, &page); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return joinLists(pages)
}

// withPage is url with its page parameter set to page. The next page is a number, never a URL,
// so the token cannot go to another host.
func withPage(url, page string) (string, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return "", fmt.Errorf("forge api: %w", err)
	}
	q := u.Query()
	q.Set("page", page)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// UpdateIssueBody replaces the description of issue number.
func (g *GitLab) UpdateIssueBody(ctx context.Context, number int, body string) error {
	return g.put(ctx, "/issues/"+strconv.Itoa(number), map[string]string{"description": body})
}

// CloseIssue posts comment on issue number when it is not empty, then closes the issue.
func (g *GitLab) CloseIssue(ctx context.Context, number int, comment string) error {
	return g.issueEventWithComment(ctx, number, comment, "close")
}

// ReopenIssue posts comment on issue number when it is not empty, then reopens the issue.
func (g *GitLab) ReopenIssue(ctx context.Context, number int, comment string) error {
	return g.issueEventWithComment(ctx, number, comment, "reopen")
}

// issueEventWithComment comments first, so a failed comment leaves the state as it was.
func (g *GitLab) issueEventWithComment(ctx context.Context, number int, comment, event string) error {
	if err := g.configured(); err != nil {
		return err
	}
	if comment != "" {
		if _, err := g.CreateComment(ctx, number, comment); err != nil {
			return err
		}
	}
	return g.put(ctx, "/issues/"+strconv.Itoa(number), map[string]string{"state_event": event})
}

// CreatePR opens a merge request. GitLab marks a draft by the "Draft: " prefix of the title.
func (g *GitLab) CreatePR(ctx context.Context, pr NewPR) (PR, error) {
	if pr.Base == "" {
		var proj struct {
			DefaultBranch string `json:"default_branch"`
		}
		if _, err := g.get(ctx, g.projectPath(""), nil, &proj); err != nil {
			return PR{}, err
		}
		if proj.DefaultBranch == "" {
			return PR{}, fmt.Errorf("forge: project %s has no default branch", g.Repo)
		}
		pr.Base = proj.DefaultBranch
	}
	title := pr.Title
	if pr.Draft && !strings.HasPrefix(title, "Draft:") {
		title = "Draft: " + title
	}
	fields := map[string]string{"title": title, "description": pr.Body, "source_branch": pr.Head, "target_branch": pr.Base}
	var out glMR
	if _, err := g.do(ctx, http.MethodPost, g.projectPath("/merge_requests"), nil, fields, &out); err != nil {
		return PR{}, err
	}
	return mrToPR(out), nil
}

// UpdatePRBody replaces the description of merge request number.
func (g *GitLab) UpdatePRBody(ctx context.Context, number int, body string) error {
	return g.put(ctx, "/merge_requests/"+strconv.Itoa(number), map[string]string{"description": body})
}

// ClosePR posts comment on merge request number when it is not empty, then closes it unmerged.
// The note goes on the merge request: its notes are not the notes of the issue with that number.
func (g *GitLab) ClosePR(ctx context.Context, number int, comment string) error {
	if err := g.configured(); err != nil {
		return err
	}
	if comment != "" {
		path := g.projectPath("/merge_requests/" + strconv.Itoa(number) + "/notes")
		if _, err := g.do(ctx, http.MethodPost, path, nil, map[string]string{"body": comment}, nil); err != nil {
			return err
		}
	}
	return g.put(ctx, "/merge_requests/"+strconv.Itoa(number), map[string]string{"state_event": "close"})
}

func (g *GitLab) put(ctx context.Context, sub string, fields map[string]string) error {
	_, err := g.do(ctx, http.MethodPut, g.projectPath(sub), nil, fields, nil)
	return err
}

// FindByMarker lists the issues and merge requests, open and closed, whose description contains
// marker. It reads every page and filters here: the search parameter of GitLab matches words,
// not a literal marker.
func (g *GitLab) FindByMarker(ctx context.Context, marker string) ([]Item, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	if marker == "" {
		return nil, errors.New("forge: FindByMarker needs a marker")
	}
	out := []Item{}
	all := neturl.Values{"state": {"all"}}
	its, err := getAll[glIssue](ctx, g, g.projectPath("/issues"), all)
	if err != nil {
		return nil, err
	}
	for _, it := range its {
		if !strings.Contains(it.Desc, marker) {
			continue
		}
		item := Item{Number: it.IID, State: "open", Body: it.Desc}
		if it.State != "opened" {
			item.State = "closed"
		}
		if it.Author != nil {
			item.Author = it.Author.Username
		}
		out = append(out, item)
	}
	mrs, err := getAll[glMR](ctx, g, g.projectPath("/merge_requests"), all)
	if err != nil {
		return nil, err
	}
	for _, m := range mrs {
		if !strings.Contains(m.Description, marker) {
			continue
		}
		item := Item{Number: m.IID, IsPR: true, State: "open", Body: m.Description, HeadRef: m.SourceBranch}
		if m.State != "opened" && m.State != "locked" {
			item.State = "closed"
		}
		if m.Author != nil {
			item.Author = m.Author.Username
		}
		out = append(out, item)
	}
	return out, nil
}

// DeleteBranch deletes branch of the project. A branch that is already gone (404) is not an error.
func (g *GitLab) DeleteBranch(ctx context.Context, branch string) error {
	if err := g.configured(); err != nil {
		return err
	}
	_, err := g.do(ctx, http.MethodDelete, g.projectPath("/repository/branches/"+neturl.PathEscape(branch)), nil, nil, nil)
	var se *httpStatusError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil
	}
	return err
}
