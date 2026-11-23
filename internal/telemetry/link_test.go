package telemetry

import (
	"slices"
	"testing"
)

func TestIssueFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   int
		wantOK bool
	}{
		{"issue-373-telemetrie", 373, true},
		{"issue-373", 373, true},
		{"issue-373x", 0, false},
		{"fix-x", 0, false},
		{"main", 0, false},
		{"", 0, false},
		{"issue-0-x", 0, false},
		{"issue-0", 0, false},
		{"xissue-5-a", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.branch, func(t *testing.T) {
			got, ok := IssueFromBranch(tc.branch)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("IssueFromBranch(%q) = %d, %v; want %d, %v", tc.branch, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func rep(l Line, n int) []Line {
	out := make([]Line, n)
	for i := range out {
		out[i] = l
	}
	return out
}

func join(parts ...[]Line) []Line {
	var out []Line
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestLink(t *testing.T) {
	tests := []struct {
		name          string
		defaultBranch string
		lines         []Line
		want          LinkResult
	}{
		{"main only without number", "main", rep(Line{Branch: "main"}, 3), LinkResult{Reason: "main_only"}},
		{"main only with number", "main", rep(Line{Branch: "main", Issue: 373}, 2), LinkResult{Issue: 373, Basis: "transcript"}},
		{"50 main lines and one issue line", "main",
			join(rep(Line{Branch: "main"}, 50), []Line{{Branch: "issue-5-x"}}),
			LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5-x"}},
		{"bare issue branch", "main", []Line{{Branch: "issue-5"}}, LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5"}},
		{"tie goes to last occurring issue", "main",
			[]Line{{Branch: "issue-5-a"}, {Branch: "issue-6-b"}, {Branch: "issue-5-a"}, {Branch: "issue-6-b"}},
			LinkResult{Issue: 6, Basis: "branch", Branch: "issue-6-b"}},
		{"branch tie goes to last occurring issue, not first", "main",
			[]Line{{Branch: "issue-5-a"}, {Branch: "issue-6-b"}, {Branch: "issue-6-b"}, {Branch: "issue-5-a"}},
			LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5-a"}},
		{"transcript tie goes to last seen, not first", "main",
			[]Line{{Branch: "fix-x", Issue: 5}, {Branch: "fix-x", Issue: 6}, {Branch: "fix-x", Issue: 6}, {Branch: "fix-x", Issue: 5}},
			LinkResult{Issue: 5, Basis: "transcript"}},
		{"two branches of one issue count together", "main",
			join(rep(Line{Branch: "issue-5-a"}, 3), []Line{{Branch: "issue-5-b"}}, rep(Line{Branch: "issue-6-c"}, 3)),
			LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5-b"}},
		{"winner branch is last occurrence of that issue", "main",
			[]Line{{Branch: "issue-5-b"}, {Branch: "issue-5-a"}, {Branch: "issue-6-c"}},
			LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5-a"}},
		{"only non-issue branch", "main", []Line{{Branch: "fix-x"}}, LinkResult{Reason: "no_issue_branch"}},
		{"non-issue branch with number", "main", []Line{{Branch: "fix-x", Issue: 7}}, LinkResult{Issue: 7, Basis: "transcript"}},
		{"no lines", "main", nil, LinkResult{Reason: "no_issue_number"}},
		{"only empty branches", "main", rep(Line{}, 2), LinkResult{Reason: "no_issue_number"}},
		{"empty branches plus one main line", "main",
			[]Line{{}, {Branch: "main"}, {}}, LinkResult{Reason: "main_only"}},
		{"transcript numbers by count", "main",
			[]Line{{Branch: "main", Issue: 5}, {Branch: "main", Issue: 6}, {Branch: "main", Issue: 6}},
			LinkResult{Issue: 6, Basis: "transcript"}},
		{"transcript tie goes to last seen", "main",
			[]Line{{Branch: "main", Issue: 6}, {Branch: "main", Issue: 5}},
			LinkResult{Issue: 5, Basis: "transcript"}},
		{"main number plus non-issue branch", "main",
			[]Line{{Branch: "main", Issue: 5}, {Branch: "fix-x"}},
			LinkResult{Issue: 5, Basis: "transcript"}},
		{"issue branch beats transcript number", "main",
			[]Line{{Branch: "main", Issue: 9}, {Branch: "main", Issue: 9}, {Branch: "issue-5-a"}},
			LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5-a"}},
		{"custom default branch counts as main", "trunk",
			join(rep(Line{Branch: "trunk"}, 4), []Line{{Branch: "issue-5-x"}}),
			LinkResult{Issue: 5, Basis: "branch", Branch: "issue-5-x"}},
		{"custom default branch only", "trunk", rep(Line{Branch: "trunk"}, 2), LinkResult{Reason: "main_only"}},
		{"main is not default when default is trunk", "trunk", []Line{{Branch: "main"}}, LinkResult{Reason: "no_issue_branch"}},
		{"issue-0 branch is not an issue branch", "main", []Line{{Branch: "issue-0-x"}}, LinkResult{Reason: "no_issue_branch"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := slices.Clone(tc.lines)
			got := Link(tc.defaultBranch, tc.lines)
			if got != tc.want {
				t.Errorf("Link(%q, ...) = %+v; want %+v", tc.defaultBranch, got, tc.want)
			}
			if !slices.Equal(orig, tc.lines) {
				t.Errorf("Link modified its input")
			}
		})
	}
}
