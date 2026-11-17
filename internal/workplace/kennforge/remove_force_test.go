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
