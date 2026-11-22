package forge

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

// planIssue is an open issue in GitHub's REST shape, with an optional milestone.
func planIssue(number int, milestone map[string]any) map[string]any {
	it := map[string]any{
		"number": number, "title": fmt.Sprintf("Issue %d", number), "state": "open",
		"html_url": fmt.Sprintf("https://github.com/o/r/issues/%d", number),
	}
	if milestone != nil {
		it["milestone"] = milestone
	}
	return it
}

func planBlocker(srv *forgetest.Server, repo string, number int, state string) map[string]any {
	return map[string]any{"number": number, "state": state, "repository_url": srv.URL() + "/repos/" + repo}
}

func TestGitHubPlanningMapsIssuesAndBlockers(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open", "description": "Eerste."}
	srv.Issues = map[int]map[string]any{
		1: planIssue(1, m1),
		2: planIssue(2, nil),
		3: {"number": 3, "title": "Dicht", "state": "closed"},
	}
	srv.BlockedBy = map[int][]map[string]any{2: {
		planBlocker(srv, "o/r", 1, "open"),
		planBlocker(srv, "other/repo", 9, "open"),
		planBlocker(srv, "o/r", 3, "closed"),
	}}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 2 {
		t.Fatalf("Issues = %+v, want 2 (the closed #3 is not listed)", got.Issues)
	}
	want := []Blocker{{Repo: "o/r", Issue: 1, State: "OPEN"}, {Repo: "other/repo", Issue: 9, State: "OPEN"}}
	if !reflect.DeepEqual(got.Issues[1].BlockedBy, want) {
		t.Errorf("Issues[1].BlockedBy = %+v, want %+v (closed #3 left out)", got.Issues[1].BlockedBy, want)
	}
	first := got.Issues[0]
	if first.Number != 1 || first.Title != "Issue 1" || first.URL != "https://github.com/o/r/issues/1" {
		t.Errorf("Issues[0] = %+v, want #1 with title and URL", first)
	}
	if first.Milestone == nil || *first.Milestone != (Milestone{Number: 1, Title: "M1", State: "open", Description: "Eerste."}) {
		t.Errorf("Issues[0].Milestone = %+v, want M1 open with its description", first.Milestone)
	}
	if got.Issues[1].Milestone != nil || len(got.Issues[0].BlockedBy) != 0 {
		t.Errorf("Issues[1].Milestone = %+v, Issues[0].BlockedBy = %+v; want none", got.Issues[1].Milestone, got.Issues[0].BlockedBy)
	}
}

func TestGitHubPlanningKeepsClosedMilestoneState(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{
		4: planIssue(4, map[string]any{"number": 7, "title": "Oud", "state": "closed"}),
	}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Milestone == nil || got.Issues[0].Milestone.State != "closed" {
		t.Fatalf("Issues = %+v, want one open issue whose milestone state is closed", got.Issues)
	}
	if got.Milestones == nil || len(got.Milestones) != 0 {
		t.Errorf("Milestones = %#v, want an empty non-nil list (the closed milestone is no open milestone)", got.Milestones)
	}
}

func TestGitHubPlanningCountsIssuesOnly(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.Milestones = []map[string]any{
		{"number": 1, "title": "M1", "state": "open", "description": "d"},
		{"number": 2, "title": "M2 — leeg", "state": "open"},
		{"number": 3, "title": "Dicht", "state": "closed"},
	}
	srv.Issues = map[int]map[string]any{}
	add := func(n int, state, reason string, pr bool) {
		it := planIssue(n, m1)
		it["state"] = state
		if reason != "" {
			it["state_reason"] = reason
		}
		if pr {
			it["pull_request"] = map[string]any{}
		}
		srv.Issues[n] = it
	}
	for n := 1; n <= 3; n++ {
		add(n, "open", "", false)
	}
	add(4, "closed", "completed", false)
	add(5, "closed", "completed", false)
	add(6, "closed", "not_planned", false)
	add(7, "open", "", true)
	add(8, "closed", "completed", true)
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []MilestoneCount{
		{Milestone: Milestone{Number: 1, Title: "M1", State: "open", Description: "d"}, Open: 3, Done: 2, Closed: []ClosedIssue{
			{Number: 4, Title: "Issue 4", URL: "https://github.com/o/r/issues/4"},
			{Number: 5, Title: "Issue 5", URL: "https://github.com/o/r/issues/5"},
		}},
		{Milestone: Milestone{Number: 2, Title: "M2 — leeg", State: "open"}, Closed: []ClosedIssue{}},
	}
	if !reflect.DeepEqual(got.Milestones, want) {
		t.Errorf("Milestones = %+v, want %+v (open issues and completed ones; no PRs, no not-planned; closed milestone left out)", got.Milestones, want)
	}
}

func TestGitHubPlanningPaginates(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{}
	for n := 1; n <= 101; n++ {
		srv.Issues[n] = planIssue(n, nil)
	}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, is := range got.Issues {
		if seen[is.Number] {
			t.Errorf("issue #%d twice", is.Number)
		}
		seen[is.Number] = true
	}
	if len(got.Issues) != 101 || len(seen) != 101 {
		t.Fatalf("got %d issues (%d distinct), want 101", len(got.Issues), len(seen))
	}
	if n := srv.Count("/repos/o/r/issues"); n != 2 {
		t.Errorf("Count(issues) = %d, want 2 pages", n)
	}
}

func TestGitHubPlanningMilestonesPaginate(t *testing.T) {
	srv, c := boardClient(t)
	for n := 1; n <= 101; n++ {
		srv.Milestones = append(srv.Milestones, map[string]any{"number": n, "title": fmt.Sprintf("M%d", n), "state": "open"})
	}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Milestones) != 101 || got.Milestones[100].Title != "M101" {
		t.Fatalf("got %d milestones, want 101 ending at M101", len(got.Milestones))
	}
}

func TestPlanningReadsLabels(t *testing.T) {
	srv, c := boardClient(t)
	labelled := planIssue(1, nil)
	labelled["labels"] = []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}
	srv.Issues = map[int]map[string]any{1: labelled, 2: planIssue(2, nil)}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 2 {
		t.Fatalf("Issues = %+v, want 2", got.Issues)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(got.Issues[0].Labels, want) {
		t.Errorf("Issues[0].Labels = %#v, want %#v", got.Issues[0].Labels, want)
	}
	if want := []string{}; !reflect.DeepEqual(got.Issues[1].Labels, want) {
		t.Errorf("Issues[1].Labels = %#v, want an empty non-nil list", got.Issues[1].Labels)
	}
}

func TestPlanningClosedIssues(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.Milestones = []map[string]any{
		{"number": 1, "title": "M1", "state": "open"},
		{"number": 2, "title": "M2", "state": "open"},
	}
	srv.Issues = map[int]map[string]any{}
	add := func(n int, state, reason string, pr bool) {
		it := planIssue(n, m1)
		it["state"] = state
		if reason != "" {
			it["state_reason"] = reason
		}
		if pr {
			it["pull_request"] = map[string]any{}
		}
		srv.Issues[n] = it
	}
	add(9, "closed", "completed", false)
	add(4, "closed", "completed", false)
	add(6, "closed", "not_planned", false)
	add(8, "closed", "completed", true)
	add(10, "closed", "", false) // an old issue without a close reason: stateReason null
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []ClosedIssue{
		{Number: 4, Title: "Issue 4", URL: "https://github.com/o/r/issues/4"},
		{Number: 9, Title: "Issue 9", URL: "https://github.com/o/r/issues/9"},
		{Number: 10, Title: "Issue 10", URL: "https://github.com/o/r/issues/10"},
	}
	if !reflect.DeepEqual(got.Milestones[0].Closed, want) || got.Milestones[0].Done != 3 {
		t.Errorf("M1 Closed = %+v, Done = %d; want completed and reasonless issues #4, #9 and #10 in number order, Done 3", got.Milestones[0].Closed, got.Milestones[0].Done)
	}
	if got.Milestones[1].Closed == nil || len(got.Milestones[1].Closed) != 0 {
		t.Errorf("M2 Closed = %#v, want an empty non-nil list", got.Milestones[1].Closed)
	}
}

// Classification: contract. Fails when Planning reads any of its data over GraphQL again.
func TestPlanningNoGraphQL(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.Milestones = []map[string]any{m1}
	srv.Issues = map[int]map[string]any{1: planIssue(1, m1), 2: planIssue(2, nil)}
	srv.BlockedBy = map[int][]map[string]any{2: {planBlocker(srv, "o/r", 1, "open")}}
	if _, err := c.Planning(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := srv.GraphQLCount(); n != 0 {
		t.Errorf("GraphQLCount = %d, want 0: Planning reads REST only", n)
	}
}

// Classification: io. Fails when a page of the second read is asked without If-None-Match, or
// when the second read gives another result than the first.
func TestPlanningSecondReadIs304(t *testing.T) {
	srv := forgetest.New(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.Milestones = []map[string]any{m1}
	srv.Issues = map[int]map[string]any{1: planIssue(1, m1), 2: planIssue(2, nil), 3: planIssue(3, m1)}
	srv.Issues[3]["state"] = "closed"
	srv.Issues[3]["state_reason"] = "completed"
	srv.BlockedBy = map[int][]map[string]any{2: {planBlocker(srv, "o/r", 1, "open")}}
	f, err := Open(Config{Repo: "o/r", Token: "secret-token", BaseURL: srv.URL(), Cache: NewMemoryCache()})
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"/repos/o/r/issues", "/repos/o/r/milestones", "/repos/o/r/issues/2/dependencies/blocked_by"}
	first, err := f.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][2]int{}
	for _, p := range paths {
		before[p] = [2]int{srv.Count(p), srv.NotModified(p)}
	}
	second, err := f.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second read = %+v, want %+v", second, first)
	}
	for _, p := range paths {
		reqs, nm := srv.Count(p)-before[p][0], srv.NotModified(p)-before[p][1]
		if reqs == 0 || reqs != nm {
			t.Errorf("%s: second read made %d requests, %d answered 304; want all of them 304", p, reqs, nm)
		}
	}
}

// Classification: logic. Fails when Done follows the closed_issues of the milestone (3 here)
// instead of the issues closed as completed.
func TestPlanningDoneCountsOnlyCompleted(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.Milestones = []map[string]any{m1}
	srv.Issues = map[int]map[string]any{}
	for n, reason := range map[int]string{9: "completed", 4: "not_planned", 6: ""} {
		it := planIssue(n, m1)
		it["state"] = "closed"
		if reason != "" {
			it["state_reason"] = reason
		}
		srv.Issues[n] = it
	}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m := got.Milestones[0]
	if m.Done != 2 || len(m.Closed) != 2 || m.Closed[0].Number != 6 || m.Closed[1].Number != 9 {
		t.Errorf("milestone = %+v, want Done 2 with Closed #6 then #9 (not_planned #4 left out)", m)
	}
	if ms, _ := c.Milestones(t.Context(), "open"); ms[0].ClosedIssues != 3 {
		t.Fatalf("fake closed_issues = %d, want 3: the premise of this test", ms[0].ClosedIssues)
	}
}

// Classification: logic. Fails when a pull request in the issue list counts as an issue.
func TestPlanningOpenCountExcludesPullRequests(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.Milestones = []map[string]any{m1}
	pr := planIssue(2, m1)
	pr["pull_request"] = map[string]any{}
	srv.Issues = map[int]map[string]any{1: planIssue(1, m1), 2: pr}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Number != 1 || got.Milestones[0].Open != 1 {
		t.Errorf("Issues = %+v, Open = %d; want only #1 and Open 1", got.Issues, got.Milestones[0].Open)
	}
}

// Classification: io. Fails when blocked_by is read for an issue whose summary says none, or
// not read when the summary is absent, or when a closed blocker is kept.
func TestPlanningBlockers(t *testing.T) {
	path := func(n int) string { return fmt.Sprintf("/repos/o/r/issues/%d/dependencies/blocked_by", n) }
	t.Run("summary", func(t *testing.T) {
		srv, c := boardClient(t)
		srv.Issues = map[int]map[string]any{1: planIssue(1, nil), 2: planIssue(2, nil)}
		srv.BlockedBy = map[int][]map[string]any{2: {planBlocker(srv, "o/r", 1, "open"), planBlocker(srv, "o/r", 5, "closed")}}
		got, err := c.Planning(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if want := []Blocker{{Repo: "o/r", Issue: 1, State: "OPEN"}}; !reflect.DeepEqual(got.Issues[1].BlockedBy, want) {
			t.Errorf("Issues[1].BlockedBy = %+v, want %+v", got.Issues[1].BlockedBy, want)
		}
		if n := srv.Count(path(1)); n != 0 {
			t.Errorf("blocked_by of #1 asked %d times, want 0 (total_blocked_by is 0)", n)
		}
		if n := srv.Count(path(2)); n != 1 {
			t.Errorf("blocked_by of #2 asked %d times, want 1", n)
		}
	})
	t.Run("no summary", func(t *testing.T) {
		srv, c := boardClient(t)
		srv.NoDependencySummary = true
		srv.Issues = map[int]map[string]any{1: planIssue(1, nil)}
		if _, err := c.Planning(t.Context()); err != nil {
			t.Fatal(err)
		}
		if n := srv.Count(path(1)); n != 1 {
			t.Errorf("blocked_by of #1 asked %d times, want 1 (no summary to trust)", n)
		}
	})
}

// Classification: logic. Fails when a closed milestone, a missing milestone or an empty open
// milestone gets another shape than the Planning contract names.
func TestPlanningMilestoneShapes(t *testing.T) {
	srv, c := boardClient(t)
	srv.Milestones = []map[string]any{{"number": 5, "title": "Leeg", "state": "open"}, {"number": 7, "title": "Oud", "state": "closed"}}
	srv.Issues = map[int]map[string]any{
		1: planIssue(1, map[string]any{"number": 7, "title": "Oud", "state": "closed"}),
		2: planIssue(2, nil),
	}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Issues[0].Milestone == nil || got.Issues[0].Milestone.State != "closed" || got.Issues[1].Milestone != nil {
		t.Errorf("Issues = %+v, want #1 in a closed milestone and #2 without one", got.Issues)
	}
	if len(got.Milestones) != 1 || got.Milestones[0].Open != 0 || got.Milestones[0].Done != 0 || got.Milestones[0].Closed == nil || len(got.Milestones[0].Closed) != 0 {
		t.Errorf("Milestones = %#v, want only Leeg with Open 0, Done 0 and an empty non-nil Closed", got.Milestones)
	}
	for _, is := range got.Issues {
		if is.Labels == nil {
			t.Errorf("#%d Labels is nil", is.Number)
		}
	}
}
