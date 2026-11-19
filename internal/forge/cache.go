package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync"
)

// cacheHitHeader marks a response the cache rebuilt from a stored body after a 304.
const cacheHitHeader = "X-Lighthouse-Cache"

// CacheEntry is one stored 200 answer: its ETag, its Link header (a 304 carries none) and body.
type CacheEntry struct {
	ETag, Link string
	Body       []byte
}

// CacheStore keeps CacheEntry values by cacheKey. An error from it is never fatal to a request.
type CacheStore interface {
	Get(ctx context.Context, key string) (CacheEntry, bool, error)
	Put(ctx context.Context, key string, e CacheEntry) error
	Delete(ctx context.Context, key string) error
}

// cacheKey identifies a GET by its URL and its credentials, hashed so the token never appears
// in the store.
func cacheKey(req *http.Request) string {
	sum := sha256.Sum256([]byte(req.Header.Get("Authorization") + "\n" + req.URL.String()))
	return hex.EncodeToString(sum[:])
}

type memoryCache struct {
	mu sync.Mutex
	m  map[string]CacheEntry
}

// NewMemoryCache is a CacheStore in a map, for tests.
func NewMemoryCache() CacheStore { return &memoryCache{m: map[string]CacheEntry{}} }

func (c *memoryCache) Get(_ context.Context, key string) (CacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	return e, ok, nil
}

func (c *memoryCache) Put(_ context.Context, key string, e CacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = e
	return nil
}

func (c *memoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}

type cachingTransport struct {
	next  http.RoundTripper
	store CacheStore
	log   io.Writer
	once  sync.Once
}

// newCachingTransport makes GET requests conditional: a stored ETag goes out as If-None-Match,
// and a 304 comes back as the stored 200. A nil next is http.DefaultTransport, a nil log
// discards the one line a cache error produces.
func newCachingTransport(next http.RoundTripper, store CacheStore, log io.Writer) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	if log == nil {
		log = io.Discard
	}
	return &cachingTransport{next: next, store: store, log: log}
}

// fail logs the first cache error of this transport and ignores the rest.
func (t *cachingTransport) fail(err error) {
	t.once.Do(func() { _, _ = io.WriteString(t.log, "forge cache: "+err.Error()+"\n") })
}

func (t *cachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return t.next.RoundTrip(req)
	}
	key := cacheKey(req)
	entry, found, err := t.store.Get(req.Context(), key)
	if err != nil {
		t.fail(err)
		found = false
	}
	out := req
	if found && entry.ETag != "" {
		out = req.Clone(req.Context())
		out.Header.Set("If-None-Match", entry.ETag)
	} else {
		found = false
	}
	resp, err := t.next.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && found:
		_ = resp.Body.Close()
		resp.StatusCode = http.StatusOK
		resp.Status = "200 OK"
		resp.Body = io.NopCloser(bytes.NewReader(entry.Body))
		resp.ContentLength = int64(len(entry.Body))
		resp.Header.Del("Content-Length")
		if entry.Link != "" {
			resp.Header.Set("Link", entry.Link)
		}
		resp.Header.Set(cacheHitHeader, "hit")
	case resp.StatusCode == http.StatusOK && resp.Header.Get("ETag") != "":
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		e := CacheEntry{ETag: resp.Header.Get("ETag"), Link: resp.Header.Get("Link"), Body: body}
		if err := t.store.Put(req.Context(), key, e); err != nil {
			t.fail(err)
		}
	}
	return resp, nil
}
