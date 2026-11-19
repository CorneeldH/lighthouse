package forge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// etagServer answers GETs with one ETag and a 304 for a matching If-None-Match. It records
// every request.
type etagServer struct {
	mu     sync.Mutex
	reqs   []*http.Request
	status int // status of a non-conditional answer; 0 is 200
	etag   string
	body   string
	link   string
}

func (s *etagServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, r.Clone(context.Background()))
	status, etag, body, link := s.status, s.etag, s.body, s.link
	s.mu.Unlock()
	if r.Method == http.MethodGet && etag != "" && r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if link != "" {
		w.Header().Set("Link", link)
	}
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (s *etagServer) request(i int) *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs[i]
}

func (s *etagServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

func cachedClient(t *testing.T, h http.Handler, store CacheStore, token string, log *bytes.Buffer) *GitHub {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := Config{Repo: "o/r", Token: token, BaseURL: srv.URL, Cache: store}
	if log != nil {
		cfg.CacheLog = log
	}
	f, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return f.(*GitHub)
}

func TestCacheServesStoredBodyOn304(t *testing.T) {
	s := &etagServer{etag: `"v1"`, body: `{"number":7,"title":"one"}`}
	c := cachedClient(t, s, NewMemoryCache(), "tok", nil)
	for i := 0; i < 2; i++ {
		is, err := c.Issue(context.Background(), 7)
		if err != nil || is.Title != "one" {
			t.Fatalf("Issue call %d = %+v, %v; want title one", i, is, err)
		}
	}
	if got := s.request(0).Header.Get("If-None-Match"); got != "" {
		t.Errorf("first If-None-Match = %q, want none", got)
	}
	if got := s.request(1).Header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("second If-None-Match = %q, want \"v1\"", got)
	}

	// The hit marker is on the rebuilt response itself.
	req, _ := http.NewRequest(http.MethodGet, c.BaseURL+"/repos/o/r/issues/7", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 || resp.Header.Get(cacheHitHeader) != "hit" {
		t.Errorf("third answer = %d with %s=%q, want 200 and hit", resp.StatusCode, cacheHitHeader, resp.Header.Get(cacheHitHeader))
	}
}

func TestCacheKeepsLinkOf304(t *testing.T) {
	var srvURL string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"p`+r.URL.Query().Get("page")+`"`)
		if r.Header.Get("If-None-Match") == `"p`+r.URL.Query().Get("page")+`"` {
			w.WriteHeader(http.StatusNotModified) // no Link, as GitHub does
			return
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[{"number":2,"title":"b"}]`))
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/issues?state=open&per_page=100&page=2>; rel="next"`, srvURL))
		_, _ = w.Write([]byte(`[{"number":1,"title":"a"}]`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	f, err := Open(Config{Repo: "o/r", Token: "tok", BaseURL: srv.URL, Cache: NewMemoryCache()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := f.OpenIssues(context.Background())
		if err != nil || len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
			t.Fatalf("OpenIssues pass %d = %+v, %v; want issues 1 and 2", i, got, err)
		}
	}
}

func TestCacheOnlyGET(t *testing.T) {
	s := &etagServer{etag: `"v1"`, body: `{"number":7}`}
	store := NewMemoryCache()
	c := cachedClient(t, s, store, "tok", nil)
	var out map[string]any
	if _, err := c.do(context.Background(), http.MethodPost, c.BaseURL+"/repos/o/r/issues", map[string]string{"a": "b"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := c.do(context.Background(), http.MethodPatch, c.BaseURL+"/repos/o/r/issues/7", map[string]string{"a": "b"}, &out); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if got := s.request(i).Header.Get("If-None-Match"); got != "" {
			t.Errorf("%s sent If-None-Match %q, want none", s.request(i).Method, got)
		}
		req := s.request(i)
		key := cacheKey(mustReq(t, req.Method, c.BaseURL+req.URL.Path, "tok"))
		if _, ok, _ := store.Get(context.Background(), key); ok {
			t.Errorf("%s was stored, want only GET stored", req.Method)
		}
	}
}

func mustReq(t *testing.T, method, url, token string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestCacheKeyPerToken(t *testing.T) {
	s := &etagServer{etag: `"v1"`, body: `{"number":7}`}
	store := NewMemoryCache()
	a := cachedClient(t, s, store, "token-aaa", nil)
	if _, err := a.Issue(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	b2, err := Open(Config{Repo: "o/r", Token: "token-bbb", BaseURL: a.BaseURL, Cache: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b2.Issue(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got := s.request(1).Header.Get("If-None-Match"); got != "" {
		t.Errorf("second token sent If-None-Match %q, want none", got)
	}
	key := cacheKey(mustReq(t, http.MethodGet, a.BaseURL+"/repos/o/r/issues/7", "token-aaa"))
	if want := "GET " + a.BaseURL + "/repos/o/r/issues/7 1e0b0a2673baaa4b"; key != want {
		t.Errorf("cacheKey = %q, want %q", key, want)
	}
	if strings.Contains(key, "token-aaa") {
		t.Errorf("cacheKey %q contains the token", key)
	}
}

func TestCacheStoresOnlyComplete200WithETag(t *testing.T) {
	cases := []struct {
		name   string
		status int
		etag   string
		body   string
	}{
		{"200 without etag", 200, "", `{"number":7}`},
		{"404 with etag", 404, `"v1"`, `{"message":"Not Found"}`},
		{"500 with etag", 500, `"v1"`, `{"message":"boom"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &etagServer{status: tc.status, etag: tc.etag, body: tc.body}
			store := NewMemoryCache()
			c := cachedClient(t, s, store, "tok", nil)
			_, _ = c.Issue(context.Background(), 7)
			key := cacheKey(mustReq(t, http.MethodGet, c.BaseURL+"/repos/o/r/issues/7", "tok"))
			if _, ok, _ := store.Get(context.Background(), key); ok {
				t.Errorf("%s was stored, want nothing", tc.name)
			}
		})
	}
}

type failingStore struct{}

func (failingStore) Get(context.Context, string) (CacheEntry, bool, error) {
	return CacheEntry{}, false, errors.New("disk on fire")
}
func (failingStore) Put(context.Context, string, CacheEntry) error { return errors.New("disk on fire") }
func (failingStore) Delete(context.Context, string) error          { return errors.New("disk on fire") }

func TestCacheErrorIsNotFatal(t *testing.T) {
	s := &etagServer{etag: `"v1"`, body: `{"number":7,"title":"one"}`}
	var log bytes.Buffer
	c := cachedClient(t, s, failingStore{}, "tok", &log)
	for i := 0; i < 3; i++ {
		is, err := c.Issue(context.Background(), 7)
		if err != nil || is.Title != "one" {
			t.Fatalf("Issue call %d = %+v, %v; want success despite the cache", i, is, err)
		}
	}
	if lines := strings.Split(strings.TrimSpace(log.String()), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], "forge cache: ") {
		t.Errorf("CacheLog = %q, want exactly one line starting with \"forge cache: \"", log.String())
	}
}

func TestCacheUndecodableEntryIsDropped(t *testing.T) {
	s := &etagServer{etag: `"v1"`, body: `{"number":7,"title":"one"}`}
	store := NewMemoryCache()
	c := cachedClient(t, s, store, "tok", nil)
	key := cacheKey(mustReq(t, http.MethodGet, c.BaseURL+"/repos/o/r/issues/7", "tok"))
	if err := store.Put(context.Background(), key, CacheEntry{ETag: `"v1"`, Body: []byte("{")}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Issue(context.Background(), 7); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("Issue = %v, want a decode error", err)
	}
	if _, ok, _ := store.Get(context.Background(), key); ok {
		t.Fatal("undecodable entry still in the store")
	}
	is, err := c.Issue(context.Background(), 7)
	if err != nil || is.Title != "one" {
		t.Fatalf("Issue after drop = %+v, %v; want success", is, err)
	}
	if got := s.request(s.count() - 1).Header.Get("If-None-Match"); got != "" {
		t.Errorf("last If-None-Match = %q, want none after the drop", got)
	}
}
