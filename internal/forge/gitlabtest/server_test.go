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

func TestServerRefusesGraphQLUntilOperationsExist(t *testing.T) {
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
