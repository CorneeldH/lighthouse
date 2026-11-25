package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	branchRe = regexp.MustCompile(`^release/v(\d+)\.(\d+)$`)
	preTagRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-(dev|rc|test)\.(\d+)$`)
	nextKind = []string{"dev", "rc", "test", "minor", "major", "patch"}
)

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return n
}

// highestFinal returns the highest `vX.Y.Z` among tags, by number.
func highestFinal(tags []string) ([3]int, bool) {
	var best [3]int
	found := false
	for _, tag := range tags {
		v, ok := parseTag(tag)
		if !ok {
			continue
		}
		if !found || v[0] > best[0] || v[0] == best[0] && (v[1] > best[1] || v[1] == best[1] && v[2] > best[2]) {
			best, found = v, true
		}
	}
	return best, found
}

// nextMinorBase is the X.Y.0 of the next release line: the minor after the highest open release
// branch, or after the last release when no branch is open. A branch is open while its `vX.Y.0`
// tag is missing.
func nextMinorBase(tags, branches []string) ([2]int, error) {
	var top [2]int
	found := false
	raise := func(x, y int) {
		if !found || x > top[0] || x == top[0] && y > top[1] {
			top, found = [2]int{x, y}, true
		}
	}
	if v, ok := highestFinal(tags); ok {
		raise(v[0], v[1])
	}
	for _, b := range branches {
		m := branchRe.FindStringSubmatch(b)
		if m == nil {
			continue
		}
		x, y := atoi(m[1]), atoi(m[2])
		if !contains(tags, fmt.Sprintf("v%d.%d.0", x, y)) {
			raise(x, y)
		}
	}
	if !found {
		return top, &releaseError{msg: "no release tag or release branch to count from"}
	}
	return [2]int{top[0], top[1] + 1}, nil
}

// nextPre returns `<base>-<suffix>.N`, N one above the highest existing number for that base.
func nextPre(base, suffix string, tags []string) string {
	highest := 0
	for _, tag := range tags {
		m := preTagRe.FindStringSubmatch(tag)
		if m == nil || m[4] != suffix || fmt.Sprintf("v%s.%s.%s", m[1], m[2], m[3]) != base {
			continue
		}
		if n := atoi(m[5]); n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s-%s.%d", base, suffix, highest+1)
}

// nextTag computes the next tag of kind. dev and test count up on the next minor; rc and patch
// work on the release branch; minor and major name the version of a new split.
func nextTag(kind, branch string, tags, branches []string) (string, error) {
	if !contains(nextKind, kind) {
		return "", fmt.Errorf("unknown kind '%s' (choose from %s)", kind, strings.Join(nextKind, ", "))
	}
	switch kind {
	case "dev", "test", "minor":
		base, err := nextMinorBase(tags, branches)
		if err != nil {
			return "", err
		}
		v := fmt.Sprintf("v%d.%d.0", base[0], base[1])
		if kind == "minor" {
			return v, nil
		}
		return nextPre(v, kind, tags), nil
	case "major":
		v, ok := highestFinal(tags)
		for _, b := range branches {
			if m := branchRe.FindStringSubmatch(b); m != nil && (!ok || atoi(m[1]) > v[0]) {
				v, ok = [3]int{atoi(m[1])}, true
			}
		}
		if !ok {
			return "", &releaseError{msg: "no release tag or release branch to count from"}
		}
		return fmt.Sprintf("v%d.0.0", v[0]+1), nil
	}
	m := branchRe.FindStringSubmatch(branch)
	if m == nil {
		return "", fmt.Errorf("--branch must be release/vX.Y, got '%s'", branch)
	}
	x, y := atoi(m[1]), atoi(m[2])
	first := fmt.Sprintf("v%d.%d.0", x, y)
	if kind == "rc" {
		if contains(tags, first) {
			return "", &releaseError{msg: first + " is already released"}
		}
		return nextPre(first, "rc", tags), nil
	}
	if !contains(tags, first) {
		return "", &releaseError{msg: first + " is not released yet, no patch"}
	}
	patch := 0
	for _, tag := range tags {
		if v, ok := parseTag(tag); ok && v[0] == x && v[1] == y && v[2] > patch {
			patch = v[2]
		}
	}
	return fmt.Sprintf("v%d.%d.%d", x, y, patch+1), nil
}

// releaseBranches lists the release/vX.Y branches, local and on origin.
func releaseBranches(root string) ([]string, error) {
	out, err := gitOut(root, "for-each-ref", "--format=%(refname:short)", "refs/heads/release", "refs/remotes/origin/release")
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, b := range strings.Fields(out) {
		b = strings.TrimPrefix(b, "origin/")
		if !contains(branches, b) {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// allTags lists every tag of the repo.
func allTags(root string) ([]string, error) {
	out, err := gitOut(root, "tag", "--list", "v*")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}
