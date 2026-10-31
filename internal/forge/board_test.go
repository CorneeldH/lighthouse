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
					"headRefOid": "def", "headRefName": "issue-7-y", "statusCheckRollup": map[string]any{"state": "SUCCESS"}},
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
		BlockedBy:      []Blocker{{Repo: "o/x", Issue: 5, State: "CLOSED", MergedPR: true}},
		ClosingPRs: []PR{
			{Number: 12, State: "OPEN", Draft: true, Mergeable: "UNKNOWN", HeadSHA: "abc", HeadRef: "issue-7-x", Checks: "", Closes: true},
			{Number: 13, State: "MERGED", Draft: false, Mergeable: "CONFLICTING", HeadSHA: "def", HeadRef: "issue-7-y", Checks: "SUCCESS", Closes: true},
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
