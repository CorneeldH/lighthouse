package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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

// milestoneServer answers the candidate list and the events of each issue, and keeps every
// request it got as "path?rawquery".
type milestoneServer struct {
	mu       sync.Mutex
	requests []string
	c        *GitHub
}

func newMilestoneServer(t *testing.T, list string, events map[int]func(w http.ResponseWriter, r *http.Request)) *milestoneServer {
	t.Helper()
	s := &milestoneServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.URL.Path+"?"+r.URL.RawQuery)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/o/r/issues" {
			_, _ = fmt.Fprint(w, list)
			return
		}
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/o/r/issues/%d/events", &n); err == nil && events[n] != nil {
			events[n](w, r)
			return
		}
		t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	s.c = New("o/r", "tok")
	s.c.BaseURL = srv.URL
	return s
}

func candidate(n int, milestone string, extra string) string {
	return fmt.Sprintf(`{"number":%d,"title":"Issue %d","state":"open","milestone":{"number":3,"title":%q,"state":"open"}%s}`, n, n, milestone, extra)
}

func eventsOf(evs ...string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintf(w, "[%s]", strings.Join(evs, ",")) }
}

func milestonedEvent(at, title string) string {
	return fmt.Sprintf(`{"event":"milestoned","created_at":%q,"milestone":{"title":%q}}`, at, title)
}

func TestMilestonedIssuesInPeriod(t *testing.T) {
	s := newMilestoneServer(t, "["+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: eventsOf(`{"event":"labeled","created_at":"2026-11-16T09:00:00Z"}`, milestonedEvent("2026-11-16T10:00:00Z", "M3")),
	})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Issue.Number != 5 || got[0].Milestone.Number != 3 || got[0].Milestone.Title != "M3" ||
		!got[0].At.Equal(time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("MilestonedIssues = %+v, want #5 in M3 (number 3) at 10:00Z", got)
	}
}

func TestMilestonedIssuesEventOutsidePeriod(t *testing.T) {
	for name, at := range map[string]string{
		"before since": "2026-11-15T22:59:59Z",
		"at until":     "2026-11-16T23:00:00Z",
	} {
		t.Run(name, func(t *testing.T) {
			s := newMilestoneServer(t, "["+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
				5: eventsOf(milestonedEvent(at, "M3")),
			})
			got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
			if err != nil || len(got) != 0 {
				t.Errorf("MilestonedIssues = %+v, %v; want none", got, err)
			}
		})
	}
}

func TestMilestonedIssuesNowInOtherMilestone(t *testing.T) {
	s := newMilestoneServer(t, "["+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: eventsOf(milestonedEvent("2026-11-16T10:00:00Z", "M2")),
	})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil || len(got) != 0 {
		t.Errorf("MilestonedIssues = %+v, %v; want none", got, err)
	}
}

func TestMilestonedIssuesLastEventWins(t *testing.T) {
	s := newMilestoneServer(t, "["+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: eventsOf(milestonedEvent("2026-11-16T12:00:00Z", "M3"), milestonedEvent("2026-11-16T08:00:00Z", "M3")),
	})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil || len(got) != 1 || !got[0].At.Equal(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("MilestonedIssues = %+v, %v; want #5 at 12:00Z", got, err)
	}
}

func TestMilestonedIssuesSkipsPullRequests(t *testing.T) {
	// No events route for 6: a request for it is an unexpected request and fails the test.
	s := newMilestoneServer(t, "["+candidate(6, "M3", `,"pull_request":{}`)+","+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: eventsOf(milestonedEvent("2026-11-16T10:00:00Z", "M3")),
	})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil || len(got) != 1 || got[0].Issue.Number != 5 {
		t.Errorf("MilestonedIssues = %+v, %v; want only #5", got, err)
	}
}

func TestMilestonedIssuesPaginatesEvents(t *testing.T) {
	var base string
	s := newMilestoneServer(t, "["+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				_, _ = fmt.Fprintf(w, "[%s]", milestonedEvent("2026-11-16T10:00:00Z", "M3"))
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/issues/5/events?per_page=100&page=2>; rel="next"`, base))
			_, _ = fmt.Fprint(w, `[{"event":"labeled","created_at":"2026-11-16T09:00:00Z"}]`)
		},
	})
	base = s.c.BaseURL
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil || len(got) != 1 || got[0].Issue.Number != 5 {
		t.Errorf("MilestonedIssues = %+v, %v; want #5 from the event on page 2", got, err)
	}
}

func TestMilestonedIssuesEventsErrorFailsMethod(t *testing.T) {
	s := newMilestoneServer(t, "["+candidate(5, "M3", "")+","+candidate(7, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: eventsOf(milestonedEvent("2026-11-16T10:00:00Z", "M3")),
		7: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		},
	})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err == nil || got != nil {
		t.Errorf("MilestonedIssues = %+v, %v; want an error and no list", got, err)
	}
}

func TestMilestonedIssuesCandidatesRequest(t *testing.T) {
	s := newMilestoneServer(t, "[]", nil)
	if _, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 1 {
		t.Fatalf("requests = %v, want one", s.requests)
	}
	path, raw, _ := strings.Cut(s.requests[0], "?")
	q := httptest.NewRequest(http.MethodGet, "/x?"+raw, nil).URL.Query()
	if path != "/repos/o/r/issues" || q.Get("state") != "open" || q.Get("milestone") != "*" || q.Get("since") != "2026-11-16T00:00:00+01:00" {
		t.Errorf("request %q: want /repos/o/r/issues with state=open, milestone=*, since=2026-11-16T00:00:00+01:00", s.requests[0])
	}
	if !strings.Contains(raw, "since=2026-11-16T00%3A00%3A00%2B01%3A00") {
		t.Errorf("raw query %q: since must be URL-encoded", raw)
	}
}

func TestMilestonedIssuesSortedByMilestoneThenNumber(t *testing.T) {
	ev := eventsOf(milestonedEvent("2026-11-16T10:00:00Z", "M3"))
	ev2 := eventsOf(milestonedEvent("2026-11-16T10:00:00Z", "M2"))
	s := newMilestoneServer(t, "["+candidate(9, "M3", "")+","+candidate(5, "M3", "")+","+candidate(7, "M2", "")+"]",
		map[int]func(http.ResponseWriter, *http.Request){9: ev, 5: ev, 7: ev2})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, m := range got {
		nums = append(nums, m.Issue.Number)
	}
	if fmt.Sprint(nums) != "[7 5 9]" {
		t.Errorf("order = %v, want [7 5 9] (M2 first, then M3 by number)", nums)
	}
}

func TestMilestonedIssuesIgnoresDemilestoned(t *testing.T) {
	s := newMilestoneServer(t, "["+candidate(5, "M3", "")+"]", map[int]func(http.ResponseWriter, *http.Request){
		5: eventsOf(`{"event":"demilestoned","created_at":"2026-11-16T10:00:00Z","milestone":{"title":"M3"}}`),
	})
	got, err := s.c.MilestonedIssues(context.Background(), periodSince, periodUntil)
	if err != nil || len(got) != 0 {
		t.Errorf("MilestonedIssues = %+v, %v; want none", got, err)
	}
}

func statusNode(n int, draft bool, mergeable string, checks string) map[string]any {
	node := map[string]any{
		"number": n, "title": fmt.Sprintf("PR %d", n), "url": fmt.Sprintf("https://github.com/o/r/pull/%d", n),
		"isDraft": draft, "mergeable": mergeable,
	}
	if checks != "" {
		node["statusCheckRollup"] = map[string]any{"state": checks}
	}
	return node
}

func TestOpenPRsWithStatusFields(t *testing.T) {
	srv, c := boardClient(t)
	srv.OpenPRNodes = []map[string]any{
		statusNode(9, true, "UNKNOWN", ""),
		statusNode(7, false, "CONFLICTING", "FAILURE"),
	}
	got, err := c.OpenPRsWithStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []PRSummary{
		{Number: 7, Title: "PR 7", URL: "https://github.com/o/r/pull/7", Mergeable: "CONFLICTING", Checks: "FAILURE"},
		{Number: 9, Title: "PR 9", URL: "https://github.com/o/r/pull/9", Draft: true, Mergeable: "UNKNOWN", Checks: ""},
	}
	if !slices.Equal(got, want) {
		t.Errorf("OpenPRsWithStatus = %+v, want %+v", got, want)
	}
}

func TestOpenPRsWithStatusPaginates(t *testing.T) {
	srv, c := boardClient(t)
	for n := 101; n >= 1; n-- { // newest first, as GitHub lists them
		srv.OpenPRNodes = append(srv.OpenPRNodes, statusNode(n, false, "MERGEABLE", "SUCCESS"))
	}
	got, err := c.OpenPRsWithStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 {
		t.Fatalf("OpenPRsWithStatus = %d PRs, want 101 over two pages", len(got))
	}
	for i, p := range got {
		if p.Number != i+1 {
			t.Fatalf("got[%d].Number = %d, want %d: not sorted by number", i, p.Number, i+1)
		}
	}
	if n := srv.Count("OpenPRsWithStatus"); n != 2 {
		t.Errorf("OpenPRsWithStatus requests = %d, want 2", n)
	}
}

func TestOpenPRsWithStatusGraphQLError(t *testing.T) {
	srv, c := boardClient(t)
	srv.OpenPRNodes = []map[string]any{statusNode(1, false, "MERGEABLE", "")}
	srv.GraphQLErrors = []string{"Resource limit exceeded"}
	got, err := c.OpenPRsWithStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Resource limit exceeded") {
		t.Fatalf("err = %v, want the GraphQL message", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestClosedIssuesSkipsDuplicate(t *testing.T) {
	s := newSearchServer(t, answer(2, false,
		closedItem(1, "2026-11-16T08:00:00Z", "duplicate"),
		closedItem(2, "2026-11-16T09:00:00Z", "completed"),
	))
	got, err := s.c.ClosedIssues(context.Background(), periodSince, periodUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 2 {
		t.Errorf("ClosedIssues = %+v, want only #2", got)
	}
}
