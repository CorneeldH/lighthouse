package main

import (
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strings"
)

// gitOut runs git in root and returns its trimmed stdout. The error carries git's stderr.
func gitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), detail)
	}
	return strings.TrimSpace(string(out)), nil
}

// releaseCommitOf returns the commit `chore(release): <tag>` reachable from tag (a squash merge may
// append ` (#N)`), or "" when the tag has none.
func releaseCommitOf(root, tag string) (string, error) {
	re := "^chore\\(release\\): " + regexp.QuoteMeta(tag) + "( \\(#[0-9]+\\))?$"
	return gitOut(root, "log", "-1", "-E", "--grep", re, "--format=%H", tag)
}

// removedFragments lists the basenames of fragments the commit removed from changelog.d/.
func removedFragments(root, sha string) ([]string, error) {
	out, err := gitOut(root, "show", "-m", "--first-parent", "--diff-filter=D", "--name-only",
		"--no-renames", "--format=", sha, "--", fragmentDir+"/")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range strings.Split(out, "\n") {
		if p != "" && path.Base(p) != marker && strings.HasSuffix(p, ".md") {
			names = append(names, path.Base(p))
		}
	}
	return names, nil
}

// releaseTags lists the `vX.Y.Z` tags of the repo.
func releaseTags(root string) ([]string, error) {
	out, err := gitOut(root, "tag", "--list", "v*")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, tag := range strings.Fields(out) {
		if _, ok := parseTag(tag); ok {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

// releasedFragments returns the fragment names (basename under changelog.d/) that the commit
// `chore(release): vX.Y.Z` of a `vX.Y.Z` tag removed. The source is git, never CHANGELOG.md: a
// fragment released on a releasebranch is still open on main until the sync.
func releasedFragments(root string) (map[string]bool, error) {
	tags, err := releaseTags(root)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, tag := range tags {
		sha, err := releaseCommitOf(root, tag)
		if err != nil {
			return nil, err
		}
		if sha == "" {
			continue
		}
		names, err := removedFragments(root, sha)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			set[n] = true
		}
	}
	return set, nil
}
