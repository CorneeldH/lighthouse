package kennforge

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// A forced Remove on another node changes nothing: the worktree is the node's, and the tick
// host has no git for it. The node fixture's Runner fails the test on any call.
func TestRemoveForceOnNodeRefuses(t *testing.T) {
	_, _, _, a := twoNodes(t, "node-a")
	err := a.Remove(t.Context(), "ws1", workplace.RemoveOptions{Force: true})
	if err == nil || !strings.Contains(err.Error(), "on node node-a") {
		t.Fatalf("forced Remove on a node: err = %v, want a refusal naming the node", err)
	}
}

// The forced clean also discards a change to a tracked file, and runs no fsmonitor or hook the
// agent planted in the worktree's config.
func TestRemoveForceRunsNoPlantedFsmonitor(t *testing.T) {
	f := newForgeFixture(t).(*forgeFixture)
	ws := f.Workspace(t, "scheduled/nightly-20261116T0400-1")
	marker := filepath.Join(t.TempDir(), "planted-ran")
	script := filepath.Join(t.TempDir(), "planted.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"core.fsmonitor", script}, {"core.hooksPath", filepath.Dir(script)}} {
		if out, err := exec.Command("git", "-C", ws.Path, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v\n%s", kv[0], err, out)
		}
	}
	tracked, err := exec.Command("git", "-c", "core.fsmonitor=false", "-C", ws.Path, "ls-files").Output()
	if err != nil || len(tracked) == 0 {
		t.Fatalf("git ls-files: %v (%q)", err, tracked)
	}
	first := filepath.Join(ws.Path, strings.SplitN(string(tracked), "\n", 2)[0])
	if err := os.WriteFile(first, []byte("changed by the agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Remove(t.Context(), ws.ID, workplace.RemoveOptions{Force: true}); err != nil {
		t.Fatalf("forced Remove of a worktree with a tracked change: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("planted fsmonitor ran (stat %v), want it never started", err)
	}
}

// A forced Remove throws work away only from a linked worktree on a scheduled branch. Fails if
// discardWork resets and cleans the main checkout, or a worktree of a branch of someone's work,
// that the forge names as a workspace's worktree_path.
func TestRemoveForceRefusesWhatIsNotAScheduledWorktree(t *testing.T) {
	cases := []struct {
		name    string
		headRef string
		main    bool   // worktree_path is the main checkout instead of a linked worktree
		onGit   string // the branch the linked worktree really has; "" is headRef
		want    string
	}{
		{"main checkout", "scheduled/nightly-20261116T0400-1", true, "", "main checkout"},
		{"branch of other work", "issue-7-x", false, "", "not a scheduled run's"},
		{"no branch", "", false, "wt", "not a scheduled run's"},
		{"forge lies about the branch", "scheduled/x", false, "issue-7", "not \"scheduled/x\" as the forge says"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, a := newFakeForge(t)
			a.Run = workplace.ExecRunner()
			main := filepath.Join(t.TempDir(), "main")
			branch := c.onGit
			if branch == "" {
				branch = strings.TrimPrefix(c.headRef, "refs/heads/")
			}
			if c.main {
				branch = "wt" // the main checkout itself is on the scheduled branch, made below
			}
			for _, args := range [][]string{
				{"init", "--quiet", main},
				{"-C", main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--quiet", "--allow-empty", "-m", "init"},
				{"-C", main, "worktree", "add", "--quiet", "-b", branch, filepath.Join(filepath.Dir(main), "wt")},
			} {
				if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			f.path, f.headRef = filepath.Join(filepath.Dir(main), "wt"), c.headRef
			if c.main {
				if out, err := exec.Command("git", "-C", main, "checkout", "-q", "-b", c.headRef).CombinedOutput(); err != nil {
					t.Fatalf("git checkout: %v\n%s", err, out)
				}
				f.path = main
			}
			keep := filepath.Join(f.path, "untracked.txt")
			if err := os.WriteFile(keep, []byte("work"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := a.Remove(t.Context(), "ws1", workplace.RemoveOptions{Force: true})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("forced Remove: err = %v, want a refusal containing %q", err, c.want)
			}
			if _, serr := os.Stat(keep); serr != nil {
				t.Errorf("untracked file after the refusal: %v, want it kept", serr)
			}
			if n := f.count("DELETE /api/v1/workspaces/ws1"); n != 0 {
				t.Errorf("DELETE sent %d times, want none after the refusal", n)
			}
		})
	}
}
