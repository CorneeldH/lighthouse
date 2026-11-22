package forge

import (
	"context"
	"errors"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
)

// awardPath is the award_emoji path of one note.
func (g *GitLab) awardPath(number int, commentID int64) string {
	return g.projectPath("/issues/" + strconv.Itoa(number) + "/notes/" + strconv.FormatInt(commentID, 10) + "/award_emoji")
}

// Acknowledge puts a rocket on one comment. A rocket that is already there is no error: GitLab
// answers 404 (and some versions 409) for an award the token's user gave before.
func (g *GitLab) Acknowledge(ctx context.Context, number int, commentID int64) error {
	_, err := g.do(ctx, http.MethodPost, g.awardPath(number, commentID), neturl.Values{"name": {"rocket"}}, nil, nil)
	var se *httpStatusError
	if errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusConflict) {
		return nil
	}
	return err
}

// Acknowledgers lists the usernames that put a rocket on one comment, following every page.
func (g *GitLab) Acknowledgers(ctx context.Context, number int, commentID int64) ([]string, error) {
	awards, err := getAll[glAward](ctx, g, g.awardPath(number, commentID), nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range awards {
		if a.Name == "rocket" && a.User != nil {
			out = append(out, a.User.Username)
		}
	}
	return out, nil
}

// glAward is an award emoji in GitLab's REST shape.
type glAward struct {
	Name string `json:"name"`
	User *struct {
		Username string `json:"username"`
	} `json:"user"`
}

// glLink is an issue link as GET …/issues/:iid/links returns it: the linked issue, with the
// link type from the point of view of the issue asked about.
type glLink struct {
	IID        int    `json:"iid"`
	State      string `json:"state"`
	LinkType   string `json:"link_type"`
	References struct {
		Full string `json:"full"` // "group/project#iid"
	} `json:"references"`
}

// BlockedBy lists the issues that block issue number: the links of type is_blocked_by, also
// those into other projects. A closed blocker costs one more call, to the merge requests that
// close it, to set MergedPR; an open one costs none. That is the only call for a blocker, so
// a read is bounded by the closed blockers of that one issue.
func (g *GitLab) BlockedBy(ctx context.Context, number int) ([]Blocker, error) {
	links, err := getAll[glLink](ctx, g, g.projectPath("/issues/"+strconv.Itoa(number)+"/links"), nil)
	if err != nil {
		return nil, err
	}
	var out []Blocker
	for _, l := range links {
		if l.LinkType != "is_blocked_by" {
			continue
		}
		repo, _, _ := strings.Cut(l.References.Full, "#")
		b := Blocker{Repo: repo, Issue: l.IID, State: "OPEN"}
		if l.State == "closed" {
			b.State = "CLOSED"
			if b.MergedPR, err = g.closedByMerged(ctx, repo, l.IID); err != nil {
				return nil, err
			}
		}
		out = append(out, b)
	}
	return out, nil
}

// Blocking lists the issues that issue number blocks: the links of type blocks, also those into
// other projects, with the state of the linked issue. MergedPR stays false.
func (g *GitLab) Blocking(ctx context.Context, number int) ([]Blocker, error) {
	links, err := getAll[glLink](ctx, g, g.projectPath("/issues/"+strconv.Itoa(number)+"/links"), nil)
	if err != nil {
		return nil, err
	}
	var out []Blocker
	for _, l := range links {
		if l.LinkType != "blocks" {
			continue
		}
		repo, _, _ := strings.Cut(l.References.Full, "#")
		b := Blocker{Repo: repo, Issue: l.IID, State: "OPEN"}
		if l.State == "closed" {
			b.State = "CLOSED"
		}
		out = append(out, b)
	}
	return out, nil
}

// closedByMerged reports whether a merge request that closes issue iid of project is merged.
func (g *GitLab) closedByMerged(ctx context.Context, project string, iid int) (bool, error) {
	mrs, err := getAll[struct {
		State string `json:"state"`
	}](ctx, g, "/projects/"+neturl.PathEscape(project)+"/issues/"+strconv.Itoa(iid)+"/closed_by", nil)
	if err != nil {
		return false, err
	}
	for _, mr := range mrs {
		if mr.State == "merged" {
			return true, nil
		}
	}
	return false, nil
}
