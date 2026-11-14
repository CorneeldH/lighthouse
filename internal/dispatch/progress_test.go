package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

const sddCommand = "/superpowers:subagent-driven-development "

func TestPhase(t *testing.T) {
	cases := []struct{ command, want string }{
		{"/lighthouse-start-issue 3", "start-issue"},
		{"/lighthouse-review-spec x.md", "spec-review"},
		{"/lighthouse-review-plan x.md", "plan-review"},
		{"/superpowers:writing-plans x.md", "writing-plans"},
		{"/superpowers:subagent-driven-development x.md", "sdd"},
		{"/superpowers:executing-plans x.md", "executing-plans"},
		{"/lighthouse-fix-pr 4", "fix-pr"},
		{"/lighthouse-write-pr", "write-pr"},
		{"/lighthouse-close-issue 4", "close-issue"},
		{"/superpowers:brainstorming", "brainstorming"},
		{"/lighthouse-audit-worktrees", "audit-worktrees"},
		{"/superpowers:finishing-a-development-branch", "finishing-a-development-branch"},
		{"/other-thing arg", "other-thing"},
		{"", ""},
		{"/paia-start-issue 3", "start-issue"},
		{"/lighthouse-spec-adversarial-review x.md", "spec-review"},
	}
	for _, c := range cases {
		if got := Phase(c.command); got != c.want {
			t.Errorf("Phase(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sddWorktree builds a worktree with a four-task plan and a work directory with the ledger.
// It returns the root, the work directory and the run command.
func sddWorktree(t *testing.T, ledger string) (root, work, command string) {
	t.Helper()
	root = t.TempDir()
	write(t, filepath.Join(root, ".git"), "gitdir: elsewhere\n")
	write(t, filepath.Join(root, "plans/p/plan.md"), "# Plan\n\n### Task 1: a\n\n### Task 2: b\n\n### Task 3: c\n\n## Task 4: d\n\n### Task 4: d again\n")
	work = filepath.Join(root, ".superpowers/sdd/plan")
	write(t, filepath.Join(work, "plan-path"), "plans/p/plan.md\n")
	write(t, filepath.Join(work, "progress.md"), ledger)
	return root, work, sddCommand + filepath.Join(root, "plans/p/plan.md")
}

func checkProgress(t *testing.T, command string, want Progress) {
	t.Helper()
	got, ok := ReadProgress(command)
	if !ok || got != want {
		t.Errorf("ReadProgress = %+v, %v, want %+v, true", got, ok, want)
	}
}

func TestReadProgressBuild(t *testing.T) {
	_, _, cmd := sddWorktree(t, "Task 1: complete (abc)\nTask 2: complete\n")
	checkProgress(t, cmd, Progress{Task: 3, Tasks: 4, Step: "build"})
}

func TestReadProgressReview(t *testing.T) {
	_, work, cmd := sddWorktree(t, "Task 1: complete (abc)\nTask 2: complete\n")
	write(t, filepath.Join(work, "task-3-report.md"), "r")
	checkProgress(t, cmd, Progress{Task: 3, Tasks: 4, Step: "review"})
}

func TestReadProgressFinalReview(t *testing.T) {
	_, _, cmd := sddWorktree(t, "Task 1: complete\nTask 2: complete\nTask 3: complete\nTask 4: complete\n")
	checkProgress(t, cmd, Progress{Task: 4, Tasks: 4, Step: "final-review"})
}

func TestReadProgressDutchAndLateStart(t *testing.T) {
	_, _, cmd := sddWorktree(t, "Taak 3: complete\n")
	checkProgress(t, cmd, Progress{Task: 4, Tasks: 4, Step: "build"})
}

func TestReadProgressNoLedger(t *testing.T) {
	_, work, cmd := sddWorktree(t, "")
	if err := os.Remove(filepath.Join(work, "progress.md")); err != nil {
		t.Fatal(err)
	}
	if p, ok := ReadProgress(cmd); ok {
		t.Errorf("ReadProgress = %+v, true, want false", p)
	}
}

func TestReadProgressMissingWorktree(t *testing.T) {
	root, _, cmd := sddWorktree(t, "Task 1: complete\n")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if p, ok := ReadProgress(cmd); ok {
		t.Errorf("ReadProgress = %+v, true, want false", p)
	}
}

func TestReadProgressOtherPlanIgnored(t *testing.T) {
	root, _, cmd := sddWorktree(t, "Task 1: complete\n")
	other := filepath.Join(root, ".superpowers/sdd/other")
	write(t, filepath.Join(other, "plan-path"), "plans/p/other.md\n")
	write(t, filepath.Join(other, "progress.md"), "Task 1: complete\nTask 2: complete\nTask 3: complete\nTask 4: complete\n")
	checkProgress(t, cmd, Progress{Task: 2, Tasks: 4, Step: "build"})
}

func TestReadProgressNoRepo(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "plan.md"), "### Task 1\n")
	if p, ok := ReadProgress(sddCommand + filepath.Join(dir, "plan.md")); ok {
		t.Errorf("ReadProgress = %+v, true, want false", p)
	}
}

func TestReadProgressOtherCommand(t *testing.T) {
	root, _, _ := sddWorktree(t, "Task 1: complete\n")
	if p, ok := ReadProgress("/lighthouse-review-plan " + filepath.Join(root, "plans/p/plan.md")); ok {
		t.Errorf("ReadProgress = %+v, true, want false", p)
	}
}

// A directory whose name starts with ".." is inside the worktree, not above it.
func TestReadProgressDotDotDirectoryInsideRoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".git"), "gitdir: elsewhere\n")
	write(t, filepath.Join(root, "..plans/plan.md"), "# Plan\n\n### Task 1: a\n\n### Task 2: b\n")
	work := filepath.Join(root, ".superpowers/sdd/plan")
	write(t, filepath.Join(work, "plan-path"), "..plans/plan.md\n")
	write(t, filepath.Join(work, "progress.md"), "Task 1: complete\n")
	checkProgress(t, sddCommand+filepath.Join(root, "..plans/plan.md"), Progress{Task: 2, Tasks: 2, Step: "build"})
}
