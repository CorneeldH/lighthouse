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
	"BlockedBy":                3,
	"Acknowledge":              3,
	"Pagination/Acknowledgers": 3,
	"Pagination/BlockedBy":     3,
	"PRFields":                 4,
	"Pagination/OpenPRs":       4,
	"BoardStatusAndPriority":   5,
	"IssueItem":                5,
	"AddToBoardThenSetStatus":  5,
	"SetStatusUnknownName":     5,
	"Pagination/BoardItems":    5,
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

func (g *gitlabFixture) AddAcknowledgement(t *testing.T, _ int, _ int64, _ string) {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.AddAcknowledgement: not written yet")
}

func (g *gitlabFixture) AddBlocker(t *testing.T, _ int, _ forge.Blocker) {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.AddBlocker: not written yet")
}

func (g *gitlabFixture) AddOpenPR(t *testing.T, _ forge.OpenPR) {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.AddOpenPR: not written yet")
}

func (g *gitlabFixture) AddBranchPR(t *testing.T, _ string, _ forge.PR) {
	t.Helper()
	skipIfPending(t)
	t.Fatal("gitlabFixture.AddBranchPR: not written yet")
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
