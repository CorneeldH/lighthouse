package forge

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

func releaseClient(srv *forgetest.Server) *GitHub {
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return c
}

func relItem(i int) map[string]any {
	return map[string]any{"tag_name": fmt.Sprintf("v1.0.%d", i), "name": fmt.Sprintf("n%d", i),
		"body": "b", "html_url": fmt.Sprintf("https://x/%d", i), "draft": i%10 == 0, "prerelease": i%7 == 0}
}

func TestReleases_paginates(t *testing.T) {
	srv := forgetest.New(t)
	var want []Release
	srv.Releases = map[string][]map[string]any{}
	for i := 0; i < 70; i++ { // per_page=30: three pages
		srv.Releases["up/stream"] = append(srv.Releases["up/stream"], relItem(i))
		it := relItem(i)
		want = append(want, Release{Tag: it["tag_name"].(string), Name: it["name"].(string), Body: "b",
			URL: it["html_url"].(string), Draft: it["draft"].(bool), Prerelease: it["prerelease"].(bool)})
	}
	got, err := releaseClient(srv).Releases(context.Background(), "up/stream")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %d releases, want the union of 3 pages (%d)", len(got), len(want))
	}
	if n := srv.Count("/repos/up/stream/releases"); n != 3 {
		t.Fatalf("requests = %d, want 3", n)
	}
}

func TestReleases_retriesOn5xxOnce(t *testing.T) {
	srv := forgetest.New(t)
	srv.Releases = map[string][]map[string]any{"up/stream": {relItem(1)}}
	srv.FailFirst = map[string]int{"GET /repos/up/stream/releases": 504}
	got, err := releaseClient(srv).Releases(context.Background(), "up/stream")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v; want the page after one retry", got, err)
	}
	if n := srv.Count("/repos/up/stream/releases"); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
}

func TestReleases_failsAfterSecond5xx(t *testing.T) {
	srv := forgetest.New(t)
	srv.Fail = map[string]int{"GET /repos/up/stream/releases": 504}
	if _, err := releaseClient(srv).Releases(context.Background(), "up/stream"); err == nil {
		t.Fatal("want an error after the retry also fails")
	}
	if n := srv.Count("/repos/up/stream/releases"); n != 2 {
		t.Fatalf("requests = %d, want exactly 2 (one retry)", n)
	}
}

func TestReleases_noRetryOn4xx(t *testing.T) {
	srv := forgetest.New(t)
	srv.Fail = map[string]int{"GET /repos/up/stream/releases": 404}
	if _, err := releaseClient(srv).Releases(context.Background(), "up/stream"); err == nil {
		t.Fatal("want an error")
	}
	if n := srv.Count("/repos/up/stream/releases"); n != 1 {
		t.Fatalf("requests = %d, want 1: a 404 is not retried", n)
	}
}

func TestReleases_refusesForeignNextPage(t *testing.T) {
	ft := &fakeTransport{t: t, getOnly: true, responses: map[string]fakeResponse{
		"https://api.github.com/repos/up/stream/releases?per_page=30": {status: 200, body: "[]",
			link: `<https://evil.example/x?page=2>; rel="next"`},
	}}
	c := New("o/r", "tok")
	c.HTTP.Transport = ft
	if _, err := c.Releases(context.Background(), "up/stream"); err == nil {
		t.Fatal("want sameOrigin to refuse a next page on another host")
	}
}

var _ ReleaseReader = (*GitHub)(nil)
