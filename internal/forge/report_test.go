package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// searchServer answers GET /search/issues with handler and keeps the raw queries it got.
type searchServer struct {
	mu   sync.Mutex
	raws []string
	c    *GitHub
}

func newSearchServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *searchServer {
	t.Helper()
	s := &searchServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/issues" {
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		s.raws = append(s.raws, r.URL.RawQuery)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	s.c = New("o/r", "tok")
	s.c.BaseURL = srv.URL
	return s
}

func answer(total int, incomplete bool, items ...string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"total_count":%d,"incomplete_results":%t,"items":[%s]}`, total, incomplete, strings.Join(items, ","))
	}
}

func prItem(n int, mergedAt string) string {
	return fmt.Sprintf(`{"number":%d,"title":"PR %d","html_url":"https://github.com/o/r/pull/%d","draft":false,"state":"closed","closed_at":%q,"pull_request":{"merged_at":%q}}`, n, n, n, mergedAt, mergedAt)
}

func closedItem(n int, closedAt, reason string) string {
	r := ""
	if reason != "" {
		r = fmt.Sprintf(`,"state_reason":%q`, reason)
	}
	return fmt.Sprintf(`{"number":%d,"title":"Issue %d","html_url":"https://github.com/o/r/issues/%d","state":"closed","closed_at":%q%s}`, n, n, n, closedAt, r)
}

var (
	periodSince = time.Date(2026, 11, 16, 0, 0, 0, 0, time.FixedZone("CET", 3600))
	periodUntil = time.Date(2026, 11, 17, 0, 0, 0, 0, time.FixedZone("CET", 3600))
)

func TestMergedPRsQuery(t *testing.T) {
	s := newSearchServer(t, answer(0, false))
	if _, err := s.c.MergedPRs(context.Background(), periodSince, periodUntil); err != nil {
		t.Fatal(err)
	}
	if len(s.raws) != 1 {
		t.Fatalf("requests = %d, want 1", len(s.raws))
	}
	raw := s.raws[0]
	if !strings.Contains(raw, "%2B01%3A00") || strings.Contains(raw, "+01:00") {
		t.Errorf("raw query %q: the offset must be %%2B01%%3A00", raw)
	}
	r := httptest.NewRequest(http.MethodGet, "/search/issues?"+raw, nil)
	want := "repo:o/r is:pr is:merged merged:2026-11-16T00:00:00+01:00..2026-11-17T00:00:00+01:00"
	if got := r.URL.Query().Get("q"); got != want {
		t.Errorf("q = %q, want %q", got, want)
	}
}

func TestClosedIssuesQuery(t *testing.T) {
	s := newSearchServer(t, answer(0, false))
	if _, err := s.c.ClosedIssues(context.Background(), periodSince, periodUntil); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/search/issues?"+s.raws[0], nil)
	want := "repo:o/r is:issue is:closed closed:2026-11-16T00:00:00+01:00..2026-11-17T00:00:00+01:00"
	if got := r.URL.Query().Get("q"); got != want {
		t.Errorf("q = %q, want %q", got, want)
	}
}

func TestMergedPRsDropsResultAtUntil(t *testing.T) {
	s := newSearchServer(t, answer(3, false,
		prItem(3, "2026-11-16T23:00:00Z"), // exactly until
		prItem(2, "2026-11-16T10:00:00Z"),
		prItem(1, "2026-11-15T23:00:00Z"), // exactly since
	))
	got, err := s.c.MergedPRs(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Fatalf("MergedPRs = %+v, want #1 then #2", got)
	}
	if got[0].Title != "PR 1" || got[0].URL != "https://github.com/o/r/pull/1" || got[0].Mergeable != "" ||
		!got[0].MergedAt.Equal(time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("MergedPRs[0] = %+v", got[0])
	}
}

func TestMergedPRsPaginates(t *testing.T) {
	var srvURL string
	s := newSearchServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			answer(2, false, prItem(2, "2026-11-16T09:00:00Z"))(w, r)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/search/issues?q=x&page=2>; rel="next"`, srvURL))
		answer(2, false, prItem(1, "2026-11-16T08:00:00Z"))(w, r)
	})
	srvURL = s.c.BaseURL
	got, err := s.c.MergedPRs(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Errorf("MergedPRs = %+v, want #1 and #2", got)
	}
}

func TestMergedPRsErrorsAbove1000(t *testing.T) {
	s := newSearchServer(t, answer(1001, false, prItem(1, "2026-11-16T08:00:00Z")))
	got, err := s.c.MergedPRs(context.Background(), periodSince, periodUntil)
	if err == nil || !strings.Contains(err.Error(), "1000") || got != nil {
		t.Errorf("MergedPRs = %+v, %v; want nil and an error naming 1000", got, err)
	}
}

func TestMergedPRsErrorsOnIncomplete(t *testing.T) {
	s := newSearchServer(t, answer(1, true, prItem(1, "2026-11-16T08:00:00Z")))
	got, err := s.c.MergedPRs(context.Background(), periodSince, periodUntil)
	if err == nil || !strings.Contains(err.Error(), "incomplete") || got != nil {
		t.Errorf("MergedPRs = %+v, %v; want nil and an incomplete error", got, err)
	}
}

func TestMergedPRsForgeError(t *testing.T) {
	s := newSearchServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	got, err := s.c.MergedPRs(context.Background(), periodSince, periodUntil)
	if err == nil || got != nil {
		t.Errorf("MergedPRs = %+v, %v; want nil and an error", got, err)
	}
}

func TestClosedIssuesSkipsNotPlanned(t *testing.T) {
	s := newSearchServer(t, answer(3, false,
		closedItem(1, "2026-11-16T08:00:00Z", "completed"),
		closedItem(2, "2026-11-16T09:00:00Z", "not_planned"),
		closedItem(3, "2026-11-16T10:00:00Z", ""),
	))
	got, err := s.c.ClosedIssues(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 3 {
		t.Fatalf("ClosedIssues = %+v, want #1 and #3", got)
	}
	if got[0].StateReason != "completed" || got[1].StateReason != "" {
		t.Errorf("StateReason = %q, %q; want completed and empty", got[0].StateReason, got[1].StateReason)
	}
}

func TestClosedIssuesDropsResultAtUntil(t *testing.T) {
	s := newSearchServer(t, answer(3, false,
		closedItem(3, "2026-11-16T23:00:00Z", "completed"), // exactly until
		closedItem(2, "2026-11-16T12:00:00Z", "completed"),
		closedItem(1, "2026-11-15T23:00:00Z", "completed"), // exactly since
	))
	got, err := s.c.ClosedIssues(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Errorf("ClosedIssues = %+v, want #1 then #2", got)
	}
}
