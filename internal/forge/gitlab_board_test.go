package forge_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// gitlabBoardOn opens the client with board on a fresh fake of project g/p whose lifecycle
// holds statuses.
func gitlabBoardOn(t *testing.T, board repoconfig.Board, statuses ...string) (*gitlabtest.Server, forge.Forge) {
	t.Helper()
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = "g/p"
	srv.SetStatuses(statuses...)
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: srv.Project, Token: "secret-token", BaseURL: srv.URL(), Board: &board})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return srv, f
}

// boardIssue is issue n in GitLab's REST shape, with the global ID 9000+n and updated at.
func boardIssue(n int, state string, updated time.Time) map[string]any {
	return map[string]any{"iid": n, "id": 9000 + n, "title": "Issue", "state": state,
		"labels": []string{}, "updated_at": updated.UTC().Format(time.RFC3339)}
}

func graphqlRequests(srv *gitlabtest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if r == "POST /api/graphql" {
			n++
		}
	}
	return n
}

func TestGitLabBoardItemsPageCount(t *testing.T) {
	srv, f := gitlabBoardOn(t, gitlabBoard, gitlabLifecycle...)
	now := time.Now()
	for n := 1; n <= 101; n++ {
		srv.AddIssue(boardIssue(n, "opened", now))
	}
	for n := 1001; n <= 1300; n++ {
		srv.AddIssue(boardIssue(n, "closed", now.Add(-60*24*time.Hour)))
	}
	items, err := f.BoardItems(t.Context())
	if err != nil {
		t.Fatalf("BoardItems: %v", err)
	}
	if len(items) != 101 {
		t.Errorf("BoardItems = %d items, want the 101 open ones", len(items))
	}
	if got := graphqlRequests(srv); got != 3 || len(srv.Requests()) != 3 {
		t.Errorf("requests = %q; want 3, all GraphQL: 2 pages open, 1 closed", srv.Requests())
	}
}

func TestGitLabBoardItemsClosedWindow(t *testing.T) {
	srv, f := gitlabBoardOn(t, gitlabBoard, gitlabLifecycle...)
	now := time.Now()
	srv.AddIssue(boardIssue(1, "closed", now.Add(-30*24*time.Hour)))
	srv.AddIssue(boardIssue(2, "closed", now.Add(-3*24*time.Hour)))
	srv.AddIssue(boardIssue(3, "opened", now.Add(-90*24*time.Hour)))
	items, err := f.BoardItems(t.Context())
	if err != nil {
		t.Fatalf("BoardItems: %v", err)
	}
	got := map[int]string{}
	for _, it := range items {
		got[it.Issue] = it.IssueState
	}
	want := map[int]string{2: "CLOSED", 3: "OPEN"}
	if len(got) != len(want) || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("BoardItems issue states = %v; want %v (30 days closed is out of the window)", got, want)
	}
}

func TestGitLabBoardItemsClosingPRs(t *testing.T) {
	srv, f := gitlabBoardOn(t, gitlabBoard, gitlabLifecycle...)
	updated := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	is := boardIssue(7, "opened", time.Now())
	is["labels"] = []string{"bug", "priority::P2"}
	srv.AddIssue(is)
	srv.SetWorkItemStatus(7, "In progress") // in the lifecycle, not in the board config
	srv.AddClosedBy("g/p", 7, map[string]any{"iid": 12, "state": "opened", "sha": "abc123", "source_branch": "issue-7-x",
		"head_pipeline": map[string]any{"status": "failed"}, "detailed_merge_status": "mergeable", "draft": true, "web_url": "https://gitlab.example/g/p/-/merge_requests/12",
		"source_project_id": 1, "target_project_id": 2})
	srv.AddClosedBy("g/p", 7, map[string]any{"iid": 13, "state": "merged", "sha": "def456", "source_branch": "issue-7-y",
		"detailed_merge_status": "conflict", "merged_at": updated.Format(time.RFC3339),
		"web_url": "https://gitlab.example/g/p/-/merge_requests/13", "source_project_id": 1, "target_project_id": 1})
	srv.AddLink(7, map[string]any{"iid": 5, "state": "closed", "link_type": "is_blocked_by",
		"references": map[string]any{"full": "o/other#5"}})
	srv.AddClosedBy("o/other", 5, map[string]any{"iid": 1, "state": "merged"})
	srv.AddLink(7, map[string]any{"iid": 6, "state": "opened", "link_type": "is_blocked_by",
		"references": map[string]any{"full": "g/p#6"}})
	srv.AddLink(7, map[string]any{"iid": 8, "state": "opened", "link_type": "relates_to",
		"references": map[string]any{"full": "g/p#8"}})

	items, err := f.BoardItems(t.Context())
	if err != nil {
		t.Fatalf("BoardItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("BoardItems = %+v; want one item", items)
	}
	it := items[0]
	if it.ItemID != "7" || it.Issue != 7 || it.Repo != "g/p" || it.IssueState != "OPEN" ||
		it.Status != "unknown:In progress" || it.Priority != "P2" || !slices.Equal(it.Labels, []string{"bug", "priority::P2"}) {
		t.Errorf("item = %+v; want ItemID 7, Repo g/p, OPEN, Status unknown:In progress, Priority P2, both labels", it)
	}
	wantPRs := []forge.PR{
		{Number: 12, State: "OPEN", HeadSHA: "abc123", HeadRef: "issue-7-x", Checks: "FAILURE", Mergeable: "MERGEABLE",
			CrossRepo: true, Closes: true, Draft: true, URL: "https://gitlab.example/g/p/-/merge_requests/12"},
		{Number: 13, State: "MERGED", HeadSHA: "def456", HeadRef: "issue-7-y", Mergeable: "CONFLICTING", Closes: true,
			MergedAt: updated, ClosedAt: updated, URL: "https://gitlab.example/g/p/-/merge_requests/13"},
	}
	if len(it.ClosingPRs) != len(wantPRs) {
		t.Fatalf("ClosingPRs = %+v; want %+v", it.ClosingPRs, wantPRs)
	}
	for i, w := range wantPRs {
		g := it.ClosingPRs[i]
		if g.Number != w.Number || g.State != w.State || g.HeadSHA != w.HeadSHA || g.HeadRef != w.HeadRef ||
			g.Checks != w.Checks || g.Mergeable != w.Mergeable || g.CrossRepo != w.CrossRepo || g.Closes != w.Closes || g.Draft != w.Draft || g.URL != w.URL ||
			!g.MergedAt.Equal(w.MergedAt) || !g.ClosedAt.Equal(w.ClosedAt) {
			t.Errorf("ClosingPRs[%d] = %+v; want %+v", i, g, w)
		}
	}
	wantBlockers := []forge.Blocker{
		{Repo: "o/other", Issue: 5, State: "CLOSED", MergedPR: true},
		{Repo: "g/p", Issue: 6, State: "OPEN"},
	}
	if !slices.Equal(it.BlockedBy, wantBlockers) {
		t.Errorf("BlockedBy = %+v; want %+v", it.BlockedBy, wantBlockers)
	}
}

func TestGitLabSetStatusMissingLifecycleStatus(t *testing.T) {
	// The top-level group never got Controller.
	srv, f := gitlabBoardOn(t, gitlabBoard, "To do", "In progress", "Done")
	srv.AddIssue(boardIssue(7, "opened", time.Now()))
	srv.SetWorkItemStatus(7, "To do")
	err := f.SetStatus(t.Context(), forge.BoardItem{ItemID: "7", Repo: "g/p", Issue: 7}, "Controller")
	if err == nil || !strings.Contains(err.Error(), `"Controller"`) || !strings.Contains(err.Error(), "Settings → Work items → Statuses") {
		t.Errorf("SetStatus(Controller) = %v; want an error naming Controller and Settings → Work items → Statuses", err)
	}
	if got := srv.WorkItemStatus(7); got != "To do" {
		t.Errorf("status = %q after the failed SetStatus, want To do", got)
	}
}

// TestGitLabStatusMapsByGitLabName pins that a lighthouse name travels as its GitLab name and
// comes back, case-insensitively, as the lighthouse name.
func TestGitLabStatusMapsByGitLabName(t *testing.T) {
	board := repoconfig.Board{StatusOptions: map[string]string{"Controller": "bot at work"}}
	srv, f := gitlabBoardOn(t, board, "To do", "Bot at work")
	srv.AddIssue(boardIssue(7, "opened", time.Now()))
	it, err := f.AddToBoard(t.Context(), 7)
	if err != nil {
		t.Fatalf("AddToBoard(7): %v", err)
	}
	if err := f.SetStatus(t.Context(), it, "Controller"); err != nil {
		t.Fatalf("SetStatus(Controller): %v", err)
	}
	if got := srv.WorkItemStatus(7); got != "Bot at work" {
		t.Errorf("GitLab status = %q, want Bot at work", got)
	}
	got, ok, err := f.IssueItem(t.Context(), 7)
	if err != nil || !ok || got.Status != "Controller" {
		t.Errorf("IssueItem(7) = %+v, %v, %v; want Status Controller", got, ok, err)
	}
}

func TestGitLabBoardNeedsBoardConfig(t *testing.T) {
	_, f := gitlabOn(t, "g/p") // no [forge.board]
	if _, err := f.BoardItems(t.Context()); err == nil || !strings.Contains(err.Error(), "[forge.board]") {
		t.Errorf("BoardItems without [forge.board] = %v; want an error naming it", err)
	}
	if err := f.SetStatus(t.Context(), forge.BoardItem{ItemID: "7"}, "To do"); err == nil {
		t.Error("SetStatus without [forge.board] = nil; want an error")
	}
}

// An issue that no longer exists is not on the board: ok is false and the tick goes on, so one
// deleted issue with an open run cannot fail dispatch.Prepare. AddToBoard still fails on it.
func TestGitLabIssueItemMissingIssueIsNotOK(t *testing.T) {
	_, f := gitlabBoardOn(t, repoconfig.Board{StatusOptions: map[string]string{"To do": "To do"}}, "To do")
	if _, ok, err := f.IssueItem(t.Context(), 99); err != nil || ok {
		t.Errorf("IssueItem(99) = ok %v, err %v; want ok false and no error", ok, err)
	}
	if _, err := f.AddToBoard(t.Context(), 99); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("AddToBoard(99) = %v; want a not found error", err)
	}
}
