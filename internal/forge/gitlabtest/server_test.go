package gitlabtest

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recorder stands in for *testing.T so a test can observe the Errorf the server raises for an
// unexpected request without failing itself.
type recorder struct {
	testing.TB
	errs    []string
	cleanup []func()
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recorder) Cleanup(f func()) { r.cleanup = append(r.cleanup, f) }
func (r *recorder) close() {
	for _, f := range r.cleanup {
		f()
	}
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("PRIVATE-TOKEN", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestServerRejectsUnknownRoute(t *testing.T) {
	rec := &recorder{TB: t}
	srv := newServer(rec)
	defer rec.close()
	srv.Project = "g/p"

	resp, _ := get(t, srv.URL()+"/nope")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", resp.StatusCode)
	}
	resp, _ = get(t, srv.URL()+"/projects/other%2Fproject/issues")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown project status = %d, want 404", resp.StatusCode)
	}
	if len(rec.errs) != 2 {
		t.Fatalf("errors = %q, want one per unexpected request", rec.errs)
	}
	if !strings.Contains(rec.errs[0], "/nope") || !strings.Contains(rec.errs[1], "other%2Fproject") {
		t.Errorf("errors do not name the requests: %q", rec.errs)
	}
}

func TestServerRefusesUnknownGraphQLOperation(t *testing.T) {
	rec := &recorder{TB: t}
	srv := newServer(rec)
	defer rec.close()
	req, _ := http.NewRequest(http.MethodPost, strings.TrimSuffix(srv.URL(), "/v4")+"/graphql", strings.NewReader(`{"query":"query Foo { x }"}`))
	req.Header.Set("PRIVATE-TOKEN", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || len(rec.errs) != 1 {
		t.Errorf("status = %d, errors = %q; want 404 and one error", resp.StatusCode, rec.errs)
	}
}

// postGraphQL posts body to the GraphQL endpoint and returns the response body.
func postGraphQL(t *testing.T, srv *Server, body string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, strings.TrimSuffix(srv.URL(), "/v4")+"/graphql", strings.NewReader(body))
	req.Header.Set("PRIVATE-TOKEN", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestServerRefusesWorkItemUpdateOfUnknownID(t *testing.T) {
	const mutation = `mutation LighthouseSetStatus($id: WorkItemID!, $status: String!) { x }`
	for _, tc := range []struct{ name, id string }{
		{"the iid instead of the global ID", "gid://gitlab/WorkItem/7"},
		{"no gid", "9007"},
		{"an issue that does not exist", "gid://gitlab/WorkItem/9008"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{TB: t}
			srv := newServer(rec)
			defer rec.close()
			srv.SetStatuses("To do", "Controller")
			srv.AddIssue(map[string]any{"iid": 7, "id": 9007, "state": "opened"})
			srv.SetWorkItemStatus(7, "To do")
			body := postGraphQL(t, srv, fmt.Sprintf(`{"query":%q,"variables":{"id":%q,"status":"Controller"}}`, mutation, tc.id))
			if len(rec.errs) != 1 || !strings.Contains(body, `"errors"`) || srv.WorkItemStatus(7) != "To do" {
				t.Errorf("errors = %q, body %s, status %q; want one test error, a GraphQL error and To do unchanged",
					rec.errs, body, srv.WorkItemStatus(7))
			}
		})
	}
	t.Run("the global ID of a known issue", func(t *testing.T) {
		rec := &recorder{TB: t}
		srv := newServer(rec)
		defer rec.close()
		srv.SetStatuses("To do", "Controller")
		srv.AddIssue(map[string]any{"iid": 7, "id": 9007, "state": "opened"})
		postGraphQL(t, srv, fmt.Sprintf(`{"query":%q,"variables":{"id":"gid://gitlab/WorkItem/9007","status":"controller"}}`, mutation))
		if len(rec.errs) != 0 || srv.WorkItemStatus(7) != "Controller" {
			t.Errorf("errors = %q, status %q; want none and Controller", rec.errs, srv.WorkItemStatus(7))
		}
	})
}

func TestServerPaginates(t *testing.T) {
	srv := New(t)
	srv.Project = "g/sub/p"
	for i := 1; i <= 101; i++ {
		srv.AddIssue(map[string]any{"iid": i, "title": fmt.Sprint("issue ", i), "state": "opened"})
	}
	base := srv.URL() + "/projects/g%2Fsub%2Fp/issues?per_page=100"

	resp, body := get(t, base+"&page=1")
	if n := strings.Count(body, `"iid"`); n != 100 || resp.Header.Get("x-next-page") != "2" {
		t.Errorf("page 1: %d items, x-next-page %q; want 100 and 2", n, resp.Header.Get("x-next-page"))
	}
	resp, body = get(t, base+"&page=2")
	if n := strings.Count(body, `"iid"`); n != 1 || resp.Header.Get("x-next-page") != "" {
		t.Errorf("page 2: %d items, x-next-page %q; want 1 and empty", n, resp.Header.Get("x-next-page"))
	}
	if !strings.Contains(body, `"iid":101`) {
		t.Errorf("page 2 lacks issue 101: %s", body)
	}
}

func TestServerDecodesEncodedProject(t *testing.T) {
	rec := &recorder{TB: t}
	srv := newServer(rec)
	defer rec.close()
	srv.Project = "g/sub/p"
	srv.AddIssue(map[string]any{"iid": 1, "title": "one", "state": "opened"})

	resp, body := get(t, srv.URL()+"/projects/g%2Fsub%2Fp/issues/1")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"one"`) {
		t.Errorf("encoded project: status %d, body %s", resp.StatusCode, body)
	}
	if len(rec.errs) != 0 {
		t.Errorf("encoded project raised %q", rec.errs)
	}
	resp, _ = get(t, srv.URL()+"/projects/g/sub/p/issues/1")
	if resp.StatusCode != http.StatusNotFound || len(rec.errs) != 1 {
		t.Errorf("unencoded project: status %d, errors %q; want 404 and one error", resp.StatusCode, rec.errs)
	}
}

func TestServerSeedAndRoutes(t *testing.T) {
	srv := New(t)
	srv.Project = "g/p"
	srv.Login = "lh-bot"
	srv.AddIssue(map[string]any{"iid": 3, "title": "t", "state": "opened"})
	user := func(name string) string {
		_, body := get(t, srv.URL()+"/users?username="+name)
		return body
	}
	if b := user("lh-bot"); !strings.Contains(b, `"username":"lh-bot"`) {
		t.Errorf("users?username=lh-bot = %s", b)
	}
	srv.SetMember("lh-bot", 30)
	_, b := get(t, srv.URL()+"/user")
	id := between(b, `"id":`, ",")
	resp, b := get(t, srv.URL()+"/projects/g%2Fp/members/all/"+id)
	if resp.StatusCode != 200 || !strings.Contains(b, `"access_level":30`) {
		t.Errorf("member: %d %s", resp.StatusCode, b)
	}
	if resp, _ = get(t, srv.URL()+"/projects/g%2Fp/members/all/9999"); resp.StatusCode != 404 {
		t.Errorf("non-member status = %d, want 404", resp.StatusCode)
	}

	nid := srv.AddNote(3, "someone", "hi", false)
	srv.AddNote(3, "gitlab", "changed label", true)
	srv.AddAward(3, nid, "rocket", "lh-bot")
	_, b = get(t, srv.URL()+"/projects/g%2Fp/issues/3/notes")
	if !strings.Contains(b, `"hi"`) || !strings.Contains(b, `"system":true`) {
		t.Errorf("notes = %s", b)
	}
	_, b = get(t, srv.URL()+fmt.Sprintf("/projects/g%%2Fp/issues/3/notes/%d/award_emoji", nid))
	if !strings.Contains(b, `"rocket"`) {
		t.Errorf("award_emoji = %s", b)
	}

	srv.SetStatuses("To do", "Done")
	srv.SetWorkItemStatus(3, "Done")
	if got := srv.WorkItemStatus(3); got != "Done" {
		t.Errorf("WorkItemStatus = %q, want Done", got)
	}
	if reqs := srv.Requests(); len(reqs) == 0 || !strings.HasPrefix(reqs[0], "GET /api/v4/users") {
		t.Errorf("Requests = %q", reqs)
	}
}

func between(s, a, b string) string {
	_, rest, _ := strings.Cut(s, a)
	v, _, _ := strings.Cut(rest, b)
	return v
}

func TestServerFiltersIssuesOnMilestoneAndUpdatedAfter(t *testing.T) {
	srv := New(t)
	srv.AddIssue(map[string]any{"iid": 1, "state": "closed", "updated_at": "2026-11-01T10:00:00Z", "milestone": map[string]any{"title": "M3"}})
	srv.AddIssue(map[string]any{"iid": 2, "state": "closed", "updated_at": "2026-10-01T10:00:00Z", "milestone": map[string]any{"title": "M3"}})
	srv.AddIssue(map[string]any{"iid": 3, "state": "closed", "updated_at": "2026-11-01T10:00:00Z", "milestone": map[string]any{"title": "M4"}})
	srv.AddIssue(map[string]any{"iid": 4, "state": "closed", "updated_at": "2026-11-01T10:00:00Z"})

	_, body := get(t, srv.URL()+"/projects/group%2Fproject/issues?milestone=M3&updated_after=2026-10-15T00:00:00Z")
	if iids := strings.Count(body, `"iid"`); iids != 1 || !strings.Contains(body, `"iid":1,`) {
		t.Errorf("milestone M3 updated after 15 Oct = %s; want only issue 1", body)
	}
	_, body = get(t, srv.URL()+"/projects/group%2Fproject/issues?milestone=M3")
	if iids := strings.Count(body, `"iid"`); iids != 2 {
		t.Errorf("milestone M3 = %s; want issues 1 and 2", body)
	}
}

func TestServerClosedByAnswersForAnyProjectAndRefusesDuplicateAwards(t *testing.T) {
	srv := New(t)
	srv.Project = "g/p"
	srv.AddClosedBy("o/other", 6, map[string]any{"iid": 9, "state": "merged"})

	resp, body := get(t, srv.URL()+"/projects/o%2Fother/issues/6/closed_by")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"merged"`) {
		t.Errorf("closed_by = %d %s; want 200 with the merged MR", resp.StatusCode, body)
	}
	resp, body = get(t, srv.URL()+"/projects/o%2Fother/issues/7/closed_by")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Errorf("closed_by of an issue without MRs = %d %s; want 200 and []", resp.StatusCode, body)
	}

	srv.Login = "lh-bot"
	id := srv.AddNote(7, "ann", "hi", false)
	post := func() int {
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/projects/g%%2Fp/issues/7/notes/%d/award_emoji?name=rocket", srv.URL(), id), nil)
		req.Header.Set("PRIVATE-TOKEN", "secret")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		return r.StatusCode
	}
	if first, second := post(), post(); first != http.StatusCreated || second != http.StatusNotFound {
		t.Errorf("award twice = %d then %d; want 201 then 404, as GitLab answers an award that exists", first, second)
	}
}

func send(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("PRIVATE-TOKEN", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp
}

func TestServerListsMergeRequestsByStateAndBranch(t *testing.T) {
	srv := New(t)
	srv.Project = "g/p"
	srv.AddMR(map[string]any{"iid": 1, "state": "opened", "source_branch": "a"})
	srv.AddMR(map[string]any{"iid": 2, "state": "merged", "source_branch": "a"})
	srv.AddMR(map[string]any{"iid": 3, "state": "opened", "source_branch": "b"})
	base := srv.URL() + "/projects/g%2Fp/merge_requests"
	for q, want := range map[string]string{"?state=opened": "1,3", "?source_branch=a": "1,2", "?state=opened&source_branch=b": "3", "": "1,2,3"} {
		_, body := get(t, base+q)
		var got []string
		for _, id := range []string{"1", "2", "3"} {
			if strings.Contains(body, `"iid":`+id+",") || strings.Contains(body, `"iid":`+id+"}") {
				got = append(got, id)
			}
		}
		if strings.Join(got, ",") != want {
			t.Errorf("GET merge_requests%s lists %v, want %s", q, got, want)
		}
	}
}

func TestServerMergeRefusals(t *testing.T) {
	srv := New(t)
	srv.Project = "g/p"
	srv.AddMR(map[string]any{"iid": 1, "state": "opened", "sha": "abc"})
	srv.AddMR(map[string]any{"iid": 2, "state": "opened", "sha": "abc", "has_conflicts": true})
	srv.AddMR(map[string]any{"iid": 3, "state": "closed", "sha": "abc"})
	base := srv.URL() + "/projects/g%2Fp/merge_requests/"
	for _, tc := range []struct {
		iid, body string
		want      int
	}{
		{"1", `{"sha":"zzz","squash":true}`, http.StatusConflict},
		{"2", `{"sha":"abc","squash":true}`, http.StatusNotAcceptable},
		{"3", `{"sha":"abc","squash":true}`, http.StatusMethodNotAllowed},
		{"1", `{"sha":"abc","squash":true}`, http.StatusOK},
		{"1", `{"sha":"abc","squash":true}`, http.StatusMethodNotAllowed},
	} {
		if got := send(t, http.MethodPut, base+tc.iid+"/merge", tc.body).StatusCode; got != tc.want {
			t.Errorf("merge !%s %s = %d, want %d", tc.iid, tc.body, got, tc.want)
		}
	}
}

// A merge while the list is read must not race on the seeded map; go test -race fails it.
func TestServerMergeWhileListing(t *testing.T) {
	srv := New(t)
	srv.Project = "g/p"
	for i := 1; i <= 20; i++ {
		srv.AddMR(map[string]any{"iid": i, "state": "opened", "sha": "abc"})
	}
	base := srv.URL() + "/projects/g%2Fp/merge_requests"
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 20; i++ {
			send(t, http.MethodPut, fmt.Sprintf("%s/%d/merge", base, i), `{"sha":"abc","squash":true}`)
		}
	}()
	for i := 0; i < 20; i++ {
		get(t, base)
	}
	<-done
}
