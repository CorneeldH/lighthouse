package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func noLeftovers(context.Context) ([]workplace.Leftover, error) { return []workplace.Leftover{}, nil }

func noRuns(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error) {
	return nil, nil, nil
}

// testDeps serves src as the one repo o/r, narrowed by inController, with no leftovers and no runs.
func testDeps(src overview.Source, inController overview.Filter) Deps {
	repos := []overview.RepoSource{{Slug: "o/r", Src: src, InController: inController}}
	return Deps{
		Report:    fakeReport,
		Forge:     repos,
		Overview:  func(ctx context.Context) (overview.Overview, error) { return overview.BuildAll(ctx, repos) },
		Leftovers: noLeftovers,
		Runs:      noRuns,
	}
}

// fakeSource serves canned issues and comments, or err from OpenIssues.
type fakeSource struct {
	issues   []forge.Issue
	comments map[int][]protocol.Comment
	err      error
}

func (f fakeSource) OpenIssues(context.Context) ([]forge.Issue, error) { return f.issues, f.err }

func (f fakeSource) Comments(_ context.Context, n int) ([]protocol.Comment, error) {
	return f.comments[n], nil
}

func populated() fakeSource {
	return fakeSource{
		issues: []forge.Issue{
			{Number: 1, Title: "one", URL: "https://x/issues/1", Comments: 1},
			{Number: 2, Title: "two", URL: "https://x/issues/2", Comments: 1},
		},
		comments: map[int][]protocol.Comment{
			1: {{Body: "🤖 Question\n\nWhich port?", CreatedAt: "2026-01-01T00:00:00Z", URL: "https://x/c1"}},
			2: {{Body: "🤖 Returned\nreason: too big", CreatedAt: "2026-01-01T00:00:00Z", URL: "https://x/c2"}},
		},
	}
}

func decode(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return m
}

func TestQuestionsEndpoint(t *testing.T) {
	rec := get(New(testSite(), testDeps(populated(), nil)), "/api/questions")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		Questions []map[string]any `json:"questions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Questions) != 1 {
		t.Fatalf("questions = %v, want one", body.Questions)
	}
	q := body.Questions[0]
	want := map[string]any{"number": 1.0, "title": "one", "url": "https://x/issues/1", "answer_url": "https://x/c1"}
	for k, v := range want {
		if q[k] != v {
			t.Errorf("%s = %v, want %v", k, q[k], v)
		}
	}
	if s, _ := q["question"].(string); !strings.Contains(s, "Which port?") {
		t.Errorf("question = %v", q["question"])
	}
}

func TestReturnedEndpoint(t *testing.T) {
	rec := get(New(testSite(), testDeps(populated(), nil)), "/api/returned")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		Returned []map[string]any `json:"returned"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Returned) != 1 {
		t.Fatalf("returned = %v, want one", body.Returned)
	}
	r := body.Returned[0]
	want := map[string]any{"number": 2.0, "title": "two", "url": "https://x/issues/2", "reason": "too big"}
	for k, v := range want {
		if r[k] != v {
			t.Errorf("%s = %v, want %v", k, r[k], v)
		}
	}
}

var listRoutes = []struct{ path, key string }{{"/api/questions", "questions"}, {"/api/returned", "returned"}}

func TestEmptyListIsArray(t *testing.T) {
	for _, route := range listRoutes {
		rec := get(New(testSite(), testDeps(stubSource{}, nil)), route.path)
		if rec.Code != 200 {
			t.Fatalf("%s: status = %d", route.path, rec.Code)
		}
		if got := string(decode(t, rec.Body.Bytes())[route.key]); got != "[]" {
			t.Errorf("%s: %s = %s, want []", route.path, route.key, got)
		}
	}
}

func TestEndpointNotConfigured(t *testing.T) {
	assertEndpointError(t, fmt.Errorf("forge: %w", forge.ErrNotConfigured), 503)
}

func TestEndpointForgeDown(t *testing.T) {
	assertEndpointError(t, errors.New("dial tcp: connection refused"), 502)
}

func assertEndpointError(t *testing.T, err error, status int) {
	t.Helper()
	for _, route := range listRoutes {
		rec := get(New(testSite(), testDeps(fakeSource{err: err}, nil)), route.path)
		if rec.Code != status {
			t.Fatalf("%s: status = %d, want %d, body %s", route.path, rec.Code, status, rec.Body)
		}
		if _, ok := decode(t, rec.Body.Bytes())[route.key]; ok {
			t.Errorf("%s: error body has key %q: %s", route.path, route.key, rec.Body)
		}
	}
}

func TestCleanupEndpoint(t *testing.T) {
	want := workplace.Leftover{Repo: "o/r", Issue: 7, WorkspaceID: "ws-1", Path: "/w/ws-1", Reason: "dirty worktree"}
	leftovers := func(context.Context) ([]workplace.Leftover, error) { return []workplace.Leftover{want}, nil }
	d := testDeps(stubSource{}, nil)
	d.Leftovers = leftovers
	rec := get(New(testSite(), d), "/api/cleanup")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	got := string(decode(t, rec.Body.Bytes())["cleanup"])
	const wantJSON = `[{"repo":"o/r","issue":7,"workspace_id":"ws-1","path":"/w/ws-1","reason":"dirty worktree"}]`
	if got != wantJSON {
		t.Errorf("cleanup = %s, want %s", got, wantJSON)
	}
}

func TestCleanupEndpointEmptyIsArray(t *testing.T) {
	nilList := func(context.Context) ([]workplace.Leftover, error) { return nil, nil }
	d := testDeps(stubSource{}, nil)
	d.Leftovers = nilList
	rec := get(New(testSite(), d), "/api/cleanup")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := string(decode(t, rec.Body.Bytes())["cleanup"]); got != "[]" {
		t.Errorf("cleanup = %s, want []", got)
	}
}

func TestCleanupEndpointError(t *testing.T) {
	leftovers := func(context.Context) ([]workplace.Leftover, error) { return nil, errors.New("db closed") }
	d := testDeps(stubSource{}, nil)
	d.Leftovers = leftovers
	rec := get(New(testSite(), d), "/api/cleanup")
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500, body %s", rec.Code, rec.Body)
	}
	if _, ok := decode(t, rec.Body.Bytes())["cleanup"]; ok {
		t.Errorf("error body has key cleanup: %s", rec.Body)
	}
}

// only is a filter that keeps the given issue numbers.
func only(numbers ...int) func(context.Context) (map[int]bool, error) {
	return func(context.Context) (map[int]bool, error) {
		m := map[int]bool{}
		for _, n := range numbers {
			m[n] = true
		}
		return m, nil
	}
}

// listLen returns the length of the JSON list under key in an endpoint body.
func listLen(t *testing.T, body []byte, key string) int {
	t.Helper()
	var list []json.RawMessage
	if err := json.Unmarshal(decode(t, body)[key], &list); err != nil {
		t.Fatalf("%s in %s: %v", key, body, err)
	}
	if list == nil {
		t.Fatalf("%s is null in %s, want a list", key, body)
	}
	return len(list)
}

func TestQuestionsEndpointFilter(t *testing.T) {
	// Issue 1 has the open question; the filter keeps only 2.
	rec := get(New(testSite(), testDeps(populated(), only(2))), "/api/questions")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if n := listLen(t, rec.Body.Bytes(), "questions"); n != 0 {
		t.Errorf("questions = %s, want empty", rec.Body)
	}
	rec = get(New(testSite(), testDeps(populated(), nil)), "/api/questions")
	if n := listLen(t, rec.Body.Bytes(), "questions"); n != 1 {
		t.Errorf("questions without filter = %s, want one", rec.Body)
	}
}

func TestReturnedEndpointFilter(t *testing.T) {
	// Issue 2 is returned; the filter keeps only 1.
	rec := get(New(testSite(), testDeps(populated(), only(1))), "/api/returned")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if n := listLen(t, rec.Body.Bytes(), "returned"); n != 0 {
		t.Errorf("returned = %s, want empty", rec.Body)
	}
	rec = get(New(testSite(), testDeps(populated(), nil)), "/api/returned")
	if n := listLen(t, rec.Body.Bytes(), "returned"); n != 1 {
		t.Errorf("returned without filter = %s, want one", rec.Body)
	}
}

func TestRunsList(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	d := testDeps(stubSource{}, nil)
	d.Runs = func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error) {
		return []dispatch.RunView{{Repo: "o/r", Issue: 7, Title: "zeven", State: "open", Command: "/lighthouse-start-issue 7",
				Phase: "start-issue", Trigger: "board", Profile: "software", WorkspaceID: "ws-7", StartedAt: at, Stand: "working", At: at}},
			[]dispatch.DeferredView{{Repo: "o/r", Issue: 8, Title: "acht", Reason: "no slot", At: at}}, nil
	}
	rec := get(New(testSite(), d), "/api/runs")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	body := decode(t, rec.Body.Bytes())
	const wantRuns = `[{"repo":"o/r","issue":7,"title":"zeven","state":"open","command":"/lighthouse-start-issue 7",` +
		`"phase":"start-issue","task":0,"tasks":0,"step":"",` +
		`"trigger":"board","profile":"software","workspace_id":"ws-7","started_at":"2026-11-01T09:00:00Z",` +
		`"stand":"working","at":"2026-11-01T09:00:00Z"}]`
	if got := string(body["runs"]); got != wantRuns {
		t.Errorf("runs = %s, want %s", got, wantRuns)
	}
	const wantDeferred = `[{"repo":"o/r","issue":8,"title":"acht","reason":"no slot","at":"2026-11-01T09:00:00Z"}]`
	if got := string(body["deferred"]); got != wantDeferred {
		t.Errorf("deferred = %s, want %s", got, wantDeferred)
	}
}

func TestRunsMeta(t *testing.T) {
	d := testDeps(stubSource{}, nil)
	d.RunsMeta = RunsMeta{WorkspaceURL: "http://127.0.0.1:8091", Night: "21:00-08:00"}
	rec := get(New(testSite(), d), "/api/runs")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	body := decode(t, rec.Body.Bytes())
	if got := string(body["workspace_url"]); got != `"http://127.0.0.1:8091"` {
		t.Errorf("workspace_url = %s", got)
	}
	if got := string(body["night"]); got != `"21:00-08:00"` {
		t.Errorf("night = %s", got)
	}
}

func TestRunsEmpty(t *testing.T) {
	rec := get(New(testSite(), testDeps(stubSource{}, nil)), "/api/runs")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"runs":[],"deferred":[],"workspace_url":"","night":""}` {
		t.Errorf("body = %s, want empty runs, deferred, workspace_url and night", got)
	}
}

func TestRunsError(t *testing.T) {
	d := testDeps(stubSource{}, nil)
	d.Runs = func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error) {
		return nil, nil, errors.New("db closed")
	}
	rec := get(New(testSite(), d), "/api/runs")
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500, body %s", rec.Code, rec.Body)
	}
	if _, ok := decode(t, rec.Body.Bytes())["runs"]; ok {
		t.Errorf("error body has key runs: %s", rec.Body)
	}
}

// countingSource counts OpenIssues calls and otherwise serves populated().
type countingSource struct {
	fakeSource
	calls atomic.Int32
}

func (c *countingSource) OpenIssues(ctx context.Context) ([]forge.Issue, error) {
	c.calls.Add(1)
	return c.fakeSource.OpenIssues(ctx)
}

func TestEndpointsShareOneBuild(t *testing.T) {
	src := &countingSource{fakeSource: populated()}
	repos := []overview.RepoSource{{Slug: "o/r", Src: src}}
	r := NewRefresher(func(ctx context.Context) (overview.Overview, error) {
		return overview.BuildAll(ctx, repos)
	}, time.Hour, time.Hour, time.Minute)
	r.refresh(context.Background())

	d := testDeps(populated(), nil)
	d.Overview = r.Overview
	h := New(testSite(), d)
	for _, path := range []string{"/api/questions", "/api/returned", "/api/questions", "/api/returned"} {
		rec := get(h, path)
		if rec.Code != 200 {
			t.Fatalf("%s: status %d, body %s", path, rec.Code, rec.Body)
		}
	}
	rec := get(h, "/api/questions")
	if !strings.Contains(rec.Body.String(), "Which port?") {
		t.Errorf("questions body lacks the question: %s", rec.Body)
	}
	if got := src.calls.Load(); got != 1 {
		t.Errorf("OpenIssues calls = %d, want 1", got)
	}
}

func TestEndpointFailureAfterSuccessIs502(t *testing.T) {
	builds := 0
	r := NewRefresher(func(context.Context) (overview.Overview, error) {
		builds++
		if builds == 1 {
			return overview.Overview{Questions: []overview.Question{{}}}, nil
		}
		return overview.Overview{}, errors.New("dial tcp: connection refused")
	}, time.Hour, time.Hour, time.Minute)
	r.refresh(context.Background())
	r.refresh(context.Background())

	d := testDeps(populated(), nil)
	d.Overview = r.Overview
	h := New(testSite(), d)
	for _, path := range []string{"/api/questions", "/api/returned"} {
		rec := get(h, path)
		if rec.Code != 502 {
			t.Errorf("%s: status %d, want 502", path, rec.Code)
		}
		body := decode(t, rec.Body.Bytes())
		for _, key := range []string{"questions", "returned"} {
			if _, ok := body[key]; ok {
				t.Errorf("%s: body has %q: %s", path, key, rec.Body)
			}
		}
	}
}
