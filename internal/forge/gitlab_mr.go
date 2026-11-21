package forge

import (
	"context"
	"errors"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// glMR is a merge request in GitLab's REST shape.
type glMR struct {
	IID                 int          `json:"iid"`
	Title               string       `json:"title"`
	Description         string       `json:"description"`
	State               string       `json:"state"`
	Draft               bool         `json:"draft"`
	SourceBranch        string       `json:"source_branch"`
	SHA                 string       `json:"sha"`
	WebURL              string       `json:"web_url"`
	SourceProjectID     int64        `json:"source_project_id"`
	TargetProjectID     int64        `json:"target_project_id"`
	HasConflicts        bool         `json:"has_conflicts"`
	DetailedMergeStatus string       `json:"detailed_merge_status"`
	CreatedAt           time.Time    `json:"created_at"`
	MergedAt            *time.Time   `json:"merged_at"`
	ClosedAt            *time.Time   `json:"closed_at"`
	TargetBranch        string       `json:"target_branch"`
	Labels              []string     `json:"labels"`
	Milestone           *glMilestone `json:"milestone"`
	Author              *struct {
		Username string `json:"username"`
	} `json:"author"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
}

// pipelineChecks translates the status of a head pipeline to the check states of a PR.
var pipelineChecks = map[string]string{
	"success": "SUCCESS", "skipped": "SUCCESS",
	"failed": "FAILURE", "canceled": "FAILURE",
	"running": "PENDING", "pending": "PENDING", "created": "PENDING", "preparing": "PENDING",
	"waiting_for_resource": "PENDING", "scheduled": "PENDING", "manual": "PENDING",
}

// mrToPR translates a merge request to a PR by the table of the forge spec. Closes stays
// false: only the board knows which issue a merge request closes (closingMergeRequests).
func mrToPR(m glMR) PR {
	p := PR{Number: m.IID, Draft: m.Draft, HeadSHA: m.SHA, HeadRef: m.SourceBranch, BaseRef: m.TargetBranch, URL: m.WebURL,
		CrossRepo: m.SourceProjectID != m.TargetProjectID}
	switch m.State {
	case "merged":
		p.State = "MERGED"
	case "closed":
		p.State = "CLOSED"
	default: // opened, and locked while it merges
		p.State = "OPEN"
	}
	if m.HeadPipeline != nil {
		p.Checks = pipelineChecks[m.HeadPipeline.Status]
	}
	// Mergeable only says "no conflicts", as on GitHub: approvals, threads and a running
	// pipeline do not count.
	switch {
	case m.HasConflicts:
		p.Mergeable = "CONFLICTING"
	case m.DetailedMergeStatus == "unchecked" || m.DetailedMergeStatus == "checking" || m.DetailedMergeStatus == "preparing":
		p.Mergeable = "UNKNOWN"
	default:
		p.Mergeable = "MERGEABLE"
	}
	if m.MergedAt != nil {
		p.MergedAt = *m.MergedAt
		p.ClosedAt = *m.MergedAt // a merged PR has a ClosedAt too, as on GitHub; GitLab sends none
	}
	if m.ClosedAt != nil {
		p.ClosedAt = *m.ClosedAt
	}
	return p
}

// prs reads the merge requests of the project that match q, following every page.
func (g *GitLab) prs(ctx context.Context, q neturl.Values) ([]glMR, error) {
	return getAll[glMR](ctx, g, g.projectPath("/merge_requests"), q)
}

// OpenPRs lists all open merge requests of the project, following every page.
func (g *GitLab) OpenPRs(ctx context.Context) ([]OpenPR, error) {
	mrs, err := g.prs(ctx, neturl.Values{"state": {"opened"}})
	if err != nil {
		return nil, err
	}
	out := make([]OpenPR, 0, len(mrs))
	for _, m := range mrs {
		out = append(out, OpenPR{Number: m.IID, Title: m.Title, Body: m.Description, HeadRef: m.SourceBranch})
	}
	return out, nil
}

// BranchPRs lists the merge requests whose source branch is branch, open, closed and merged.
// Closes is false on each, as on GitHub: the branch says nothing about which issue a merge
// request closes.
//
// GitLab's merge request list carries no head_pipeline; only the single merge request does.
// So each open merge request costs one more call, GET …/merge_requests/:iid, for its Checks.
// A closed or merged one is not read again and has Checks "": the dispatcher reads the checks
// of an open PR only.
func (g *GitLab) BranchPRs(ctx context.Context, branch string) ([]PR, error) {
	mrs, err := g.prs(ctx, neturl.Values{"source_branch": {branch}, "state": {"all"}})
	if err != nil {
		return nil, err
	}
	out := make([]PR, 0, len(mrs))
	for _, m := range mrs {
		if m.State == "opened" || m.State == "locked" {
			var detail glMR
			if _, err := g.get(ctx, g.projectPath("/merge_requests/"+strconv.Itoa(m.IID)), nil, &detail); err != nil {
				return nil, err
			}
			m.HeadPipeline = detail.HeadPipeline
		}
		out = append(out, mrToPR(m))
	}
	return out, nil
}

// glState is the PR.State of a merge request state.
func glState(state string) string {
	switch state {
	case "merged":
		return "MERGED"
	case "closed":
		return "CLOSED"
	}
	return "OPEN" // opened, and locked while it merges
}

// PullHeads lists every merge request of the project in any state, following every page.
func (g *GitLab) PullHeads(ctx context.Context) ([]PullHead, error) {
	mrs, err := g.prs(ctx, neturl.Values{"state": {"all"}})
	if err != nil {
		return nil, err
	}
	out := make([]PullHead, 0, len(mrs))
	for _, m := range mrs {
		h := PullHead{Number: m.IID, State: glState(m.State), HeadRef: m.SourceBranch}
		if m.MergedAt != nil {
			h.MergedAt = rfc3339(*m.MergedAt)
		}
		out = append(out, h)
	}
	return out, nil
}

// IssueClosingHeads lists the merge requests that close issue number (closed_by) and the ones
// that only refer to it (related_merge_requests). A merge request in both counts once, as
// closing. Repo is read from its full reference, group/project!iid.
func (g *GitLab) IssueClosingHeads(ctx context.Context, number int) ([]ClosingHead, error) {
	type mr struct {
		glMR
		References struct {
			Full string `json:"full"`
		} `json:"references"`
	}
	type key struct {
		repo string
		iid  int
	}
	seen := map[key]bool{}
	out := []ClosingHead{}
	for _, src := range []struct {
		path   string
		closes bool
	}{{"closed_by", true}, {"related_merge_requests", false}} {
		mrs, err := getAll[mr](ctx, g, g.projectPath("/issues/"+strconv.Itoa(number)+"/"+src.path), nil)
		if err != nil {
			return nil, err
		}
		for _, m := range mrs {
			repo, _, _ := strings.Cut(m.References.Full, "!")
			if seen[key{repo, m.IID}] {
				continue
			}
			seen[key{repo, m.IID}] = true
			out = append(out, ClosingHead{HeadRef: m.SourceBranch, Repo: repo, Body: m.Description,
				Merged: m.State == "merged", Closes: src.closes, CrossRepo: m.SourceProjectID != m.TargetProjectID})
		}
	}
	return out, nil
}

// MergePR squash-merges merge request number, only when its head is still sha. GitLab answers
// 409 for a head that moved, and 405 or 406 for a merge it will not do.
func (g *GitLab) MergePR(ctx context.Context, number int, sha string) error {
	_, err := g.do(ctx, http.MethodPut, g.projectPath("/merge_requests/"+strconv.Itoa(number)+"/merge"), nil,
		map[string]any{"sha": sha, "squash": true}, nil)
	var se *httpStatusError
	if err == nil || !errors.As(err, &se) {
		return err
	}
	switch se.Code {
	case http.StatusConflict:
		return ErrHeadMoved
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable:
		return &MergeRefused{Message: se.Message}
	}
	return err
}
