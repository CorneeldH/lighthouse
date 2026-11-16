package forge

import (
	"context"
	"reflect"
	"strings"
	"testing"

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
	return &repoconfig.Board{ProjectID: "PVT_x", StatusFieldID: "SF", PriorityFieldID: "PF",
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

func TestBoardItemsStatusNames(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	srv.BoardNodes = []map[string]any{
		selectNode("A", 1, map[string]string{"SF": "c1", "PF": "p1"}),
		selectNode("B", 2, nil),
		selectNode("C", 3, map[string]string{"SF": "zz", "PF": "qq"}),
	}
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type sp struct{ status, priority string }
	var gotSP []sp
	for _, it := range got {
		gotSP = append(gotSP, sp{it.Status, it.Priority})
	}
	want := []sp{{"Controller", "P1"}, {"", ""}, {"unknown:zz", ""}}
	if !reflect.DeepEqual(gotSP, want) {
		t.Errorf("status, priority = %v, want %v", gotSP, want)
	}
	if vars := srv.Posts[0].Body["variables"].(map[string]any); vars["project"] != "PVT_x" {
		t.Errorf("variables = %v, want project PVT_x", vars)
	}
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

func TestSetStatusMapsName(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	if err := c.SetStatus(context.Background(), BoardItem{ItemID: "I7", Repo: "o/r", Issue: 7}, "On hold"); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"project": "PVT_x", "item": "I7", "field": "SF", "option": "h1"}}
	if !reflect.DeepEqual(srv.StatusSets, want) {
		t.Errorf("StatusSets = %v, want %v", srv.StatusSets, want)
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

func TestAddToBoardUsesCachedNodeID(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	srv.Issues = map[int]map[string]any{7: {"number": 7, "node_id": "I_7"}}
	ctx := context.Background()
	if _, err := c.Issue(ctx, 7); err != nil {
		t.Fatal(err)
	}
	item, err := c.AddToBoard(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if want := (BoardItem{ItemID: "item-I_7", Repo: "o/r", Issue: 7}); !reflect.DeepEqual(item, want) {
		t.Errorf("AddToBoard = %+v, want %+v", item, want)
	}
	if n := srv.Count("/repos/o/r/issues/7"); n != 1 {
		t.Errorf("Count(issue 7) = %d, want 1: the node ID comes from the cache", n)
	}
}

func TestAddToBoardOpenIssuesFillCache(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	srv.Issues = map[int]map[string]any{7: {"number": 7, "state": "open", "node_id": "I_7"}}
	ctx := context.Background()
	if _, err := c.OpenIssues(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddToBoard(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if n := srv.Count("/repos/o/r/issues/7"); n != 0 {
		t.Errorf("Count(issue 7) = %d, want 0: OpenIssues filled the cache", n)
	}
}

func TestAddToBoardReadsNodeID(t *testing.T) {
	srv, c := boardClient(t)
	c.Board = testBoard()
	srv.Issues = map[int]map[string]any{7: {"number": 7, "node_id": "I_7"}}
	item, err := c.AddToBoard(context.Background(), 7)
	if err != nil || item.ItemID != "item-I_7" {
		t.Fatalf("AddToBoard = %+v, %v; want item-I_7", item, err)
	}
	if n := srv.Count("/repos/o/r/issues/7"); n != 1 {
		t.Errorf("Count(issue 7) = %d, want 1", n)
	}
	vars := srv.Posts[0].Body["variables"].(map[string]any)
	if vars["project"] != "PVT_x" || vars["content"] != "I_7" {
		t.Errorf("variables = %v, want project PVT_x and content I_7", vars)
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
