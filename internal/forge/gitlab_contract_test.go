package forge_test

import (
	"cmp"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgesuite"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

func TestGitLabConformance(t *testing.T) { forgesuite.Run(t, newGitLabFixture) }

// gitlabFixture is the GitLab client against gitlabtest.Server, which serves GitLab's shapes.
type gitlabFixture struct {
	srv *gitlabtest.Server
	f   forge.Forge
	// milestones counts the seeded milestones, for their ids.
	milestones int
	nextMR     int // numbers the merge requests of AddClosingPR
}

func newGitLabFixture(t *testing.T) forgesuite.Fixture {
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = "g/sub/p"
	srv.SetStatuses(gitlabLifecycle...)
	board := gitlabBoard
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: srv.Project, Token: "secret-token", BaseURL: srv.URL(), Board: &board})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return &gitlabFixture{srv: srv, f: f}
}

// gitlabBoard is the [forge.board] of the GitLab fixture: lighthouse names to GitLab status
// names and priority labels.
var gitlabBoard = repoconfig.Board{
	StatusOptions:   map[string]string{"To do": "To do", "Controller": "Controller", "On hold": "On hold"},
	PriorityOptions: map[string]string{"P1": "priority::P1", "P2": "priority::P2"},
}

// gitlabLifecycle is the status lifecycle of the fixture's top-level group: GitLab's defaults
// plus the two lighthouse adds.
var gitlabLifecycle = []string{"To do", "In progress", "Done", "Won't do", "Duplicate", "On hold", "Controller"}

func (g *gitlabFixture) Forge() forge.Forge { return g.f }
func (g *gitlabFixture) Repo() string       { return g.srv.Project }
func (g *gitlabFixture) Login() string      { return g.srv.Login }

// gitlabState is the state word of GitLab's REST shape for a neutral issue state.
func gitlabState(s string) string {
	if s == "open" {
		return "opened"
	}
	return s
}

// UniqueMilestoneTitles: GitLab allows the title of a group milestone on a project one too.
func (g *gitlabFixture) UniqueMilestoneTitles() bool { return false }

// AddNamedMilestone seeds a milestone whose global id is the number the client returns; the iid
// differs on purpose.
func (g *gitlabFixture) AddNamedMilestone(_ *testing.T, title string, group bool) forge.Milestone {
	g.milestones++
	id := 500 + g.milestones
	m := map[string]any{"id": id, "iid": g.milestones, "title": title, "state": "active", "description": ""}
	if group {
		m["group_id"] = 1
	} else {
		m["project_id"] = 1
	}
	g.srv.AddMilestone(m)
	return forge.Milestone{Number: id, Title: title, State: "open"}
}

func (g *gitlabFixture) AddCommit(_ *testing.T, sha string)  { g.srv.AddCommit(sha) }
func (g *gitlabFixture) FailCommit(_ *testing.T, sha string) { g.srv.FailCommit(sha) }
func (g *gitlabFixture) SetBranchRelation(_ *testing.T, branch, sha, relation string) {
	g.srv.SetMergeBase(sha, branch, relation)
}
func (g *gitlabFixture) LabelColor(_ *testing.T, name string) string {
	return g.srv.LabelColor(name)
}

func (g *gitlabFixture) SetLabels(_ *testing.T, names ...string) { g.srv.SetLabels(names...) }

func (g *gitlabFixture) AddIssue(_ *testing.T, is forge.Issue) {
	updated := time.Now().UTC()
	it := map[string]any{
		"iid": is.Number, "id": 9000 + is.Number, "title": is.Title, "description": is.Body,
		"state": gitlabState(is.State), "web_url": fmt.Sprintf("https://gitlab.example/%s/-/issues/%d", g.srv.Project, is.Number),
		"author": map[string]any{"username": is.Author}, "labels": append([]string{}, is.Labels...),
	}
	if m := is.Milestone; m != nil {
		state := m.State
		if state == "open" {
			state = "active"
		}
		// The milestone ID is the number the client must return; the iid differs on purpose.
		it["milestone"] = map[string]any{"id": m.Number, "iid": m.Number + 10, "title": m.Title, "state": state}
	}
	if !is.ClosedAt.IsZero() {
		it["closed_at"] = is.ClosedAt.UTC().Format(time.RFC3339)
		updated = is.ClosedAt
	}
	it["updated_at"] = updated.UTC().Format(time.RFC3339)
	g.srv.AddIssue(it)
}

func (g *gitlabFixture) AddComment(_ *testing.T, number int, author, body string) int64 {
	return g.srv.AddNote(number, author, body, false)
}

// accessLevels are the GitLab access levels of the neutral roles the suite uses.
var accessLevels = map[string]int{"read": 10, "triage": 20, "write": 30, "maintain": 40, "admin": 50}

func (g *gitlabFixture) SetRole(t *testing.T, user, role string) {
	t.Helper()
	level, ok := accessLevels[role]
	if !ok {
		t.Fatalf("gitlabFixture: no access level for role %q", role)
	}
	g.srv.SetMember(user, level)
}

func (g *gitlabFixture) AddAcknowledgement(_ *testing.T, number int, commentID int64, login string) {
	g.srv.AddAward(number, commentID, "rocket", login)
}

// AddBlocker seeds an is_blocked_by link whose blocker lives in b.Repo, and a merged closing
// merge request when b.MergedPR.
func (g *gitlabFixture) AddBlocker(_ *testing.T, number int, b forge.Blocker) {
	g.srv.AddLink(number, map[string]any{
		"iid": b.Issue, "state": gitlabState(strings.ToLower(b.State)), "link_type": "is_blocked_by",
		"references": map[string]any{"full": fmt.Sprintf("%s#%d", b.Repo, b.Issue)},
	})
	if b.MergedPR {
		g.srv.AddClosedBy(b.Repo, b.Issue, map[string]any{"iid": 1, "state": "merged"})
	}
}

func (g *gitlabFixture) FailBlockingPage2(_ *testing.T, number int) { g.srv.FailLinksPage2(number) }

func (g *gitlabFixture) FailListChangesPage2(*testing.T) { g.srv.FailMRsPage2() }

func (g *gitlabFixture) AddChange(_ *testing.T, c forge.Change) {
	m := map[string]any{"iid": c.Number, "title": c.Title, "state": mrState[strings.ToUpper(c.State)],
		"source_branch": c.HeadRef, "source_project_id": 1, "target_project_id": 1}
	if !c.MergedAt.IsZero() {
		m["merged_at"] = c.MergedAt.UTC().Format(time.RFC3339)
	}
	g.srv.AddMR(m)
}

// AddIssueChange: a closing merge request is in closed_by and, as on GitLab, in
// related_merge_requests too; a mentioning one only there. A fork has another source project.
func (g *gitlabFixture) AddIssueChange(_ *testing.T, number int, c forge.IssueChange) {
	source := 1
	if c.CrossRepo {
		source = 2
	}
	mr := map[string]any{"iid": c.Number, "state": mrState[c.State], "source_branch": c.HeadRef, "description": c.Body,
		"project_id": 1, "source_project_id": source, "target_project_id": 1,
		"references": map[string]any{"full": fmt.Sprintf("%s!%d", c.Repo, c.Number)}}
	if c.Closes {
		g.srv.AddClosedBy(g.srv.Project, number, mr)
	}
	g.srv.AddRelatedMR(g.srv.Project, number, mr)
}

func (g *gitlabFixture) FailIssueChangesPage2(_ *testing.T, number int) {
	g.srv.FailIssueMRsPage2(number)
}

func (g *gitlabFixture) FailComments(_ *testing.T, number int) { g.srv.FailNotes(number) }

// AddStateEvent: the source_merge_request names its project in references.full, as GitLab's
// merge request entity does.
func (g *gitlabFixture) AddStateEvent(_ *testing.T, number int, state string, closer int, repo string) {
	ev := map[string]any{"state": state, "source_merge_request": nil}
	if closer != 0 {
		ev["source_merge_request"] = map[string]any{"iid": closer,
			"references": map[string]any{"full": fmt.Sprintf("%s!%d", cmp.Or(repo, g.srv.Project), closer)}}
	}
	g.srv.AddStateEvent(number, ev)
}

func (g *gitlabFixture) AddOpenPR(_ *testing.T, pr forge.OpenPR) {
	g.srv.AddMR(map[string]any{"iid": pr.Number, "state": "opened", "title": pr.Title, "description": pr.Body,
		"source_branch": pr.HeadRef, "source_project_id": 1, "target_project_id": 1})
}

// SupportsPlanning: Planning reads REST issues, links and the closed issues per milestone.
func (g *gitlabFixture) SupportsPlanning() bool { return true }

// NotPlannedCountsAsDone: GitLab has no close reason, so every closed issue is done.
func (g *gitlabFixture) NotPlannedCountsAsDone() bool { return true }

// SetCloseReason does nothing: GitLab keeps no close reason, only the closed state.
func (g *gitlabFixture) SetCloseReason(*testing.T, int, string) {}

func (g *gitlabFixture) AddMilestone(_ *testing.T, m forge.Milestone) {
	state := m.State
	if state == "open" {
		state = "active"
	}
	g.srv.AddMilestone(map[string]any{"id": m.Number, "iid": m.Number + 10, "title": m.Title, "state": state, "description": m.Description})
}

// PRCommentBodies reads the notes of the merge request: the issue with that iid is another item.
func (g *gitlabFixture) PRCommentBodies(_ *testing.T, number int) []string {
	return g.srv.MRNotes(number)
}

// ListsClosedIssues: every issue is on the board, closed ones within the closed window.
func (g *gitlabFixture) ListsClosedIssues() bool { return true }

// pipelineStatus, mergeStatus and mrState are the GitLab words of the neutral PR values.
var (
	pipelineStatus = map[string]string{"SUCCESS": "success", "FAILURE": "failed", "PENDING": "running"}
	mergeStatus    = map[string]string{"MERGEABLE": "mergeable", "UNKNOWN": "checking"}
	mrState        = map[string]string{"OPEN": "opened", "CLOSED": "closed", "MERGED": "merged"}
)

func (g *gitlabFixture) AddBranchPR(_ *testing.T, branch string, pr forge.PR) {
	m := map[string]any{"iid": pr.Number, "state": mrState[pr.State], "source_branch": branch, "sha": pr.HeadSHA,
		"draft": pr.Draft, "has_conflicts": pr.Mergeable == "CONFLICTING", "web_url": pr.URL,
		"source_project_id": 1, "target_project_id": 1, "target_branch": pr.BaseRef}
	if pr.CrossRepo {
		m["source_project_id"] = 2
	}
	if s, ok := mergeStatus[pr.Mergeable]; ok {
		m["detailed_merge_status"] = s
	}
	if s, ok := pipelineStatus[pr.Checks]; ok {
		m["head_pipeline"] = map[string]any{"status": s}
	}
	if !pr.MergedAt.IsZero() {
		m["merged_at"] = pr.MergedAt.UTC().Format(time.RFC3339)
	}
	if !pr.ClosedAt.IsZero() {
		m["closed_at"] = pr.ClosedAt.UTC().Format(time.RFC3339)
	}
	g.srv.AddMR(m)
}

func (g *gitlabFixture) AddPullHead(_ *testing.T, h forge.PullHead) {
	m := map[string]any{"iid": h.Number, "state": mrState[h.State], "source_branch": h.HeadRef,
		"source_project_id": 1, "target_project_id": 1}
	if h.MergedAt != "" {
		m["merged_at"] = h.MergedAt
	}
	g.srv.AddMR(m)
}

func (g *gitlabFixture) AddClosingPR(_ *testing.T, number int, h forge.ClosingHead) {
	g.nextMR++
	state := "opened"
	if h.Merged {
		state = "merged"
	}
	m := map[string]any{"iid": 900 + g.nextMR, "state": state, "source_branch": h.HeadRef, "description": h.Body,
		"source_project_id": 1, "target_project_id": 1,
		"references": map[string]any{"full": fmt.Sprintf("%s!%d", h.Repo, 900+g.nextMR)}}
	if h.CrossRepo {
		m["source_project_id"] = 2
	}
	if h.Closes {
		g.srv.AddClosedBy(g.Repo(), number, m)
	} else {
		g.srv.AddRelatedMR(g.Repo(), number, m)
	}
}

// PutOnBoard gives issue number the GitLab status of status and the label of priority. Every
// issue is on the GitLab board; "" leaves the status or label unset.
func (g *gitlabFixture) PutOnBoard(t *testing.T, number int, status, priority string) {
	t.Helper()
	if status != "" {
		name, ok := gitlabBoard.StatusOptions[status]
		if !ok {
			t.Fatalf("gitlabFixture: status %q is not in the board config", status)
		}
		g.srv.SetWorkItemStatus(number, name)
	}
	if priority != "" {
		label, ok := gitlabBoard.PriorityOptions[priority]
		if !ok {
			t.Fatalf("gitlabFixture: priority %q is not in the board config", priority)
		}
		g.srv.AddLabel(number, label)
	}
}

// PutOnBoardUnmapped gives issue number a lifecycle status the board config does not map.
func (g *gitlabFixture) PutOnBoardUnmapped(_ *testing.T, number int) string {
	g.srv.SetWorkItemStatus(number, "Won't do")
	return "Won't do"
}

// StatusOf names the GitLab status of issue number by its lighthouse name, case-insensitively
// as GitLab matches it; "" without a status.
func (g *gitlabFixture) StatusOf(t *testing.T, number int) string {
	t.Helper()
	raw := g.srv.WorkItemStatus(number)
	if raw == "" {
		return ""
	}
	for name, gl := range gitlabBoard.StatusOptions {
		if strings.EqualFold(gl, raw) {
			return name
		}
	}
	t.Fatalf("gitlabFixture: status %q of #%d is not in the board config", raw, number)
	return ""
}

// SupportsReport: GitLab has no period report yet.
func (g *gitlabFixture) SupportsReport() bool { return false }

func (g *gitlabFixture) AddOpenPRWithStatus(t *testing.T, _ forge.PRSummary) {
	t.Skip("GitLab has no period report")
}
func (g *gitlabFixture) AddMergedPR(t *testing.T, _ forge.PRSummary) {
	t.Skip("GitLab has no period report")
}
func (g *gitlabFixture) AddMilestonedEvent(t *testing.T, _ int, _ string, _ time.Time) {
	t.Skip("GitLab has no period report")
}
