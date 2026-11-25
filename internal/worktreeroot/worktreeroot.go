// Package worktreeroot resolves where the worktrees of a repo live: the `[worktree] root` key.
package worktreeroot

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitpath"
)

// Default is the root of the worktrees when `[worktree] root` is not set.
const Default = ".claude/worktrees"

// Resolve turns the value of `[worktree] root` into an absolute-or-repo-based path. An empty
// value is the default, an absolute value stays as it is, a relative one is taken from repoRoot.
func Resolve(repoRoot, value string) string {
	if value == "" {
		value = Default
	}
	// A leading `/` or `\` counts as absolute on every OS, as in the skills' shell `case`: on
	// Windows filepath.IsAbs is false for a rooted path without a drive letter.
	if filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
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
	common := gitpath.Native(string(out))
	if common == "" {
		return "", fmt.Errorf("main checkout of %s not found: empty git common dir", dir)
	}
	return filepath.Dir(common), nil
}

// InSyncFolder reports whether path runs through a folder that a sync client (OneDrive, Dropbox,
// Google Drive, iCloud Drive) manages, and which provider. Only whole path segments count, case-insensitive,
// with `/` or `\` as separator: MyOneDriveNotes is not OneDrive. A segment prefix counts for the
// names the clients give a folder: `OneDrive - <name>`, `OneDrive-<name>`, `Dropbox (<team>)`,
// `GoogleDrive-<account>`.
func InSyncFolder(path string) (provider string, ok bool) {
	segs := strings.FieldsFunc(strings.ToLower(path), func(r rune) bool { return r == '/' || r == '\\' })
	for i, s := range segs {
		switch {
		case s == "onedrive" || strings.HasPrefix(s, "onedrive - ") || strings.HasPrefix(s, "onedrive-"):
			return "OneDrive", true
		case s == "dropbox" || strings.HasPrefix(s, "dropbox ("):
			return "Dropbox", true
		case s == "google drive" || s == "googledrive" || strings.HasPrefix(s, "googledrive-"):
			return "Google Drive", true
		case s == "icloud drive" || s == "iclouddrive":
			return "iCloud Drive", true
		case s == "library" && i+2 < len(segs) && segs[i+1] == "mobile documents" && segs[i+2] == "com~apple~clouddocs":
			return "iCloud Drive", true
		}
	}
	return "", false
}
