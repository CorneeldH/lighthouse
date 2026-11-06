package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEmpty(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRelocate(t *testing.T) {
	wt := t.TempDir()
	writeEmpty(t, filepath.Join(wt, "docs", "plans", "p.md"))
	writeEmpty(t, filepath.Join(wt, "root.md"))
	cases := []struct {
		name, command, want string
	}{
		{
			// Run 19 of #237: the handoff was written in a worktree of the main checkout.
			name:    "a plan in another checkout",
			command: "/superpowers:subagent-driven-development /home/u/repo/.claude/worktrees/issue-147-x/docs/plans/p.md",
			want:    "/superpowers:subagent-driven-development " + wt + "/docs/plans/p.md",
		},
		{
			name:    "a plan in the run's worktree stays",
			command: "/lighthouse-review-plan " + wt + "/docs/plans/p.md",
			want:    "/lighthouse-review-plan " + wt + "/docs/plans/p.md",
		},
		{
			name:    "no path argument",
			command: "/lighthouse-start-issue 7",
			want:    "/lighthouse-start-issue 7",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Relocate(c.command, wt)
			if err != nil || got != c.want {
				t.Errorf("Relocate = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestRelocateNoTailInTheWorktreeIsAnError(t *testing.T) {
	wt := t.TempDir()
	writeEmpty(t, filepath.Join(wt, "p.md"))
	for _, command := range []string{
		"/lighthouse-review-spec /elsewhere/docs/specs/s.md", // nothing of it in the worktree
		"/lighthouse-review-spec /elsewhere/docs/p.md",       // only the bare file name matches
	} {
		if got, err := Relocate(command, wt); err == nil {
			t.Errorf("Relocate(%q) = %q, nil; want an error", command, got)
		}
	}
}
