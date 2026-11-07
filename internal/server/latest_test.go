package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// getTarget is get for a target with a query string, which get would put in the path.
func getTarget(h http.Handler, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// failingComments fails every comment read.
type failingComments struct{ fakeSource }

func (failingComments) Comments(context.Context, int) ([]protocol.Comment, error) {
	return nil, errors.New("rate limited")
}

func TestLatestCommentRedirectsToLastComment(t *testing.T) {
	src := fakeSource{comments: map[int][]protocol.Comment{
		7: {
			{Body: "first", URL: "https://github.com/o/r/issues/7#issuecomment-1"},
			{Body: "second", URL: "https://github.com/o/r/issues/7#issuecomment-2"},
		},
	}}
	rec := getTarget(New(testSite(), testDeps(src, nil)), "/api/latest-comment?repo=o/r&issue=7")
	if rec.Code != 302 {
		t.Fatalf("status = %d, want 302, body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Location"); got != "https://github.com/o/r/issues/7#issuecomment-2" {
		t.Errorf("Location = %q", got)
	}
}

func TestLatestCommentFallsBackToIssuePage(t *testing.T) {
	cases := map[string]struct {
		src overview.Source
	}{
		"no comments":   {fakeSource{}},
		"comment error": {failingComments{}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := getTarget(New(testSite(), testDeps(c.src, nil)), "/api/latest-comment?repo=o/r&issue=7")
			if rec.Code != 302 {
				t.Fatalf("status = %d, want 302, body %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Location"); got != "https://github.com/o/r/issues/7" {
				t.Errorf("Location = %q", got)
			}
		})
	}
}

// Only the served repos redirect: anything else would make the server an open redirect.
func TestLatestCommentRefusesUnknownRepo(t *testing.T) {
	rec := getTarget(New(testSite(), testDeps(fakeSource{}, nil)), "/api/latest-comment?repo=evil/x&issue=7")
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Errorf("Location = %q, want none", got)
	}
}

func TestLatestCommentRefusesBadIssue(t *testing.T) {
	for _, q := range []string{"repo=o/r&issue=0", "repo=o/r", "issue=7"} {
		rec := getTarget(New(testSite(), testDeps(fakeSource{}, nil)), "/api/latest-comment?"+q)
		if rec.Code != 422 {
			t.Errorf("%s: status = %d, want 422", q, rec.Code)
		}
	}
}
