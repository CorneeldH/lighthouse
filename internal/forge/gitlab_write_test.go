package forge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

const glBase = "/api/v4/projects/group%2Fproject"

func glClient(t *testing.T) (*gitlabtest.Server, *GitLab) {
	t.Helper()
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	return srv, newGitLab(Config{Kind: "gitlab", Repo: "group/project", Token: "secret-token", BaseURL: srv.URL()})
}

// glMilestones seeds a project milestone (id 501, iid 1) and a group milestone with the same
// iid (id 777), as GitLab does.
func glMilestones(srv *gitlabtest.Server) {
	srv.AddMilestone(map[string]any{"id": 501, "iid": 1, "title": "P1", "state": "active", "description": "dP", "project_id": 1})
	srv.AddMilestone(map[string]any{"id": 777, "iid": 1, "title": "G1", "state": "active", "description": "dG", "group_id": 9})
	srv.AddMilestone(map[string]any{"id": 888, "iid": 2, "title": "Old", "state": "closed", "description": "", "project_id": 1})
}

func lastWrite(t *testing.T, srv *gitlabtest.Server) gitlabtest.Write {
	t.Helper()
	w := srv.Writes()
	if len(w) == 0 {
		t.Fatal("no write request was made")
	}
	return w[len(w)-1]
}

func glIssueFixture(iid int, labels ...string) map[string]any {
	return map[string]any{"iid": iid, "title": "T", "state": "opened", "description": "b", "labels": labels,
		"web_url": "https://gitlab.example/group/project/-/issues/7", "created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-02T03:04:05Z", "author": map[string]any{"username": "kim"}}
}

func TestGitLabCreateIssueMilestoneByTitleInGroup(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	got, err := g.CreateIssue(context.Background(), NewIssue{Title: "Nieuw", Body: "Tekst", Labels: []string{"bug", "x"}, Milestone: "G1"})
	if err != nil {
		t.Fatal(err)
	}
	w := lastWrite(t, srv)
	if w.Method != "POST" || w.Path != glBase+"/issues" {
		t.Errorf("write = %s %s, want POST %s/issues", w.Method, w.Path, glBase)
	}
	want := map[string]any{"title": "Nieuw", "description": "Tekst", "labels": "bug,x", "milestone_id": float64(777)}
	if !reflect.DeepEqual(w.Body, want) {
		t.Errorf("body = %v, want %v", w.Body, want)
	}
	if got.Title != "Nieuw" || got.Milestone == nil || got.Milestone.Number != 777 || got.Milestone.Title != "G1" || got.State != "open" {
		t.Errorf("issue = %+v, want open issue Nieuw in milestone G1 (777)", got)
	}
	if !reflect.DeepEqual(got.Labels, []string{"bug", "x"}) {
		t.Errorf("labels = %v, want [bug x]", got.Labels)
	}
}

func TestGitLabCreateIssueMilestoneTitleErrors(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	srv.AddMilestone(map[string]any{"id": 999, "iid": 3, "title": "G1", "state": "active", "project_id": 1})
	_, err := g.CreateIssue(context.Background(), NewIssue{Title: "x", Milestone: "G1"})
	var amb *AmbiguousMilestone
	if !errors.As(err, &amb) || !reflect.DeepEqual(amb.Numbers, []int{777, 999}) {
		t.Errorf("err = %v, want *AmbiguousMilestone with numbers [777 999]", err)
	}
	_, err = g.CreateIssue(context.Background(), NewIssue{Title: "x", Milestone: "Nope"})
	if !errors.Is(err, ErrMilestoneNotFound) {
		t.Errorf("err = %v, want ErrMilestoneNotFound", err)
	}
	if n := len(srv.Writes()); n != 0 {
		t.Errorf("%d writes, want none after a milestone error", n)
	}
}

func TestGitLabMilestoneNumberIsGlobalID(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	got, err := g.MilestoneByNumber(context.Background(), 777)
	want := Milestone{Number: 777, Title: "G1", State: "open", Description: "dG"}
	if err != nil || got != want {
		t.Errorf("MilestoneByNumber(777) = %+v, %v; want %+v", got, err, want)
	}
	if _, err := g.MilestoneByNumber(context.Background(), 1); !errors.Is(err, ErrMilestoneNotFound) {
		t.Errorf("MilestoneByNumber(1) err = %v, want ErrMilestoneNotFound (1 is an iid, not an id)", err)
	}
	open, err := g.Milestones(context.Background(), "")
	if err != nil || !reflect.DeepEqual(open, []Milestone{{Number: 501, Title: "P1", State: "open", Description: "dP"}, {Number: 777, Title: "G1", State: "open", Description: "dG"}}) {
		t.Errorf("Milestones(open) = %+v, %v; want project and group milestone", open, err)
	}
	closed, err := g.Milestones(context.Background(), "closed")
	if err != nil || !reflect.DeepEqual(closed, []Milestone{{Number: 888, Title: "Old", State: "closed"}}) {
		t.Errorf("Milestones(closed) = %+v, %v", closed, err)
	}
	all, _ := g.Milestones(context.Background(), "all")
	if len(all) != 3 {
		t.Errorf("Milestones(all) = %d, want 3", len(all))
	}
	if !slices.ContainsFunc(srv.Requests(), func(r string) bool {
		return strings.Contains(r, "/milestones?") && strings.Contains(r, "include_ancestors=true") && strings.Contains(r, "state=active")
	}) {
		t.Errorf("requests %v: want a milestones list with include_ancestors=true and state=active", srv.Requests())
	}
}

func TestGitLabEditGroupMilestoneRefused(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	title := "Nieuw"
	_, err := g.EditMilestone(context.Background(), 777, &title, nil, nil)
	var gm *GroupMilestone
	if !errors.As(err, &gm) || gm.Number != 777 {
		t.Errorf("err = %v, want *GroupMilestone 777", err)
	}
	if n := len(srv.Writes()); n != 0 {
		t.Errorf("%d writes, want none for a group milestone", n)
	}
}

func TestGitLabMilestoneCreateAndEditUseGlobalID(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	title, desc := "P1b", "nieuw"
	got, err := g.EditMilestone(context.Background(), 501, &title, &desc, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := lastWrite(t, srv)
	if w.Method != "PUT" || w.Path != glBase+"/milestones/501" || !reflect.DeepEqual(w.Body, map[string]any{"title": "P1b", "description": "nieuw"}) {
		t.Errorf("write = %+v, want PUT …/milestones/501 (the global id, not iid 1) with title and description", w)
	}
	if got != (Milestone{Number: 501, Title: "P1b", State: "open", Description: "nieuw"}) {
		t.Errorf("edited = %+v", got)
	}
	created, err := g.CreateMilestone(context.Background(), "Nieuw", "omschrijving")
	if err != nil || created.Title != "Nieuw" || created.State != "open" || created.Description != "omschrijving" || created.Number == 0 {
		t.Errorf("CreateMilestone = %+v, %v", created, err)
	}
	if w := lastWrite(t, srv); w.Method != "POST" || w.Path != glBase+"/milestones" {
		t.Errorf("write = %s %s, want POST …/milestones", w.Method, w.Path)
	}
}

func TestGitLabCreateChangeIsMR(t *testing.T) {
	srv, g := glClient(t)
	got, err := g.CreateChange(context.Background(), NewPR{Title: "Titel", Body: "Closes #7", Head: "issue-7-x", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	w := lastWrite(t, srv)
	want := map[string]any{"title": "Titel", "description": "Closes #7", "source_branch": "issue-7-x", "target_branch": "main"}
	if w.Method != "POST" || w.Path != glBase+"/merge_requests" || !reflect.DeepEqual(w.Body, want) {
		t.Errorf("write = %+v, want POST …/merge_requests with %v", w, want)
	}
	if got.URL != "https://gitlab.example/group/project/-/merge_requests/1" || got.State != "open" || got.Number != 1 ||
		got.Title != "Titel" || got.Body != "Closes #7" || got.HeadRef != "issue-7-x" || got.BaseRef != "main" ||
		got.Author != "lh-bot" || got.CreatedAt.IsZero() || got.Milestone != nil {
		t.Errorf("change = %+v", got)
	}
}

func TestGitLabCreateChangeMilestoneByTitle(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	got, err := g.CreateChange(context.Background(), NewPR{Title: "T", Head: "h", Base: "main", Milestone: "G1"})
	if err != nil {
		t.Fatal(err)
	}
	if w := lastWrite(t, srv); w.Body["milestone_id"] != float64(777) {
		t.Errorf("body = %v, want milestone_id 777", w.Body)
	}
	if got.Milestone == nil || got.Milestone.Number != 777 || got.Milestone.State != "open" {
		t.Errorf("milestone = %+v, want G1 (777) open", got.Milestone)
	}
}

func TestGitLabChangeStates(t *testing.T) {
	srv, g := glClient(t)
	for i, st := range []string{"opened", "merged", "closed"} {
		srv.AddMR(map[string]any{"iid": i + 1, "state": st, "title": "MR", "web_url": "u", "source_branch": "h", "target_branch": "main",
			"created_at": "2026-02-03T04:05:06Z", "labels": []string{"a"}, "author": map[string]any{"username": "kim"}})
	}
	for i, want := range []string{"open", "merged", "closed"} {
		got, err := g.Change(context.Background(), i+1)
		if err != nil || got.State != want || got.Author != "kim" || got.BaseRef != "main" ||
			got.CreatedAt.Format("2006-01-02") != "2026-02-03" || !reflect.DeepEqual(got.Labels, []string{"a"}) {
			t.Errorf("Change(%d) = %+v, %v; want state %s", i+1, got, err, want)
		}
	}
}

func TestGitLabCreateChangeComment(t *testing.T) {
	srv, g := glClient(t)
	srv.AddMR(map[string]any{"iid": 4, "state": "opened"})
	c, err := g.CreateChangeComment(context.Background(), 4, "Klaar")
	if err != nil || c.Body != "Klaar" || c.Author != "lh-bot" {
		t.Fatalf("comment = %+v, %v", c, err)
	}
	if w := lastWrite(t, srv); w.Path != glBase+"/merge_requests/4/notes" || w.Body["body"] != "Klaar" {
		t.Errorf("write = %+v, want POST …/merge_requests/4/notes", w)
	}
	if !strings.Contains(c.URL, "/-/merge_requests/4#note_") && c.URL != "" {
		t.Errorf("URL = %q, want a merge request note URL", c.URL)
	}
}

func TestGitLabAddBlockerWritesALink(t *testing.T) {
	srv, g := glClient(t)
	srv.AddIssue(glIssueFixture(5))
	if err := g.AddBlocker(context.Background(), 7, 5); err != nil {
		t.Fatal(err)
	}
	w := lastWrite(t, srv)
	want := map[string]any{"target_project_id": "group/project", "target_issue_iid": "5", "link_type": "is_blocked_by"}
	if w.Method != "POST" || w.Path != glBase+"/issues/7/links" || !reflect.DeepEqual(w.Body, want) {
		t.Errorf("write = %+v, want POST …/issues/7/links with %v", w, want)
	}
	got, err := g.BlockedBy(context.Background(), 7)
	if err != nil || !reflect.DeepEqual(got, []Blocker{{Repo: "group/project", Issue: 5, State: "OPEN"}}) {
		t.Errorf("BlockedBy = %+v, %v; want open blocker 5", got, err)
	}
}

func TestGitLabAddBlockerFreeRefusal(t *testing.T) {
	srv, g := glClient(t)
	srv.Free = true
	err := g.AddBlocker(context.Background(), 7, 5)
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden - blocking links need a paid plan") {
		t.Errorf("err = %v, want the API message", err)
	}
}

func TestGitLabBlockedByFreeEmpty(t *testing.T) {
	srv, g := glClient(t)
	srv.Free = true
	srv.AddLink(7, map[string]any{"iid": 5, "state": "opened", "link_type": "is_blocked_by", "references": map[string]any{"full": "group/project#5"}})
	got, err := g.BlockedBy(context.Background(), 7)
	if err != nil || len(got) != 0 {
		t.Errorf("BlockedBy = %+v, %v; want an empty list and no error on Free", got, err)
	}
}

func TestGitLabEditIssueLabels(t *testing.T) {
	srv, g := glClient(t)
	srv.AddIssue(glIssueFixture(7, "a", "b"))
	got, err := g.EditIssue(context.Background(), 7, IssueEdit{AddLabels: []string{"c", "d"}, RemoveLabels: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(srv.Writes()); n != 1 {
		t.Fatalf("%d writes, want one PUT", n)
	}
	w := lastWrite(t, srv)
	want := map[string]any{"add_labels": "c,d", "remove_labels": "a"}
	if w.Method != "PUT" || w.Path != glBase+"/issues/7" || !reflect.DeepEqual(w.Body, want) {
		t.Errorf("write = %+v, want PUT …/issues/7 with %v", w, want)
	}
	if !reflect.DeepEqual(got.Labels, []string{"b", "c", "d"}) {
		t.Errorf("labels = %v, want [b c d]", got.Labels)
	}
}

func TestGitLabEditIssueTitleBodyMilestone(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	srv.AddIssue(glIssueFixture(7))
	title, body, ms := "Anders", "Nieuwe tekst", "G1"
	got, err := g.EditIssue(context.Background(), 7, IssueEdit{Title: &title, Body: &body, Milestone: &ms})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"title": "Anders", "description": "Nieuwe tekst", "milestone_id": float64(777)}
	if w := lastWrite(t, srv); !reflect.DeepEqual(w.Body, want) {
		t.Errorf("body = %v, want %v", w.Body, want)
	}
	if got.Title != "Anders" || got.Body != "Nieuwe tekst" || got.Milestone == nil || got.Milestone.Number != 777 {
		t.Errorf("issue = %+v", got)
	}
	none := ""
	got, err = g.EditIssue(context.Background(), 7, IssueEdit{Milestone: &none})
	if err != nil {
		t.Fatal(err)
	}
	if w := lastWrite(t, srv); !reflect.DeepEqual(w.Body, map[string]any{"milestone_id": float64(0)}) || got.Milestone != nil {
		t.Errorf("body = %v, milestone = %+v; want milestone_id 0 and none", w.Body, got.Milestone)
	}
}

func TestGitLabEditIssueWithoutChangesOnlyReads(t *testing.T) {
	srv, g := glClient(t)
	srv.AddIssue(glIssueFixture(7, "a"))
	got, err := g.EditIssue(context.Background(), 7, IssueEdit{})
	if err != nil || got.Number != 7 || len(srv.Writes()) != 0 {
		t.Errorf("EditIssue = %+v, %v, %d writes; want the issue and no write", got, err, len(srv.Writes()))
	}
}

func TestGitLabSearchIssues(t *testing.T) {
	srv, g := glClient(t)
	one, two := glIssueFixture(1), glIssueFixture(2)
	one["title"], two["title"] = "Login bug", "Other"
	srv.AddIssue(one)
	srv.AddIssue(two)
	got, err := g.SearchIssues(context.Background(), "login")
	if err != nil || len(got) != 1 || got[0].Number != 1 {
		t.Errorf("SearchIssues = %+v, %v; want only issue 1", got, err)
	}
	if !slices.ContainsFunc(srv.Requests(), func(r string) bool { return strings.Contains(r, "search=login") }) {
		t.Errorf("requests %v: want a search=login query", srv.Requests())
	}
}

func TestGitLabListIssuesFilters(t *testing.T) {
	srv, g := glClient(t)
	glMilestones(srv)
	a, b, c := glIssueFixture(1, "bug"), glIssueFixture(2, "bug"), glIssueFixture(3)
	b["state"] = "closed"
	a["milestone"] = map[string]any{"id": 777, "title": "G1", "state": "active"}
	srv.AddIssue(a)
	srv.AddIssue(b)
	srv.AddIssue(c)
	numbers := func(f IssueFilter) []int {
		got, err := g.ListIssues(context.Background(), f)
		if err != nil {
			t.Fatalf("ListIssues(%+v): %v", f, err)
		}
		var out []int
		for _, is := range got {
			out = append(out, is.Number)
		}
		return out
	}
	for _, tc := range []struct {
		f    IssueFilter
		want []int
	}{
		{IssueFilter{}, []int{1, 3}},
		{IssueFilter{State: "closed"}, []int{2}},
		{IssueFilter{State: "all"}, []int{1, 2, 3}},
		{IssueFilter{State: "all", Label: "bug"}, []int{1, 2}},
		{IssueFilter{Milestone: "G1"}, []int{1}},
	} {
		if got := numbers(tc.f); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ListIssues(%+v) = %v, want %v", tc.f, got, tc.want)
		}
	}
	if _, err := g.ListIssues(context.Background(), IssueFilter{State: "merged"}); err == nil {
		t.Error("ListIssues with state merged: want an error")
	}
}

func TestGitLabLabels(t *testing.T) {
	srv, g := glClient(t)
	srv.SetLabels("bug", "area-x")
	got, err := g.Labels(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"bug", "area-x"}) {
		t.Errorf("Labels = %v, %v", got, err)
	}
}

// rawAPI is a server that records what the API method sends.
type rawAPI struct {
	method, path, query, token, body string
}

func glRawClient(t *testing.T) (*rawAPI, *GitLab) {
	t.Helper()
	rec := &rawAPI{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*rec = rawAPI{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("PRIVATE-TOKEN"), string(b)}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return rec, newGitLab(Config{Kind: "gitlab", Repo: "group/project", Token: "secret-token", BaseURL: srv.URL + "/api/v4"})
}

func TestGitLabAPIKeepsToken(t *testing.T) {
	rec, g := glRawClient(t)
	out, err := g.API(context.Background(), APIRequest{Path: "projects/1/labels", Fields: []APIField{{Name: "name", Value: "x"}, {Name: "n", Value: "3", Typed: true}}})
	if err != nil || (string(out) != `{"ok":true}`+"\n" && string(out) != `{"ok":true}`) {
		t.Fatalf("API = %q, %v", out, err)
	}
	if rec.method != "POST" || rec.path != "/api/v4/projects/1/labels" || rec.token != "secret-token" {
		t.Errorf("request = %+v, want POST /api/v4/projects/1/labels with the token", rec)
	}
	if rec.body != `{"n":3,"name":"x"}` {
		t.Errorf("body = %s, want typed number 3 and text x", rec.body)
	}
	if _, err := g.API(context.Background(), APIRequest{Path: "/api/v4/projects/1/labels", Fields: []APIField{{Name: "page", Value: "2"}}, Method: "get"}); err != nil {
		t.Fatal(err)
	}
	if rec.method != "GET" || rec.path != "/api/v4/projects/1/labels" || rec.query != "page=2" {
		t.Errorf("request = %+v, want GET /api/v4/projects/1/labels?page=2 (prefix not doubled)", rec)
	}
	*rec = rawAPI{}
	if _, err := g.API(context.Background(), APIRequest{Path: "https://evil.example/x"}); err == nil || rec.method != "" {
		t.Errorf("absolute URL: err = %v, request = %+v; want a refusal without a request", err, rec)
	}
}

func TestGitLabAPIGraphQLPath(t *testing.T) {
	rec, g := glRawClient(t)
	if _, err := g.API(context.Background(), APIRequest{Path: "api/graphql", Fields: []APIField{{Name: "query", Value: "{ x }"}}}); err != nil {
		t.Fatal(err)
	}
	if rec.method != "POST" || rec.path != "/api/graphql" || rec.token != "secret-token" {
		t.Errorf("request = %+v, want POST /api/graphql with the token (not /api/v4/api/graphql)", rec)
	}
}

func TestGitLabAPIFollowsPages(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("page") == "2" {
			_, _ = io.WriteString(w, `[{"n":3}]`)
			return
		}
		w.Header().Set("x-next-page", "2")
		_, _ = io.WriteString(w, `[{"n":1},{"n":2}]`)
	}))
	t.Cleanup(srv.Close)
	g := newGitLab(Config{Kind: "gitlab", Repo: "group/project", Token: "secret-token", BaseURL: srv.URL + "/api/v4"})
	out, err := g.API(context.Background(), APIRequest{Method: "GET", Path: "projects/1/labels", Fields: []APIField{{Name: "per_page", Value: "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]int
	if err := json.Unmarshal(out, &got); err != nil || !reflect.DeepEqual(got, []map[string]int{{"n": 1}, {"n": 2}, {"n": 3}}) {
		t.Errorf("output = %s (%v), want one list of 3", out, err)
	}
	if !reflect.DeepEqual(queries, []string{"per_page=2", "page=2&per_page=2"}) {
		t.Errorf("queries = %q, want the second page asked with the same fields", queries)
	}
}

func TestGitLabChangeMergeable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{"has_conflicts", map[string]any{"has_conflicts": true, "detailed_merge_status": "mergeable"}, "conflicting"},
		{"conflict", map[string]any{"detailed_merge_status": "conflict"}, "conflicting"},
		{"need_rebase", map[string]any{"detailed_merge_status": "need_rebase"}, "conflicting"},
		{"unchecked", map[string]any{"detailed_merge_status": "unchecked"}, "unknown"},
		{"checking", map[string]any{"detailed_merge_status": "checking"}, "unknown"},
		{"preparing", map[string]any{"detailed_merge_status": "preparing"}, "unknown"},
		{"approvals_syncing", map[string]any{"detailed_merge_status": "approvals_syncing"}, "unknown"},
		{"mergeable", map[string]any{"detailed_merge_status": "mergeable"}, "mergeable"},
		{"not_approved", map[string]any{"detailed_merge_status": "not_approved"}, "mergeable"},
		{"ci_must_pass", map[string]any{"detailed_merge_status": "ci_must_pass"}, "mergeable"},
		{"ci_still_running", map[string]any{"detailed_merge_status": "ci_still_running"}, "mergeable"},
		{"discussions_not_resolved", map[string]any{"detailed_merge_status": "discussions_not_resolved"}, "mergeable"},
		{"draft_status", map[string]any{"detailed_merge_status": "draft_status"}, "mergeable"},
		{"blocked_status", map[string]any{"detailed_merge_status": "blocked_status"}, "mergeable"},
		{"requested_changes", map[string]any{"detailed_merge_status": "requested_changes"}, "mergeable"},
		{"external_status_checks", map[string]any{"detailed_merge_status": "external_status_checks"}, "mergeable"},
		{"not_open", map[string]any{"detailed_merge_status": "not_open"}, "mergeable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, g := glClient(t)
			mr := map[string]any{"iid": 3, "state": "opened", "draft": true}
			for k, v := range tc.extra {
				mr[k] = v
			}
			srv.AddMR(mr)
			got, err := g.Change(context.Background(), 3)
			if err != nil || got.Mergeable != tc.want || !got.Draft {
				t.Errorf("Change(3) = mergeable %q draft %v, %v; want %q draft", got.Mergeable, got.Draft, err, tc.want)
			}
		})
	}
}

func TestGitLabChangeCommentsSkipsSystemNotes(t *testing.T) {
	srv, g := glClient(t)
	srv.AddMR(map[string]any{"iid": 4, "state": "opened"})
	srv.AddMRNote(4, "kim", "added 1 commit", true)
	srv.AddMRNote(4, "ann", "Mooi", false)
	got, err := g.ChangeComments(context.Background(), 4)
	if err != nil || len(got) != 1 || got[0].Body != "Mooi" || got[0].Author != "ann" {
		t.Errorf("ChangeComments = %+v, %v; want only ann's note", got, err)
	}
}
