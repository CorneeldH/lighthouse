package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

// A kenn-forge clone with core.bare=false (#478) makes the sweep print a cleanup line with the
// repair, and the tick fail; a bare clone prints nothing.
func TestSweepReposReportsNonBareClone(t *testing.T) {
	isolateGit(t)
	home := t.TempDir()
	// git reports the resolved path; on macOS t.TempDir() lies under the symlink /var.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(dir, "r.git")
	mustGit(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", clone)
	seed := t.TempDir()
	mustGit(t, seed, "init", "-q", "-b", "main")
	mustGit(t, seed, "commit", "-q", "--allow-empty", "-m", "seed")
	mustGit(t, seed, "push", "-q", clone, "main")
	root := kennforge.WorktreeRoot(home, "github", "", "o/r")
	mustGit(t, clone, "worktree", "add", "-q", "--detach", filepath.Join(root, "work-a"), "main")

	_, dbPath := tickFixture(t)
	env := tickEnv{Getenv: func(k string) string { return map[string]string{"HOME": home}[k] }}
	cfg := hostconfig.DefaultDispatch()
	repo := repoconfig.Repo{Slug: "o/r", Kind: "github"}
	repos := []checkedRepo{{repo: repo}}
	run := func() (string, bool) {
		var out bytes.Buffer
		failed := sweepRepos(context.Background(), env, openStore(t, dbPath), cfg, nil, repos, &out)
		return out.String(), failed
	}

	if out, failed := run(); failed || out != "" {
		t.Fatalf("bare clone: failed=%v out=%q, want silence", failed, out)
	}
	mustGit(t, clone, "config", "core.bare", "false")
	out, failed := run()
	if !failed || !strings.Contains(out, "lighthouse: cleanup o/r: kenn-forge clone "+clone) ||
		!strings.Contains(out, "git -C "+clone+" config core.bare true") {
		t.Errorf("non-bare clone: failed=%v out=%q, want a cleanup line with the repair", failed, out)
	}
}

// The fake workspaces of the tick tests live at made-up paths under /work/ that are never on
// disk; only a path that really does not exist counts as a missing worktree there.
func init() {
	real := workplace.WorktreeMissing
	workplace.WorktreeMissing = func(path string) bool {
		return !strings.HasPrefix(path, "/work/") && real(path)
	}
}
