package forge_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgesuite"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

func TestGitHubConformance(t *testing.T) { forgesuite.Run(t, newGitHubFixture) }

// githubBoard is the [forge.board] of the GitHub fixture.
var githubBoard = repoconfig.Board{
	ProjectNumber:   1,
	ProjectID:       "PVT_1",
	StatusFieldID:   "F_status",
	PriorityFieldID: "F_prio",
	StatusOptions:   map[string]string{"To do": "opt-todo", "Controller": "opt-ctrl", "On hold": "opt-hold", "Done": "opt-done"},
	PriorityOptions: map[string]string{"P1": "opt-p1", "P2": "opt-p2"},
}

// githubFixture is the GitHub client against forgetest.Server, which serves GitHub's shapes.
type githubFixture struct {
	srv   *forgetest.Server
	f     forge.Forge
	board repoconfig.Board
	// nodes are the board nodes per issue number, the same maps as in srv.BoardNodes.
	nodes map[int]map[string]any
	// blockers are the blockers per issue number in the board's GraphQL shape.
	blockers map[int][]any
}

func newGitHubFixture(t *testing.T) forgesuite.Fixture {
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	srv.ProjectID = githubBoard.ProjectID
	board := githubBoard
	f, err := forge.Open(forge.Config{Repo: "o/r", Token: "secret-token", BaseURL: srv.URL(), Board: &board})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return &githubFixture{srv: srv, f: f, board: board, nodes: map[int]map[string]any{}, blockers: map[int][]any{}}
}

func (g *githubFixture) Forge() forge.Forge { return g.f }
func (g *githubFixture) Repo() string       { return "o/r" }
func (g *githubFixture) Login() string      { return g.srv.Login }

func (g *githubFixture) PRCommentBodies(t *testing.T, number int) []string {
	t.Helper()
	cs, err := g.f.Comments(t.Context(), number)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Body)
	}
	return out
}

// ListsClosedIssues: BoardItems lists only the items of the project.
func (g *githubFixture) ListsClosedIssues() bool { return false }

// UniqueMilestoneTitles: GitHub refuses a second milestone with the title of one that exists.
func (g *githubFixture) UniqueMilestoneTitles() bool { return true }

func (g *githubFixture) AddNamedMilestone(t *testing.T, title string, group bool) forge.Milestone {
	t.Helper()
	if group {
		t.Fatal("githubFixture: GitHub has no group milestones")
	}
	n := len(g.srv.Milestones) + 1
	g.srv.Milestones = append(g.srv.Milestones, map[string]any{"number": n, "title": title, "state": "open", "description": ""})
	return forge.Milestone{Number: n, Title: title, State: "open"}
}

func (g *githubFixture) AddCommit(_ *testing.T, sha string) {
	g.srv.Commits = append(g.srv.Commits, sha)
}

func (g *githubFixture) FailCommit(_ *testing.T, sha string) {
	if g.srv.Fail == nil {
		g.srv.Fail = map[string]int{}
	}
	g.srv.Fail["GET /repos/o/r/commits/"+sha] = http.StatusInternalServerError
}

func (g *githubFixture) LabelColor(_ *testing.T, name string) string { return g.srv.LabelColors[name] }

func (g *githubFixture) SetLabels(_ *testing.T, names ...string) { g.srv.Labels = names }

func (g *githubFixture) AddIssue(_ *testing.T, is forge.Issue) {
	labels := []any{}
	for _, l := range is.Labels {
		labels = append(labels, map[string]any{"name": l})
	}
	it := map[string]any{
		"number": is.Number, "id": 9000 + is.Number, "node_id": fmt.Sprintf("I_%d", is.Number), "title": is.Title, "body": is.Body,
		"state": is.State, "html_url": fmt.Sprintf("https://github.com/o/r/issues/%d", is.Number),
		"user": map[string]any{"login": is.Author}, "labels": labels,
	}
	if m := is.Milestone; m != nil {
		it["milestone"] = map[string]any{"number": m.Number, "title": m.Title, "state": m.State}
	}
	if !is.ClosedAt.IsZero() {
		it["closed_at"] = is.ClosedAt.UTC().Format(time.RFC3339)
	}
	if is.StateReason != "" {
		it["state_reason"] = is.StateReason
	}
	if g.srv.Issues == nil {
		g.srv.Issues = map[int]map[string]any{}
	}
	g.srv.Issues[is.Number] = it
}

func (g *githubFixture) SetCloseReason(t *testing.T, number int, reason string) {
	t.Helper()
	it, ok := g.srv.Issues[number]
	if !ok {
		t.Fatalf("githubFixture: SetCloseReason(%d) before AddIssue", number)
	}
	it["state_reason"] = reason
}

func (g *githubFixture) AddMilestone(_ *testing.T, m forge.Milestone) {
	g.srv.PlanMilestones = append(g.srv.PlanMilestones, map[string]any{
		"number": m.Number, "title": m.Title, "state": strings.ToUpper(m.State), "description": m.Description,
	})
}

// SupportsPlanning: Planning reads GraphQL issues and search counts.
func (g *githubFixture) SupportsPlanning() bool { return true }

// NotPlannedCountsAsDone: the search asks reason:completed.
func (g *githubFixture) NotPlannedCountsAsDone() bool { return false }

func (g *githubFixture) AddComment(_ *testing.T, number int, author, body string) int64 {
	c := g.srv.AddComment(number, author, body)
	return c["id"].(int64)
}

func (g *githubFixture) AddAcknowledgement(_ *testing.T, _ int, commentID int64, login string) {
	if g.srv.Rockets == nil {
		g.srv.Rockets = map[int64][]string{}
	}
	g.srv.Rockets[commentID] = append(g.srv.Rockets[commentID], login)
}

func (g *githubFixture) SetRole(_ *testing.T, user, role string) {
	if g.srv.Roles == nil {
		g.srv.Roles = map[string]string{}
	}
	g.srv.Roles[user] = role
}

// AddBlocker adds b to the REST dependencies of number and to the blockedBy of its board node.
func (g *githubFixture) AddBlocker(_ *testing.T, number int, b forge.Blocker) {
	if g.srv.BlockedBy == nil {
		g.srv.BlockedBy = map[int][]map[string]any{}
	}
	g.srv.BlockedBy[number] = append(g.srv.BlockedBy[number], map[string]any{
		"number": b.Issue, "state": strings.ToLower(b.State), "repository_url": g.srv.URL() + "/repos/" + b.Repo,
	})
	g.blockers[number] = append(g.blockers[number], map[string]any{
		"number": b.Issue, "state": b.State, "repository": map[string]any{"nameWithOwner": b.Repo},
	})
	if n, ok := g.nodes[number]; ok {
		n["content"].(map[string]any)["blockedBy"] = map[string]any{"nodes": g.blockers[number]}
	}
}

func (g *githubFixture) AddOpenPR(_ *testing.T, pr forge.OpenPR) {
	g.srv.Pulls = append(g.srv.Pulls, map[string]any{
		"number": pr.Number, "title": pr.Title, "body": pr.Body, "head": map[string]any{"ref": pr.HeadRef},
	})
}

func (g *githubFixture) AddOpenPRWithStatus(_ *testing.T, pr forge.PRSummary) {
	node := map[string]any{
		"number": pr.Number, "title": pr.Title, "url": pr.URL, "isDraft": pr.Draft, "mergeable": pr.Mergeable,
	}
	if pr.Checks != "" {
		node["statusCheckRollup"] = map[string]any{"state": pr.Checks}
	}
	g.srv.OpenPRNodes = append(g.srv.OpenPRNodes, node)
}

func (g *githubFixture) AddMergedPR(_ *testing.T, pr forge.PRSummary) {
	merged := pr.MergedAt.UTC().Format(time.RFC3339)
	if g.srv.MergedPRs == nil {
		g.srv.MergedPRs = map[int]map[string]any{}
	}
	g.srv.MergedPRs[pr.Number] = map[string]any{
		"number": pr.Number, "title": pr.Title, "html_url": pr.URL, "draft": pr.Draft, "state": "closed",
		"closed_at": merged, "pull_request": map[string]any{"merged_at": merged},
	}
}

// AddMilestonedEvent adds a milestoned event, which names the milestone by title only, and
// moves the issue's updated_at to it when that is later, as GitHub does.
func (g *githubFixture) AddMilestonedEvent(t *testing.T, number int, milestoneTitle string, at time.Time) {
	t.Helper()
	if g.srv.Events == nil {
		g.srv.Events = map[int][]map[string]any{}
	}
	stamp := at.UTC().Format(time.RFC3339)
	g.srv.Events[number] = append(g.srv.Events[number], map[string]any{
		"event": "milestoned", "created_at": stamp, "milestone": map[string]any{"title": milestoneTitle},
	})
	it, ok := g.srv.Issues[number]
	if !ok {
		t.Fatalf("AddMilestonedEvent: issue %d is not added", number)
	}
	if cur, _ := it["updated_at"].(string); cur < stamp {
		it["updated_at"] = stamp
	}
}

func (g *githubFixture) AddBranchPR(_ *testing.T, branch string, pr forge.PR) {
	node := map[string]any{
		"number": pr.Number, "state": pr.State, "isDraft": pr.Draft, "mergeable": pr.Mergeable,
		"headRefOid": pr.HeadSHA, "headRefName": pr.HeadRef, "baseRefName": pr.BaseRef, "isCrossRepository": pr.CrossRepo, "url": pr.URL,
		"mergedAt": timeOrNil(pr.MergedAt), "closedAt": timeOrNil(pr.ClosedAt),
	}
	if pr.Checks != "" {
		node["statusCheckRollup"] = map[string]any{"state": pr.Checks}
	}
	if g.srv.Branches == nil {
		g.srv.Branches = map[string][]map[string]any{}
	}
	g.srv.Branches[branch] = append(g.srv.Branches[branch], node)
}

// timeOrNil is t in GraphQL's DateTime shape, or null for the zero time.
func timeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func (g *githubFixture) PutOnBoard(t *testing.T, number int, status, priority string) {
	t.Helper()
	g.putOnBoard(t, number, g.option(t, g.board.StatusOptions, status), g.option(t, g.board.PriorityOptions, priority))
}

func (g *githubFixture) PutOnBoardUnmapped(t *testing.T, number int) string {
	t.Helper()
	g.putOnBoard(t, number, "opt-unmapped", "")
	return "In review"
}

// option is the option ID of name in options; "" for "".
func (g *githubFixture) option(t *testing.T, options map[string]string, name string) string {
	t.Helper()
	if name == "" {
		return ""
	}
	id, ok := options[name]
	if !ok {
		t.Fatalf("githubFixture: board maps no option %q", name)
	}
	return id
}

// optionName is the name the board gives option id: the name status_options or
// priority_options maps to it, and "In review" for opt-unmapped, which neither lists.
func (g *githubFixture) optionName(id string) string {
	for _, options := range []map[string]string{g.board.StatusOptions, g.board.PriorityOptions} {
		for name, oid := range options {
			if oid == id {
				return name
			}
		}
	}
	return "In review"
}

// putOnBoard adds the board node "item-<number>" of an issue added before, with the single-select
// values status and priority (option IDs, "" none) and the blockers added so far.
func (g *githubFixture) putOnBoard(t *testing.T, number int, status, priority string) {
	t.Helper()
	is, ok := g.srv.Issues[number]
	if !ok {
		t.Fatalf("githubFixture: PutOnBoard(%d) before AddIssue", number)
	}
	values := []any{}
	for field, opt := range map[string]string{g.board.StatusFieldID: status, g.board.PriorityFieldID: priority} {
		if opt != "" {
			values = append(values, map[string]any{"optionId": opt, "name": g.optionName(opt), "field": map[string]any{"id": field}})
		}
	}
	node := map[string]any{
		"id": fmt.Sprintf("item-%d", number), "updatedAt": "2026-11-01T09:00:00Z",
		"fieldValues": map[string]any{"nodes": values},
		"content": map[string]any{
			"number": number, "title": is["title"], "state": strings.ToUpper(is["state"].(string)),
			"updatedAt": "2026-11-01T09:00:00Z", "repository": map[string]any{"nameWithOwner": "o/r"},
			"blockedBy": map[string]any{"nodes": g.blockers[number]},
		},
	}
	g.nodes[number] = node
	g.srv.BoardNodes = append(g.srv.BoardNodes, node)
}

// StatusOf reads the last SetStatus on the item of number, "item-<n>" from PutOnBoard or
// "item-I_<n>" from AddToBoard, and names its option.
func (g *githubFixture) StatusOf(t *testing.T, number int) string {
	t.Helper()
	items := []string{fmt.Sprintf("item-%d", number), fmt.Sprintf("item-I_%d", number)}
	for i := len(g.srv.StatusSets) - 1; i >= 0; i-- {
		set := g.srv.StatusSets[i]
		if set["item"] != items[0] && set["item"] != items[1] {
			continue
		}
		if set["field"] != g.board.StatusFieldID {
			t.Fatalf("githubFixture: SetStatus on field %v, want %s", set["field"], g.board.StatusFieldID)
		}
		for name, id := range g.board.StatusOptions {
			if id == set["option"] {
				return name
			}
		}
		t.Fatalf("githubFixture: SetStatus to option %v, which the board does not map", set["option"])
	}
	return ""
}

// SupportsReport: GitHub reads the period lists.
func (g *githubFixture) SupportsReport() bool { return true }
