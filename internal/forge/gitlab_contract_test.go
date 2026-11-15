package forge_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgesuite"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

func TestGitLabConformance(t *testing.T) { forgesuite.Run(t, newGitLabFixture) }

// conformance is the name of the test that runs the suite; pending is keyed below it.
const conformance = "TestGitLabConformance"

// pending maps a suite case, or one subtest of Pagination, to the task that implements the
// methods it calls. Such a case skips until then, so no case runs on a stub. Each task removes
// its names.
var pending = map[string]int{
	"BoardStatusAndPriority":  5,
	"IssueItem":               5,
	"AddToBoardThenSetStatus": 5,
	"SetStatusUnknownName":    5,
	"Pagination/BoardItems":   5,
}

// skipIfPending skips t when its name, or the case it belongs to, is in pending.
func skipIfPending(t *testing.T) {
	t.Helper()
	name := strings.TrimPrefix(t.Name(), conformance+"/")
	for {
		if task, ok := pending[name]; ok {
			t.Skipf("taak %d", task)
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return
		}
		name = name[:i]
	}
}

// gitlabFixture is the GitLab client against gitlabtest.Server, which serves GitLab's shapes.
type gitlabFixture struct {
	srv *gitlabtest.Server
	f   forge.Forge
}

func newGitLabFixture(t *testing.T) forgesuite.Fixture {
	skipIfPending(t)
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = "g/sub/p"
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: srv.Project, Token: "secret-token", BaseURL: srv.URL()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return &gitlabFixture{srv: srv, f: f}
}

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

func (g *gitlabFixture) AddOpenPR(_ *testing.T, pr forge.OpenPR) {
	g.srv.AddMR(map[string]any{"iid": pr.Number, "state": "opened", "title": pr.Title, "description": pr.Body,
		"source_branch": pr.HeadRef, "source_project_id": 1, "target_project_id": 1})
}

// pipelineStatus, mergeStatus and mrState are the GitLab words of the neutral PR values.
var (
	pipelineStatus = map[string]string{"SUCCESS": "success", "FAILURE": "failed", "PENDING": "running"}
	mergeStatus    = map[string]string{"MERGEABLE": "mergeable", "UNKNOWN": "checking"}
	mrState        = map[string]string{"OPEN": "opened", "CLOSED": "closed", "MERGED": "merged"}
)

func (g *gitlabFixture) AddBranchPR(_ *testing.T, branch string, pr forge.PR) {
	m := map[string]any{"iid": pr.Number, "state": mrState[pr.State], "source_branch": branch, "sha": pr.HeadSHA,
		"draft": pr.Draft, "has_conflicts": pr.Mergeable == "CONFLICTING", "web_url": pr.URL,
		"source_project_id": 1, "target_project_id": 1}
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

func (g *gitlabFixture) PutOnBoard(t *testing.T, _ int, _, _ string) {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.PutOnBoard: not written yet")
}

func (g *gitlabFixture) PutOnBoardUnmapped(t *testing.T, _ int) string {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.PutOnBoardUnmapped: not written yet")
	return ""
}

func (g *gitlabFixture) StatusOf(t *testing.T, _ int) string {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.StatusOf: not written yet")
	return ""
}
