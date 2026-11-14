package forge

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

// boardClient returns a forgetest server and a client pointed at it.
func boardClient(t *testing.T) (*forgetest.Server, *Client) {
	t.Helper()
	srv := forgetest.New(t)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return srv, c
}

func TestBoardItemsFollowsPages(t *testing.T) {
	srv, c := boardClient(t)
	for i := range 101 {
		srv.BoardNodes = append(srv.BoardNodes, map[string]any{"id": fmt.Sprintf("I%d", i), "content": map[string]any{}})
	}
	got, err := c.BoardItems(context.Background(), "PVT_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 || got[0].ItemID != "I0" || got[100].ItemID != "I100" {
		t.Fatalf("got %d items; want 101 from I0 to I100", len(got))
	}
	if n := srv.Count("Board"); n != 2 {
		t.Fatalf("Count(Board) = %d, want 2", n)
	}
	first := srv.Posts[0].Body["variables"].(map[string]any)
	second := srv.Posts[1].Body["variables"].(map[string]any)
	if first["project"] != "PVT_1" || first["after"] != nil {
		t.Errorf("first variables = %v, want project PVT_1 and after null", first)
	}
	if second["project"] != "PVT_1" || second["after"] != "cursor-100" {
		t.Errorf("second variables = %v, want after = cursor-100 (endCursor of page 1)", second)
	}
}

func TestBoardItemsStuckCursorFails(t *testing.T) {
	// hasNextPage with an endCursor that is empty or the one just asked would loop for ever
	// while tick holds tick.lock.
	for name, tc := range map[string]struct {
		cursor string
		boards int // Board requests before the error
	}{
		"empty":     {"", 1},
		"unchanged": {"cursor-0", 2},
	} {
		t.Run(name, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.BoardNodes = []map[string]any{{"id": "I0", "content": map[string]any{}}}
			srv.BoardStuckCursor = &tc.cursor
			got, err := c.BoardItems(context.Background(), "PVT_1")
			if err == nil || !strings.Contains(err.Error(), "board PVT_1: pagination stuck") || got != nil {
				t.Fatalf("BoardItems = %v, %v; want a pagination-stuck error and nil items", got, err)
			}
			if n := srv.Count("Board"); n != tc.boards {
				t.Errorf("Count(Board) = %d, want %d", n, tc.boards)
			}
		})
	}
}

func TestBoardItemsMapsFields(t *testing.T) {
	srv, c := boardClient(t)
	srv.BoardNodes = []map[string]any{{
		"id":        "PVTI_1",
		"updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{
			map[string]any{"optionId": "opt-ctrl", "field": map[string]any{"id": "F_status"}},
			map[string]any{"optionId": "opt-p1", "field": map[string]any{"id": "F_prio"}},
			map[string]any{}, // a text field: no single-select value
		}},
		"content": map[string]any{
			"number": 7, "title": "Iets doen", "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
			"repository": map[string]any{"nameWithOwner": "o/r"},
			"labels":     map[string]any{"nodes": []any{map[string]any{"name": "1-loc:mac"}}},
			"blockedBy": map[string]any{"nodes": []any{map[string]any{
				"number": 5, "state": "CLOSED", "repository": map[string]any{"nameWithOwner": "o/x"},
				"closedByPullRequestsReferences": map[string]any{"nodes": []any{
					// the merged PR in the middle: neither the first nor the last decides
					map[string]any{"merged": false}, map[string]any{"merged": true}, map[string]any{"merged": false},
				}},
			}}},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{
				map[string]any{"number": 12, "state": "OPEN", "isDraft": true, "mergeable": "UNKNOWN",
					"headRefOid": "abc", "headRefName": "issue-7-x", "statusCheckRollup": nil},
				map[string]any{"number": 13, "state": "MERGED", "isDraft": false, "mergeable": "CONFLICTING",
					"headRefOid": "def", "headRefName": "issue-7-y", "isCrossRepository": true,
					"statusCheckRollup": map[string]any{"state": "SUCCESS"}},
			}},
		},
	}}
	got, err := c.BoardItems(context.Background(), "PVT_1")
	if err != nil {
		t.Fatal(err)
	}
	want := []BoardItem{{
		ItemID:         "PVTI_1",
		ItemUpdatedAt:  time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC),
		SingleSelect:   map[string]string{"F_status": "opt-ctrl", "F_prio": "opt-p1"},
		Repo:           "o/r",
		Issue:          7,
		Title:          "Iets doen",
		IssueState:     "OPEN",
		IssueUpdatedAt: time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC),
		Labels:         []string{"1-loc:mac"},
		BlockedBy:      []Blocker{{Repo: "o/x", Issue: 5, State: "CLOSED", MergedPR: true}},
		ClosingPRs: []PR{
			{Number: 12, State: "OPEN", Draft: true, Mergeable: "UNKNOWN", HeadSHA: "abc", HeadRef: "issue-7-x", Checks: "", Closes: true},
			{Number: 13, State: "MERGED", Draft: false, Mergeable: "CONFLICTING", HeadSHA: "def", HeadRef: "issue-7-y", CrossRepo: true, Checks: "SUCCESS", Closes: true},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestBoardItemsNonIssueContent(t *testing.T) {
	srv, c := boardClient(t)
	srv.BoardNodes = []map[string]any{
		{"id": "draft", "updatedAt": "2026-10-30T09:00:00Z", "content": map[string]any{}},
		{"id": "hidden", "updatedAt": "2026-10-30T09:00:00Z", "content": nil},
	}
	got, err := c.BoardItems(context.Background(), "PVT_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	for _, it := range got {
		if it.Repo != "" || it.Issue != 0 {
			t.Errorf("item %s: Repo %q, Issue %d; want empty", it.ItemID, it.Repo, it.Issue)
		}
	}
}

func TestBoardItemsTruncatedNestedFails(t *testing.T) {
	more := map[string]any{"hasNextPage": true}
	cases := []struct {
		name     string
		truncate func(node, content, blocker map[string]any)
		want     string
	}{
		{"fieldValues", func(n, _, _ map[string]any) {
			n["fieldValues"].(map[string]any)["pageInfo"] = more
		}, "board item PVTI_1: fieldValues truncated at 1"},
		{"blockedBy", func(_, c, _ map[string]any) {
			c["blockedBy"].(map[string]any)["pageInfo"] = more
		}, "board item PVTI_1: blockedBy truncated at 1"},
		{"closing PRs of a blocker", func(_, _, b map[string]any) {
			b["closedByPullRequestsReferences"].(map[string]any)["pageInfo"] = more
		}, "board item PVTI_1: blocker o/r#5 closedByPullRequestsReferences truncated at 1"},
		{"labels", func(_, c, _ map[string]any) {
			c["labels"] = map[string]any{"pageInfo": more, "nodes": []any{map[string]any{"name": "a"}}}
		}, "board item PVTI_1: labels truncated at 1"},
		{"closing PRs of the issue", func(_, c, _ map[string]any) {
			c["closedByPullRequestsReferences"].(map[string]any)["pageInfo"] = more
		}, "board item PVTI_1: closedByPullRequestsReferences truncated at 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocker := map[string]any{
				"number": 5, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"},
				"closedByPullRequestsReferences": map[string]any{"nodes": []any{map[string]any{"merged": false}}},
			}
			content := map[string]any{
				"number": 7, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"},
				"blockedBy":                      map[string]any{"nodes": []any{blocker}},
				"closedByPullRequestsReferences": map[string]any{"nodes": []any{map[string]any{"number": 12}}},
			}
			node := map[string]any{
				"id":          "PVTI_1",
				"fieldValues": map[string]any{"nodes": []any{map[string]any{"optionId": "o", "field": map[string]any{"id": "f"}}}},
				"content":     content,
			}
			tc.truncate(node, content, blocker)
			srv, c := boardClient(t)
			srv.BoardNodes = []map[string]any{node}
			got, err := c.BoardItems(context.Background(), "PVT_1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if got != nil {
				t.Fatalf("items = %+v, want nil", got)
			}
		})
	}
}

func TestBoardQueryKeepsBlockerNestingCheap(t *testing.T) {
	// blockedBy × its closing PRs priced a page at 53 GraphQL points on a real board at
	// first: 50 × 10; the precheck reads the board every tick. A longer list stays a
	// truncation error (TestBoardItemsTruncatedNestedFails).
	for _, want := range []string{
		"blockedBy(first: 10) {",
		"repository { nameWithOwner }\n                  closedByPullRequestsReferences(first: 5, includeClosedPrs: true) {",
	} {
		if !strings.Contains(BoardQuery, want) {
			t.Errorf("BoardQuery lacks %q", want)
		}
	}
}

func TestBoardItemsNotAProjectFails(t *testing.T) {
	// A project ID that names another kind of node gives {"node": {}} and no errors on GitHub;
	// that must not read as an empty board.
	srv, c := boardClient(t)
	srv.BoardNotAProject = true
	got, err := c.BoardItems(context.Background(), "I_issue")
	if err == nil || !strings.Contains(err.Error(), "board I_issue: not a ProjectV2") || got != nil {
		t.Fatalf("BoardItems = %v, %v; want a not-a-ProjectV2 error and nil items", got, err)
	}
}

func TestGraphQLErrorsOn200(t *testing.T) {
	const msg = "Could not resolve to a node with the global id of 'PVT_1'"
	calls := []struct {
		name string
		call func(c *Client) (empty bool, err error)
	}{
		{"Board", func(c *Client) (bool, error) {
			items, err := c.BoardItems(context.Background(), "PVT_1")
			return items == nil, err
		}},
		{"BranchPRs", func(c *Client) (bool, error) {
			prs, err := c.BranchPRs(context.Background(), "issue-1-x")
			return prs == nil, err
		}},
		{"SetStatus", func(c *Client) (bool, error) {
			return true, c.SetStatus(context.Background(), "PVT_1", "I", "F", "O")
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.GraphQLErrors = []string{msg}
			srv.BoardNodes = []map[string]any{{"id": "PVTI_1"}}
			srv.Branches = map[string][]map[string]any{"issue-1-x": {{"number": 1}}}
			empty, err := tc.call(c)
			if err == nil || !strings.Contains(err.Error(), "Could not resolve to a node") {
				t.Fatalf("err = %v, want the GraphQL message", err)
			}
			if !empty {
				t.Fatal("result is not nil, want nil")
			}
		})
	}
}

func TestBranchPRs(t *testing.T) {
	srv, c := boardClient(t)
	srv.Branches = map[string][]map[string]any{"issue-1-x": {{
		"number": 11, "state": "OPEN", "isDraft": false, "mergeable": "MERGEABLE",
		"headRefOid": "abc", "headRefName": "issue-1-x", "statusCheckRollup": map[string]any{"state": "PENDING"},
	}}}
	got, err := c.BranchPRs(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	want := []PR{{Number: 11, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "abc", HeadRef: "issue-1-x", Checks: "PENDING", Closes: false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	vars := srv.Posts[0].Body["variables"]
	if wantVars := map[string]any{"owner": "o", "name": "r", "branch": "issue-1-x"}; !reflect.DeepEqual(vars, wantVars) {
		t.Fatalf("variables = %v, want %v", vars, wantVars)
	}
}

func TestBranchPRsTruncatedFails(t *testing.T) {
	srv, c := boardClient(t)
	nodes := make([]map[string]any, 21) // forgetest serves first: 20, like GitHub
	for i := range nodes {
		nodes[i] = map[string]any{"number": i + 1, "state": "CLOSED"}
	}
	srv.Branches = map[string][]map[string]any{"issue-1-x": nodes}
	got, err := c.BranchPRs(context.Background(), "issue-1-x")
	if err == nil || !strings.Contains(err.Error(), "branch issue-1-x: pullRequests truncated at 20") {
		t.Fatalf("err = %v, want truncated at 20", err)
	}
	if got != nil {
		t.Fatalf("PRs = %+v, want nil", got)
	}
}

func TestSetStatusPosts(t *testing.T) {
	srv, c := boardClient(t)
	if err := c.SetStatus(context.Background(), "PVT_1", "PVTI_1", "F_status", "opt-ctrl"); err != nil {
		t.Fatal(err)
	}
	if len(srv.Posts) != 1 || srv.Posts[0].Path != "/graphql" {
		t.Fatalf("posts = %+v, want one POST /graphql", srv.Posts)
	}
	body := srv.Posts[0].Body
	want := map[string]any{"project": "PVT_1", "item": "PVTI_1", "field": "F_status", "option": "opt-ctrl"}
	if !reflect.DeepEqual(body["variables"], want) {
		t.Fatalf("variables = %v, want %v", body["variables"], want)
	}
	query, _ := body["query"].(string)
	if !strings.HasPrefix(query, "mutation SetStatus(") || strings.Count(query, "(input:") != 1 ||
		!strings.Contains(query, "updateProjectV2ItemFieldValue(input:") {
		t.Fatalf("query = %q, want exactly one mutation, updateProjectV2ItemFieldValue", query)
	}
	if srv.Count("SetStatus") != 1 {
		t.Fatalf("Count(SetStatus) = %d, want 1", srv.Count("SetStatus"))
	}
}

func TestBoardPRClosedAt(t *testing.T) {
	srv, c := boardClient(t)
	srv.BoardNodes = []map[string]any{{
		"id": "PVTI_1", "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{}},
		"content": map[string]any{
			"number": 7, "title": "t", "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
			"repository": map[string]any{"nameWithOwner": "o/r"},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{
				map[string]any{"number": 13, "state": "CLOSED", "headRefName": "issue-7-y",
					"mergedAt": nil, "closedAt": "2026-10-30T12:30:00Z"},
				map[string]any{"number": 14, "state": "OPEN", "headRefName": "issue-7-z",
					"mergedAt": nil, "closedAt": nil},
			}},
		},
	}}
	got, err := c.BoardItems(context.Background(), "PVT_1")
	if err != nil {
		t.Fatal(err)
	}
	if query, _ := srv.Posts[0].Body["query"].(string); !strings.Contains(query, " closedAt ") {
		t.Errorf("the board query does not ask closedAt: %s", query)
	}
	prs := got[0].ClosingPRs
	if len(prs) != 2 {
		t.Fatalf("PRs = %+v", prs)
	}
	if !prs[0].ClosedAt.Equal(time.Date(2026, 10, 30, 12, 30, 0, 0, time.UTC)) {
		t.Errorf("closed PR: ClosedAt = %v, want 2026-10-30T12:30:00Z", prs[0].ClosedAt)
	}
	if !prs[1].ClosedAt.IsZero() {
		t.Errorf("open PR: ClosedAt = %v, want the zero time", prs[1].ClosedAt)
	}
}

func TestBoardItemsPRURLAndMergedAt(t *testing.T) {
	srv, c := boardClient(t)
	srv.BoardNodes = []map[string]any{{
		"id": "PVTI_1", "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{}},
		"content": map[string]any{
			"number": 7, "title": "t", "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
			"repository": map[string]any{"nameWithOwner": "o/r"},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{
				map[string]any{"number": 13, "state": "MERGED", "isDraft": false, "mergeable": "UNKNOWN",
					"headRefOid": "def", "headRefName": "issue-7-y", "statusCheckRollup": nil,
					"mergedAt": "2026-10-30T12:00:00Z", "url": "https://github.com/o/r/pull/13"},
				map[string]any{"number": 14, "state": "OPEN", "isDraft": false, "mergeable": "UNKNOWN",
					"headRefOid": "ghi", "headRefName": "issue-7-z", "statusCheckRollup": nil,
					"mergedAt": nil, "url": "https://github.com/o/r/pull/14"},
			}},
		},
	}}
	got, err := c.BoardItems(context.Background(), "PVT_1")
	if err != nil {
		t.Fatal(err)
	}
	prs := got[0].ClosingPRs
	if len(prs) != 2 {
		t.Fatalf("PRs = %+v", prs)
	}
	if prs[0].URL != "https://github.com/o/r/pull/13" || !prs[0].MergedAt.Equal(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("merged PR: %+v", prs[0])
	}
	if prs[1].URL != "https://github.com/o/r/pull/14" || !prs[1].MergedAt.IsZero() {
		t.Errorf("open PR: %+v", prs[1])
	}
}

// issueNode is a board node of issue number with Status option opt, as the board serves it.
func issueNode(id string, number int, opt string) map[string]any {
	return map[string]any{
		"id": id, "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{
			map[string]any{"optionId": opt, "field": map[string]any{"id": "F_status"}},
		}},
		"content": map[string]any{"number": number},
	}
}

func TestIssueItem(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	srv.BoardNodes = []map[string]any{issueNode("PVTI_7", 7, "s1"), issueNode("PVTI_9", 9, "s2")}
	ctx := context.Background()

	got, ok, err := c.IssueItem(ctx, "P1", 7)
	if err != nil || !ok {
		t.Fatalf("IssueItem(P1, 7) = %v, %v, %v; want the item", got, ok, err)
	}
	if got.ItemID != "PVTI_7" || got.SingleSelect["F_status"] != "s1" || got.Issue != 7 || got.Repo != "o/r" {
		t.Errorf("item = %+v; want PVTI_7, Status s1, issue 7 of o/r", got)
	}
	if _, ok, err := c.IssueItem(ctx, "P1", 8); ok || err != nil {
		t.Errorf("IssueItem(P1, 8) ok=%v err=%v; want not on the board and no error", ok, err)
	}
	if _, ok, err := c.IssueItem(ctx, "P2", 7); ok || err != nil {
		t.Errorf("IssueItem(P2, 7) ok=%v err=%v; want not on that board (only on P1)", ok, err)
	}
	vars := srv.Posts[0].Body["variables"].(map[string]any)
	if vars["owner"] != "o" || vars["name"] != "r" || vars["number"] != float64(7) {
		t.Errorf("variables = %v, want owner o, name r, number 7", vars)
	}
	if n := srv.Count("IssueItems"); n != 3 {
		t.Errorf("Count(IssueItems) = %d, want 3", n)
	}
}

// An archived item is still an item of the issue: IssueItem returns it, marked Archived.
func TestIssueItemArchived(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	archived := issueNode("PVTI_7", 7, "s1")
	archived["isArchived"] = true
	srv.BoardNodes = []map[string]any{archived, issueNode("PVTI_9", 9, "s2")}
	ctx := context.Background()

	got, ok, err := c.IssueItem(ctx, "P1", 7)
	if err != nil || !ok || got.ItemID != "PVTI_7" || !got.Archived {
		t.Errorf("IssueItem(P1, 7) = %+v, %v, %v; want PVTI_7, archived", got, ok, err)
	}
	got, ok, err = c.IssueItem(ctx, "P1", 9)
	if err != nil || !ok || got.ItemID != "PVTI_9" || got.Archived {
		t.Errorf("IssueItem(P1, 9) = %+v, %v, %v; want PVTI_9, not archived", got, ok, err)
	}
}

func TestIssueItemTruncated(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	node := issueNode("PVTI_7", 7, "s1")
	node["fieldValues"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
	srv.BoardNodes = []map[string]any{node}
	_, ok, err := c.IssueItem(context.Background(), "P1", 7)
	if err == nil || ok || !strings.Contains(err.Error(), "fieldValues truncated") {
		t.Fatalf("IssueItem ok=%v err=%v; want a fieldValues truncated error", ok, err)
	}
}

func TestAddToBoard(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	srv.Issues = map[int]map[string]any{7: {"number": 7, "node_id": "I_kw7"}}
	ctx := context.Background()

	id, err := c.AddToBoard(ctx, "P1", "I_kw7")
	if err != nil || id != "item-I_kw7" {
		t.Fatalf("AddToBoard = %q, %v; want item-I_kw7", id, err)
	}
	vars := srv.Posts[0].Body["variables"].(map[string]any)
	if vars["project"] != "P1" || vars["content"] != "I_kw7" {
		t.Errorf("variables = %v, want project P1 and content I_kw7", vars)
	}
	if srv.Count("AddToBoard") != 1 {
		t.Errorf("Count(AddToBoard) = %d, want 1", srv.Count("AddToBoard"))
	}
	got, ok, err := c.IssueItem(ctx, "P1", 7)
	if err != nil || !ok || got.ItemID != "item-I_kw7" {
		t.Fatalf("IssueItem after AddToBoard = %+v, %v, %v; want item-I_kw7", got, ok, err)
	}
}
