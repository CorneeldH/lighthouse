package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func noLeftovers(context.Context) ([]workplace.Leftover, error) { return []workplace.Leftover{}, nil }

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
	rec := get(New(testSite(), fakeReport, populated(), noLeftovers), "/api/questions")
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
	rec := get(New(testSite(), fakeReport, populated(), noLeftovers), "/api/returned")
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
		rec := get(New(testSite(), fakeReport, stubSource{}, noLeftovers), route.path)
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
		rec := get(New(testSite(), fakeReport, fakeSource{err: err}, noLeftovers), route.path)
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
	rec := get(New(testSite(), fakeReport, stubSource{}, leftovers), "/api/cleanup")
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
	rec := get(New(testSite(), fakeReport, stubSource{}, nilList), "/api/cleanup")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := string(decode(t, rec.Body.Bytes())["cleanup"]); got != "[]" {
		t.Errorf("cleanup = %s, want []", got)
	}
}

func TestCleanupEndpointError(t *testing.T) {
	leftovers := func(context.Context) ([]workplace.Leftover, error) { return nil, errors.New("db closed") }
	rec := get(New(testSite(), fakeReport, stubSource{}, leftovers), "/api/cleanup")
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500, body %s", rec.Code, rec.Body)
	}
	if _, ok := decode(t, rec.Body.Bytes())["cleanup"]; ok {
		t.Errorf("error body has key cleanup: %s", rec.Body)
	}
}
