package main

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var milestoneReleaseRe = regexp.MustCompile(`(?im)^[ \t]*release:[ \t]*ja[ \t]*\r?$`)

// milestoneRelease reports whether a milestone description marks a release milestone: a line
// `Release: ja`, any case. The sentence "Release: ja, misschien" or the marker inside a line of
// prose does not count.
func milestoneRelease(description string) bool { return milestoneReleaseRe.MatchString(description) }

// branchRef is the ref of a release branch: origin's when it exists, else the local one.
func branchRef(root, branch string) string {
	ref := "refs/remotes/origin/" + branch
	if _, err := gitOut(root, "rev-parse", "--verify", "-q", ref); err != nil {
		return "refs/heads/" + branch
	}
	return ref
}

// branchFragments lists the fragment names that changelog.d/ holds on a branch.
func branchFragments(root, branch string) (map[string]bool, error) {
	out, err := gitOut(root, "ls-tree", "--name-only", branchRef(root, branch), fragmentDir+"/")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range strings.Fields(out) {
		set[path.Base(p)] = true
	}
	return set, nil
}

// openBranches lists the release branches without a `vX.Y.0` tag, highest first.
func openBranches(tags, branches []string) []string {
	var open []string
	for _, b := range branches {
		m := branchRe.FindStringSubmatch(b)
		if m != nil && !contains(tags, fmt.Sprintf("v%s.%s.0", m[1], m[2])) {
			open = append(open, b)
		}
	}
	for i := range open {
		for j := i + 1; j < len(open); j++ {
			a, b := branchRe.FindStringSubmatch(open[i]), branchRe.FindStringSubmatch(open[j])
			if atoi(b[1]) > atoi(a[1]) || atoi(b[1]) == atoi(a[1]) && atoi(b[2]) > atoi(a[2]) {
				open[i], open[j] = open[j], open[i]
			}
		}
	}
	return open
}

// decideSplit says what a split run does for kind (minor or major): `split <branch> <rc-tag>`
// for a new release branch, `merge <branch> <rc-tag>` when the previous rc is still open (main is
// merged into that branch), or `skip <reason>`.
func decideSplit(root, kind string) (string, error) {
	tags, err := allTags(root)
	if err != nil {
		return "", err
	}
	branches, err := releaseBranches(root)
	if err != nil {
		return "", err
	}
	frags, err := readFragments(filepath.Join(root, fragmentDir))
	if err != nil {
		return "", err
	}
	released, err := releasedFragments(root)
	if err != nil {
		return "", err
	}
	open := openBranches(tags, branches)
	var onBranch map[string]bool
	if len(open) > 0 {
		if onBranch, err = branchFragments(root, open[0]); err != nil {
			return "", err
		}
	}
	fresh := 0
	for _, f := range frags {
		name := filepath.Base(f.path)
		if contains(releasable, f.typ) && !released[name] && !onBranch[name] {
			fresh++
		}
	}
	if len(open) > 0 {
		if fresh == 0 {
			return "skip no new releasable fragment on main for " + open[0], nil
		}
		rc, err := nextTag("rc", open[0], tags, branches)
		if err != nil {
			return "", err
		}
		return "merge " + open[0] + " " + rc, nil
	}
	if fresh == 0 {
		return "skip no releasable fragment that is not in a release branch yet", nil
	}
	version, err := nextTag(kind, "", tags, branches)
	if err != nil {
		return "", err
	}
	v, _ := parseTag(version)
	branch := fmt.Sprintf("release/v%d.%d", v[0], v[1])
	rc, err := nextTag("rc", branch, tags, append(branches, branch))
	if err != nil {
		return "", err
	}
	return "split " + branch + " " + rc, nil
}
