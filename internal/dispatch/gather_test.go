package dispatch

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

var testRepo = repoconfig.Repo{Slug: "o/r", Board: &repoconfig.Board{
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
	pre, err := Prepare(context.Background(), c, testRepo, nil)
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
			BlockedBy: []forge.Blocker{{Repo: "o/r", Issue: 5, State: "CLOSED", MergedPR: true}}},
	}
	if !reflect.DeepEqual(pre.Items, want) {
		t.Errorf("Items =\n%+v\nwant\n%+v", pre.Items, want)
	}
	if pre.Repo.Slug != "o/r" {
		t.Errorf("Repo.Slug = %q, want o/r", pre.Repo.Slug)
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
	pre, err := Prepare(context.Background(), c, testRepo, open)
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
	if n := srv.Count("BranchPRs"); n != 1 {
		t.Errorf("BranchPRs requests = %d, want 1 (issue 4 has no open run, issue 6 no branch)", n)
	}
	if !reflect.DeepEqual(pre.OpenRuns, open) {
		t.Errorf("OpenRuns = %+v, want %+v", pre.OpenRuns, open)
	}
}

func TestPrepareWithoutBoardFails(t *testing.T) {
	_, c := client(t)
	if _, err := Prepare(context.Background(), c, repoconfig.Repo{Slug: "o/r"}, nil); err == nil {
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
