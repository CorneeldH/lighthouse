package packcontract

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The worktree root is `[worktree] root` (default `.claude/worktrees`), so pack text asks
// `lighthouse config worktree.root` at runtime and does not hardcode the default.

// resolveRoot is the one shell form every skill uses: a relative root hangs under the main
// checkout (never the cwd or --show-toplevel), an absolute one stays as it is.
const resolveRoot = `ROOT=$(lighthouse config worktree.root) || exit 2
case "$ROOT" in /*|[A-Za-z]:*) ;; *) ROOT="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")/$ROOT" ;; esac`

// hardcodeAllowed is the exact set of pack files that may name `.claude/worktrees`, and how
// often: the one sentence of the managed block that states the default.
var hardcodeAllowed = map[string]int{"core/instructions/AGENTS.md": 1}

// A production change that fails this test: a pack file names `.claude/worktrees` again (a skill
// goes back to the hardcoded folder), or the managed block stops naming the default exactly once.
func TestPackHardcodesNoWorktreeFolder(t *testing.T) {
	got := map[string]int{}
	err := filepath.WalkDir(packRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if n := strings.Count(string(data), ".claude/worktrees"); n > 0 {
			rel, err := filepath.Rel(packRoot, path)
			if err != nil {
				return err
			}
			got[filepath.ToSlash(rel)] = n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for file, n := range got {
		if hardcodeAllowed[file] != n {
			t.Errorf("%s names .claude/worktrees %d times, allowed %d: use `lighthouse config worktree.root`", file, n, hardcodeAllowed[file])
		}
	}
	for file, n := range hardcodeAllowed {
		if got[file] != n {
			t.Errorf("%s names .claude/worktrees %d times, want %d", file, got[file], n)
		}
	}
}

// A production change that fails this test: fix-pr or close-issue (which build a worktree path)
// keep only `ROOT=$(lighthouse config worktree.root)` and drop the `case` line or the
// `--git-common-dir` anchor, or apply the anchor to an absolute root unconditionally.
func TestWorktreePathSkillsResolveTheRoot(t *testing.T) {
	for _, dir := range []string{"core/skills/fix-pr", "core/skills/close-issue"} {
		for variant, text := range variants(t, dir) {
			if !strings.Contains(text, resolveRoot) {
				t.Errorf("%s (%s) lacks the ROOT/case resolution of worktree.root", dir, variant)
			}
		}
	}
}

// A production change that fails this test: some skill reads `lighthouse config worktree.root` into
// ROOT without the `case` line right after it, so a relative root is used from the cwd.
func TestEveryRootAssignmentIsFollowedByTheCase(t *testing.T) {
	for _, dir := range skillDirs(t) {
		for variant, text := range variants(t, dir) {
			if strings.Contains(text, "ROOT=$(lighthouse config worktree.root)") && !strings.Contains(text, resolveRoot) {
				t.Errorf("%s (%s) assigns ROOT without the case line", dir, variant)
			}
		}
	}
}
