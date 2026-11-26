package review

import "testing"

func TestTrace(t *testing.T) {
	a := ServedRepo{Slug: "o/a", Dir: "/a"}
	b := ServedRepo{Slug: "o/b", Dir: "/b"}
	has := func(in ...string) func(dir, sha string) bool {
		return func(dir, sha string) bool {
			for _, d := range in {
				if d == dir && sha == "abc" {
					return true
				}
			}
			return false
		}
	}
	seven := 7
	for _, tc := range []struct {
		name  string
		chain string
		spec  *int
		repos []ServedRepo
		has   func(string, string) bool
		want  Trajectory
		ok    bool
	}{
		{"issue in chain key", "2026-issue-572-x", nil, []ServedRepo{a, b}, has("/b"), Trajectory{"o/b", 572}, true},
		{"issue from issue_spec", "plan-x", &seven, []ServedRepo{a, b}, has("/a"), Trajectory{"o/a", 7}, true},
		{"chain key wins over issue_spec", "issue-3-x", &seven, []ServedRepo{a}, has("/a"), Trajectory{"o/a", 3}, true},
		{"same issue twice", "issue-3-issue-3", nil, []ServedRepo{a}, has("/a"), Trajectory{"o/a", 3}, true},
		{"reissue-5 is no issue", "reissue-5-x", nil, []ServedRepo{a}, has("/a"), Trajectory{}, false},
		{"issue-5bar is no issue", "foo-issue-5bar", nil, []ServedRepo{a}, has("/a"), Trajectory{}, false},
		{"issue-5bar falls back to issue_spec", "foo-issue-5bar", &seven, []ServedRepo{a}, has("/a"), Trajectory{"o/a", 7}, true},
		{"issue at start", "issue-9", nil, []ServedRepo{a}, has("/a"), Trajectory{"o/a", 9}, true},
		{"sha in two repos", "issue-572-x", nil, []ServedRepo{a, b}, has("/a", "/b"), Trajectory{}, false},
		{"sha in no repo", "issue-572-x", nil, []ServedRepo{a, b}, has(), Trajectory{}, false},
		{"two different issues", "issue-1-and-issue-2", nil, []ServedRepo{a}, has("/a"), Trajectory{}, false},
		{"no issue anywhere", "plan-x", nil, []ServedRepo{a}, has("/a"), Trajectory{}, false},
		{"no repos", "issue-1", nil, nil, has("/a"), Trajectory{}, false},
		{"empty sha", "issue-1", nil, []ServedRepo{a}, has("/a"), Trajectory{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sha := "abc"
			if tc.name == "empty sha" {
				sha = ""
			}
			got, ok := Trace(tc.chain, tc.spec, sha, tc.repos, tc.has)
			if ok != tc.ok || got != tc.want {
				t.Errorf("Trace = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
