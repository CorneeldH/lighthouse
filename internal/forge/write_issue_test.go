package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

func milestoneFixture(number int, title string) map[string]any {
	return map[string]any{"number": number, "title": title, "state": "open", "description": "d" + title}
}

// writes are the recorded non-GET requests, as "METHOD path".
func writes(srv *forgetest.Server) []string {
	var out []string
	for _, p := range srv.Posts {
		out = append(out, p.Method+" "+p.Path)
	}
	return out
}

func TestGitHubCreateIssueResolvesMilestoneByTitle(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{milestoneFixture(2, "M2"), milestoneFixture(3, "M3")}
	got, err := c.CreateIssue(context.Background(), NewIssue{Title: "Nieuw", Body: "Tekst", Labels: []string{"bug"}, Milestone: "M3"})
	if err != nil {
		t.Fatal(err)
	}
	post := srv.Posts[len(srv.Posts)-1]
	if post.Path != "/repos/o/r/issues" || post.Body["milestone"] != float64(3) || post.Body["title"] != "Nieuw" {
		t.Errorf("POST = %+v, want milestone 3 and title Nieuw on /repos/o/r/issues", post)
	}
	if got.Title != "Nieuw" || got.Milestone == nil || got.Milestone.Title != "M3" || got.Milestone.Number != 3 {
		t.Errorf("issue = %+v, want title Nieuw in milestone M3 (3)", got)
	}
	if !reflect.DeepEqual(got.Labels, []string{"bug"}) {
		t.Errorf("labels = %v, want [bug]", got.Labels)
	}
}

func TestGitHubEditIssueAddsAndRemovesLabels(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{7: fieldIssue(7, "open")}
	got, err := c.EditIssue(context.Background(), 7, IssueEdit{AddLabels: []string{"bug"}, RemoveLabels: []string{"area-x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /repos/o/r/issues/7/labels", "DELETE /repos/o/r/issues/7/labels/area-x"}
	if g := writes(srv); !reflect.DeepEqual(g, want) {
		t.Errorf("writes = %v, want %v (no PATCH for labels only)", g, want)
	}
	if !reflect.DeepEqual(got.Labels, []string{"needs-mind", "bug"}) {
		t.Errorf("labels = %v, want [needs-mind bug]", got.Labels)
	}
}

// A 404 on the DELETE counts as "already off" only when the read afterwards agrees: a 404 while
// the label stays (such as a mismatch in how the name is encoded) is an error, never a release.
// GitHub compares label names without regard to case, so "Area-X" still on as "area-x" counts too.
func TestGitHubEditIssueErrorsWhenARemovedLabelStays(t *testing.T) {
	for _, l := range []string{"area-x", "Area-X"} {
		t.Run(l, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.Issues = map[int]map[string]any{7: fieldIssue(7, "open")}
			srv.Fail = map[string]int{"DELETE /repos/o/r/issues/7/labels/" + l: http.StatusNotFound}
			_, err := c.EditIssue(context.Background(), 7, IssueEdit{RemoveLabels: []string{l}})
			if err == nil || !strings.Contains(err.Error(), `"`+l+`" is still on #7`) {
				t.Fatalf("EditIssue = %v, want an error that label %q is still on #7", err, l)
			}
		})
	}
}

func TestGitHubEditIssuePatchesTitleBodyMilestone(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{7: fieldIssue(7, "open")}
	srv.Milestones = []map[string]any{milestoneFixture(3, "M3")}
	title, ms := "Anders", "M3"
	got, err := c.EditIssue(context.Background(), 7, IssueEdit{Title: &title, Milestone: &ms})
	if err != nil {
		t.Fatal(err)
	}
	post := srv.Posts[len(srv.Posts)-1]
	if post.Method != "PATCH" || post.Body["title"] != "Anders" || post.Body["milestone"] != float64(3) {
		t.Errorf("last write = %+v, want PATCH with title and milestone 3", post)
	}
	if _, has := post.Body["body"]; has {
		t.Errorf("PATCH carries body %v, want it left out when IssueEdit.Body is nil", post.Body["body"])
	}
	if got.Title != "Anders" || got.Milestone == nil || got.Milestone.Title != "M3" {
		t.Errorf("issue = %+v, want title Anders in M3", got)
	}
}

func TestGitHubEditIssueEmptyMilestoneClearsIt(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{7: fieldIssue(7, "open")}
	none := ""
	got, err := c.EditIssue(context.Background(), 7, IssueEdit{Milestone: &none})
	if err != nil {
		t.Fatal(err)
	}
	post := srv.Posts[len(srv.Posts)-1]
	if v, has := post.Body["milestone"]; !has || v != nil {
		t.Errorf("PATCH body = %v, want milestone: null", post.Body)
	}
	if got.Milestone != nil {
		t.Errorf("milestone = %+v, want none", got.Milestone)
	}
}

func TestGitHubMilestoneTitleAmbiguous(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{milestoneFixture(2, "M3"), milestoneFixture(5, "M3")}
	_, err := c.CreateIssue(context.Background(), NewIssue{Title: "x", Milestone: "M3"})
	var amb *AmbiguousMilestone
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want *AmbiguousMilestone", err)
	}
	if amb.Title != "M3" || !reflect.DeepEqual(amb.Numbers, []int{2, 5}) {
		t.Errorf("ambiguous = %+v, want M3 with [2 5]", amb)
	}
	if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "5") {
		t.Errorf("message %q does not name both numbers", err)
	}
	if len(srv.Posts) != 0 {
		t.Errorf("writes = %v, want none after an ambiguous title", writes(srv))
	}
}

func TestGitHubMilestoneUnknown(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{milestoneFixture(2, "M2")}
	_, err := c.CreateIssue(context.Background(), NewIssue{Title: "x", Milestone: "M9"})
	if !errors.Is(err, ErrMilestoneNotFound) {
		t.Fatalf("err = %v, want ErrMilestoneNotFound", err)
	}
	if !strings.Contains(err.Error(), "M9") {
		t.Errorf("message %q does not name M9", err)
	}
}

func TestGitHubMilestonesFilterByState(t *testing.T) {
	srv, c := boardClient(t)
	closed := milestoneFixture(1, "M1")
	closed["state"] = "closed"
	srv.Milestones = []map[string]any{closed, milestoneFixture(2, "M2")}
	got, err := c.Milestones(context.Background(), "open")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Milestone{{Number: 2, Title: "M2", State: "open", Description: "dM2"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("open = %+v, want %+v", got, want)
	}
	all, err := c.Milestones(context.Background(), "all")
	if err != nil || len(all) != 2 {
		t.Errorf("all = %+v, %v; want 2 milestones", all, err)
	}
}

func TestGitHubMilestoneCreateEditAndRead(t *testing.T) {
	srv, c := boardClient(t)
	ctx := context.Background()
	m, err := c.CreateMilestone(ctx, "M7", "Eerste")
	if err != nil {
		t.Fatal(err)
	}
	if m != (Milestone{Number: 1, Title: "M7", State: "open", Description: "Eerste"}) {
		t.Errorf("created = %+v", m)
	}
	desc := "Tweede"
	m, err = c.EditMilestone(ctx, 1, nil, &desc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "M7" || m.Description != "Tweede" {
		t.Errorf("edited = %+v, want title kept, description Tweede", m)
	}
	if body := srv.Posts[len(srv.Posts)-1].Body; len(body) != 1 || body["description"] != "Tweede" {
		t.Errorf("PATCH body = %v, want only description", body)
	}
	got, err := c.MilestoneByNumber(ctx, 1)
	if err != nil || got != m {
		t.Errorf("MilestoneByNumber = %+v, %v; want %+v", got, err, m)
	}
}

func TestGitHubLabelsFollowsPages(t *testing.T) {
	srv, c := boardClient(t)
	for i := range 101 {
		srv.Labels = append(srv.Labels, fmt.Sprintf("l%03d", i))
	}
	got, err := c.Labels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 || got[0] != "l000" || got[100] != "l100" {
		t.Errorf("labels: %d, first %q, last %q; want 101, l000, l100", len(got), got[0], got[len(got)-1])
	}
}

func TestGitHubAddBlockerSendsDatabaseID(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{
		7: fieldIssue(7, "open"),
		5: {"number": 5, "id": 9005, "title": "Blocker", "state": "open"},
	}
	if err := c.AddBlocker(context.Background(), 7, 5); err != nil {
		t.Fatal(err)
	}
	post := srv.Posts[len(srv.Posts)-1]
	if post.Path != "/repos/o/r/issues/7/dependencies/blocked_by" || post.Body["issue_id"] != float64(9005) {
		t.Errorf("POST = %+v, want issue_id 9005 as a number on issue 7", post)
	}
	got, err := c.BlockedBy(context.Background(), 7)
	if err != nil || len(got) != 1 || got[0].Issue != 5 {
		t.Errorf("BlockedBy = %+v, %v; want blocker 5", got, err)
	}
}

func TestGitHubAddBlockerRefusedKeepsMessage(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{
		7: fieldIssue(7, "open"),
		5: {"number": 5, "id": 9005, "title": "Blocker", "state": "open"},
	}
	srv.Fail = map[string]int{"POST /repos/o/r/issues/7/dependencies/blocked_by": 422}
	srv.FailMessage = map[string]string{"POST /repos/o/r/issues/7/dependencies/blocked_by": "Issue dependencies are not enabled"}
	err := c.AddBlocker(context.Background(), 7, 5)
	if err == nil || !strings.Contains(err.Error(), "Issue dependencies are not enabled") {
		t.Errorf("err = %v, want the API message", err)
	}
}

func TestGitHubListIssuesFollowsPages(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{}
	for i := 1; i <= 101; i++ {
		srv.Issues[i] = map[string]any{"number": i, "title": "t", "state": "open"}
	}
	pr := map[string]any{"number": 500, "title": "pr", "state": "open", "pull_request": map[string]any{"url": "x"}}
	srv.Issues[500] = pr
	got, err := c.ListIssues(context.Background(), IssueFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 {
		t.Errorf("issues = %d, want 101 (every page, no pull request)", len(got))
	}
}

func TestGitHubListIssuesFilters(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{milestoneFixture(3, "M3")}
	withMilestone := fieldIssue(1, "closed") // label area-x, milestone 3
	other := fieldIssue(2, "closed")
	other["milestone"] = map[string]any{"number": 4, "title": "M4", "state": "open"}
	open := fieldIssue(3, "open")
	srv.Issues = map[int]map[string]any{1: withMilestone, 2: other, 3: open}
	got, err := c.ListIssues(context.Background(), IssueFilter{State: "closed", Label: "area-x", Milestone: "M3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("issues = %+v, want only issue 1", got)
	}
	if _, err := c.ListIssues(context.Background(), IssueFilter{State: "bogus"}); err == nil {
		t.Error("state bogus: want an error")
	}
}

func TestGitHubSearchIssuesExcludesPullRequests(t *testing.T) {
	srv, c := boardClient(t)
	pr := fieldIssue(2, "open")
	pr["title"] = "Zoekterm in PR"
	pr["pull_request"] = map[string]any{"url": "x"}
	hit := fieldIssue(1, "open")
	hit["title"] = "Zoekterm hier"
	miss := fieldIssue(3, "open")
	srv.Issues = map[int]map[string]any{1: hit, 2: pr, 3: miss}
	got, err := c.SearchIssues(context.Background(), "zoekterm")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("issues = %+v, want only issue 1", got)
	}
}

func TestGitHubChangeStateMerged(t *testing.T) {
	srv, c := boardClient(t)
	srv.Pulls = []map[string]any{
		{"number": 4, "title": "Gemerged", "body": "b", "state": "closed", "merged_at": "2026-11-01T10:00:00Z",
			"html_url": "https://github.com/o/r/pull/4", "created_at": "2026-10-30T09:00:00Z",
			"user": map[string]any{"login": "anna"}, "head": map[string]any{"ref": "feat"}, "base": map[string]any{"ref": "main"},
			"labels": []any{map[string]any{"name": "bug"}}},
		{"number": 5, "title": "Dicht", "state": "closed", "merged_at": nil},
		{"number": 6, "title": "Open", "state": "open"},
	}
	ctx := context.Background()
	got, err := c.Change(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := Change{Number: 4, Title: "Gemerged", Body: "b", State: "merged", Labels: []string{"bug"},
		URL: "https://github.com/o/r/pull/4", CreatedAt: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC),
		MergedAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), Author: "anna", HeadRef: "feat", BaseRef: "main", Mergeable: "unknown"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Change(4) = %+v, want %+v", got, want)
	}
	for n, state := range map[int]string{5: "closed", 6: "open"} {
		if c5, err := c.Change(ctx, n); err != nil || c5.State != state {
			t.Errorf("Change(%d) = %+v, %v; want state %s", n, c5, err, state)
		}
	}
}

func TestGitHubCreateChangeSetsMilestoneViaIssuePatch(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{milestoneFixture(3, "M3")}
	got, err := c.CreateChange(context.Background(), NewPR{Title: "PR", Body: "Closes #1", Head: "feat", Base: "main", Milestone: "M3"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /repos/o/r/pulls", "PATCH /repos/o/r/issues/1"}
	if g := writes(srv); !reflect.DeepEqual(g, want) {
		t.Fatalf("writes = %v, want %v", g, want)
	}
	if srv.Posts[0].Body["head"] != "feat" || srv.Posts[0].Body["base"] != "main" {
		t.Errorf("POST pulls body = %v, want head feat and base main", srv.Posts[0].Body)
	}
	if srv.Posts[1].Body["milestone"] != float64(3) {
		t.Errorf("PATCH body = %v, want milestone 3", srv.Posts[1].Body)
	}
	if got.Number != 1 || got.Milestone == nil || got.Milestone.Title != "M3" || got.HeadRef != "feat" || got.State != "open" {
		t.Errorf("change = %+v, want PR 1 open from feat in M3", got)
	}
}

func TestGitHubCreateChangeKeepsPRWhenMilestonePatchFails(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{milestoneFixture(3, "M3")}
	srv.Fail = map[string]int{"PATCH /repos/o/r/issues/1": http.StatusForbidden}
	got, err := c.CreateChange(context.Background(), NewPR{Title: "PR", Head: "feat", Base: "main", Milestone: "M3"})
	if err == nil || !strings.Contains(err.Error(), "PR #1 created") || !strings.Contains(err.Error(), "milestone") {
		t.Fatalf("err = %v, want it to say PR #1 was created and the milestone failed", err)
	}
	if got.Number != 1 || got.URL != "https://github.com/o/r/pull/1" {
		t.Errorf("change = %+v, want number 1 and its URL next to the error", got)
	}
}

func TestGitHubCreateChangeWithoutMilestoneOnlyPosts(t *testing.T) {
	srv, c := boardClient(t)
	if _, err := c.CreateChange(context.Background(), NewPR{Title: "PR", Head: "feat", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if g := writes(srv); !reflect.DeepEqual(g, []string{"POST /repos/o/r/pulls"}) {
		t.Errorf("writes = %v, want one POST pulls", g)
	}
}

func TestGitHubCreateChangeComment(t *testing.T) {
	srv, c := boardClient(t)
	srv.Login = "lh-bot"
	got, err := c.CreateChangeComment(context.Background(), 4, "Klaar")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "Klaar" || got.Author != "lh-bot" || srv.Posts[0].Path != "/repos/o/r/issues/4/comments" {
		t.Errorf("comment = %+v on %s, want Klaar by lh-bot on issues/4/comments", got, srv.Posts[0].Path)
	}
}

func TestGitHubAPIPostsTypedFields(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "body.md")
	if err := os.WriteFile(file, []byte("uit bestand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.RequestURI(), string(raw)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	out, err := c.API(context.Background(), APIRequest{Method: "POST", Path: "repos/{owner}/{repo}/issues", Fields: []APIField{
		{Name: "n", Value: "3", Typed: true}, {Name: "ok", Value: "true", Typed: true},
		{Name: "body", Value: "@" + file, Typed: true}, {Name: "s", Value: "3"}, {Name: "t", Value: "true"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{\"ok\":true}" {
		t.Errorf("output = %q, want the raw response", out)
	}
	if gotMethod != "POST" || gotPath != "/repos/o/r/issues" {
		t.Errorf("request = %s %s, want POST /repos/o/r/issues", gotMethod, gotPath)
	}
	want := `{"body":"uit bestand\n","n":3,"ok":true,"s":"3","t":"true"}`
	if strings.TrimSpace(gotBody) != want {
		t.Errorf("body = %s, want %s", gotBody, want)
	}
}

func TestGitHubAPIGetPutsFieldsInQuery(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotURI, gotBody = r.Method+" "+r.URL.RequestURI(), string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	out, err := c.API(context.Background(), APIRequest{Method: "GET", Path: "/repos/o/r/issues", Fields: []APIField{{Name: "state", Value: "closed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if gotURI != "GET /repos/o/r/issues?state=closed" || gotBody != "" || len(out) != 0 {
		t.Errorf("request %q body %q out %q; want fields in the query, no body, empty output for a 204", gotURI, gotBody, out)
	}
}

func TestGitHubAPIRefusalKeepsMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"Validation Failed"}`)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	_, err := c.API(context.Background(), APIRequest{Method: "POST", Path: "repos/o/r/issues"})
	if err == nil || !strings.Contains(err.Error(), "Validation Failed") {
		t.Errorf("err = %v, want the API message", err)
	}
}

func TestGitHubAPIFollowsPages(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", `<`+srvURL+`/repos/o/r/labels?state=x&page=2>; rel="next"`)
			_, _ = io.WriteString(w, `[{"n":1},{"n":2}]`)
		case "2":
			_, _ = io.WriteString(w, `[{"n":3}]`)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	out, err := c.API(context.Background(), APIRequest{Method: "GET", Path: "repos/o/r/labels", Fields: []APIField{{Name: "state", Value: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]int
	if err := json.Unmarshal(out, &got); err != nil || !reflect.DeepEqual(got, []map[string]int{{"n": 1}, {"n": 2}, {"n": 3}}) {
		t.Errorf("output = %s (%v), want one list of 3", out, err)
	}
}

func TestGitHubAPIDoesNotFollowPagesOnPOST(t *testing.T) {
	calls := 0
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Link", `<`+srvURL+`/x>; rel="next"`)
		_, _ = io.WriteString(w, `[1]`)
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	if _, err := c.API(context.Background(), APIRequest{Method: "POST", Path: "x"}); err != nil || calls != 1 {
		t.Errorf("err %v, %d calls, want one call and no error", err, calls)
	}
}

func TestGitHubChangeMergeable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pull  map[string]any
		want  string
		draft bool
	}{
		{"true", map[string]any{"mergeable": true}, "mergeable", false},
		{"false", map[string]any{"mergeable": false}, "conflicting", false},
		{"null", map[string]any{"mergeable": nil}, "unknown", false},
		{"absent", map[string]any{}, "unknown", false},
		{"draft", map[string]any{"mergeable": true, "draft": true}, "mergeable", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := boardClient(t)
			pull := map[string]any{"number": 8, "title": "P", "state": "open"}
			for k, v := range tc.pull {
				pull[k] = v
			}
			srv.Pulls = []map[string]any{pull}
			got, err := c.Change(context.Background(), 8)
			if err != nil || got.Mergeable != tc.want || got.Draft != tc.draft {
				t.Errorf("Change(8) = mergeable %q draft %v, %v; want %q %v", got.Mergeable, got.Draft, err, tc.want, tc.draft)
			}
		})
	}
}

// Classification: contract. Fails when CreateLabel treats every 422 as "exists already", or
// matches the listed names case-sensitively: a refused label must be an error, an existing one
// under another case must not.
func TestGitHubCreateLabelUnprocessable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listed  string
		wantErr bool
	}{
		{"area:new", `[{"name":"bug"}]`, true},
		{"Area:New", `[{"name":"area:new"}]`, false},
	} {
		c := peopleServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"message":"Validation Failed"}`)
				return
			}
			_, _ = io.WriteString(w, tc.listed)
		})
		err := c.CreateLabel(context.Background(), tc.name, "0e8a16")
		if (err != nil) != tc.wantErr {
			t.Errorf("CreateLabel(%q) with labels %s: err = %v, want error %v", tc.name, tc.listed, err, tc.wantErr)
		}
	}
}
