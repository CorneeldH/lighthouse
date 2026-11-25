package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// soak is how long a release candidate runs on the dev channel before its release PR opens.
const soak = 7 * 24 * time.Hour

// Action is what the reconcile run does for one release branch.
type Action string

const (
	ActionNone           Action = "none"
	ActionTagRC          Action = "tag-rc"
	ActionOpenReleasePR  Action = "open-release-pr"
	ActionCloseReleasePR Action = "close-release-pr"
	ActionOpenPatchPR    Action = "open-patch-pr"
	// ActionReleaseTip: the tip is a `chore(release)` commit without a tag, so the release PR was
	// merged and the tag step (macOS gate, dist commit, tag, sync) must run again, not a second PR.
	ActionReleaseTip Action = "release-tip"
	// ActionRecover: the tip is released and tagged, but main still lacks the section: only the
	// sync step runs again.
	ActionRecover Action = "recover"
	// ActionBlocked: the tip is a milestone release commit whose parent is not the source of the
	// newest rc, so something was merged after the soak and release.yml would refuse the tag every
	// hour, after a full macOS run. Only a revert of the release commit by hand clears it.
	ActionBlocked Action = "blocked"
)

// BranchState is everything decide needs to know about one release branch.
type BranchState struct {
	Branch, Tip, TipSubject string
	LatestRC                string    // newest vX.Y.0-rc.N tag, or ""
	RCCommit                string    // commit of that tag
	RCTime                  time.Time // when that tag was made
	Released                bool      // the vX.Y.0 tag exists
	ReleaseTag              string    // newest vX.Y.Z tag of the line, or ""
	ReleasePROpen           bool
	ReleasePRClosedByHand   bool
	// NewCommitsSinceLatestTag: the tip holds commits the newest tag (rc on an open branch,
	// release on a released one) does not.
	NewCommitsSinceLatestTag bool
	TipTagged                bool // the tip, or the commit a tag sits on, carries a release tag
	// ReleasableOpen: the branch holds a fragment of a releasable type that no earlier release
	// shipped, so a patch release has something to release.
	ReleasableOpen bool
	// ReleasePRStale: the open release PR was cut before the newest commit of the branch.
	ReleasePRStale bool
	// ParentMismatch: see ActionBlocked.
	ParentMismatch bool
	SyncPending    bool // the tag of the tip has no section in main's CHANGELOG.md yet
}

var releaseSubjectRe = regexp.MustCompile(`^chore\(release\): v\d+\.\d+\.\d+( \(#\d+\))?$`)

// decide is a pure function of the state: the one action the reconcile run takes now.
func decide(s BranchState, now time.Time) Action {
	if releaseSubjectRe.MatchString(s.TipSubject) {
		switch {
		case !s.TipTagged && s.ParentMismatch:
			return ActionBlocked
		case !s.TipTagged:
			return ActionReleaseTip
		case s.SyncPending:
			return ActionRecover
		}
		return ActionNone
	}
	if s.Released {
		switch {
		case !s.NewCommitsSinceLatestTag || !s.ReleasableOpen:
			return ActionNone
		case s.ReleasePROpen && s.ReleasePRStale:
			// A fix landed after the patch PR was cut: it would ship the code without its entry.
			return ActionCloseReleasePR
		case s.ReleasePROpen || s.ReleasePRClosedByHand:
			return ActionNone
		}
		return ActionOpenPatchPR
	}
	if s.LatestRC == "" || s.NewCommitsSinceLatestTag {
		if s.ReleasePROpen {
			return ActionCloseReleasePR
		}
		return ActionTagRC
	}
	if s.ReleasePROpen || s.ReleasePRClosedByHand {
		return ActionNone
	}
	if now.Sub(s.RCTime) >= soak {
		return ActionOpenReleasePR
	}
	return ActionNone
}

// loadState reads the BranchState of branch (release/vX.Y) from git. The PR flags are not in git;
// the caller sets them.
func loadState(root, branch string) (BranchState, error) {
	m := branchRe.FindStringSubmatch(branch)
	if m == nil {
		return BranchState{}, fmt.Errorf("--branch must be release/vX.Y, got '%s'", branch)
	}
	x, y := atoi(m[1]), atoi(m[2])
	s := BranchState{Branch: branch}
	ref := "refs/remotes/origin/" + branch
	if _, err := gitOut(root, "rev-parse", "--verify", "-q", ref); err != nil {
		ref = "refs/heads/" + branch
	}
	tip, err := gitOut(root, "rev-parse", ref)
	if err != nil {
		return s, err
	}
	s.Tip = tip
	if s.TipSubject, err = gitOut(root, "log", "-1", "--format=%s", tip); err != nil {
		return s, err
	}
	tags, err := allTags(root)
	if err != nil {
		return s, err
	}
	rcN := -1
	patch := -1
	for _, tag := range tags {
		if v, ok := parseTag(tag); ok && v[0] == x && v[1] == y {
			if v[2] == 0 {
				s.Released = true
			}
			if v[2] > patch {
				patch, s.ReleaseTag = v[2], tag
			}
		}
		if p := preTagRe.FindStringSubmatch(tag); p != nil && p[4] == "rc" &&
			atoi(p[1]) == x && atoi(p[2]) == y && p[3] == "0" {
			if n := atoi(p[5]); n > rcN {
				rcN, s.LatestRC = n, tag
			}
		}
	}
	newer := func(tag string) (bool, error) {
		out, err := gitOut(root, "rev-list", "--count", tag+".."+tip)
		if err != nil {
			return false, err
		}
		n, _ := strconv.Atoi(out)
		return n > 0, nil
	}
	if s.LatestRC != "" {
		if s.RCCommit, err = gitOut(root, "rev-parse", s.LatestRC+"^{commit}"); err != nil {
			return s, err
		}
		stamp, err := gitOut(root, "log", "-1", "--format=%cI", s.LatestRC)
		if err != nil {
			return s, err
		}
		if s.RCTime, err = time.Parse(time.RFC3339, stamp); err != nil {
			return s, err
		}
	}
	since := s.LatestRC
	if s.Released {
		since = s.ReleaseTag
	}
	s.NewCommitsSinceLatestTag = true
	if since != "" {
		if s.NewCommitsSinceLatestTag, err = newer(since); err != nil {
			return s, err
		}
	}
	if s.ReleasableOpen, err = releasableOpen(root, ref, branch); err != nil {
		return s, err
	}
	if releaseSubjectRe.MatchString(s.TipSubject) && s.LatestRC != "" {
		version := strings.Fields(s.TipSubject)[1]
		if strings.HasSuffix(version, ".0") {
			parent, err1 := gitOut(root, "rev-parse", "-q", "--verify", tip+"^")
			source, err2 := gitOut(root, "rev-parse", "-q", "--verify", s.LatestRC+"^")
			s.ParentMismatch = err1 == nil && err2 == nil && parent != source
		}
	}
	// The tip is tagged when a release tag sits on it or on a commit directly above it (the dist
	// commit of tag-with-ui).
	for _, tag := range tags {
		if _, ok := parseTag(tag); !ok {
			continue
		}
		on, err := gitOut(root, "rev-parse", tag+"^{commit}")
		if err != nil {
			return s, err
		}
		parent, _ := gitOut(root, "rev-parse", "-q", "--verify", tag+"^")
		if on == tip || parent == tip {
			s.TipTagged = true
			if releaseSubjectRe.MatchString(s.TipSubject) {
				cl, err := gitOut(root, "show", mainRef(root)+":CHANGELOG.md")
				s.SyncPending = err != nil || !strings.Contains(cl, "## ["+tag+"] - ")
			}
		}
	}
	return s, nil
}

// mainRef is origin/main when it exists, else main.
func mainRef(root string) string {
	if _, err := gitOut(root, "rev-parse", "--verify", "-q", "refs/remotes/origin/main"); err == nil {
		return "origin/main"
	}
	return "main"
}

var fragmentTypeRe = regexp.MustCompile(`(?m)^type:[ \t]*(\w+)`)

// releasableOpen reports whether the branch holds an open fragment of a releasable type that no
// earlier release shipped.
func releasableOpen(root, ref, branch string) (bool, error) {
	names, err := branchFragments(root, branch)
	if err != nil {
		return false, err
	}
	released, err := releasedFragments(root)
	if err != nil {
		return false, err
	}
	for name := range names {
		if released[name] || !strings.HasSuffix(name, ".md") {
			continue
		}
		body, err := gitOut(root, "show", ref+":"+fragmentDir+"/"+name)
		if err != nil {
			return false, err
		}
		if m := fragmentTypeRe.FindStringSubmatch(body); m != nil && contains(releasable, m[1]) {
			return true, nil
		}
	}
	return false, nil
}
