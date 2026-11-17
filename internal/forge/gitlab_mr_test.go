package forge_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

// mr is a merge request of project 1 into project 1, open, in GitLab's REST shape.
func mr(iid int, extra map[string]any) map[string]any {
	m := map[string]any{"iid": iid, "state": "opened", "title": "MR", "description": "", "source_branch": "issue-7-x",
		"sha": "abc123", "source_project_id": 1, "target_project_id": 1, "draft": false,
		"web_url": "https://gitlab.example/g/p/-/merge_requests/12", "detailed_merge_status": "mergeable"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func branchPR(t *testing.T, extra map[string]any) forge.PR {
	t.Helper()
	srv, f := gitlabOn(t, "g/p")
	srv.AddMR(mr(12, extra))
	prs, err := f.BranchPRs(t.Context(), "issue-7-x")
	if err != nil || len(prs) != 1 {
		t.Fatalf("BranchPRs = %+v, %v; want one PR", prs, err)
	}
	return prs[0]
}

// TestMRToPR runs the translation table of the forge spec through BranchPRs.
func TestMRToPR(t *testing.T) {
	pipeline := func(status string) map[string]any {
		return map[string]any{"head_pipeline": map[string]any{"status": status}}
	}
	t.Run("State", func(t *testing.T) {
		for state, want := range map[string]string{"opened": "OPEN", "closed": "CLOSED", "merged": "MERGED", "locked": "OPEN"} {
			if got := branchPR(t, map[string]any{"state": state}).State; got != want {
				t.Errorf("state %s → %q, want %q", state, got, want)
			}
		}
	})
	t.Run("Checks", func(t *testing.T) {
		for status, want := range map[string]string{
			"success": "SUCCESS", "skipped": "SUCCESS", "failed": "FAILURE", "canceled": "FAILURE",
			"running": "PENDING", "pending": "PENDING", "created": "PENDING", "preparing": "PENDING",
			"waiting_for_resource": "PENDING", "scheduled": "PENDING", "manual": "PENDING",
		} {
			if got := branchPR(t, pipeline(status)).Checks; got != want {
				t.Errorf("pipeline %s → %q, want %q", status, got, want)
			}
		}
		if got := branchPR(t, nil).Checks; got != "" {
			t.Errorf("no pipeline → %q, want empty", got)
		}
		if got := branchPR(t, map[string]any{"head_pipeline": nil}).Checks; got != "" {
			t.Errorf("null pipeline → %q, want empty", got)
		}
	})
	t.Run("Mergeable", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			extra map[string]any
			want  string
		}{
			{"mergeable", map[string]any{"detailed_merge_status": "mergeable"}, "MERGEABLE"},
			{"ci running does not count", map[string]any{"detailed_merge_status": "ci_still_running"}, "MERGEABLE"},
			{"unapproved does not count", map[string]any{"detailed_merge_status": "not_approved"}, "MERGEABLE"},
			{"unchecked", map[string]any{"detailed_merge_status": "unchecked"}, "UNKNOWN"},
			{"checking", map[string]any{"detailed_merge_status": "checking"}, "UNKNOWN"},
			{"preparing", map[string]any{"detailed_merge_status": "preparing"}, "UNKNOWN"},
			{"conflicts", map[string]any{"has_conflicts": true, "detailed_merge_status": "conflict"}, "CONFLICTING"},
			{"conflicts win over checking", map[string]any{"has_conflicts": true, "detailed_merge_status": "checking"}, "CONFLICTING"},
		} {
			if got := branchPR(t, tc.extra).Mergeable; got != tc.want {
				t.Errorf("%s → %q, want %q", tc.name, got, tc.want)
			}
		}
	})
	t.Run("Fields", func(t *testing.T) {
		p := branchPR(t, map[string]any{"draft": true, "source_project_id": 2, "state": "merged",
			"merged_at": "2026-11-01T12:00:00Z"})
		want := forge.PR{Number: 12, State: "MERGED", Draft: true, Mergeable: "MERGEABLE", HeadSHA: "abc123",
			HeadRef: "issue-7-x", CrossRepo: true, URL: "https://gitlab.example/g/p/-/merge_requests/12",
			MergedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
		want.ClosedAt = want.MergedAt // a merged PR has a ClosedAt too, as on GitHub
		if p != want {
			t.Errorf("PR = %+v, want %+v", p, want)
		}
		if p := branchPR(t, nil); p.CrossRepo || p.Draft {
			t.Errorf("PR = %+v, want a same-project, non-draft PR", p)
		}
	})
}

// A merge request on the branch may close another issue than the one of the branch, so
// BranchPRs never says Closes, as on GitHub; the board's closingMergeRequests does.
func TestBranchPRsNeverCloses(t *testing.T) {
	for _, desc := range []string{"", "Closes #7", "fixes #7"} {
		if p := branchPR(t, map[string]any{"description": desc, "state": "merged"}); p.Closes {
			t.Errorf("description %q: Closes = true, want false", desc)
		}
	}
}

func TestBranchPRsListsEveryState(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	srv.AddMR(mr(1, map[string]any{"state": "merged"}))
	srv.AddMR(mr(2, map[string]any{"state": "closed"}))
	srv.AddMR(mr(3, map[string]any{"state": "opened"}))
	srv.AddMR(mr(4, map[string]any{"source_branch": "other"}))
	prs, err := f.BranchPRs(t.Context(), "issue-7-x")
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, p := range prs {
		got = append(got, p.Number)
	}
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("BranchPRs numbers = %v, want [1 2 3]", got)
	}
}

// The merge request list carries no head_pipeline, so BranchPRs reads each open merge request
// on its own for its checks, and no other.
func TestBranchPRsReadsPipelineOfOpenMRsOnly(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	failed := map[string]any{"status": "failed"}
	srv.AddMR(mr(1, map[string]any{"state": "merged", "head_pipeline": failed}))
	srv.AddMR(mr(2, map[string]any{"state": "closed", "head_pipeline": failed}))
	srv.AddMR(mr(3, map[string]any{"state": "opened", "head_pipeline": failed}))
	srv.AddMR(mr(4, map[string]any{"state": "locked", "head_pipeline": failed}))
	prs, err := f.BranchPRs(t.Context(), "issue-7-x")
	if err != nil || len(prs) != 4 {
		t.Fatalf("BranchPRs = %+v, %v; want four PRs", prs, err)
	}
	for _, p := range prs {
		want := map[int]string{1: "", 2: "", 3: "FAILURE", 4: "FAILURE"}[p.Number]
		if p.Checks != want {
			t.Errorf("PR %d Checks = %q, want %q", p.Number, p.Checks, want)
		}
	}
	var details []string
	for _, r := range srv.Requests() {
		if strings.HasPrefix(r, "GET /api/v4/projects/g%2Fp/merge_requests/") {
			details = append(details, r)
		}
	}
	want := []string{"GET /api/v4/projects/g%2Fp/merge_requests/3", "GET /api/v4/projects/g%2Fp/merge_requests/4"}
	if !slices.Equal(details, want) {
		t.Errorf("single merge request reads = %q, want %q", details, want)
	}
}

func TestMergePR(t *testing.T) {
	t.Run("squash merges at the sha", func(t *testing.T) {
		srv, f := gitlabOn(t, "g/p")
		srv.AddMR(mr(12, nil))
		if err := f.MergePR(t.Context(), 12, "abc123"); err != nil {
			t.Fatalf("MergePR: %v", err)
		}
		open, err := f.OpenPRs(t.Context())
		if err != nil || len(open) != 0 {
			t.Errorf("OpenPRs after merge = %+v, %v; want none", open, err)
		}
	})
	t.Run("head moved", func(t *testing.T) {
		srv, f := gitlabOn(t, "g/p")
		srv.AddMR(mr(12, nil))
		if err := f.MergePR(t.Context(), 12, "old"); !errors.Is(err, forge.ErrHeadMoved) {
			t.Errorf("MergePR at a stale sha = %v, want ErrHeadMoved", err)
		}
	})
	t.Run("refused", func(t *testing.T) {
		for name, extra := range map[string]map[string]any{
			"conflicts": {"has_conflicts": true}, "not open": {"state": "closed"},
		} {
			srv, f := gitlabOn(t, "g/p")
			srv.AddMR(mr(12, extra))
			var refused *forge.MergeRefused
			if err := f.MergePR(t.Context(), 12, "abc123"); !errors.As(err, &refused) {
				t.Errorf("%s: MergePR = %v, want *MergeRefused", name, err)
			}
		}
	})
}

// MergePR goes through the client that checks the token, like every other call.
func TestMergePRWithoutTokenIsNotConfigured(t *testing.T) {
	srv := gitlabtest.New(t)
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: srv.Project, BaseURL: srv.URL()})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.MergePR(t.Context(), 1, "x"); !errors.Is(err, forge.ErrNotConfigured) {
		t.Errorf("MergePR without token = %v, want ErrNotConfigured", err)
	}
}
