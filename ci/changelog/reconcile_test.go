package main

import (
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func TestDecide(t *testing.T) {
	open := BranchState{Branch: "release/v0.28", Tip: "t1", TipSubject: "fix: x (#1)",
		LatestRC: "v0.28.0-rc.1", RCCommit: "r1", RCTime: t0}
	with := func(f func(*BranchState)) BranchState { s := open; f(&s); return s }
	released := BranchState{Branch: "release/v0.28", Tip: "t2", TipSubject: "fix: y (#2)", Released: true,
		ReleaseTag: "v0.28.0", LatestRC: "v0.28.0-rc.1", RCTime: t0}
	tests := []struct {
		name string
		s    BranchState
		now  time.Time
		want Action
	}{
		{"fresh rc waits", open, t0.Add(24 * time.Hour), ActionNone},
		{"rc six days waits", open, t0.Add(6 * 24 * time.Hour), ActionNone},
		{"rc seven days opens pr", open, t0.Add(7 * 24 * time.Hour), ActionOpenReleasePR},
		{"new commit tags rc", with(func(s *BranchState) { s.NewCommitsSinceLatestTag = true }), t0.Add(time.Hour), ActionTagRC},
		{"new commit with pr open closes pr first", with(func(s *BranchState) { s.NewCommitsSinceLatestTag, s.ReleasePROpen = true, true }), t0.Add(time.Hour), ActionCloseReleasePR},
		{"no rc yet tags rc", with(func(s *BranchState) { s.LatestRC, s.NewCommitsSinceLatestTag = "", true }), t0, ActionTagRC},
		{"pr open after soak does nothing", with(func(s *BranchState) { s.ReleasePROpen = true }), t0.Add(8 * 24 * time.Hour), ActionNone},
		{"closed by hand waits for a new rc", with(func(s *BranchState) { s.ReleasePRClosedByHand = true }), t0.Add(30 * 24 * time.Hour), ActionNone},
		{"closed by hand then new commit tags rc", with(func(s *BranchState) { s.ReleasePRClosedByHand, s.NewCommitsSinceLatestTag = true, true }), t0.Add(30 * 24 * time.Hour), ActionTagRC},
		{"untagged release tip releases", with(func(s *BranchState) { s.TipSubject = "chore(release): v0.28.0" }), t0.Add(8 * 24 * time.Hour), ActionReleaseTip},
		{"untagged release tip with squash suffix", with(func(s *BranchState) { s.TipSubject = "chore(release): v0.28.0 (#40)" }), t0, ActionReleaseTip},
		{"untagged release tip never opens a second pr", with(func(s *BranchState) { s.TipSubject = "chore(release): v0.28.0"; s.ReleasePROpen = false }), t0.Add(30 * 24 * time.Hour), ActionReleaseTip},
		{"tagged release tip with sync pending recovers", with(func(s *BranchState) { s.TipSubject, s.TipTagged, s.SyncPending = "chore(release): v0.28.0", true, true }), t0, ActionRecover},
		{"tagged release tip in sync does nothing", with(func(s *BranchState) { s.TipSubject, s.TipTagged = "chore(release): v0.28.0", true }), t0, ActionNone},
		{"released branch without new commits", released, t0, ActionNone},
		{"released branch with new commit opens patch pr", with2(released, func(s *BranchState) { s.NewCommitsSinceLatestTag = true }), t0, ActionOpenPatchPR},
		{"released branch patch pr already open", with2(released, func(s *BranchState) { s.NewCommitsSinceLatestTag, s.ReleasePROpen = true, true }), t0, ActionNone},
		{"released branch closed by hand", with2(released, func(s *BranchState) { s.NewCommitsSinceLatestTag, s.ReleasePRClosedByHand = true, true }), t0, ActionNone},
		{"released branch untagged patch tip", with2(released, func(s *BranchState) { s.TipSubject = "chore(release): v0.28.1"; s.NewCommitsSinceLatestTag = true }), t0, ActionReleaseTip},
	}
	for _, tc := range tests {
		if got := decide(tc.s, tc.now); got != tc.want {
			t.Errorf("%s: decide = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func with2(s BranchState, f func(*BranchState)) BranchState { f(&s); return s }

func TestDecideRCSixDaysWaits(t *testing.T) {
	s := BranchState{Branch: "release/v0.28", Tip: "t", TipSubject: "x", LatestRC: "v0.28.0-rc.1", RCTime: t0}
	if got := decide(s, t0.Add(7*24*time.Hour-time.Second)); got != ActionNone {
		t.Errorf("one second short of seven days: %q, want none", got)
	}
}

// reconcileRepo is a repo with a split release/v0.2 branch, an rc tag on a dist commit above its
// source, and main holding the changelog.
func reconcileRepo(t *testing.T) string {
	t.Helper()
	root := taggedRepo(t, nil)
	git(t, root, "checkout", "-q", "-b", "release/v0.2")
	write(t, filepath.Join(root, "src.txt"), "rc source\n")
	commit(t, root, "feat: thing (#5)")
	git(t, root, "checkout", "-q", "--detach")
	write(t, filepath.Join(root, "dist.txt"), "dist\n")
	commit(t, root, "chore: dist")
	git(t, root, "tag", "v0.2.0-rc.1")
	git(t, root, "checkout", "-q", "release/v0.2")
	return root
}

func TestLoadStateOpenBranch(t *testing.T) {
	root := reconcileRepo(t)
	s, err := loadState(root, "release/v0.2")
	if err != nil {
		t.Fatal(err)
	}
	if s.LatestRC != "v0.2.0-rc.1" || s.Released || s.NewCommitsSinceLatestTag || s.TipSubject != "feat: thing (#5)" || s.RCTime.IsZero() {
		t.Fatalf("state = %+v", s)
	}
	write(t, filepath.Join(root, "more.txt"), "x\n")
	commit(t, root, "fix: later (#6)")
	s, err = loadState(root, "release/v0.2")
	if err != nil || !s.NewCommitsSinceLatestTag {
		t.Fatalf("state after a new commit = %+v, %v", s, err)
	}
}

func TestLoadStateReleasedAndSync(t *testing.T) {
	root := reconcileRepo(t)
	write(t, filepath.Join(root, "rel.txt"), "release\n")
	commit(t, root, "chore(release): v0.2.0 (#7)")
	s, _ := loadState(root, "release/v0.2")
	if s.TipTagged || s.Released {
		t.Fatalf("untagged release tip: %+v", s)
	}
	git(t, root, "tag", "v0.2.0")
	s, _ = loadState(root, "release/v0.2")
	if !s.TipTagged || !s.Released || s.ReleaseTag != "v0.2.0" || !s.SyncPending {
		t.Fatalf("tagged, main lacks the section: %+v", s)
	}
	git(t, root, "checkout", "-q", "main")
	write(t, filepath.Join(root, "CHANGELOG.md"), preamble+"## [v0.2.0] - 2026-11-20\n\n### Added\n\n- X.\n\n"+oldRelease)
	commit(t, root, "chore: synced")
	s, _ = loadState(root, "release/v0.2")
	if s.SyncPending {
		t.Fatalf("sync done but pending: %+v", s)
	}
}

func TestRunReconcilePrintsAction(t *testing.T) {
	root := reconcileRepo(t)
	now := time.Now().Add(8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	code, stdout, stderr := runCLI("reconcile", "--branch", "release/v0.2", "--now", now,
		"--pr-open", "false", "--pr-closed-by-hand", "false", "--root", root)
	if code != 0 || stdout != "open-release-pr\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, _ := runCLI("reconcile", "--branch", "main", "--root", root); code != 2 {
		t.Errorf("bad branch: code=%d, want usage error 2", code)
	}
}
