package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/priorities"
)

// fakeMilestones keeps milestone descriptions in memory. EditMilestone call number failAt
// (1-based; 0 never) fails with failErr without writing; readErr fails every read.
type fakeMilestones struct {
	forge.Forge
	mu      sync.Mutex
	desc    map[int]string
	edits   int
	failAt  int
	failErr error
	readErr error
}

func (f *fakeMilestones) MilestoneByNumber(_ context.Context, number int) (forge.Milestone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return forge.Milestone{}, f.readErr
	}
	d, ok := f.desc[number]
	if !ok {
		return forge.Milestone{}, fmt.Errorf("milestone %d: not found", number)
	}
	return forge.Milestone{Number: number, Title: fmt.Sprintf("M%d", number), State: "open", Description: d}, nil
}

func (f *fakeMilestones) EditMilestone(_ context.Context, number int, title, description, _ *string) (forge.Milestone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits++
	if title != nil {
		return forge.Milestone{}, errors.New("title must not be edited")
	}
	if description == nil {
		return forge.Milestone{}, errors.New("no description given")
	}
	if f.edits == f.failAt {
		return forge.Milestone{}, f.failErr
	}
	f.desc[number] = *description
	return forge.Milestone{Number: number, Title: fmt.Sprintf("M%d", number), State: "open", Description: *description}, nil
}

func (f *fakeMilestones) description(number int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.desc[number]
}

func (f *fakeMilestones) editCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.edits
}

// prioCache is a Refresher that has built repos once; builds counts every build.
type prioCache struct {
	r      *Refresher[[]priorities.Repo]
	mu     sync.Mutex
	builds int
}

func newPrioCache(t *testing.T, repos []priorities.Repo) *prioCache {
	t.Helper()
	c := &prioCache{}
	c.r = NewRefresher(func(context.Context) ([]priorities.Repo, error) {
		c.mu.Lock()
		c.builds++
		c.mu.Unlock()
		return repos, nil
	}, time.Hour, time.Hour, time.Minute)
	if !c.r.refresh(context.Background()) {
		t.Fatal("seed build failed")
	}
	return c
}

// cached returns the description of milestone number of repo o/r in the cache.
func (c *prioCache) cached(t *testing.T, number int) string {
	t.Helper()
	repos, err := c.r.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		for _, m := range r.Milestones {
			if r.Slug == "o/r" && m.Number == number {
				return m.Description
			}
		}
	}
	t.Fatalf("milestone %d not in cache", number)
	return ""
}

// rankSetup serves f as repo o/r with milestones 4 and 6 in the cache, with the descriptions
// f holds at the start.
func rankSetup(t *testing.T, f *fakeMilestones) (http.Handler, *prioCache) {
	t.Helper()
	cache := newPrioCache(t, []priorities.Repo{{
		Slug:   "o/r",
		Issues: []priorities.Issue{},
		Milestones: []priorities.Milestone{
			{Number: 4, Title: "M4", Description: f.desc[4]},
			{Number: 6, Title: "M6", Description: f.desc[6]},
		},
	}})
	d := testDeps(populated(), nil)
	d.PriorityForges = map[string]forge.Forge{"o/r": f}
	d.PatchPriorities = cache.r.Modify
	return New(testSite(), d), cache
}

const rankHost = "127.0.0.1:8127"

// postRanks posts body to the endpoint with Host rankHost; headers holds Origin and
// Content-Type, a missing key sends no such header.
func postRanks(h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/priorities/milestone-ranks", strings.NewReader(body))
	req.Host = rankHost
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var okHeaders = map[string]string{"Origin": "http://" + rankHost, "Content-Type": "application/json"}

const swapBody = `{"repo":"o/r","changes":[{"number":6,"from":2,"to":1},{"number":4,"from":1,"to":2}]}`

func writtenOf(t *testing.T, rec *httptest.ResponseRecorder) []int {
	t.Helper()
	var out struct {
		Written []int `json:"written"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return out.Written
}

func TestMilestoneRanksSwap(t *testing.T) {
	tests := []struct {
		name         string
		desc4, desc6 string
		want4, want6 string
	}{
		{
			name:  "lf",
			desc4: "Pitch four.\n\nPriority: 1\nStatus: shaping",
			desc6: "Pitch six.\nPriority: 2",
			want4: "Pitch four.\n\nPriority: 2\nStatus: shaping",
			want6: "Pitch six.\nPriority: 1",
		},
		{
			name:  "crlf",
			desc4: "Pitch four.\r\n\r\nPriority: 1\r\nStatus: shaping",
			desc6: "Pitch six.\r\nPriority: 2\r\n",
			want4: "Pitch four.\r\n\r\nPriority: 2\r\nStatus: shaping",
			want6: "Pitch six.\r\nPriority: 1\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeMilestones{desc: map[int]string{4: tt.desc4, 6: tt.desc6}}
			h, cache := rankSetup(t, f)
			rec := postRanks(h, swapBody, okHeaders)
			if rec.Code != 200 {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if got := writtenOf(t, rec); !reflect.DeepEqual(got, []int{6, 4}) {
				t.Errorf("written = %v, want [6 4]", got)
			}
			if got := f.description(4); got != tt.want4 {
				t.Errorf("forge 4 = %q, want %q", got, tt.want4)
			}
			if got := f.description(6); got != tt.want6 {
				t.Errorf("forge 6 = %q, want %q", got, tt.want6)
			}
			if got := cache.cached(t, 4); got != tt.want4 {
				t.Errorf("cache 4 = %q, want %q", got, tt.want4)
			}
			if got := cache.cached(t, 6); got != tt.want6 {
				t.Errorf("cache 6 = %q, want %q", got, tt.want6)
			}
		})
	}
}

func swapFake() *fakeMilestones {
	return &fakeMilestones{desc: map[int]string{4: "Priority: 1", 6: "Priority: 2"}}
}

func TestMilestoneRanksNoOrigin(t *testing.T) {
	f := swapFake()
	h, _ := rankSetup(t, f)
	rec := postRanks(h, swapBody, map[string]string{"Content-Type": "application/json"})
	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403, body %s", rec.Code, rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
}

func TestMilestoneRanksWrongOrigin(t *testing.T) {
	for _, origin := range []string{"http://evil.example", "http://127.0.0.1:9999", "null"} {
		t.Run(origin, func(t *testing.T) {
			f := swapFake()
			h, _ := rankSetup(t, f)
			rec := postRanks(h, swapBody, map[string]string{"Origin": origin, "Content-Type": "application/json"})
			if rec.Code != 403 {
				t.Fatalf("status = %d, want 403, body %s", rec.Code, rec.Body)
			}
			if n := f.editCount(); n != 0 {
				t.Errorf("edits = %d, want 0", n)
			}
		})
	}
}

func TestMilestoneRanksNotJSON(t *testing.T) {
	tests := []struct {
		name        string
		contentType string // "" sends no Content-Type
	}{
		{"text/plain", "text/plain"},
		{"missing", ""},
		{"json suffix", "application/merge-patch+json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := swapFake()
			h, _ := rankSetup(t, f)
			headers := map[string]string{"Origin": "http://" + rankHost}
			if tt.contentType != "" {
				headers["Content-Type"] = tt.contentType
			}
			rec := postRanks(h, swapBody, headers)
			if rec.Code != 415 {
				t.Fatalf("status = %d, want 415, body %s", rec.Code, rec.Body)
			}
			if n := f.editCount(); n != 0 {
				t.Errorf("edits = %d, want 0", n)
			}
		})
	}
}

func TestMilestoneRanksJSONWithCharset(t *testing.T) {
	f := swapFake()
	h, _ := rankSetup(t, f)
	rec := postRanks(h, swapBody, map[string]string{
		"Origin": "http://" + rankHost, "Content-Type": "application/json; charset=utf-8"})
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body)
	}
}

func TestMilestoneRanksToBelowOne(t *testing.T) {
	f := swapFake()
	h, _ := rankSetup(t, f)
	rec := postRanks(h, `{"repo":"o/r","changes":[{"number":6,"from":2,"to":0}]}`, okHeaders)
	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422, body %s", rec.Code, rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
}

func TestMilestoneRanksOutOfBounds(t *testing.T) {
	many := strings.TrimSuffix(strings.Repeat(`{"number":6,"from":2,"to":1},`, 201), ",")
	tests := []struct{ name, body string }{
		{"number zero", `{"repo":"o/r","changes":[{"number":0,"from":2,"to":1}]}`},
		{"to above the safe range", `{"repo":"o/r","changes":[{"number":6,"from":2,"to":1000001}]}`},
		{"too many changes", `{"repo":"o/r","changes":[` + many + `]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := swapFake()
			h, _ := rankSetup(t, f)
			rec := postRanks(h, tt.body, okHeaders)
			if rec.Code != 422 {
				t.Fatalf("status = %d, want 422, body %s", rec.Code, rec.Body)
			}
			if n := f.editCount(); n != 0 {
				t.Errorf("edits = %d, want 0", n)
			}
		})
	}
}

func TestMilestoneRanksEmptyChanges(t *testing.T) {
	f := swapFake()
	h, _ := rankSetup(t, f)
	rec := postRanks(h, `{"repo":"o/r","changes":[]}`, okHeaders)
	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422, body %s", rec.Code, rec.Body)
	}
}

func TestMilestoneRanksUnknownRepo(t *testing.T) {
	f := swapFake()
	h, _ := rankSetup(t, f)
	rec := postRanks(h, `{"repo":"o/other","changes":[{"number":6,"from":2,"to":1}]}`, okHeaders)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404, body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "o/other") {
		t.Errorf("body %s does not name the repo", rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
}

func TestMilestoneRanksStale409(t *testing.T) {
	// The cache still says 4 is first and 6 second; on the forge someone made 6 third.
	f := &fakeMilestones{desc: map[int]string{4: "Priority: 1", 6: "Priority: 2"}}
	h, cache := rankSetup(t, f)
	f.desc[6] = "Priority: 3"

	rec := postRanks(h, swapBody, okHeaders)
	if rec.Code != 409 {
		t.Fatalf("status = %d, want 409, body %s", rec.Code, rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
	if got := cache.cached(t, 6); got != "Priority: 3" {
		t.Errorf("cache 6 after 409 = %q, want the fresh %q", got, "Priority: 3")
	}

	// The page reloads the cache and clicks again with the rank it now shows.
	from, ok := priorities.ParseRank(cache.cached(t, 6))
	if !ok {
		t.Fatal("cache 6 has no rank")
	}
	body := fmt.Sprintf(`{"repo":"o/r","changes":[{"number":6,"from":%d,"to":1},{"number":4,"from":1,"to":3}]}`, from)
	rec = postRanks(h, body, okHeaders)
	if rec.Code != 200 {
		t.Fatalf("retry status = %d, want 200, body %s", rec.Code, rec.Body)
	}
	if got := f.description(6); got != "Priority: 1" {
		t.Errorf("forge 6 = %q, want %q", got, "Priority: 1")
	}
	cache.mu.Lock()
	builds := cache.builds
	cache.mu.Unlock()
	if builds != 1 {
		t.Errorf("builds = %d, want 1 (no rebuild)", builds)
	}
}

func TestMilestoneRanksFromNull(t *testing.T) {
	f := &fakeMilestones{desc: map[int]string{4: "Priority: 1", 6: "Pitch six."}}
	h, cache := rankSetup(t, f)
	rec := postRanks(h, `{"repo":"o/r","changes":[{"number":6,"from":null,"to":2}]}`, okHeaders)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if got := f.description(6); got != "Pitch six.\n\nPriority: 2" {
		t.Errorf("forge 6 = %q, want %q", got, "Pitch six.\n\nPriority: 2")
	}
	if got := cache.cached(t, 6); got != "Pitch six.\n\nPriority: 2" {
		t.Errorf("cache 6 = %q", got)
	}
}

func TestMilestoneRanksFromNullButRanked409(t *testing.T) {
	f := swapFake()
	h, _ := rankSetup(t, f)
	rec := postRanks(h, `{"repo":"o/r","changes":[{"number":6,"from":null,"to":1}]}`, okHeaders)
	if rec.Code != 409 {
		t.Fatalf("status = %d, want 409, body %s", rec.Code, rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
}

func TestMilestoneRanksPartialFailure(t *testing.T) {
	f := swapFake()
	f.failAt, f.failErr = 2, errors.New("github: 500")
	h, cache := rankSetup(t, f)
	rec := postRanks(h, swapBody, okHeaders)
	if rec.Code != 502 {
		t.Fatalf("status = %d, want 502, body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "written=[6]") {
		t.Errorf("body %s does not name written=[6]", rec.Body)
	}
	if got := cache.cached(t, 6); got != "Priority: 1" {
		t.Errorf("cache 6 = %q, want the written %q", got, "Priority: 1")
	}
	if got := cache.cached(t, 4); got != "Priority: 1" {
		t.Errorf("cache 4 = %q, want the unwritten %q", got, "Priority: 1")
	}
	if got := f.description(4); got != "Priority: 1" {
		t.Errorf("forge 4 = %q, want it unchanged %q", got, "Priority: 1")
	}
	if n := f.editCount(); n != 2 {
		t.Errorf("edits = %d, want 2 (one written, one failed)", n)
	}
}

func TestMilestoneRanksNotRegisteredWithoutPatch(t *testing.T) {
	d := testDeps(populated(), nil)
	f := swapFake()
	d.PriorityForges = map[string]forge.Forge{"o/r": f}
	d.PatchPriorities = nil
	h := New(testSite(), d)
	rec := postRanks(h, swapBody, okHeaders)
	if strings.Contains(rec.Body.String(), "written") {
		t.Fatalf("endpoint answered: %d %s", rec.Code, rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
}

func TestMilestoneRanksForgeNotConfigured(t *testing.T) {
	f := swapFake()
	f.readErr = fmt.Errorf("o/r: %w", forge.ErrNotConfigured)
	h, _ := rankSetup(t, f)
	rec := postRanks(h, swapBody, okHeaders)
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503, body %s", rec.Code, rec.Body)
	}
}

func TestMilestoneRanksReadFailureIs502(t *testing.T) {
	f := swapFake()
	f.readErr = errors.New("github: 500")
	h, _ := rankSetup(t, f)
	rec := postRanks(h, swapBody, okHeaders)
	if rec.Code != 502 {
		t.Fatalf("status = %d, want 502, body %s", rec.Code, rec.Body)
	}
	if n := f.editCount(); n != 0 {
		t.Errorf("edits = %d, want 0", n)
	}
}
