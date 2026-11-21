package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

func TestEffectiveStatus(t *testing.T) {
	tests := []struct {
		name string
		item BoardItem
		want string
	}{
		{"listed status", BoardItem{Status: "Controller", StatusName: "Controller"}, "Controller"},
		{"unlisted status uses the board name", BoardItem{Status: UnknownStatusPrefix + "abc", StatusName: "On hold"}, "On hold"},
		{"no status", BoardItem{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.EffectiveStatus(); got != tt.want {
				t.Errorf("EffectiveStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

// boardClient returns a forgetest server and a client pointed at it.
func boardClient(t *testing.T) (*forgetest.Server, *GitHub) {
	t.Helper()
	srv := forgetest.New(t)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return srv, c
}

// restBoard is projectBoard with the Status and Priority options the REST board tests use.
func restBoard() *repoconfig.Board {
	b := projectBoard()
	b.StatusOptions = map[string]string{"Controller": "opt-ctrl"}
	b.PriorityOptions = map[string]string{"P1": "opt-p1"}
	return b
}

// restIssue is a board node of issue number of repo in GraphQL shape, as forgetest serves it
// translated to REST, with the closing PR nodes prs and the blocker nodes blockers.
func restIssue(id string, number int, repo string, prs, blockers []any) map[string]any {
	return map[string]any{
		"id": id, "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{}},
		"content": map[string]any{
			"number": number, "title": fmt.Sprintf("Issue %d", number), "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
			"repository":                     map[string]any{"nameWithOwner": repo},
			"blockedBy":                      map[string]any{"nodes": blockers},
			"closedByPullRequestsReferences": map[string]any{"nodes": prs},
		},
	}
}

// Classification: io (REST client against the fake). Fails when an item field, a closing PR
// field, the upper-casing of the issue state or the enrichment of the open PR goes missing.
func TestBoardItemsREST(t *testing.T) {
	srv, c := projectServer(t)
	srv.BoardNodes = []map[string]any{{
		"id":        "PVTI_1",
		"updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{
			map[string]any{"optionId": "opt-ctrl", "name": "Controller", "field": map[string]any{"id": "SF"}},
			map[string]any{"optionId": "opt-p1", "name": "P1", "field": map[string]any{"id": "PF"}},
			map[string]any{}, // a text field: no single-select value
		}},
		"content": map[string]any{
			"number": 7, "title": "Iets doen", "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
			"repository": map[string]any{"nameWithOwner": "o/r"},
			"labels":     map[string]any{"nodes": []any{map[string]any{"name": "1-loc:mac"}, map[string]any{"name": "bug"}}},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{
				map[string]any{"number": 12, "state": "OPEN", "isDraft": true, "mergeable": "MERGEABLE",
					"headRefOid": "abc", "headRefName": "issue-7-x", "baseRefName": "main",
					"url": "https://github.com/o/r/pull/12", "statusCheckRollup": map[string]any{"state": "SUCCESS"}},
				map[string]any{"number": 13, "state": "MERGED", "headRefOid": "def", "headRefName": "issue-7-y", "baseRefName": "main",
					"url": "https://github.com/o/r/pull/13", "mergedAt": "2026-10-30T12:00:00Z", "closedAt": "2026-10-30T12:00:05Z"},
			}},
		},
	}}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []BoardItem{{
		ItemID:         "PVTI_1",
		ItemUpdatedAt:  time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC),
		Status:         "Controller",
		StatusName:     "Controller",
		Priority:       "P1",
		Repo:           "o/r",
		Issue:          7,
		Title:          "Iets doen",
		IssueState:     "OPEN",
		IssueUpdatedAt: time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC),
		Labels:         []string{"1-loc:mac", "bug"},
		ClosingPRs: []PR{
			{Number: 12, State: "OPEN", Draft: true, Mergeable: "MERGEABLE", HeadSHA: "abc", HeadRef: "issue-7-x", BaseRef: "main",
				Checks: "SUCCESS", Closes: true, URL: "https://github.com/o/r/pull/12"},
			{Number: 13, State: "MERGED", Mergeable: "MERGEABLE", HeadSHA: "def", HeadRef: "issue-7-y", BaseRef: "main",
				Closes: true, URL: "https://github.com/o/r/pull/13",
				MergedAt: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), ClosedAt: time.Date(2026, 10, 30, 12, 0, 5, 0, time.UTC)},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if n := srv.Count("/repos/o/r/pulls/13"); n != 0 {
		t.Errorf("Count(pulls/13) = %d, want 0: a merged PR needs no enrichment", n)
	}
}

// Classification: pure mapping through the fake. Fails when an option status_options does not
// list loses its ID or its name, or an unknown priority is not "".
func TestBoardItemsUnknownOption(t *testing.T) {
	srv, c := projectServer(t)
	node := func(id string, values ...map[string]any) map[string]any {
		nodes := []any{}
		for _, v := range values {
			nodes = append(nodes, v)
		}
		return map[string]any{"id": id, "fieldValues": map[string]any{"nodes": nodes}, "content": map[string]any{}}
	}
	value := func(field, opt, name string) map[string]any {
		return map[string]any{"optionId": opt, "name": name, "field": map[string]any{"id": field}}
	}
	srv.BoardNodes = []map[string]any{
		node("A", value("SF", "zz", "In progress"), value("PF", "qq", "P9")),
		node("B"),
	}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type sp struct{ status, name, priority string }
	var gotSP []sp
	for _, it := range got {
		gotSP = append(gotSP, sp{it.Status, it.StatusName, it.Priority})
	}
	want := []sp{{"unknown:zz", "In progress", ""}, {"", "", ""}}
	if !reflect.DeepEqual(gotSP, want) {
		t.Errorf("status, name, priority = %v, want %v", gotSP, want)
	}
}

// Classification: io. Fails when an item without issue content gets a Repo or Issue, or loses
// its ItemID, time or Status.
func TestBoardItemsNonIssueContent(t *testing.T) {
	srv, c := projectServer(t)
	status := map[string]any{"nodes": []any{map[string]any{"optionId": "opt-ctrl", "name": "Controller", "field": map[string]any{"id": "SF"}}}}
	srv.BoardNodes = []map[string]any{
		{"id": "draft", "updatedAt": "2026-10-30T09:00:00Z", "fieldValues": status, "content": map[string]any{"title": "a draft"}},
		{"id": "hidden", "updatedAt": "2026-10-30T09:00:00Z", "content": nil},
	}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	want := []BoardItem{
		{ItemID: "draft", ItemUpdatedAt: at, Status: "Controller", StatusName: "Controller"},
		{ItemID: "hidden", ItemUpdatedAt: at},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// Classification: pure-logic. Fails when content of a project item whose content_type is not
// "Issue" is read as an issue, with or without a pull_request key on the content.
func TestRestItemNonIssueContent(t *testing.T) {
	const content = `"content": {"number": 12, "title": "a PR", "state": "open", "repository_url": "https://api.github.com/repos/o/r"`
	for name, raw := range map[string]string{
		"pull request with pull_request key": `{"id": 1, "node_id": "PVTI_pr", "updated_at": "2026-10-30T09:00:00Z", "content_type": "PullRequest",
			` + content + `, "pull_request": {"url": "https://api.github.com/repos/o/r/pulls/12"}}}`,
		"pull request content_type only": `{"id": 1, "node_id": "PVTI_pr", "updated_at": "2026-10-30T09:00:00Z", "content_type": "PullRequest",
			` + content + `}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var r restItem
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				t.Fatal(err)
			}
			it, pulls := r.boardItem(project{status: 100}, restBoard())
			want := BoardItem{ItemID: "PVTI_pr", ItemUpdatedAt: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)}
			if !reflect.DeepEqual(it, want) || pulls != nil {
				t.Fatalf("boardItem = %+v, %v; want %+v and no pulls", it, pulls, want)
			}
		})
	}
}

// Classification: io. Fails when BoardItems lists an item with archived_at.
func TestBoardItemsSkipsArchived(t *testing.T) {
	srv, c := projectServer(t)
	archived := restIssue("PVTI_old", 3, "o/r", nil, nil)
	archived["isArchived"] = true
	srv.BoardNodes = []map[string]any{archived, restIssue("PVTI_new", 4, "o/r", nil, nil)}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ItemID != "PVTI_new" {
		t.Fatalf("items = %+v, want only PVTI_new", got)
	}
}

// Classification: io. Fails when BoardItems stops after the first page.
func TestBoardItemsFollowsPages(t *testing.T) {
	srv, c := projectServer(t)
	for i := range 150 {
		srv.BoardNodes = append(srv.BoardNodes, map[string]any{"id": fmt.Sprintf("I%d", i), "content": map[string]any{}})
	}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 150 || got[0].ItemID != "I0" || got[149].ItemID != "I149" {
		t.Fatalf("got %d items; want 150 from I0 to I149", len(got))
	}
	if n := srv.Count("/users/o/projectsV2/1/items"); n != 2 {
		t.Fatalf("Count(items) = %d, want 2", n)
	}
}

// Classification: io. Fails when a page that fails after a good one gives the items read so far.
func TestBoardItemsLaterPageFails(t *testing.T) {
	srv, c := projectServer(t)
	for i := range 150 {
		srv.BoardNodes = append(srv.BoardNodes, map[string]any{"id": fmt.Sprintf("I%d", i), "content": map[string]any{}})
	}
	srv.FailPage = map[string]int{"GET /users/o/projectsV2/1/items?page=2": 502}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 502") || got != nil {
		t.Fatalf("BoardItems = %d items, %v; want a 502 error and nil", len(got), err)
	}
	if n := srv.Count("/users/o/projectsV2/1/items"); n != 2 {
		t.Fatalf("Count(items) = %d, want 2: page 1 read, page 2 failed", n)
	}
}

// Classification: io. Fails when MergedPR comes from anything but a merged linked PR of the
// blocker's own board item (archived ones included), when the state is not upper case, or when
// an item without blockers asks the dependencies route.
func TestBoardItemsBlockers(t *testing.T) {
	srv, c := projectServer(t)
	merged := []any{map[string]any{"number": 15, "state": "MERGED", "headRefName": "issue-5-x", "mergedAt": "2026-10-30T12:00:00Z"}}
	open := []any{map[string]any{"number": 16, "state": "CLOSED", "headRefName": "issue-6-x"}}
	blockers := []any{
		map[string]any{"number": 5, "state": "CLOSED", "repository": map[string]any{"nameWithOwner": "o/r"}},
		// off the board: the merged closing PR GraphQL showed is not looked up
		map[string]any{"number": 6, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/x"},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{map[string]any{"merged": true}}}},
		map[string]any{"number": 4, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"}},
		map[string]any{"number": 3, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"}},
	}
	archived := restIssue("PVTI_4", 4, "o/r", merged, nil)
	archived["isArchived"] = true
	srv.BoardNodes = []map[string]any{
		restIssue("PVTI_7", 7, "o/r", nil, blockers),
		restIssue("PVTI_5", 5, "o/r", merged, nil),
		archived,
		restIssue("PVTI_3", 3, "o/r", open, nil),
		restIssue("PVTI_8", 8, "o/r", nil, nil),
	}
	c.Board = restBoard()
	got, err := c.BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Blocker{
		{Repo: "o/r", Issue: 5, State: "CLOSED", MergedPR: true},
		{Repo: "o/x", Issue: 6, State: "OPEN"},
		{Repo: "o/r", Issue: 4, State: "OPEN", MergedPR: true},
		{Repo: "o/r", Issue: 3, State: "OPEN"},
	}
	if got[0].Issue != 7 || !reflect.DeepEqual(got[0].BlockedBy, want) {
		t.Fatalf("item %d BlockedBy = %+v\nwant %+v", got[0].Issue, got[0].BlockedBy, want)
	}
	if n := srv.Count("/repos/o/r/issues/7/dependencies/blocked_by"); n != 1 {
		t.Errorf("Count(blocked_by of 7) = %d, want 1", n)
	}
	for _, n := range []int{5, 3, 8} {
		if k := srv.Count(fmt.Sprintf("/repos/o/r/issues/%d/dependencies/blocked_by", n)); k != 0 {
			t.Errorf("Count(blocked_by of %d) = %d, want 0: total_blocked_by is 0", n, k)
		}
	}
}

// Classification: io. Fails when a 403 or 404 on an extra call of an item in another repo
// breaks the board, or when one on an item of the repo itself, or another status, is ignored.
func TestBoardItemsOtherRepoNotVisible(t *testing.T) {
	cases := []struct {
		name, repo, path string
		status           int
		message          string
		ok               bool
	}{
		{"other repo, pull 404", "x/y", "pulls/21", 404, "", true},
		{"other repo, pull 403", "x/y", "pulls/21", 403, "", true},
		{"other repo, checks 404", "x/y", "commits/sha21/check-runs", 404, "", true},
		{"other repo, blockers 404", "x/y", "issues/9/dependencies/blocked_by", 404, "", true},
		{"other repo, blockers 403", "x/y", "issues/9/dependencies/blocked_by", 403, "", true},
		{"other repo, pull 403 rate limit", "x/y", "pulls/21", 403, "API rate limit exceeded", false},
		{"other repo, pull 500", "x/y", "pulls/21", 500, "", false},
		{"own repo, pull 404", "o/r", "pulls/21", 404, "", false},
		{"own repo, blockers 404", "o/r", "issues/9/dependencies/blocked_by", 404, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := projectServer(t)
			pr := map[string]any{"number": 21, "state": "OPEN", "mergeable": "MERGEABLE", "headRefOid": "sha21",
				"headRefName": "issue-9-x", "statusCheckRollup": map[string]any{"state": "SUCCESS"}}
			blocker := map[string]any{"number": 2, "state": "OPEN", "repository": map[string]any{"nameWithOwner": tc.repo}}
			srv.BoardNodes = []map[string]any{restIssue("PVTI_9", 9, tc.repo, []any{pr}, []any{blocker})}
			key := "GET /repos/" + tc.repo + "/" + tc.path
			srv.Fail = map[string]int{key: tc.status}
			if tc.message != "" {
				srv.FailMessage = map[string]string{key: tc.message}
			}
			c.Board = restBoard()
			got, err := c.BoardItems(context.Background())
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status %d", tc.status)) || got != nil {
					t.Fatalf("BoardItems = %+v, %v; want the %d as an error and nil", got, err, tc.status)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			it := got[0]
			if strings.Contains(tc.path, "blocked_by") {
				if it.BlockedBy != nil || it.ClosingPRs[0].Mergeable != "MERGEABLE" || it.ClosingPRs[0].Checks != "SUCCESS" {
					t.Fatalf("item = %+v; want no blockers and the PR enriched", it)
				}
				return
			}
			if pr := it.ClosingPRs[0]; pr.Mergeable != "UNKNOWN" || pr.Checks != "" || pr.Number != 21 || pr.State != "OPEN" {
				t.Fatalf("PR = %+v; want #21 OPEN with Mergeable UNKNOWN and Checks \"\"", pr)
			}
			if !reflect.DeepEqual(it.BlockedBy, []Blocker{{Repo: "x/y", Issue: 2, State: "OPEN"}}) {
				t.Fatalf("BlockedBy = %+v; want x/y#2", it.BlockedBy)
			}
		})
	}
}

// cachedBoardClient is a client through Open with cache, on srv's board restBoard.
func cachedBoardClient(t *testing.T, srv *forgetest.Server, cache CacheStore) Forge {
	t.Helper()
	f, err := Open(Config{Repo: "o/r", Token: "secret-token", BaseURL: srv.URL(), Board: restBoard(), Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Classification: io. Fails when a 304 on a page loses its Link, so the second read stops at
// page 1, or when the cache is not asked at all.
func TestBoardItems304FirstOfTwoPages(t *testing.T) {
	srv := forgetest.New(t)
	srv.ProjectID = "PVT_1"
	for i := range 150 {
		srv.BoardNodes = append(srv.BoardNodes, map[string]any{"id": fmt.Sprintf("I%d", i), "content": map[string]any{}})
	}
	f := cachedBoardClient(t, srv, NewMemoryCache())
	const items = "/users/o/projectsV2/1/items"
	for round := 1; round <= 2; round++ {
		got, err := f.BoardItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 150 || got[149].ItemID != "I149" {
			t.Fatalf("round %d: got %d items, want 150 up to I149", round, len(got))
		}
	}
	if n, nm := srv.Count(items), srv.NotModified(items); n != 4 || nm != 2 {
		t.Fatalf("items: %d requests, %d answered 304; want 4 and 2 (both pages of round 2)", n, nm)
	}
}

// Classification: io. Fails when a second client on the same store asks anything without
// If-None-Match, or reads other items from the stored answers.
func TestBoardItemsSecondClientSameStore(t *testing.T) {
	srv := forgetest.New(t)
	srv.ProjectID = "PVT_1"
	pr := map[string]any{"number": 12, "state": "OPEN", "mergeable": "MERGEABLE", "headRefOid": "abc",
		"headRefName": "issue-7-x", "statusCheckRollup": map[string]any{"state": "SUCCESS"}}
	blocker := map[string]any{"number": 5, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"}}
	srv.BoardNodes = []map[string]any{restIssue("PVTI_7", 7, "o/r", []any{pr}, []any{blocker})}
	cache := NewMemoryCache()
	first, err := cachedBoardClient(t, srv, cache).BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"/users/o/projectsV2/1", "/users/o/projectsV2/1/fields", "/users/o/projectsV2/1/items",
		"/repos/o/r/issues/7/dependencies/blocked_by", "/repos/o/r/pulls/12",
		"/repos/o/r/commits/abc/check-runs", "/repos/o/r/commits/abc/status", "/repos/o/r/commits/abc/check-suites",
	}
	before := map[string]int{}
	for _, p := range paths {
		if srv.Count(p) != 1 || srv.NotModified(p) != 0 {
			t.Fatalf("first client: %s %d requests, %d 304; want 1 and 0", p, srv.Count(p), srv.NotModified(p))
		}
		before[p] = srv.Count(p)
	}
	second, err := cachedBoardClient(t, srv, cache).BoardItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if n, nm := srv.Count(p)-before[p], srv.NotModified(p); n != 1 || nm != 1 {
			t.Errorf("second client: %s %d requests, %d 304; want 1 and 1", p, n, nm)
		}
	}
	if !reflect.DeepEqual(first, second) || len(second) != 1 || second[0].ClosingPRs[0].Checks != "SUCCESS" {
		t.Fatalf("second = %+v\nfirst  = %+v; want the same item with its enriched PR", second, first)
	}
}

// issueNode is a board node of issue number with Status option opt, named "Option <opt>" on
// the board, as the board serves it.
func issueNode(id string, number int, opt string) map[string]any {
	return map[string]any{
		"id": id, "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{
			map[string]any{"optionId": opt, "name": "Option " + opt, "field": map[string]any{"id": "F_status"}},
		}},
		"content": map[string]any{"number": number},
	}
}

func TestIssueItem(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	srv.BoardNodes = []map[string]any{issueNode("PVTI_7", 7, "s1"), issueNode("PVTI_9", 9, "s2")}
	srv.Issues = map[int]map[string]any{8: {"number": 8}} // exists, on no board
	ctx := context.Background()
	c.Board = &repoconfig.Board{ProjectID: "P1", StatusFieldID: "F_status", StatusOptions: map[string]string{"S1": "s1"}}

	got, ok, err := c.IssueItem(ctx, 7)
	if err != nil || !ok {
		t.Fatalf("IssueItem(P1, 7) = %v, %v, %v; want the item", got, ok, err)
	}
	if got.ItemID != "PVTI_7" || got.Status != "S1" || got.StatusName != "Option s1" || got.Issue != 7 || got.Repo != "o/r" {
		t.Errorf("item = %+v; want PVTI_7, Status S1, StatusName Option s1, issue 7 of o/r", got)
	}
	// s2 is no option of status_options: Status says unknown by its ID, StatusName has its name.
	got, ok, err = c.IssueItem(ctx, 9)
	if err != nil || !ok || got.Status != "unknown:s2" || got.StatusName != "Option s2" {
		t.Errorf("IssueItem(P1, 9) = %+v, %v, %v; want Status unknown:s2, StatusName Option s2", got, ok, err)
	}
	if _, ok, err := c.IssueItem(ctx, 8); ok || err != nil {
		t.Errorf("IssueItem(P1, 8) ok=%v err=%v; want not on the board and no error", ok, err)
	}
	c.Board = &repoconfig.Board{ProjectID: "P2"}
	if _, ok, err := c.IssueItem(ctx, 7); ok || err != nil {
		t.Errorf("IssueItem(P2, 7) ok=%v err=%v; want not on that board (only on P1)", ok, err)
	}
	vars := srv.Posts[0].Body["variables"].(map[string]any)
	if vars["owner"] != "o" || vars["name"] != "r" || vars["number"] != float64(7) {
		t.Errorf("variables = %v, want owner o, name r, number 7", vars)
	}
	if n := srv.Count("IssueItems"); n != 4 {
		t.Errorf("Count(IssueItems) = %d, want 4", n)
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
	c.Board = &repoconfig.Board{ProjectID: "P1"}

	got, ok, err := c.IssueItem(ctx, 7)
	if err != nil || !ok || got.ItemID != "PVTI_7" || !got.Archived {
		t.Errorf("IssueItem(P1, 7) = %+v, %v, %v; want PVTI_7, archived", got, ok, err)
	}
	got, ok, err = c.IssueItem(ctx, 9)
	if err != nil || !ok || got.ItemID != "PVTI_9" || got.Archived {
		t.Errorf("IssueItem(P1, 9) = %+v, %v, %v; want PVTI_9, not archived", got, ok, err)
	}
}

// Only GitHub's answer for a missing issue, a NOT_FOUND on repository.issue, is
// ErrIssueNotFound; any other GraphQL error on IssueItems stays a plain error.
func TestIssueItemGraphQLErrors(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	c.Board = &repoconfig.Board{ProjectID: "P1"}
	ctx := context.Background()
	_, ok, err := c.IssueItem(ctx, 99)
	if ok || !errors.Is(err, ErrIssueNotFound) || !strings.Contains(err.Error(), "o/r#99") {
		t.Errorf("IssueItem(99) ok=%v err=%v; want an error wrapping ErrIssueNotFound naming o/r#99", ok, err)
	}
	srv.FailOps = map[string]string{"IssueItems": "Something went wrong"}
	_, ok, err = c.IssueItem(ctx, 99)
	if ok || err == nil || errors.Is(err, ErrIssueNotFound) || !strings.Contains(err.Error(), "Something went wrong") {
		t.Errorf("IssueItem with another GraphQL error ok=%v err=%v; want that error, not ErrIssueNotFound", ok, err)
	}
}

func TestGraphQLErrorsNotFound(t *testing.T) {
	cases := []struct {
		name string
		errs graphqlErrors
		want bool
	}{
		{"not found on the path", graphqlErrors{{Type: "NOT_FOUND", Path: []any{"repository", "issue"}}}, true},
		{"second error matches", graphqlErrors{{Type: "FORBIDDEN"}, {Type: "NOT_FOUND", Path: []any{"repository", "issue"}}}, true},
		{"not found on the repository", graphqlErrors{{Type: "NOT_FOUND", Path: []any{"repository"}}}, false},
		{"other type on the path", graphqlErrors{{Type: "FORBIDDEN", Path: []any{"repository", "issue"}}}, false},
		{"no type", graphqlErrors{{Message: "boom", Path: []any{"repository", "issue"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.errs.notFound("repository", "issue"); got != tc.want {
				t.Errorf("notFound = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIssueItemTruncated(t *testing.T) {
	srv, c := boardClient(t)
	srv.ProjectID = "P1"
	node := issueNode("PVTI_7", 7, "s1")
	node["fieldValues"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
	srv.BoardNodes = []map[string]any{node}
	c.Board = &repoconfig.Board{ProjectID: "P1"}
	_, ok, err := c.IssueItem(context.Background(), 7)
	if err == nil || ok || !strings.Contains(err.Error(), "fieldValues truncated") {
		t.Fatalf("IssueItem ok=%v err=%v; want a fieldValues truncated error", ok, err)
	}
}

// Class: io. At risk: the item AddToBoard created is not the item IssueItem finds for the issue.
func TestAddToBoardThenIssueItem(t *testing.T) {
	srv, c := projectServer(t)
	srv.Issues = map[int]map[string]any{7: {"number": 7, "node_id": "I_kw7"}}
	ctx := context.Background()
	c.Board = restBoard()
	item, err := c.AddToBoard(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.IssueItem(ctx, 7)
	if err != nil || !ok || got.ItemID != item.ItemID {
		t.Fatalf("IssueItem after AddToBoard = %+v, %v, %v; want %s", got, ok, err, item.ItemID)
	}
}

// Class: io. At risk: a controller path (the board read, the branch PRs, SetStatus, AddToBoard)
// falls back to GraphQL, so an exhausted GraphQL limit stops a tick again.
func TestControllerPathsUseNoGraphQL(t *testing.T) {
	srv, c := projectServer(t)
	c.Board = restBoard()
	srv.Issues = map[int]map[string]any{7: {"number": 7, "node_id": "I_kw7"}}
	srv.BoardNodes = []map[string]any{restIssue("PVTI_a", 6, "o/r", []any{
		map[string]any{"number": 11, "state": "OPEN", "isDraft": false, "mergeable": "MERGEABLE", "headRefOid": "sha-open",
			"headRefName": "issue-6-x", "baseRefName": "main", "url": "https://github.com/o/r/pull/11",
			"statusCheckRollup": map[string]any{"state": "SUCCESS"}},
	}, nil)}
	srv.Branches = map[string][]map[string]any{"issue-6-x": {
		{"number": 11, "state": "OPEN", "isDraft": false, "mergeable": "MERGEABLE", "headRefOid": "sha-open",
			"headRefName": "issue-6-x", "baseRefName": "main", "url": "https://github.com/o/r/pull/11",
			"statusCheckRollup": map[string]any{"state": "SUCCESS"}},
	}}
	ctx := context.Background()
	items, err := c.BoardItems(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("BoardItems = %+v, %v; want one item", items, err)
	}
	if prs, err := c.BranchPRs(ctx, "issue-6-x"); err != nil || len(prs) != 1 {
		t.Fatalf("BranchPRs = %+v, %v; want one PR", prs, err)
	}
	if err := c.SetStatus(ctx, items[0], "Controller"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if _, err := c.AddToBoard(ctx, 7); err != nil {
		t.Fatalf("AddToBoard: %v", err)
	}
	if n := srv.Count("/graphql"); n != 0 {
		t.Fatalf("controller paths made %d GraphQL requests, want 0", n)
	}
	if _, err := c.Planning(ctx); err != nil {
		t.Fatalf("Planning: %v", err)
	}
	if srv.Count("/graphql") == 0 {
		t.Error("Planning made no GraphQL request; the test no longer tells REST from GraphQL")
	}
}
