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

// glRelatedMR is a merge request as closed_by and related_merge_requests list it: the glMR
// fields plus the reference that names its project.
type glRelatedMR struct {
	glMR
	References struct {
		Full string `json:"full"` // "group/project!12"
	} `json:"references"`
}

// issueChange translates a listed merge request; Repo is the project of its reference, or repo
// when the reference is missing.
func (m glRelatedMR) issueChange(repo string) IssueChange {
	p := mrToPR(m.glMR)
	if i := strings.LastIndex(m.References.Full, "!"); i > 0 {
		repo = m.References.Full[:i]
	}
	return IssueChange{Number: p.Number, State: p.State, HeadRef: p.HeadRef, CrossRepo: p.CrossRepo, Repo: repo, Body: m.Description}
}

// issueMRs lists the merge requests of list ("closed_by" or "related_merge_requests") of issue
// number, every page.
func (g *GitLab) issueMRs(ctx context.Context, number int, list string) ([]IssueChange, error) {
	mrs, err := getAll[glRelatedMR](ctx, g, g.projectPath("/issues/"+strconv.Itoa(number)+"/"+list), nil)
	if err != nil {
		return nil, err
	}
	out := make([]IssueChange, 0, len(mrs))
	for _, m := range mrs {
		out = append(out, m.issueChange(g.Repo))
	}
	return out, nil
}

// IssueChanges lists the merge requests that close issue number (closed_by) and the ones related
// to it (related_merge_requests), each once. Every page is followed; a failing page is an error.
func (g *GitLab) IssueChanges(ctx context.Context, number int) ([]IssueChange, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	closing, err := g.issueMRs(ctx, number, "closed_by")
	if err != nil {
		return nil, err
	}
	related, err := g.issueMRs(ctx, number, "related_merge_requests")
	if err != nil {
		return nil, err
	}
	return issueChanges(closing, related), nil
}

// Closer is the iid of the merge request of the last closed state event of issue number; 0 when
// that close came by hand or by a commit, or the issue was never closed.
func (g *GitLab) Closer(ctx context.Context, number int) (int, error) {
	if err := g.configured(); err != nil {
		return 0, err
	}
	events, err := getAll[struct {
		State              string `json:"state"`
		SourceMergeRequest *struct {
			IID int `json:"iid"`
		} `json:"source_merge_request"`
	}](ctx, g, g.projectPath("/issues/"+strconv.Itoa(number)+"/resource_state_events"), nil)
	if err != nil {
		return 0, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].State != "closed" {
			continue
		}
		if mr := events[i].SourceMergeRequest; mr != nil {
			return mr.IID, nil
		}
		return 0, nil
	}
	return 0, nil
}
