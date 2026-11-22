package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

var testRepo = repoconfig.Repo{Slug: "o/r", Board: &repoconfig.Board{
	ProjectNumber:   1,
	ProjectID:       "PVT_1",
	StatusFieldID:   "F_status",
	PriorityFieldID: "F_prio",
	StatusOptions:   map[string]string{"Controller": "opt-ctrl", "On hold": "opt-hold", "Done": "opt-done"},
	PriorityOptions: map[string]string{"P1": "opt-p1", "P2": "opt-p2"},
}}

// node is a board item in GraphQL shape with the given single-select options and content.
func node(id string, options map[string]string, content map[string]any) map[string]any {
	var values []any
	for field, option := range options {
		values = append(values, map[string]any{"optionId": option, "field": map[string]any{"id": field}})
	}
	return map[string]any{"id": id, "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": values}, "content": content}
}

// issue is the content of a board item: an issue of repo with no blockers and no closing PRs
// unless extra sets them.
func issue(repo string, number int, extra map[string]any) map[string]any {
	c := map[string]any{"number": number, "title": "Issue", "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
		"repository": map[string]any{"nameWithOwner": repo}}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

func prNode(number int, ref string, checks any) map[string]any {
	return map[string]any{"number": number, "state": "OPEN", "isDraft": false, "mergeable": "MERGEABLE",
		"headRefOid": "sha" + ref, "headRefName": ref, "statusCheckRollup": checks}
}

func TestPrepareBlockerStatus(t *testing.T) {
	srv, c := client(t)
	repo := testRepo
	board := *testRepo.Board
	board.StatusOptions = map[string]string{"Controller": "opt-ctrl", "On hold": "opt-hold", "In progress": "opt-prog"}
	repo.Board = &board
	c.Board = &board
	blocker := func(repo string, n int) any {
		return map[string]any{"number": n, "state": "OPEN", "repository": map[string]any{"nameWithOwner": repo},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{}}}
	}
	srv.BoardNodes = []map[string]any{
		node("I7", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 7, map[string]any{
			"blockedBy": map[string]any{"nodes": []any{blocker("o/r", 5), blocker("x/y", 9)}},
		})),
		node("I5", map[string]string{"F_status": "opt-prog"}, issue("o/r", 5, nil)),
	}
	pre, err := Prepare(context.Background(), c, repo, nil, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Items) != 1 || pre.Items[0].Issue != 7 {
		t.Fatalf("Items = %+v, want only issue 7", pre.Items)
	}
	want := []forge.Blocker{
		{Repo: "o/r", Issue: 5, State: "OPEN", Status: "In progress"},
		{Repo: "x/y", Issue: 9, State: "OPEN"},
	}
	if got := pre.Items[0].BlockedBy; !reflect.DeepEqual(got, want) {
		t.Errorf("BlockedBy = %+v, want %+v", got, want)
	}
}

// An option status_options does not list reads as no status everywhere in Pre: the forge's
// "unknown:<option ID>" must not reach a status, the Statuses map or a blocker's comment text.
func TestPrepareUnmappedStatusIsEmpty(t *testing.T) {
	srv, c := client(t)
	blocker8 := map[string]any{"blockedBy": map[string]any{"nodes": []any{map[string]any{
		"number": 8, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"},
		"closedByPullRequestsReferences": map[string]any{"nodes": []any{}},
	}}}}
	srv.BoardNodes = []map[string]any{
		node("I7", map[string]string{"F_status": "opt-prog"}, issue("o/r", 7, blocker8)),
		node("I8", map[string]string{"F_status": "opt-prog"}, issue("o/r", 8, nil)),
		node("I9", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 9, blocker8)),
	}
	open := []runs.OpenRun{{ID: 15, Issue: 7}}
	pre, err := Prepare(context.Background(), c, testRepo, open, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Items) != 2 || pre.Items[0].Issue != 7 || pre.Items[1].Issue != 9 {
		t.Fatalf("Items = %+v, want issues 7 and 9", pre.Items)
	}
	if got := pre.Items[0].Status; got != "" {
		t.Errorf("Items[7].Status = %q, want \"\"", got)
	}
	if want := map[int]string{7: "", 8: "", 9: "Controller"}; !reflect.DeepEqual(pre.Statuses, want) {
		t.Errorf("Statuses = %v, want %v", pre.Statuses, want)
	}
	want := []forge.Blocker{{Repo: "o/r", Issue: 8, State: "OPEN"}}
	for _, it := range pre.Items {
		if !reflect.DeepEqual(it.BlockedBy, want) {
			t.Errorf("Items[%d].BlockedBy = %+v, want %+v", it.Issue, it.BlockedBy, want)
		}
	}
}

func TestPrepareCarriesLabels(t *testing.T) {
	srv, c := client(t)
	srv.BoardNodes = []map[string]any{
		node("I7", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 7, map[string]any{
			"labels": map[string]any{"nodes": []any{map[string]any{"name": "1-loc:mac"}}},
		})),
	}
	pre, err := Prepare(context.Background(), c, testRepo, nil, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Items) != 1 || !reflect.DeepEqual(pre.Items[0].Labels, []string{"1-loc:mac"}) {
		t.Fatalf("Items = %+v, want issue 7 with label 1-loc:mac", pre.Items)
	}
}

func TestPrepareFiltersBoard(t *testing.T) {
	srv, c := client(t)
	srv.BoardNodes = []map[string]any{
		// issue 4 first: Items are sorted by issue, not by board order
		node("I4", map[string]string{"F_status": "opt-hold"}, issue("o/r", 4, map[string]any{
			"blockedBy": map[string]any{"nodes": []any{map[string]any{
				"number": 5, "state": "CLOSED", "repository": map[string]any{"nameWithOwner": "o/r"},
				"closedByPullRequestsReferences": map[string]any{"nodes": []any{map[string]any{"merged": true}}},
			}}},
		})),
		node("I1", map[string]string{"F_status": "opt-ctrl", "F_prio": "opt-p1"}, issue("o/r", 1, map[string]any{
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{
				prNode(10, "issue-1-x", map[string]any{"state": "SUCCESS"}),
			}},
		})),
		node("I2", map[string]string{"F_status": "opt-done"}, issue("o/r", 2, nil)),
		node("I3", map[string]string{"F_status": "opt-hold"}, issue("o/x", 3, nil)),
		node("D1", map[string]string{"F_status": "opt-ctrl"}, map[string]any{}), // a draft issue
		node("I8", map[string]string{"F_status": "opt-unknown"}, issue("o/r", 8, nil)),
		node("I9", nil, issue("o/r", 9, nil)), // no Status at all
	}
	pre, err := Prepare(context.Background(), c, testRepo, nil, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	itemAt := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	issueAt := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	want := []Item{
		{Issue: 1, Title: "Issue", State: "OPEN", ItemID: "I1", Status: "Controller", Priority: "P1",
			ItemUpdatedAt: itemAt, IssueUpdatedAt: issueAt,
			PRs: []forge.PR{{Number: 10, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "shaissue-1-x",
				HeadRef: "issue-1-x", Checks: "SUCCESS", Closes: true}}},
		{Issue: 4, Title: "Issue", State: "OPEN", ItemID: "I4", Status: "On hold", Priority: "",
			ItemUpdatedAt: itemAt, IssueUpdatedAt: issueAt,
			// #5 is not on the board: its merged closing PR is not looked up.
			BlockedBy: []forge.Blocker{{Repo: "o/r", Issue: 5, State: "CLOSED", MergedPR: false}}},
	}
	if !reflect.DeepEqual(pre.Items, want) {
		t.Errorf("Items =\n%+v\nwant\n%+v", pre.Items, want)
	}
	if pre.Repo.Slug != "o/r" {
		t.Errorf("Repo.Slug = %q, want o/r", pre.Repo.Slug)
	}
}

// Statuses holds every board item of the repo, whatever its Status; Items only the dispatch ones.
func TestPrepareStatuses(t *testing.T) {
	srv, c := client(t)
	repo := testRepo
	board := *testRepo.Board
	board.StatusOptions = map[string]string{"Controller": "opt-ctrl", "On hold": "opt-hold", "In progress": "opt-prog"}
	repo.Board = &board
	c.Board = &board
	srv.BoardNodes = []map[string]any{
		node("I945", map[string]string{"F_status": "opt-prog"}, issue("o/r", 945, nil)),
		node("I12", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 12, nil)),
		node("I13", map[string]string{"F_status": "opt-prog"}, issue("o/x", 13, nil)),
	}
	pre, err := Prepare(context.Background(), c, repo, nil, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int]string{945: "In progress", 12: "Controller"}; !reflect.DeepEqual(pre.Statuses, want) {
		t.Errorf("Statuses = %v, want %v", pre.Statuses, want)
	}
	if len(pre.Items) != 1 || pre.Items[0].Issue != 12 {
		t.Errorf("Items = %+v, want only issue 12", pre.Items)
	}
}

// An item that left Controller and On hold while a run on it is open stays in Items, so the
// tick can still close that run; without an open run it drops out.
func TestPrepareKeepsItemWithOpenRun(t *testing.T) {
	srv, c := client(t)
	srv.BoardNodes = []map[string]any{
		node("I2", map[string]string{"F_status": "opt-done"}, issue("o/r", 2, nil)),
		node("I9", map[string]string{"F_status": "opt-done"}, issue("o/r", 9, map[string]any{"state": "CLOSED"})),
		node("I5", map[string]string{"F_status": "opt-done"}, issue("o/x", 5, nil)), // open run, other repo
	}
	open := []runs.OpenRun{{ID: 15, Issue: 9}, {ID: 16, Issue: 5}}
	pre, err := Prepare(context.Background(), c, testRepo, open, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Items) != 1 || pre.Items[0].Issue != 9 || pre.Items[0].Status != "Done" || pre.Items[0].State != "CLOSED" {
		t.Fatalf("Items = %+v, want only issue 9, Done and CLOSED", pre.Items)
	}
}

// InController holds Controller and the On hold remnant, and a To do item only while its mark
// says the tick parked it: its workplace must stay for the resume (the sweep takes this func,
// TestTickSweepKeepsWorkplaceOfParkedToDo). Dropping the To do term turns issue 6 red.
func TestInController(t *testing.T) {
	in := InController(Pre{
		Statuses: map[int]string{1: "Controller", 2: "On hold", 3: "In progress", 4: "", 6: "To do", 7: "To do", 8: "In progress"},
		ToDo:     map[int]ToDoMark{6: {OwnParked: true}, 7: {OwnParked: false}, 8: {OwnParked: true}},
	})
	for issue, want := range map[int]bool{1: true, 2: true, 3: false, 4: false, 5: false, 6: true, 7: false, 8: false} {
		if got := in(issue); got != want {
			t.Errorf("InController(%d) = %v, want %v", issue, got, want)
		}
	}
}

func TestMergedHead(t *testing.T) {
	at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	tests := []struct {
		name string
		prs  []forge.PR
		want string
	}{
		{"no PR", nil, ""},
		{"open PR", []forge.PR{{Number: 1, State: "OPEN", HeadSHA: "open"}}, ""},
		{"closed, not merged", []forge.PR{{Number: 1, State: "CLOSED", HeadSHA: "closed", ClosedAt: at(2)}}, ""},
		{"last merged wins", []forge.PR{
			{Number: 2, State: "MERGED", HeadSHA: "late", MergedAt: at(5)},
			{Number: 1, State: "MERGED", HeadSHA: "early", MergedAt: at(3)},
		}, "late"},
		{"cross-repo merged does not count", []forge.PR{
			{Number: 1, State: "MERGED", HeadSHA: "own", MergedAt: at(3)},
			{Number: 2, State: "MERGED", HeadSHA: "fork", MergedAt: at(5), CrossRepo: true},
		}, "own"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MergedHead(tc.prs); got != tc.want {
				t.Errorf("MergedHead = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrepareMergesBranchPRs(t *testing.T) {
	srv, c := client(t)
	srv.BoardNodes = []map[string]any{
		node("I1", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 1, map[string]any{
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{prNode(10, "issue-1-x", nil)}},
		})),
		node("I4", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 4, map[string]any{
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{
				prNode(21, "issue-4-y", nil), // first: PRs are sorted by number, also without a branch
				prNode(20, "issue-4-x", nil),
			}},
		})),
		node("I6", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 6, nil)),
	}
	srv.Branches = map[string][]map[string]any{"issue-1-x": {
		prNode(11, "issue-1-x", nil), // first: PRs are sorted by number
		prNode(10, "issue-1-x", nil),
	}}
	open := []runs.OpenRun{{ID: 1, Issue: 1, Branch: "issue-1-x"}, {ID: 2, Issue: 6, Branch: ""}}
	branches := map[int]string{1: "issue-1-x", 6: ""}
	pre, err := Prepare(context.Background(), c, testRepo, open, branches, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	byIssue := map[int][]forge.PR{}
	for _, it := range pre.Items {
		byIssue[it.Issue] = it.PRs
	}
	got := byIssue[1]
	want := []forge.PR{
		{Number: 10, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "shaissue-1-x", HeadRef: "issue-1-x", Closes: true},
		{Number: 11, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "shaissue-1-x", HeadRef: "issue-1-x", Closes: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issue 1 PRs =\n%+v\nwant\n%+v", got, want)
	}
	var four []int
	for _, p := range byIssue[4] {
		four = append(four, p.Number)
	}
	if want := []int{20, 21}; !reflect.DeepEqual(four, want) {
		t.Errorf("issue 4 PR numbers = %v, want %v", four, want)
	}
	if len(byIssue[6]) != 0 {
		t.Errorf("issue 6 PRs = %+v, want none", byIssue[6])
	}
	if n := srv.Count("/repos/o/r/pulls?head=issue-1-x"); n != 1 {
		t.Errorf("BranchPRs requests = %d, want 1 (issue 4 has no run, issue 6 no branch)", n)
	}
	if !reflect.DeepEqual(pre.OpenRuns, open) {
		t.Errorf("OpenRuns = %+v, want %+v", pre.OpenRuns, open)
	}
}

// A merged Refs PR stays visible after its run closed: the branch comes from the history,
// not from the open runs.
func TestPrepareBranchOfClosedRun(t *testing.T) {
	srv, c := client(t)
	srv.BoardNodes = []map[string]any{
		node("I1", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 1, nil)),
	}
	srv.Branches = map[string][]map[string]any{"issue-1-x": {prNode(11, "issue-1-x", nil)}}
	pre, err := Prepare(context.Background(), c, testRepo, nil, map[int]string{1: "issue-1-x"}, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Items) != 1 {
		t.Fatalf("Items = %+v, want issue 1", pre.Items)
	}
	want := []forge.PR{{Number: 11, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "shaissue-1-x", HeadRef: "issue-1-x"}}
	if !reflect.DeepEqual(pre.Items[0].PRs, want) {
		t.Errorf("issue 1 PRs =\n%+v\nwant\n%+v", pre.Items[0].PRs, want)
	}
}

func TestPrepareWithoutBoardFails(t *testing.T) {
	_, c := client(t)
	if _, err := Prepare(context.Background(), c, repoconfig.Repo{Slug: "o/r"}, nil, nil, ToDoProbe{}); err == nil {
		t.Fatal("Prepare without [forge.board] gave no error")
	}
}

// comment is a REST issue comment by login; "" gives user: null, as for a deleted user.
func comment(id int64, login, body string) map[string]any {
	var user any
	if login != "" {
		user = map[string]any{"login": login}
	}
	return map[string]any{"id": id, "body": body, "created_at": "2026-10-30T11:00:00Z",
		"html_url": "https://github.com/o/r/issues/7#issuecomment-" + strconv.FormatInt(id, 10), "user": user}
}

// pre7 is a Pre with issue 7 in Controller.
func pre7() Pre {
	return Pre{Repo: testRepo, Items: []Item{{Issue: 7, Title: "Issue", State: "OPEN", ItemID: "I7", Status: "Controller"}}}
}

var cet = time.FixedZone("CET", 3600)

func TestCompleteDropsUntrustedComments(t *testing.T) {
	srv, c := client(t)
	srv.Roles = map[string]string{"owner": "admin", "stranger": "read"}
	srv.Comments = map[int][]map[string]any{7: {
		comment(1, "owner", "🤖 Vraag\n\nWelke kant op?"),
		comment(2, "stranger", "ja, doe maar"),
		comment(3, "stranger", "🤖 Handoff\nnext: /lighthouse-start-issue 7"),
	}}
	var log bytes.Buffer
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, cet)
	snap, err := Complete(context.Background(), c, pre7(), NewTrust(c, defaultAllowed), "lh-bot", &log, now)
	if err != nil {
		t.Fatal(err)
	}
	wantComments := []Comment{{ID: 1, Author: "owner", Body: "🤖 Vraag\n\nWelke kant op?",
		CreatedAt: "2026-10-30T11:00:00Z", URL: "https://github.com/o/r/issues/7#issuecomment-1"}}
	if len(snap.Items) != 1 || !reflect.DeepEqual(snap.Items[0].Comments, wantComments) {
		t.Fatalf("Items = %+v, want issue 7 with comments %+v", snap.Items, wantComments)
	}
	if st := protocol.Classify(snap.Items[0].ProtocolComments()).State; st != protocol.StateQuestion {
		t.Errorf("Classify state = %v, want StateQuestion", st)
	}
	wantLog := "lighthouse: o/r#7: ignored 🤖-comment https://github.com/o/r/issues/7#issuecomment-3 by stranger: role not in answer_permissions\n"
	if log.String() != wantLog {
		t.Errorf("log = %q, want %q", log.String(), wantLog)
	}
	if !snap.GatheredAt.Equal(now) || snap.GatheredAt.Location() != time.UTC {
		t.Errorf("GatheredAt = %v, want %v in UTC", snap.GatheredAt, now)
	}
	if snap.Version != 1 || snap.Repo != "o/r" || snap.TokenLogin != "lh-bot" {
		t.Errorf("Version, Repo, TokenLogin = %d, %q, %q; want 1, o/r, lh-bot", snap.Version, snap.Repo, snap.TokenLogin)
	}
}

func TestCompleteDeletedAuthorIsUntrusted(t *testing.T) {
	srv, c := client(t)
	srv.RoleStatus = http.StatusInternalServerError // any permission request would fail Complete
	srv.Comments = map[int][]map[string]any{7: {comment(1, "", "ja, doe maar")}}
	snap, err := Complete(context.Background(), c, pre7(), NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Items[0].Comments; len(got) != 0 {
		t.Errorf("Comments = %+v, want none", got)
	}
	if n := srv.Count("/repos/o/r/collaborators//permission"); n != 0 {
		t.Errorf("permission requests = %d, want 0", n)
	}
}

func TestRocketOnlyFromTokenLogin(t *testing.T) {
	srv, c := client(t)
	srv.Roles = map[string]string{"owner": "admin"}
	srv.Comments = map[int][]map[string]any{7: {
		comment(11, "owner", "A"),
		comment(12, "owner", "B"),
		comment(13, "owner", "C"),
	}}
	srv.Rockets = map[int64][]string{11: {"someone"}, 12: {"someone", "lh-bot"}}
	snap, err := Complete(context.Background(), c, pre7(), NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got []bool
	for _, cm := range snap.Items[0].Comments {
		got = append(got, cm.Rocket)
	}
	if want := []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rocket per comment = %v, want %v", got, want)
	}
	if n := srv.Count("/repos/o/r/issues/comments/13/reactions"); n != 0 {
		t.Errorf("reaction requests for comment 13 = %d, want 0 (no 🚀)", n)
	}
	var rockets []int
	for _, pc := range snap.Items[0].ProtocolComments() {
		rockets = append(rockets, pc.Rockets)
	}
	if want := []int{0, 1, 0}; !reflect.DeepEqual(rockets, want) {
		t.Errorf("ProtocolComments Rockets = %v, want %v", rockets, want)
	}
}

func TestCompleteFailsOnPermissionError(t *testing.T) {
	srv, c := client(t)
	srv.RoleStatus = http.StatusInternalServerError
	srv.Comments = map[int][]map[string]any{7: {comment(1, "owner", "ja")}}
	if _, err := Complete(context.Background(), c, pre7(), NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now()); err == nil {
		t.Fatal("Complete with a 500 on the permission route gave no error")
	}
}

func TestCompleteWritesEmptyListsAsArrays(t *testing.T) {
	_, c := client(t)
	for name, tc := range map[string]struct {
		pre  Pre
		want []string
	}{
		"empty board": {Pre{Repo: testRepo}, []string{`"items": []`, `"open_runs": []`}},
		"bare item":   {pre7(), []string{`"blocked_by": []`, `"prs": []`, `"comments": []`, `"open_runs": []`}},
	} {
		t.Run(name, func(t *testing.T) {
			snap, err := Complete(context.Background(), c, tc.pre, NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(snap, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !bytes.Contains(data, []byte(w)) {
					t.Errorf("snapshot JSON lacks %s:\n%s", w, data)
				}
			}
			if bytes.Contains(data, []byte("null")) {
				t.Errorf("snapshot JSON has a null:\n%s", data)
			}
		})
	}
}

func TestReadSnapshotChecksVersion(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"version": 1, "repo": "o/r", "token_login": "lh-bot",
		"gathered_at": "2026-10-31T11:00:00Z", "items": [{"issue": 7, "status": "Controller"}], "open_runs": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := ReadSnapshot(good)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Repo != "o/r" || len(snap.Items) != 1 || snap.Items[0].Issue != 7 || snap.Items[0].Status != "Controller" {
		t.Errorf("ReadSnapshot = %+v, want repo o/r with issue 7 in Controller", snap)
	}
	for name, body := range map[string]string{"v2.json": `{"version": 2}`, "none.json": `{}`} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadSnapshot(path); err == nil {
			t.Errorf("ReadSnapshot(%s) gave no error", body)
		}
	}
}

func preWithRun(prs ...forge.PR) Pre {
	p := pre7()
	p.Items[0].PRs = prs
	p.OpenRuns = []runs.OpenRun{{ID: 5, Issue: 7}}
	return p
}

func readyPR(n int) forge.PR {
	return forge.PR{Number: n, State: "OPEN", Mergeable: "MERGEABLE", Checks: "SUCCESS"}
}

func TestCompleteMeasuresRoborev(t *testing.T) {
	srv, c := client(t)
	srv.Roles = map[string]string{"owner": "admin"}
	pending := readyPR(13)
	pending.Checks = "PENDING"
	srv.Comments = map[int][]map[string]any{12: {
		comment(1, "github-actions[bot]", review("abc1234def", "Changes require fixes")),
	}, 13: {
		comment(2, "github-actions[bot]", review("abc1234def", "Changes require fixes")),
	}}
	pre := preWithRun(readyPR(12), pending)
	snap, err := Complete(context.Background(), c, pre, NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	prs := snap.Items[0].PRs
	if prs[0].Roborev != forge.RoborevOpen || prs[1].Roborev != "" {
		t.Errorf("Roborev = %q, %q; want open, empty", prs[0].Roborev, prs[1].Roborev)
	}
	if got := pre.Items[0].PRs[0].Roborev; got != "" {
		t.Errorf("Pre PR Roborev = %q, want it untouched", got)
	}
}

func TestCompleteReadsPRCommentsOnlyWhenMergeReady(t *testing.T) {
	conflicting := readyPR(12)
	conflicting.Mergeable = "CONFLICTING"
	draft := readyPR(12)
	draft.Draft = true
	noRun := preWithRun(readyPR(12))
	noRun.OpenRuns = nil
	cases := map[string]Pre{"no open run": noRun, "conflicting": preWithRun(conflicting), "draft": preWithRun(draft)}
	for name, pre := range cases {
		t.Run(name, func(t *testing.T) {
			srv, c := client(t)
			if _, err := Complete(context.Background(), c, pre, NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now()); err != nil {
				t.Fatal(err)
			}
			if n := srv.Count("/repos/o/r/issues/12/comments"); n != 0 {
				t.Errorf("PR comment requests = %d, want 0", n)
			}
		})
	}
}

// Classification: io (fake forge server). Fails if the gather stops reading the PRs of the branch
// of an open scheduled run, which has no board item: Decide would never see its PR, and the run
// would only end at pr_timeout. A scheduled run without a branch costs no request.
func TestPrepareReadsThePRsOfScheduledRuns(t *testing.T) {
	srv, c := client(t)
	srv.BoardNodes = []map[string]any{node("I1", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 1, nil))}
	branch := "scheduled/codebase-map-20261031T0400-1"
	srv.Branches = map[string][]map[string]any{branch: {prNode(31, branch, nil)}}
	open := []runs.OpenRun{{ID: 1, Schedule: "codebase-map", Branch: branch}, {ID: 2, Schedule: "reflect"}}
	pre, err := Prepare(context.Background(), c, testRepo, open, nil, ToDoProbe{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]forge.PR{branch: {{Number: 31, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "sha" + branch, HeadRef: branch}}}
	if !reflect.DeepEqual(pre.ScheduledPRs, want) {
		t.Errorf("ScheduledPRs = %+v\nwant %+v", pre.ScheduledPRs, want)
	}
	if n := srv.Count("/repos/o/r/pulls?head=" + branch); n != 1 {
		t.Errorf("BranchPRs requests = %d, want 1", n)
	}
	if len(pre.Items) != 1 || len(pre.Items[0].PRs) != 0 {
		t.Errorf("items = %+v, want issue 1 without the scheduled PR", pre.Items)
	}
}

// Fails if Complete leaves the scheduled PRs out of the snapshot, or does not measure the roborev
// state of a merge-ready one: the merge rule must not merge over an open review.
func TestCompleteCarriesScheduledPRsWithRoborev(t *testing.T) {
	srv, c := client(t)
	srv.Roles = map[string]string{"owner": "admin"}
	srv.Comments = map[int][]map[string]any{12: {comment(1, "github-actions[bot]", review("abc1234def", "Changes require fixes"))}}
	pre := pre7()
	pre.ScheduledPRs = map[string][]forge.PR{"scheduled/x-1": {readyPR(12)}}
	snap, err := Complete(context.Background(), c, pre, NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if prs := snap.ScheduledPRs["scheduled/x-1"]; len(prs) != 1 || prs[0].Roborev != forge.RoborevOpen {
		t.Errorf("scheduled PRs = %+v, want #12 with an open roborev review", snap.ScheduledPRs)
	}
	if pre.ScheduledPRs["scheduled/x-1"][0].Roborev != "" {
		t.Error("Complete changed the PRs of Pre")
	}
}

// prCommentsFail is a forge of the given kind whose Comments fails for any number but the issue.
type prCommentsFail struct {
	forge.Forge
	kind  string
	issue int
}

func (f prCommentsFail) Kind() string { return f.kind }

func (f prCommentsFail) Comments(_ context.Context, number int) ([]protocol.Comment, error) {
	if number != f.issue {
		return nil, errors.New("comments of a merge request number read the notes of another issue")
	}
	return nil, nil
}

// On GitLab a merge request is no issue, so its number must not be read as an issue's comments.
func TestCompleteSkipsPRCommentsOnGitLab(t *testing.T) {
	pre := preWithRun(readyPR(12))
	f := prCommentsFail{kind: "gitlab", issue: 7}
	snap, err := Complete(context.Background(), f, pre, NewTrust(f, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
	if err != nil {
		t.Fatalf("Complete on gitlab: %v; want the PR comments skipped", err)
	}
	if got := snap.Items[0].PRs[0].Roborev; got != "" {
		t.Errorf("Roborev = %q, want the zero value", got)
	}
	// The same merge-ready PR on GitHub does read them, so the test fails on a dropped Kind check.
	f.kind = "github"
	if _, err := Complete(context.Background(), f, pre, NewTrust(f, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now()); err == nil {
		t.Error("Complete on github skipped the PR comments, want them read")
	}
}

// gitlabPrepare opens a GitLab client on a fake of project g/p, whose lifecycle holds the
// statuses of the board, and returns it with the repo that carries that board.
func gitlabPrepare(t *testing.T) (*gitlabtest.Server, forge.Forge, repoconfig.Repo) {
	t.Helper()
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = "g/p"
	srv.SetStatuses("To do", "Done")
	board := &repoconfig.Board{StatusOptions: map[string]string{"Controller": "To do", "Done": "Done"}}
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: srv.Project, Token: "secret-token", BaseURL: srv.URL(), Board: board})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return srv, f, repoconfig.Repo{Slug: "g/p", Kind: "gitlab", Board: board}
}

func gitlabIssue(n int, state string, updated time.Time) map[string]any {
	return map[string]any{"iid": n, "id": 9000 + n, "title": "Issue", "state": state,
		"labels": []string{}, "updated_at": updated.UTC().Format(time.RFC3339)}
}

// A closed issue whose last update is older than the closed window of BoardItems is not in
// its list. With an open run it must still reach the tick, as CLOSED, so the run closes.
func TestPrepareClosesRunOutsideBoardWindow(t *testing.T) {
	srv, f, repo := gitlabPrepare(t)
	srv.AddIssue(gitlabIssue(4, "closed", time.Now().Add(-60*24*time.Hour)))
	srv.AddIssue(gitlabIssue(5, "closed", time.Now().Add(-60*24*time.Hour))) // no open run: stays out
	open := []runs.OpenRun{{ID: 15, Issue: 4}}
	pre, err := Prepare(context.Background(), f, repo, open, nil, ToDoProbe{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(pre.Items) != 1 || pre.Items[0].Issue != 4 || pre.Items[0].State != "CLOSED" {
		t.Fatalf("Items = %+v, want only issue 4, CLOSED", pre.Items)
	}
	if _, ok := pre.Statuses[4]; !ok {
		t.Errorf("Statuses = %v, want issue 4 in it", pre.Statuses)
	}
}

// issueItemFake is a forge whose BoardItems is empty and whose IssueItem answers as set.
type issueItemFake struct {
	forge.Forge
	err   error
	calls int
}

func (f *issueItemFake) BoardItems(context.Context) ([]forge.BoardItem, error) { return nil, nil }

func (f *issueItemFake) IssueItem(context.Context, int) (forge.BoardItem, bool, error) {
	f.calls++
	return forge.BoardItem{}, false, f.err
}

// An open run on an issue that no longer exists, on any forge, must not fail the tick of the
// whole repo; any other IssueItem error does.
func TestPrepareIssueItemErrors(t *testing.T) {
	open := []runs.OpenRun{{ID: 15, Issue: 99}}
	f := &issueItemFake{err: fmt.Errorf("forge: issue o/r#99: %w", forge.ErrIssueNotFound)}
	pre, err := Prepare(context.Background(), f, testRepo, open, nil, ToDoProbe{})
	if err != nil || len(pre.Items) != 0 || len(pre.OpenRuns) != 1 || f.calls != 1 {
		t.Errorf("Prepare with a missing issue = %+v, %v, %d calls; want no error, no item and the run kept", pre, err, f.calls)
	}
	boom := errors.New("boom")
	f = &issueItemFake{err: boom}
	if _, err := Prepare(context.Background(), f, testRepo, open, nil, ToDoProbe{}); !errors.Is(err, boom) {
		t.Errorf("Prepare with a failing IssueItem = %v; want the error", err)
	}
}

// On GitHub every issue of a run is on the board, so IssueItem only meets an issue that left
// it. A deleted or transferred issue (GitHub answers issue null plus NOT_FOUND) and an archived
// item (BoardItems lists none) both stay out, and the tick goes on, as before IssueItem existed.
func TestPrepareGitHubIssueOffBoard(t *testing.T) {
	cases := []struct {
		name  string
		setup func(srv *forgetest.Server)
	}{
		{"deleted issue", func(*forgetest.Server) {}},
		{"archived item", func(srv *forgetest.Server) {
			archived := node("I7", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 7, nil))
			archived["isArchived"] = true
			srv.BoardNodes = append(srv.BoardNodes, archived)
			srv.Issues = map[int]map[string]any{7: {"number": 7}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := client(t)
			srv.ProjectID = testRepo.Board.ProjectID
			srv.BoardNodes = []map[string]any{node("I2", map[string]string{"F_status": "opt-ctrl"}, issue("o/r", 2, nil))}
			tc.setup(srv)
			open := []runs.OpenRun{{ID: 15, Issue: 7}}
			pre, err := Prepare(context.Background(), c, testRepo, open, nil, ToDoProbe{})
			if err != nil {
				t.Fatalf("Prepare: %v; want the tick to go on", err)
			}
			if len(pre.Items) != 1 || pre.Items[0].Issue != 2 {
				t.Errorf("Items = %+v, want only issue 2", pre.Items)
			}
			if _, ok := pre.Statuses[7]; ok {
				t.Errorf("Statuses = %v, want no issue 7", pre.Statuses)
			}
			if n := srv.Count("IssueItems"); n != 1 {
				t.Errorf("Count(IssueItems) = %d, want 1", n)
			}
		})
	}
}

// Every PR gets the SHA of the newest pusher comment (the build route's handoff or the fix-pr
// route's note) whose branch is that PR's head: a push of another branch never counts, and a
// comment of an untrusted author or a run's own handoff (with a session footer) is no push.
func TestGatherPushedSHAFromPusherComment(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 40) }
	base := sha("0")
	srv, c := client(t)
	srv.Roles = map[string]string{"owner": "admin", "stranger": "read"}
	srv.Comments = map[int][]map[string]any{7: {
		comment(1, "owner", protocol.RenderPushed(7, "/w/a", "issue-7-a", base+".."+sha("1"), "w", "/lighthouse-write-pr 7", "", "nl")),
		comment(2, "owner", protocol.RenderPushNote("issue-7-a", sha("1")+".."+sha("2"), "w", "nl")),
		comment(3, "owner", protocol.RenderPushNote("issue-7-b", base+".."+sha("3"), "w", "en")),
		comment(4, "stranger", protocol.RenderPushNote("issue-7-a", base+".."+sha("4"), "w", "nl")),
		comment(5, "owner", protocol.RenderPushed(7, "/w/a", "issue-7-a", base+".."+sha("5"), "w", "/lighthouse-write-pr 7", "", "nl")+
			"\n\n"+protocol.Footer("11111111-2222-4333-8444-555555555555")),
	}}
	a, b, other := readyPR(12), readyPR(13), readyPR(14)
	a.HeadRef, b.HeadRef, other.HeadRef = "issue-7-a", "issue-7-b", "issue-7-c"
	pre := preWithRun(a, b, other)
	snap, err := Complete(context.Background(), c, pre, NewTrust(c, defaultAllowed), "lh-bot", &bytes.Buffer{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]string{}
	for _, pr := range snap.Items[0].PRs {
		got[pr.Number] = pr.PushedSHA
	}
	want := map[int]string{12: sha("2"), 13: sha("3"), 14: ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PushedSHA per PR = %v, want %v", got, want)
	}
	if pre.Items[0].PRs[0].PushedSHA != "" {
		t.Error("Complete changed the PRs of Pre")
	}
}

// The PRs of an issue stay on its issue branch after a close check: the check's own branch
// has no PR, so a Refs PR merged on the issue branch must not drop out of the next gather.
func TestIssueBranchesSkipsCloseCheckAndScheduled(t *testing.T) {
	history := []runs.Row{
		{ID: 1, Issue: 7, Branch: "issue-7-eerste"},
		{ID: 2, Issue: 7, Branch: "issue-7-bouw-de-keten"},
		{ID: 3, Issue: 7, Trigger: "closecheck", Branch: "closecheck/issue-7-pr-12"},
		{ID: 4, Issue: 8, Branch: "issue-8-ander"},
		{ID: 5, Schedule: "map-refresh", Branch: "scheduled/map-refresh-1"},
	}
	got := IssueBranches(history)
	want := map[int]string{7: "issue-7-bouw-de-keten", 8: "issue-8-ander"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IssueBranches = %v, want %v", got, want)
	}
}

// toDoRepo is testRepo with a To do option and the label names of testLabels.
func toDoRepo(c *forge.GitHub) repoconfig.Repo {
	repo := testRepo
	board := *testRepo.Board
	board.StatusOptions = map[string]string{"Controller": "opt-ctrl", "On hold": "opt-hold", "Done": "opt-done", "To do": "opt-todo"}
	repo.Board, repo.Labels = &board, testLabels
	c.Board = &board
	return repo
}

// labelled is issue content with the given label names.
func labelled(names ...string) map[string]any {
	var nodes []any
	for _, n := range names {
		nodes = append(nodes, map[string]any{"name": n})
	}
	return map[string]any{"labels": map[string]any{"nodes": nodes}}
}

const parkedBody = "🤖 Geparkeerd\nblocked_by: o/r#5"

// probe is a ToDoProbe with token login lh-bot and the default trust over c.
func probe(c forge.Forge, known map[int]ToDoMark) ToDoProbe {
	return ToDoProbe{Known: known, TokenLogin: func() (string, error) { return "lh-bot", nil }, Trust: NewTrust(c, defaultAllowed)}
}

// toDoBoard is a board of To do items: #1 with the tick's own Geparkeerd last, #2 with a human's
// Handoff after it, #3 own Geparkeerd but needs_hands, #4 own Geparkeerd followed by a fake
// 🤖-comment of an untrusted stranger, #5 with a Geparkeerd of the trusted owner, #6 an
// unlabelled backlog item without any comment, #7 own Geparkeerd but closed.
func toDoBoard(srv *forgetest.Server) {
	todo := map[string]string{"F_status": "opt-todo"}
	srv.BoardNodes = []map[string]any{
		node("I1", todo, issue("o/r", 1, nil)),
		node("I2", todo, issue("o/r", 2, nil)),
		node("I3", todo, issue("o/r", 3, labelled("needs_hands"))),
		node("I4", todo, issue("o/r", 4, nil)),
		node("I5", todo, issue("o/r", 5, nil)),
		node("I6", todo, issue("o/r", 6, nil)),
		node("I7", todo, issue("o/r", 7, map[string]any{"state": "CLOSED"})),
	}
	handoff := "🤖 Handoff\nissue: #2\nworktree: /w\nbranch: issue-2-x\nanchor: abc\nnext: /lighthouse-start-issue 2\nwhen: now"
	srv.Roles = map[string]string{"lh-bot": "admin", "owner": "admin", "stranger": "read"}
	srv.Comments = map[int][]map[string]any{
		1: {comment(10, "owner", "een notitie"), comment(11, "lh-bot", parkedBody)},
		2: {comment(20, "lh-bot", parkedBody), comment(21, "owner", handoff)},
		3: {comment(30, "lh-bot", parkedBody)},
		4: {comment(40, "lh-bot", parkedBody), comment(41, "stranger", handoff)},
		5: {comment(50, "owner", parkedBody)},
		7: {comment(70, "lh-bot", parkedBody)},
	}
}

// Prepare keeps a To do item only when its last trusted protocol comment is the tick's own
// Geparkeerd, and reads no comments of a needs_* or closed item. Treating every To do item as
// parked keeps #2, #5 and #6; letting an untrusted 🤖-comment count drops #4.
func TestPrepareReadsToDoWithOwnParked(t *testing.T) {
	srv, c := client(t)
	repo := toDoRepo(c)
	toDoBoard(srv)
	pre, err := Prepare(context.Background(), c, repo, nil, nil, probe(c, nil))
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, it := range pre.Items {
		got = append(got, it.Issue)
	}
	if want := []int{1, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("Items = %v, want %v", got, want)
	}
	for _, n := range []int{3, 7} {
		if k := "/repos/o/r/issues/" + strconv.Itoa(n) + "/comments"; srv.Count(k) != 0 {
			t.Errorf("comments of #%d read %d times, want none", n, srv.Count(k))
		}
	}
	if len(pre.ToDo) != 5 || !pre.ToDo[1].OwnParked || pre.ToDo[2].OwnParked || pre.ToDo[6].OwnParked {
		t.Errorf("ToDo = %+v, want marks of #1, #2, #4, #5, #6 with only #1 and #4 parked", pre.ToDo)
	}
}

// The budget of review finding r3-f2: Prepare runs every tick, so on an unchanged board it reads
// no comments of a To do item at all, also for the unlabelled backlog; a To do item that changed
// (a new comment moves its IssueUpdatedAt) is read once. Dropping the Known check in markToDo
// turns the second tick red: five reads instead of none.
func TestPrepareToDoCommentsNotReadWhenUnchanged(t *testing.T) {
	srv, c := client(t)
	repo := toDoRepo(c)
	toDoBoard(srv)
	reads := func() int {
		n := 0
		for i := 1; i <= 7; i++ {
			n += srv.Count("/repos/o/r/issues/" + strconv.Itoa(i) + "/comments")
		}
		return n
	}
	first, err := Prepare(context.Background(), c, repo, nil, nil, probe(c, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := reads(); got != 5 {
		t.Fatalf("first tick read %d comment lists, want 5 (#1, #2, #4, #5, #6)", got)
	}
	second, err := Prepare(context.Background(), c, repo, nil, nil, probe(c, first.ToDo))
	if err != nil {
		t.Fatal(err)
	}
	if got := reads() - 5; got != 0 {
		t.Errorf("unchanged board: %d extra Comments calls, want 0", got)
	}
	if len(second.Items) != 2 || second.Items[0].Issue != 1 || second.Items[1].Issue != 4 || !SameToDo(first.ToDo, second.ToDo) {
		t.Errorf("second tick: Items %+v, ToDo %+v; want #1 and #4 kept from the marks", second.Items, second.ToDo)
	}
	srv.BoardNodes[5]["content"].(map[string]any)["updatedAt"] = "2026-10-30T12:00:00Z" // #6 got a comment
	if _, err := Prepare(context.Background(), c, repo, nil, nil, probe(c, second.ToDo)); err != nil {
		t.Fatal(err)
	}
	if got := reads() - 5; got != 1 || srv.Count("/repos/o/r/issues/6/comments") != 2 {
		t.Errorf("after #6 changed: %d extra Comments calls, want 1 for #6", got)
	}
}

// Without a token login Prepare cannot judge a To do item and says so; it never guesses.
func TestPrepareToDoWithoutLoginFails(t *testing.T) {
	srv, c := client(t)
	repo := toDoRepo(c)
	toDoBoard(srv)
	if _, err := Prepare(context.Background(), c, repo, nil, nil, ToDoProbe{}); err == nil {
		t.Error("Prepare: err = nil, want an error for the missing token login")
	}
}
