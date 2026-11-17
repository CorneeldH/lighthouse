package forge

import (
	"fmt"
	"reflect"
	"strings"
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
	if n := srv.Count("PlanCounts"); n != 0 {
		t.Errorf("Count(PlanCounts) = %d, want 0 without an open milestone", n)
	}
}

func TestGitHubPlanningCountsIssuesOnly(t *testing.T) {
	srv, c := boardClient(t)
	m1 := map[string]any{"number": 1, "title": "M1", "state": "open"}
	srv.PlanMilestones = []map[string]any{
		{"number": 1, "title": "M1", "state": "OPEN", "description": "d"},
		{"number": 2, "title": "M2 — leeg", "state": "OPEN"},
		{"number": 3, "title": "Dicht", "state": "CLOSED"},
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
		{Milestone: Milestone{Number: 1, Title: "M1", State: "open", Description: "d"}, Open: 3, Done: 2},
		{Milestone: Milestone{Number: 2, Title: "M2 — leeg", State: "open"}},
	}
	if !reflect.DeepEqual(got.Milestones, want) {
		t.Errorf("Milestones = %+v, want %+v (open issues and completed ones; no PRs, no not-planned; closed milestone left out)", got.Milestones, want)
	}
	if n := srv.Count("PlanCounts"); n != 1 {
		t.Errorf("Count(PlanCounts) = %d, want 1: one query for all milestones", n)
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
	if n := srv.Count("Planning"); n != 2 {
		t.Errorf("Count(Planning) = %d, want 2 pages", n)
	}
	if after := srv.Posts[1].Body["variables"].(map[string]any)["after"]; after != "cursor-100" {
		t.Errorf("second page after = %v, want cursor-100", after)
	}
}

func TestGitHubPlanningStuckCursorFails(t *testing.T) {
	for name, cursor := range map[string]string{"empty": "", "unchanged": "cursor-0"} {
		t.Run(name, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.Issues = map[int]map[string]any{1: planIssue(1, nil)}
			srv.PlanStuckCursor = &cursor
			got, err := c.Planning(t.Context())
			if err == nil || !strings.Contains(err.Error(), "pagination stuck") {
				t.Fatalf("Planning = %+v, %v; want a pagination-stuck error", got, err)
			}
		})
	}
}

func TestGitHubPlanningMilestonesPaginateAndStick(t *testing.T) {
	srv, c := boardClient(t)
	for n := 1; n <= 101; n++ {
		srv.PlanMilestones = append(srv.PlanMilestones, map[string]any{"number": n, "title": fmt.Sprintf("M%d", n), "state": "OPEN"})
	}
	got, err := c.Planning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Milestones) != 101 || got.Milestones[100].Title != "M101" {
		t.Fatalf("got %d milestones, want 101 ending at M101", len(got.Milestones))
	}
	stuck := ""
	srv.PlanStuckCursor = &stuck
	if _, err := c.Planning(t.Context()); err == nil || !strings.Contains(err.Error(), "pagination stuck") {
		t.Errorf("Planning with a stuck cursor: %v, want a pagination-stuck error", err)
	}
}

func TestGitHubPlanningRefusesTruncatedBlockedBy(t *testing.T) {
	srv, c := boardClient(t)
	srv.Issues = map[int]map[string]any{1: planIssue(1, nil)}
	srv.PlanBlockedByMore = map[int]bool{1: true}
	if got, err := c.Planning(t.Context()); err == nil || !strings.Contains(err.Error(), "blockedBy truncated") {
		t.Fatalf("Planning = %+v, %v; want a blockedBy-truncated error", got, err)
	}
}

func TestGitHubPlanningRefusesQualifierBreakingTitle(t *testing.T) {
	// GitHub documents no escape inside a quoted qualifier, so a title with a quote or a
	// backslash fails visibly instead of counting the wrong milestone.
	for _, title := range []string{`Het "plan"`, `a\b`} {
		t.Run(title, func(t *testing.T) {
			srv, c := boardClient(t)
			srv.PlanMilestones = []map[string]any{{"number": 1, "title": title, "state": "OPEN"}}
			got, err := c.Planning(t.Context())
			if err == nil || !strings.Contains(err.Error(), "milestone") {
				t.Fatalf("Planning = %+v, %v; want an error naming the milestone", got, err)
			}
			if n := srv.Count("PlanCounts"); n != 0 {
				t.Errorf("Count(PlanCounts) = %d, want 0: no query is sent for the title", n)
			}
		})
	}
}

func TestGitHubPlanningSearchSendsQualifiers(t *testing.T) {
	srv, c := boardClient(t)
	srv.PlanMilestones = []map[string]any{{"number": 1, "title": "M1 — Eerste", "state": "OPEN"}}
	if _, err := c.Planning(t.Context()); err != nil {
		t.Fatal(err)
	}
	vars := srv.Posts[len(srv.Posts)-1].Body["variables"].(map[string]any)
	got := map[string]bool{}
	for _, v := range vars {
		got[v.(string)] = true
	}
	for _, want := range []string{
		`repo:o/r milestone:"M1 — Eerste" is:issue is:open`,
		`repo:o/r milestone:"M1 — Eerste" is:issue is:closed reason:completed`,
	} {
		if !got[want] {
			t.Errorf("search queries = %v, want %q", vars, want)
		}
	}
}
