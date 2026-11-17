package forge

import (
	"context"
	"errors"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
)

// CreateIssue opens an issue and returns it as GitLab stored it.
func (g *GitLab) CreateIssue(ctx context.Context, title, body string) (Issue, error) {
	var out glIssue
	fields := map[string]string{"title": title, "description": body}
	if _, err := g.do(ctx, http.MethodPost, g.projectPath("/issues"), nil, fields, &out); err != nil {
		return Issue{}, err
	}
	return out.issue(), nil
}

// UpdateIssueBody replaces the description of issue number.
func (g *GitLab) UpdateIssueBody(ctx context.Context, number int, body string) error {
	return g.put(ctx, "/issues/"+strconv.Itoa(number), map[string]string{"description": body})
}

// CloseIssue posts comment on issue number when it is not empty, then closes the issue.
func (g *GitLab) CloseIssue(ctx context.Context, number int, comment string) error {
	if err := g.configured(); err != nil {
		return err
	}
	if comment != "" {
		if _, err := g.CreateComment(ctx, number, comment); err != nil {
			return err
		}
	}
	return g.put(ctx, "/issues/"+strconv.Itoa(number), map[string]string{"state_event": "close"})
}

// CreatePR opens a merge request. GitLab marks a draft by the "Draft: " prefix of the title.
func (g *GitLab) CreatePR(ctx context.Context, pr NewPR) (PR, error) {
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
