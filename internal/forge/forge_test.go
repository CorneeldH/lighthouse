package forge

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

func TestOpenGitHubDefaults(t *testing.T) {
	f, err := Open(Config{Repo: "o/r", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	gh, ok := f.(*GitHub)
	if !ok {
		t.Fatalf("Open = %T, want *GitHub", f)
	}
	if gh.BaseURL != DefaultBaseURL || gh.Repo != "o/r" || gh.Token != "t" || gh.HTTP == nil {
		t.Errorf("GitHub = %+v, want o/r, t, %s and an HTTP client", gh, DefaultBaseURL)
	}
	if k := f.Kind(); k != "github" {
		t.Errorf("Kind = %q, want github", k)
	}
	f, err = Open(Config{Kind: "github", Repo: "o/r", Token: "t", BaseURL: "http://x/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.(*GitHub).BaseURL; got != "http://x" {
		t.Errorf("BaseURL = %q, want http://x", got)
	}
}

func TestOpenGitLab(t *testing.T) {
	f, err := Open(Config{Kind: "gitlab", Repo: "g/p", Token: "t"})
	if err != nil {
		t.Fatalf("Open(gitlab) = %v, %v; want a client", f, err)
	}
	if _, ok := f.(*GitLab); !ok || f.Kind() != "gitlab" {
		t.Errorf("Open(gitlab) = %T of kind %q, want *GitLab of kind gitlab", f, f.Kind())
	}
}

func TestOpenUnknownKind(t *testing.T) {
	f, err := Open(Config{Kind: "bitbucket", Repo: "o/r", Token: "t"})
	if err == nil || !strings.Contains(err.Error(), "bitbucket") || f != nil {
		t.Fatalf("Open(bitbucket) = %v, %v; want an error naming bitbucket", f, err)
	}
}

// testBoard is a board with a Status and a Priority field and one option of each, plus On hold.
func testBoard() *repoconfig.Board {
	return &repoconfig.Board{ProjectNumber: 1, ProjectID: "PVT_x", StatusFieldID: "SF", PriorityFieldID: "PF",
		StatusOptions:   map[string]string{"Controller": "c1", "On hold": "h1"},
		PriorityOptions: map[string]string{"P1": "p1"}}
}

// selectNode is a board node of issue number with the given single-select values per field.
func selectNode(id string, number int, values map[string]string) map[string]any {
	var nodes []any
	for field, opt := range values {
		nodes = append(nodes, map[string]any{"optionId": opt, "field": map[string]any{"id": field}})
	}
	return map[string]any{"id": id, "fieldValues": map[string]any{"nodes": nodes},
		"content": map[string]any{"number": number, "repository": map[string]any{"nameWithOwner": "o/r"}}}
}

func TestIssueItemStatusName(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	srv.ProjectID = "PVT_x"
	srv.BoardNodes = []map[string]any{selectNode("PVTI_7", 7, map[string]string{"SF": "h1", "PF": "p1"})}
	got, ok, err := c.IssueItem(context.Background(), 7)
	if err != nil || !ok || got.Status != "On hold" || got.Priority != "P1" {
		t.Fatalf("IssueItem = %+v, %v, %v; want On hold, P1", got, ok, err)
	}
}

func TestBoardMethodsWithoutBoard(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(c *GitHub) error{
		"BoardItems": func(c *GitHub) error { _, err := c.BoardItems(ctx); return err },
		"IssueItem":  func(c *GitHub) error { _, _, err := c.IssueItem(ctx, 7); return err },
		"AddToBoard": func(c *GitHub) error { _, err := c.AddToBoard(ctx, 7); return err },
		"SetStatus":  func(c *GitHub) error { return c.SetStatus(ctx, BoardItem{ItemID: "I7"}, "Controller") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			srv, c := boardClient(t)
			err := call(c)
			if err == nil || !strings.Contains(err.Error(), "o/r: missing [forge.board]") {
				t.Fatalf("err = %v, want missing [forge.board]", err)
			}
			if len(srv.Posts) != 0 {
				t.Errorf("posts = %v, want none", srv.Posts)
			}
		})
	}
}

func TestSetStatusUnknownName(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	err := c.SetStatus(context.Background(), BoardItem{ItemID: "I7", Repo: "o/r", Issue: 7}, "Gone")
	if err == nil || err.Error() != `status "Gone" is not in [forge.board] status_options` {
		t.Errorf("SetStatus = %v, want an error naming Gone", err)
	}
	if len(srv.StatusSets) != 0 || len(srv.Posts) != 0 {
		t.Errorf("StatusSets = %v, want none", srv.StatusSets)
	}
}

func TestSetStatusWithoutItemID(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	err := c.SetStatus(context.Background(), BoardItem{Repo: "o/r", Issue: 7}, "Controller")
	if err == nil || !strings.Contains(err.Error(), "o/r#7") {
		t.Errorf("SetStatus = %v, want an error naming o/r#7", err)
	}
	if len(srv.Posts) != 0 {
		t.Errorf("posts = %v, want none", srv.Posts)
	}
}

// statusBoard is restBoard with a second status, and a server whose project it is.
func statusBoard(t *testing.T, nodes ...map[string]any) (*forgetest.Server, *GitHub) {
	t.Helper()
	srv, c := projectServer(t)
	c.Board = restBoard()
	c.Board.StatusOptions["In progress"] = "opt-prog"
	srv.BoardNodes = nodes
	return srv, c
}

const itemsPath = "/users/o/projectsV2/1/items"

// Class: io (REST write against the fake). At risk: the numeric item and field IDs, the option
// value, or the node ID StatusSets reports for the item.
func TestSetStatusREST(t *testing.T) {
	srv, c := statusBoard(t, restIssue("PVTI_a", 6, "o/r", nil, nil), restIssue("PVTI_b", 7, "o/r", nil, nil))
	ctx := context.Background()
	items, err := c.BoardItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetStatus(ctx, items[1], "Controller"); err != nil {
		t.Fatal(err)
	}
	// Item PVTI_b is the second node (REST id 5001); SF is field 107 in the fake's sorted list.
	want := []forgetest.Post{{Method: "PATCH", Path: itemsPath + "/5001",
		Body: map[string]any{"fields": []any{map[string]any{"id": 107.0, "value": "opt-ctrl"}}}}}
	if !reflect.DeepEqual(srv.Posts, want) {
		t.Errorf("writes = %+v, want %+v", srv.Posts, want)
	}
	wantSets := []map[string]any{{"project": "PVT_1", "item": "PVTI_b", "field": "SF", "option": "opt-ctrl"}}
	if !reflect.DeepEqual(srv.StatusSets, wantSets) {
		t.Errorf("StatusSets = %v, want %v", srv.StatusSets, wantSets)
	}
}

// Class: io. At risk: an item the client never listed gets one list read, not none (a lost
// table) and not one per call.
func TestSetStatusUnknownNodeReadsListOnce(t *testing.T) {
	srv, c := statusBoard(t, restIssue("PVTI_a", 7, "o/r", nil, nil))
	if err := c.SetStatus(context.Background(), BoardItem{ItemID: "PVTI_a"}, "Controller"); err != nil {
		t.Fatal(err)
	}
	if n := srv.Count(itemsPath); n != 1 {
		t.Errorf("list reads = %d, want 1", n)
	}
	if len(srv.Posts) != 1 || srv.Posts[0].Path != itemsPath+"/5000" {
		t.Errorf("writes = %+v, want one PATCH of 5000", srv.Posts)
	}
}

// Class: io. At risk: a node ID that is not on the board still reaches a PATCH, or its error
// loses the node ID.
func TestSetStatusMissingNodeFails(t *testing.T) {
	srv, c := statusBoard(t, restIssue("PVTI_a", 7, "o/r", nil, nil))
	err := c.SetStatus(context.Background(), BoardItem{ItemID: "PVTI_gone"}, "Controller")
	if err == nil || !strings.Contains(err.Error(), "PVTI_gone") {
		t.Fatalf("SetStatus = %v, want an error naming PVTI_gone", err)
	}
	if len(srv.Posts) != 0 {
		t.Errorf("writes = %+v, want none", srv.Posts)
	}
	if n := srv.Count(itemsPath); n != 1 {
		t.Errorf("list reads = %d, want 1", n)
	}
}

// Class: io. At risk: the table keeps an item that left the board; a failed list loses the
// table instead of leaving it.
func TestItemTableFollowsCompleteLists(t *testing.T) {
	srv, c := statusBoard(t, restIssue("PVTI_a", 6, "o/r", nil, nil), restIssue("PVTI_b", 7, "o/r", nil, nil))
	ctx := context.Background()
	if _, err := c.BoardItems(ctx); err != nil {
		t.Fatal(err)
	}
	srv.Fail = map[string]int{"GET " + itemsPath: 500}
	if _, err := c.BoardItems(ctx); err == nil {
		t.Fatal("BoardItems = nil error, want the failed list")
	}
	if err := c.SetStatus(ctx, BoardItem{ItemID: "PVTI_b"}, "Controller"); err != nil {
		t.Fatalf("SetStatus after a failed list = %v, want the old table", err)
	}
	srv.Fail = nil
	srv.BoardNodes = srv.BoardNodes[:1] // PVTI_b leaves the board
	if _, err := c.BoardItems(ctx); err != nil {
		t.Fatal(err)
	}
	err := c.SetStatus(ctx, BoardItem{ItemID: "PVTI_b"}, "Controller")
	if err == nil || !strings.Contains(err.Error(), "PVTI_b") {
		t.Errorf("SetStatus of a removed item = %v, want an error naming PVTI_b", err)
	}
	if len(srv.Posts) != 1 {
		t.Errorf("writes = %+v, want only the first PATCH", srv.Posts)
	}
}

// Class: io. At risk: AddToBoard reads the issue, sends another body, or leaves the new item
// out of the table so SetStatus reads the list.
func TestAddToBoardREST(t *testing.T) {
	srv, c := statusBoard(t, restIssue("PVTI_a", 6, "o/r", nil, nil))
	srv.Issues = map[int]map[string]any{7: {"number": 7, "node_id": "I_7"}}
	ctx := context.Background()
	item, err := c.AddToBoard(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if want := (BoardItem{ItemID: "item-I_7", Repo: "o/r", Issue: 7}); !reflect.DeepEqual(item, want) {
		t.Errorf("AddToBoard = %+v, want %+v", item, want)
	}
	want := []forgetest.Post{{Method: "POST", Path: itemsPath,
		Body: map[string]any{"type": "Issue", "owner": "o", "repo": "r", "number": 7.0}}}
	if !reflect.DeepEqual(srv.Posts, want) {
		t.Errorf("writes = %+v, want %+v", srv.Posts, want)
	}
	if n := srv.Count("/repos/o/r/issues/7"); n != 0 {
		t.Errorf("issue reads = %d, want 0", n)
	}
	if err := c.SetStatus(ctx, item, "Controller"); err != nil {
		t.Fatal(err)
	}
	if n := srv.Count(itemsPath); n != 1 {
		t.Errorf("list reads = %d, want 1: only the POST and the PATCH hit the items path", n)
	}
	if last := srv.Posts[len(srv.Posts)-1]; last.Path != itemsPath+"/5001" {
		t.Errorf("PATCH path = %s, want the id the POST answered (5001)", last.Path)
	}
}

// Class: io. At risk: the PATCH reaches the fake but IssueItem reads the old status.
func TestSetStatusThenIssueItemReadsBack(t *testing.T) {
	node := selectNode("PVTI_7", 7, map[string]string{"SF": "opt-ctrl"})
	_, c := statusBoard(t, node)
	ctx := context.Background()
	before, ok, err := c.IssueItem(ctx, 7)
	if err != nil || !ok || before.Status != "Controller" {
		t.Fatalf("IssueItem = %+v, %v, %v; want Controller", before, ok, err)
	}
	if err := c.SetStatus(ctx, before, "In progress"); err != nil {
		t.Fatal(err)
	}
	after, ok, err := c.IssueItem(ctx, 7)
	if err != nil || !ok || after.Status != "In progress" {
		t.Errorf("IssueItem = %+v, %v, %v; want In progress", after, ok, err)
	}
}

func TestAcknowledgeRoundTrip(t *testing.T) {
	srv, c := boardClient(t)
	srv.Login = "lh-bot"
	ctx := context.Background()
	if err := c.Acknowledge(ctx, 7, 40); err != nil {
		t.Fatal(err)
	}
	got, err := c.Acknowledgers(ctx, 7, 40)
	if err != nil || !reflect.DeepEqual(got, []string{"lh-bot"}) {
		t.Errorf("Acknowledgers = %v, %v; want [lh-bot]", got, err)
	}
}
