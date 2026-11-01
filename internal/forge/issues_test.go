package forge

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func fieldIssue(n int, state string) map[string]any {
	return map[string]any{
		"number": n, "title": "Titel", "html_url": "https://github.com/o/r/issues/7", "comments": 2,
		"state": state, "body": "De body", "node_id": "I_kw7", "updated_at": "2026-10-30T12:00:00Z",
		"user":      map[string]any{"login": "anna"},
		"labels":    []any{map[string]any{"name": "needs-shaping"}, map[string]any{"name": "area-x"}},
		"milestone": map[string]any{"number": 3, "title": "M2", "state": "open", "description": "Omschrijving"},
	}
}

func wantFields(n int, state string) Issue {
	return Issue{Number: n, Title: "Titel", URL: "https://github.com/o/r/issues/7", Comments: 2, State: state,
		Body: "De body", Author: "anna", Labels: []string{"needs-shaping", "area-x"},
		UpdatedAt: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), NodeID: "I_kw7",
		Milestone: &Milestone{Number: 3, Title: "M2", State: "open", Description: "Omschrijving"}}
}

func TestOpenIssuesFields(t *testing.T) {
	srv, c := boardClient(t)
	pr := fieldIssue(8, "open")
	pr["pull_request"] = map[string]any{"url": "x"}
	srv.Issues = map[int]map[string]any{7: fieldIssue(7, "open"), 8: pr, 9: {"number": 9, "title": "Dicht", "state": "closed"}}
	got, err := c.OpenIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []Issue{wantFields(7, "open")}; !reflect.DeepEqual(got, want) {
		t.Errorf("OpenIssues = %+v, want %+v", got, want)
	}
}

func TestIssueFields(t *testing.T) {
	srv, c := boardClient(t)
	closed := fieldIssue(7, "closed")
	closed["closed_at"] = "2026-10-31T08:00:00Z"
	srv.Issues = map[int]map[string]any{7: closed, 6: {"number": 6, "title": "Kaal"}}
	got, err := c.Issue(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := wantFields(7, "closed")
	want.ClosedAt = time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Issue(7) = %+v, want %+v", got, want)
	}
	bare, err := c.Issue(context.Background(), 6)
	if err != nil || bare.Milestone != nil || bare.Labels != nil || !bare.ClosedAt.IsZero() || bare.Author != "" {
		t.Errorf("Issue(6) = %+v, %v; want no milestone, labels, author or closed_at", bare, err)
	}
}

func TestClosedInMilestone(t *testing.T) {
	srv, c := boardClient(t)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	mk := func(n, ms int, daysAgo int) map[string]any {
		return map[string]any{"number": n, "title": "t", "state": "closed",
			"closed_at": now.AddDate(0, 0, -daysAgo).Format(time.RFC3339),
			"milestone": map[string]any{"number": ms, "title": "M", "state": "open"}}
	}
	open := mk(15, 3, 1)
	open["state"] = "open"
	srv.Issues = map[int]map[string]any{
		11: mk(11, 3, 10), 12: mk(12, 3, 29), 13: mk(13, 3, 31), 14: mk(14, 4, 1), 15: open,
	}
	got, err := c.ClosedInMilestone(context.Background(), 3, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, is := range got {
		nums = append(nums, is.Number)
	}
	if want := []int{11, 12}; !reflect.DeepEqual(nums, want) {
		t.Errorf("ClosedInMilestone = %v, want %v", nums, want)
	}
	if n := srv.Count("/repos/o/r/issues"); n != 1 {
		t.Errorf("list requests = %d, want 1", n)
	}
}

func TestBlockedBy(t *testing.T) {
	srv, c := boardClient(t)
	srv.BlockedBy = map[int][]map[string]any{7: {
		{"number": 5, "state": "open", "repository_url": "https://api.github.com/repos/o/r"},
		{"number": 9, "state": "closed", "repository_url": "https://api.github.com/repos/o/x"},
	}}
	got, err := c.BlockedBy(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []Blocker{{Repo: "o/r", Issue: 5, State: "OPEN"}, {Repo: "o/x", Issue: 9, State: "CLOSED"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BlockedBy(7) = %+v, want %+v", got, want)
	}
	srv.Fail = map[string]int{"GET /repos/o/r/issues/8/dependencies/blocked_by": 404}
	if _, err := c.BlockedBy(context.Background(), 8); err == nil {
		t.Error("BlockedBy(8) with a 404: err = nil, want the error")
	}
}

func TestOpenPRs(t *testing.T) {
	srv, c := boardClient(t)
	for i := 1; i <= 101; i++ {
		srv.Pulls = append(srv.Pulls, map[string]any{"number": i, "title": "PR", "body": "Closes #7",
			"head": map[string]any{"ref": "issue-7-x"}})
	}
	got, err := c.OpenPRs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 {
		t.Fatalf("OpenPRs = %d PRs, want 101 over two pages", len(got))
	}
	if want := (OpenPR{Number: 101, Title: "PR", Body: "Closes #7", HeadRef: "issue-7-x"}); got[100] != want {
		t.Errorf("last PR = %+v, want %+v", got[100], want)
	}
}
