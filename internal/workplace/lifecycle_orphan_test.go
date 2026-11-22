package workplace

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// orphanWP is a fakeWP that also answers the branch check of a workspace whose worktree is gone.
type orphanWP struct {
	fakeWP
	nowhere    bool
	checkErr   error
	checked    []string
	orphanSeen []string // the removed ids with RemoveOptions.Orphan
}

func (o *orphanWP) BranchNowhere(_ context.Context, id, branch string) (bool, error) {
	o.checked = append(o.checked, id+":"+branch)
	return o.nowhere, o.checkErr
}

func (o *orphanWP) Remove(ctx context.Context, id string, opt RemoveOptions) error {
	if opt.Orphan {
		o.orphanSeen = append(o.orphanSeen, id)
	}
	return o.fakeWP.Remove(ctx, id, opt)
}

func goneCand(t *testing.T, mergedHead string) Candidate {
	c := cand(7)
	c.Workspace.Path = filepath.Join(t.TempDir(), "gone") // absolute, does not exist
	c.MergedHead = mergedHead
	return c
}

// A workspace whose directory is gone and whose PR merged is removed with Orphan, with no git
// measured (the runner fails the test on any call) and no leftover.
func TestCleanupMissingDirMergedPRRemoves(t *testing.T) {
	db := openLeftoverDB(t)
	w := &orphanWP{}
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), goneCand(t, "abc"), lcNow)
	if err != nil || !removed {
		t.Fatalf("Cleanup = %v, %v", removed, err)
	}
	if len(w.orphanSeen) != 1 || len(w.forced) != 0 || len(w.checked) != 0 {
		t.Errorf("orphan removes %v, forced %v, branch checks %v", w.orphanSeen, w.forced, w.checked)
	}
	if got := leftoverReasons(t, db); len(got) != 0 {
		t.Errorf("leftovers = %v, want none", got)
	}
}

// Without a merged PR the branch must be nowhere: not in the clone, not on origin.
func TestCleanupMissingDirBranchNowhereRemoves(t *testing.T) {
	db := openLeftoverDB(t)
	w := &orphanWP{nowhere: true}
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), goneCand(t, ""), lcNow)
	if err != nil || !removed || len(w.orphanSeen) != 1 {
		t.Fatalf("Cleanup = %v, %v, orphan removes %v", removed, err, w.orphanSeen)
	}
	if len(w.checked) != 1 || w.checked[0] != "w1:issue-7" {
		t.Errorf("branch checks = %v", w.checked)
	}
}

func TestCleanupMissingDirKeeps(t *testing.T) {
	cases := []struct {
		name string
		wp   *orphanWP
		want string
	}{
		{"branch exists", &orphanWP{nowhere: false}, "worktree directory is gone, but branch issue-7 still exists"},
		{"check fails", &orphanWP{checkErr: errors.New("no sibling worktree")}, "not measurable: branch of the missing worktree: no sibling worktree"},
	}
	for _, c := range cases {
		db := openLeftoverDB(t)
		removed, err := Cleanup(context.Background(), db, c.wp, noRunner(t), goneCand(t, ""), lcNow)
		if err != nil || removed || c.wp.removes != 0 {
			t.Fatalf("%s: Cleanup = %v, %v, removes %d", c.name, removed, err, c.wp.removes)
		}
		if got := leftoverReasons(t, db)["w1"]; !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: reason = %q, want prefix %q", c.name, got, c.want)
		}
	}
}

// A workplace without the branch check cannot prove the branch is nowhere.
func TestCleanupMissingDirWithoutBranchCheckKeeps(t *testing.T) {
	db := openLeftoverDB(t)
	w := &fakeWP{}
	removed, err := Cleanup(context.Background(), db, w, noRunner(t), goneCand(t, ""), lcNow)
	if err != nil || removed || w.removes != 0 {
		t.Fatalf("Cleanup = %v, %v, removes %d", removed, err, w.removes)
	}
	if got := leftoverReasons(t, db)["w1"]; !strings.HasPrefix(got, "not measurable: worktree directory is gone") {
		t.Errorf("reason = %q", got)
	}
}

// The other tests give a workspace a made-up path under /p/ that is never on disk; only a path
// that really does not exist counts as a missing worktree here.
func init() {
	real := WorktreeMissing
	WorktreeMissing = func(path string) bool {
		return !strings.HasPrefix(path, "/p/") && real(path)
	}
}
