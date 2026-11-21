package forge_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

func planLink(iid int, state, linkType, full string) map[string]any {
	return map[string]any{"iid": iid, "state": state, "link_type": linkType,
		"references": map[string]any{"full": full}}
}

func TestGitLabPlanningLinksAndClosedIssues(t *testing.T) {
	srv, f := gitlabBoardOn(t, gitlabBoard, gitlabLifecycle...)
	now := time.Now()
	srv.AddMilestone(map[string]any{"id": 3, "iid": 13, "title": "M1", "state": "active"})
	srv.AddMilestone(map[string]any{"id": 9, "iid": 19, "title": "Empty", "state": "active"})
	ms := map[string]any{"id": 3, "iid": 13, "title": "M1", "state": "active"}
	one := boardIssue(1, "opened", now)
	one["milestone"] = ms
	two := boardIssue(2, "opened", now)
	for _, n := range []int{3, 4, 5} {
		c := boardIssue(n, "closed", now)
		c["milestone"] = ms
		srv.AddIssue(c)
	}
	srv.AddIssue(one)
	srv.AddIssue(two)
	srv.AddIssue(func() map[string]any { m := boardIssue(6, "opened", now); m["milestone"] = ms; return m }())
	srv.AddLink(2, planLink(1, "opened", "is_blocked_by", "g/p#1"))
	srv.AddLink(2, planLink(9, "opened", "is_blocked_by", "other/repo#9"))
	srv.AddLink(2, planLink(7, "opened", "relates_to", "g/p#7"))
	srv.AddLink(2, planLink(8, "closed", "is_blocked_by", "g/p#8"))
	srv.AddLink(2, planLink(10, "opened", "blocks", "g/p#10"))

	got, err := f.Planning(t.Context())
	if err != nil {
		t.Fatalf("Planning: %v", err)
	}
	if len(got.Issues) != 3 || got.Issues[0].Number != 1 || got.Issues[1].Number != 2 {
		t.Fatalf("Issues = %+v, want the open #1, #2 and #6", got.Issues)
	}
	if m := got.Issues[0].Milestone; m == nil || m.Title != "M1" || m.State != "open" || m.Number != 3 {
		t.Errorf("Issues[0].Milestone = %+v, want M1 open number 3", m)
	}
	want := []forge.Blocker{{Repo: "g/p", Issue: 1, State: "OPEN"}, {Repo: "other/repo", Issue: 9, State: "OPEN"}}
	if b := got.Issues[1].BlockedBy; !slices.Equal(b, want) {
		t.Errorf("Issues[1].BlockedBy = %+v, want %+v (only open is_blocked_by)", b, want)
	}
	if len(got.Milestones) != 2 {
		t.Fatalf("Milestones = %+v, want M1 and Empty", got.Milestones)
	}
	slices.SortFunc(got.Milestones, func(a, b forge.MilestoneCount) int { return strings.Compare(a.Title, b.Title) })
	if c := got.Milestones[1]; c.Title != "M1" || c.Number != 3 || c.State != "open" || c.Open != 2 || c.Done != 3 {
		t.Errorf("M1 = %+v, want number 3, open, 2 open and 3 done", c)
	}
	if nums := closedNumbers(got.Milestones[1].Closed); !slices.Equal(nums, []int{3, 4, 5}) {
		t.Errorf("M1 Closed = %v, want #3, #4, #5 (every closed issue counts on GitLab)", nums)
	}
	if c := got.Milestones[0]; c.Title != "Empty" || c.Open != 0 || c.Done != 0 {
		t.Errorf("Empty = %+v, want 0 and 0", c)
	}
}

func closedNumbers(cs []forge.ClosedIssue) []int {
	nums := []int{}
	for _, c := range cs {
		nums = append(nums, c.Number)
	}
	return nums
}

func TestGitLabPlanningReadsLabels(t *testing.T) {
	srv, f := gitlabBoardOn(t, gitlabBoard, gitlabLifecycle...)
	one := boardIssue(1, "opened", time.Now())
	one["labels"] = []string{"a", "b"}
	srv.AddIssue(one)
	srv.AddIssue(boardIssue(2, "opened", time.Now()))
	got, err := f.Planning(t.Context())
	if err != nil {
		t.Fatalf("Planning: %v", err)
	}
	if !slices.Equal(got.Issues[0].Labels, []string{"a", "b"}) || got.Issues[1].Labels == nil || len(got.Issues[1].Labels) != 0 {
		t.Errorf("Labels = %#v and %#v, want [a b] and an empty non-nil list", got.Issues[0].Labels, got.Issues[1].Labels)
	}
}

func TestGitLabPlanningBudgetIsOneLinksCallPerOpenIssue(t *testing.T) {
	srv, f := gitlabBoardOn(t, gitlabBoard, gitlabLifecycle...)
	srv.AddMilestone(map[string]any{"id": 1, "iid": 11, "title": "A", "state": "active"})
	srv.AddMilestone(map[string]any{"id": 2, "iid": 12, "title": "B", "state": "active"})
	for n := 1; n <= 3; n++ {
		srv.AddIssue(boardIssue(n, "opened", time.Now()))
	}
	if _, err := f.Planning(t.Context()); err != nil {
		t.Fatalf("Planning: %v", err)
	}
	// 1 issues page + 3 links + 1 milestone list + 2 closed-issue reads (one page each).
	if n := len(srv.Requests()); n != 7 {
		t.Errorf("requests = %q, want 7", srv.Requests())
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := f.Planning(ctx)
	if !errors.Is(err, context.Canceled) || len(got.Issues) != 0 || len(got.Milestones) != 0 {
		t.Errorf("Planning on a cancelled context = %+v, %v; want the context error and no partial result", got, err)
	}
}
