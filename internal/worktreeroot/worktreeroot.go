// Package worktreeroot resolves where the worktrees of a repo live: the `[worktree] root` key.
package worktreeroot

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Default is the root of the worktrees when `[worktree] root` is not set.
const Default = ".claude/worktrees"

// Resolve turns the value of `[worktree] root` into an absolute-or-repo-based path. An empty
// value is the default, an absolute value stays as it is, a relative one is taken from repoRoot.
func Resolve(repoRoot, value string) string {
	if value == "" {
		value = Default
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(repoRoot, value))
}

// Contains reports whether path lies below root. By filepath.Rel, not by prefix: /tmp/wt-old is
// not below /tmp/wt, and root itself is not below root.
func Contains(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// MainCheckout is the main checkout of the repo around dir: the parent of the common git dir, so
// also from a linked worktree (where --show-toplevel is the worktree itself).
func MainCheckout(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("main checkout of %s not found: %w", dir, err)
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return "", fmt.Errorf("main checkout of %s not found: empty git common dir", dir)
	}
	return filepath.Dir(common), nil
}
