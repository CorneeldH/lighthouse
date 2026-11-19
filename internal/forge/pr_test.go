package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Classification: pure-logic. Fails when the rollup gives SUCCESS while a check still has to
// come, merges a superseded run with the newest one, or swaps the order FAILURE, ERROR.
func TestRollup(t *testing.T) {
	const gha = "github-actions"
	run := func(id int64, app, name, status, conclusion string) checkRun {
		return checkRun{ID: id, App: app, Name: name, Status: status, Conclusion: conclusion}
	}
	done := func(id int64, name, conclusion string) checkRun { return run(id, gha, name, "completed", conclusion) }
	ok := done(1, "ok", "success")
	actions := func(status string, runs int) checkSuite { return checkSuite{Status: status, Runs: runs, App: gha} }
	cases := []struct {
		name     string
		runs     []checkRun
		statuses []commitStatus
		suites   []checkSuite
		want     string
	}{
		{"nothing", nil, nil, nil, ""},
		{"all success", []checkRun{ok, done(2, "lint", "neutral"), done(3, "docs", "skipped")}, nil, []checkSuite{actions("completed", 3)}, "SUCCESS"},
		{"one failure", []checkRun{ok, done(2, "lint", "failure")}, nil, nil, "FAILURE"},
		{"timed out", []checkRun{done(2, "lint", "timed_out")}, nil, nil, "FAILURE"},
		{"cancelled", []checkRun{done(2, "lint", "cancelled")}, nil, nil, "FAILURE"},
		{"action required", []checkRun{done(2, "lint", "action_required")}, nil, nil, "FAILURE"},
		{"startup failure", []checkRun{done(2, "lint", "startup_failure")}, nil, nil, "FAILURE"},
		{"status failure", []checkRun{ok}, []commitStatus{{State: "failure"}}, nil, "FAILURE"},
		{"status error", []checkRun{ok}, []commitStatus{{State: "error"}}, nil, "ERROR"},
		{"status failure before error", nil, []commitStatus{{State: "error"}, {State: "failure"}}, nil, "FAILURE"},
		{"run failure before status error", []checkRun{done(2, "lint", "failure")}, []commitStatus{{State: "error"}}, nil, "FAILURE"},
		{"run in progress", []checkRun{ok, run(2, gha, "lint", "in_progress", "")}, nil, nil, "PENDING"},
		{"run queued", []checkRun{run(2, gha, "lint", "queued", "")}, nil, nil, "PENDING"},
		{"stale", []checkRun{done(2, "lint", "stale")}, nil, nil, "PENDING"},
		{"unknown conclusion", []checkRun{done(2, "lint", "brand_new")}, nil, nil, "PENDING"},
		{"unknown run status", []checkRun{run(2, gha, "lint", "waiting", "success")}, nil, nil, "PENDING"},
		{"status pending", []checkRun{ok}, []commitStatus{{State: "pending"}}, nil, "PENDING"},
		{"unknown status state", []checkRun{ok}, []commitStatus{{State: "brand_new"}}, nil, "PENDING"},
		{"statuses only, all success", nil, []commitStatus{{State: "success"}, {State: "success"}}, nil, "SUCCESS"},
		{"actions suite in_progress, runs all success", []checkRun{ok}, nil, []checkSuite{actions("in_progress", 1)}, "PENDING"},
		{"actions suite queued without runs", []checkRun{ok}, nil, []checkSuite{actions("queued", 0)}, "PENDING"},
		{"other app queued without runs", []checkRun{ok}, nil, []checkSuite{{Status: "queued", Runs: 0, App: "some-app"}}, "SUCCESS"},
		{"other app in_progress with runs", []checkRun{ok}, nil, []checkSuite{{Status: "in_progress", Runs: 2, App: "some-app"}}, "PENDING"},
		{"no runs, no statuses, actions suite queued without runs", nil, nil, []checkSuite{actions("queued", 0)}, ""},
		// A superseded suite: concurrency cancelled the first run of ci/build, a newer run passed.
		{"superseded cancelled run, newer success", []checkRun{done(10, "build", "cancelled"), done(20, "build", "success")}, nil, nil, "SUCCESS"},
		{"superseded cancelled run listed after the newer one", []checkRun{done(20, "build", "success"), done(10, "build", "cancelled")}, nil, nil, "SUCCESS"},
		{"older success, newer failure", []checkRun{done(10, "build", "success"), done(20, "build", "failure")}, nil, nil, "FAILURE"},
		{"older failure, newer run in progress", []checkRun{done(10, "build", "failure"), run(20, gha, "build", "in_progress", "")}, nil, nil, "PENDING"},
		{"same name, other app failing", []checkRun{done(10, "build", "success"), run(5, "other-ci", "build", "completed", "failure")}, nil, nil, "FAILURE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rollup(tc.runs, tc.statuses, tc.suites); got != tc.want {
				t.Errorf("rollup = %q, want %q", got, tc.want)
			}
		})
	}
}

// Classification: pure-logic. Fails when a REST pull maps to the wrong State, Mergeable,
// CrossRepo or times.
func TestRestPullToPR(t *testing.T) {
	merged := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	closed := time.Date(2026, 11, 1, 12, 5, 0, 0, time.UTC)
	cases := []struct {
		name   string
		json   string
		closes bool
		want   PR
	}{
		{"open", `{"number": 5, "state": "open", "draft": false, "merged_at": null, "closed_at": null, "html_url": "https://github.com/o/r/pull/5",
			"head": {"sha": "abc", "ref": "issue-1-x", "repo": {"full_name": "o/r"}}, "base": {"ref": "main", "repo": {"full_name": "o/r"}}}`, true,
			PR{Number: 5, State: "OPEN", HeadSHA: "abc", HeadRef: "issue-1-x", BaseRef: "main", Closes: true, URL: "https://github.com/o/r/pull/5"}},
		{"closed", `{"number": 6, "state": "closed", "draft": false, "merged_at": null, "closed_at": "2026-11-01T12:05:00Z",
			"head": {"sha": "def", "ref": "issue-1-x", "repo": {"full_name": "o/r"}}, "base": {"ref": "main", "repo": {"full_name": "o/r"}}}`, false,
			PR{Number: 6, State: "CLOSED", Mergeable: "UNKNOWN", HeadSHA: "def", HeadRef: "issue-1-x", BaseRef: "main", ClosedAt: closed}},
		{"merged", `{"number": 7, "state": "closed", "draft": false, "merged_at": "2026-11-01T12:00:00Z", "closed_at": "2026-11-01T12:05:00Z",
			"head": {"sha": "ghi", "ref": "issue-1-x", "repo": {"full_name": "o/r"}}, "base": {"ref": "main", "repo": {"full_name": "o/r"}}}`, false,
			PR{Number: 7, State: "MERGED", Mergeable: "MERGEABLE", HeadSHA: "ghi", HeadRef: "issue-1-x", BaseRef: "main", MergedAt: merged, ClosedAt: closed}},
		{"draft", `{"number": 8, "state": "open", "draft": true,
			"head": {"sha": "jkl", "ref": "issue-1-x", "repo": {"full_name": "o/r"}}, "base": {"ref": "main", "repo": {"full_name": "o/r"}}}`, false,
			PR{Number: 8, State: "OPEN", Draft: true, HeadSHA: "jkl", HeadRef: "issue-1-x", BaseRef: "main"}},
		{"head repo deleted", `{"number": 9, "state": "open",
			"head": {"sha": "mno", "ref": "issue-1-x", "repo": null}, "base": {"ref": "main", "repo": {"full_name": "o/r"}}}`, false,
			PR{Number: 9, State: "OPEN", HeadSHA: "mno", HeadRef: "issue-1-x", BaseRef: "main", CrossRepo: true}},
		{"head in a fork", `{"number": 10, "state": "open",
			"head": {"sha": "pqr", "ref": "issue-1-x", "repo": {"full_name": "fork/r"}}, "base": {"ref": "main", "repo": {"full_name": "o/r"}}}`, false,
			PR{Number: 10, State: "OPEN", HeadSHA: "pqr", HeadRef: "issue-1-x", BaseRef: "main", CrossRepo: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p restPull
			if err := json.Unmarshal([]byte(tc.json), &p); err != nil {
				t.Fatal(err)
			}
			if got := p.pr(tc.closes); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("pr =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestMergeable(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		in   *bool
		want string
	}{{&yes, "MERGEABLE"}, {&no, "CONFLICTING"}, {nil, "UNKNOWN"}} {
		if got := mergeable(tc.in); got != tc.want {
			t.Errorf("mergeable(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// checkList is one of the three lists checks reads: its items, and the total_count every page
// claims (-1: the number of items).
type checkList struct {
	items []any
	total int
	state string // only the combined status: its top-level state
	// pageTotals, when set, is the total_count of page 1, 2, … instead of total.
	pageTotals []int
}

// checksServer serves the check-runs, the combined status and the check-suites of commit abc
// in o/r, per_page items a page with a Link to the next page, and counts the requests per list.
type checksServer struct {
	t      *testing.T
	srv    *httptest.Server
	lists  map[string]checkList // "check-runs", "status", "check-suites"
	mu     sync.Mutex
	counts map[string]int
}

var checkListKeys = map[string]string{"check-runs": "check_runs", "status": "statuses", "check-suites": "check_suites"}

func newChecksServer(t *testing.T, lists map[string]checkList) (*checksServer, *GitHub) {
	t.Helper()
	cs := &checksServer{t: t, lists: lists, counts: map[string]int{}}
	cs.srv = httptest.NewServer(http.HandlerFunc(cs.serve))
	t.Cleanup(cs.srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = cs.srv.URL
	return cs, c
}

func (cs *checksServer) serve(w http.ResponseWriter, r *http.Request) {
	name, found := strings.CutPrefix(r.URL.Path, "/repos/o/r/commits/abc/")
	key, ok := checkListKeys[name]
	if !found || !ok {
		cs.t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
		return
	}
	cs.mu.Lock()
	cs.counts[name]++
	cs.mu.Unlock()
	q := r.URL.Query()
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per != 100 {
		cs.t.Errorf("%s: per_page = %q, want 100", r.URL, q.Get("per_page"))
		per = 100
	}
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 1)
	l := cs.lists[name]
	total := l.total
	if total < 0 {
		total = len(l.items)
	}
	if page <= len(l.pageTotals) {
		total = l.pageTotals[page-1]
	}
	start := min((page-1)*per, len(l.items))
	end := min(start+per, len(l.items))
	if end < len(l.items) {
		q.Set("page", strconv.Itoa(page+1))
		next := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: q.Encode()}
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next.String()))
	}
	body := map[string]any{"total_count": total, key: append([]any{}, l.items[start:end]...)}
	if name == "status" {
		body["state"] = l.state
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (cs *checksServer) count(name string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.counts[name]
}

func restRun(id int, name, status, conclusion string) any {
	var c any
	if conclusion != "" {
		c = conclusion
	}
	return map[string]any{"id": id, "name": name, "status": status, "conclusion": c, "app": map[string]any{"slug": "github-actions"}}
}

func restSuite(status string, runs int) any {
	return map[string]any{"status": status, "latest_check_runs_count": runs, "app": map[string]any{"slug": "github-actions"}}
}

func restStatus(state string) any { return map[string]any{"state": state, "context": "ci/" + state} }

// greenLists are three complete, green lists: one successful run, no statuses, a completed suite.
func greenLists() map[string]checkList {
	return map[string]checkList{
		"check-runs":   {items: []any{restRun(1, "build", "completed", "success")}, total: -1},
		"status":       {items: []any{}, total: -1, state: "pending"},
		"check-suites": {items: []any{restSuite("completed", 1)}, total: -1},
	}
}

// Classification: io (HTTP). Fails when checks rolls up a list shorter than its total_count:
// the item that is missing could be the one failure or the one suite still running.
func TestChecksIncompleteListFails(t *testing.T) {
	cases := map[string]checkList{
		// the third run, a failure, is missing
		"check-runs": {items: []any{restRun(1, "build", "completed", "success"), restRun(2, "lint", "completed", "success")}, total: 3},
		// the third status, a failure, is missing
		"status": {items: []any{restStatus("success"), restStatus("success")}, total: 3, state: "failure"},
		// the third suite, an Actions suite in progress, is missing
		"check-suites": {items: []any{restSuite("completed", 1), restSuite("completed", 1)}, total: 3},
	}
	for name, short := range cases {
		t.Run(name, func(t *testing.T) {
			lists := greenLists()
			lists[name] = short
			_, c := newChecksServer(t, lists)
			got, err := c.checks(context.Background(), "o/r", "abc")
			if err == nil || got != "" {
				t.Fatalf("checks = %q, %v; want an error and no state", got, err)
			}
		})
	}
}

// Classification: io (HTTP). Fails when checks accepts a list whose total_count grew between
// two pages: a run created in between shifts the pages, so page 2 repeats an item of page 1
// and the new run is never read, while the number read still matches the last total_count.
func TestChecksTotalChangedBetweenPagesFails(t *testing.T) {
	lists := greenLists()
	var runs []any
	for i := range 150 {
		runs = append(runs, restRun(i+1, fmt.Sprintf("job-%d", i), "completed", "success"))
	}
	runs = append(runs, runs[99]) // page 2 starts with the last run of page 1 again
	lists["check-runs"] = checkList{items: runs, pageTotals: []int{150, 151}}
	_, c := newChecksServer(t, lists)
	got, err := c.checks(context.Background(), "o/r", "abc")
	if err == nil || got != "" {
		t.Fatalf("checks = %q, %v; want an error and no state", got, err)
	}
}

// Classification: io (HTTP). Fails when checks reads only the first page of a list.
func TestChecksFollowPages(t *testing.T) {
	cases := map[string]struct {
		item func(i int) any // item i of 150; i == 140 is on page 2 and is the bad one
		want string
	}{
		"check-runs": {func(i int) any {
			if i == 140 {
				return restRun(i+1, fmt.Sprintf("job-%d", i), "completed", "failure")
			}
			return restRun(i+1, fmt.Sprintf("job-%d", i), "completed", "success")
		}, "FAILURE"},
		"status": {func(i int) any {
			if i == 140 {
				return restStatus("failure")
			}
			return restStatus("success")
		}, "FAILURE"},
		"check-suites": {func(i int) any {
			if i == 140 {
				return restSuite("in_progress", 1)
			}
			return restSuite("completed", 1)
		}, "PENDING"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lists := greenLists()
			l := checkList{total: -1, state: "success"}
			for i := range 150 {
				l.items = append(l.items, tc.item(i))
			}
			lists[name] = l
			cs, c := newChecksServer(t, lists)
			got, err := c.checks(context.Background(), "o/r", "abc")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("checks = %q, want %q", got, tc.want)
			}
			if n := cs.count(name); n != 2 {
				t.Errorf("%s requests = %d, want 2 pages", name, n)
			}
		})
	}
}

// Classification: io (HTTP). Fails when checks reads the top-level state of the combined
// status, which GitHub answers "pending" for a commit without any status.
func TestChecksNoStatusesIsSuccess(t *testing.T) {
	_, c := newChecksServer(t, greenLists())
	got, err := c.checks(context.Background(), "o/r", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "SUCCESS" {
		t.Errorf("checks = %q, want SUCCESS", got)
	}
}

// Classification: io (fake forge). Fails when BranchPRs stops reading mergeable and checks of
// an open PR, or reads the checks of a merged one.
func TestBranchPRsREST(t *testing.T) {
	srv, c := boardClient(t)
	srv.Branches = map[string][]map[string]any{"issue-1-x": {
		{"number": 11, "state": "OPEN", "isDraft": false, "mergeable": "CONFLICTING", "headRefOid": "sha-open",
			"headRefName": "issue-1-x", "baseRefName": "main", "url": "https://github.com/o/r/pull/11",
			"statusCheckRollup": map[string]any{"state": "SUCCESS"}},
		{"number": 10, "state": "MERGED", "isDraft": false, "mergeable": "UNKNOWN", "headRefOid": "sha-merged",
			"headRefName": "issue-1-x", "baseRefName": "main", "url": "https://github.com/o/r/pull/10",
			"mergedAt": "2026-11-01T12:00:00Z", "closedAt": "2026-11-01T12:00:00Z",
			"statusCheckRollup": map[string]any{"state": "SUCCESS"}},
	}}
	got, err := c.BranchPRs(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	want := []PR{
		{Number: 11, State: "OPEN", Mergeable: "CONFLICTING", HeadSHA: "sha-open", HeadRef: "issue-1-x", BaseRef: "main",
			Checks: "SUCCESS", URL: "https://github.com/o/r/pull/11"},
		{Number: 10, State: "MERGED", Mergeable: "MERGEABLE", HeadSHA: "sha-merged", HeadRef: "issue-1-x", BaseRef: "main",
			URL: "https://github.com/o/r/pull/10", MergedAt: at, ClosedAt: at},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BranchPRs =\n%+v\nwant\n%+v", got, want)
	}
	if n := srv.Count("/repos/o/r/pulls?head=issue-1-x"); n != 1 {
		t.Errorf("list requests = %d, want 1", n)
	}
	for _, path := range []string{"/repos/o/r/pulls/10", "/repos/o/r/commits/sha-merged/check-runs",
		"/repos/o/r/commits/sha-merged/status", "/repos/o/r/commits/sha-merged/check-suites"} {
		if n := srv.Count(path); n != 0 {
			t.Errorf("Count(%s) = %d, want 0 for a merged PR", path, n)
		}
	}
}

// Classification: io (fake forge). Fails when a request that cannot be read leaves BranchPRs
// with PRs whose checks or mergeable are missing.
func TestBranchPRsEnrichErrorFails(t *testing.T) {
	for _, path := range []string{"/repos/o/r/pulls/11", "/repos/o/r/commits/sha-open/check-runs",
		"/repos/o/r/commits/sha-open/status", "/repos/o/r/commits/sha-open/check-suites"} {
		t.Run(path, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.Branches = map[string][]map[string]any{"issue-1-x": {{"number": 11, "state": "OPEN",
				"mergeable": "MERGEABLE", "headRefOid": "sha-open", "headRefName": "issue-1-x",
				"statusCheckRollup": map[string]any{"state": "SUCCESS"}}}}
			srv.Fail = map[string]int{"GET " + path: 502}
			got, err := c.BranchPRs(context.Background(), "issue-1-x")
			if err == nil || got != nil {
				t.Fatalf("BranchPRs = %+v, %v; want an error and no PRs", got, err)
			}
		})
	}
}

// Classification: io (fake forge). Fails when the branch goes into the query unescaped: a
// space or a plus would reach GitHub as another branch.
func TestBranchPRsEscapesBranch(t *testing.T) {
	for _, branch := range []string{"fix/a b", "fix/a+b&c"} {
		t.Run(branch, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.Branches = map[string][]map[string]any{branch: {{"number": 3, "state": "CLOSED", "headRefOid": "s", "headRefName": branch}}}
			got, err := c.BranchPRs(context.Background(), branch)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Number != 3 {
				t.Errorf("BranchPRs = %+v, want PR 3", got)
			}
			if n := srv.Count("/repos/o/r/pulls?head=" + branch); n != 1 {
				t.Errorf("list requests for %q = %d, want 1", branch, n)
			}
		})
	}
}

// Classification: io (fake forge). Fails when BranchPRs reads only the first page.
func TestBranchPRsFollowsPages(t *testing.T) {
	srv, c := boardClient(t)
	var nodes []map[string]any
	for i := range 150 {
		nodes = append(nodes, map[string]any{"number": i + 1, "state": "CLOSED", "headRefOid": fmt.Sprint("s", i), "headRefName": "issue-1-x"})
	}
	srv.Branches = map[string][]map[string]any{"issue-1-x": nodes}
	got, err := c.BranchPRs(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 150 || got[0].Number != 1 || got[149].Number != 150 {
		t.Fatalf("BranchPRs = %d PRs, want 150 from 1 to 150", len(got))
	}
	if n := srv.Count("/repos/o/r/pulls?head=issue-1-x"); n != 2 {
		t.Errorf("list requests = %d, want 2 pages", n)
	}
}
