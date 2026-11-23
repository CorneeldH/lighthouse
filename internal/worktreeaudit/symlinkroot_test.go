package worktreeaudit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// Red when Collect stops resolving symlinks of the configured root (or findOrphans stops cleaning
// both sides of the `known` comparison): git lists a worktree by its real path, the root is
// configured behind a symlink, and the live worktree then shows as an orphan and the KPI counts 0.
func TestCollectResolvesASymlinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs a privilege on Windows")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	realRoot := filepath.Join(base, "real-wt")
	linkRoot := filepath.Join(base, "link-wt")
	for _, d := range []string{repo, realRoot} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "init")
	git(repo, "worktree", "add", "-q", "-b", "issue-1-x", filepath.Join(linkRoot, "issue-1-x"))
	t.Chdir(repo)

	env := Env{
		Run: procrun.Exec(GitTimeout),
		Config: func() (*config.Config, error) {
			return config.Parse([]byte("[worktree]\nroot = \""+filepath.ToSlash(linkRoot)+"\"\n"), "lighthouse.toml")
		},
		Alive:  func(int) *bool { return ptr(false) },
		Home:   t.TempDir(),
		Stderr: &strings.Builder{},
	}
	audit, err := Collect(context.Background(), env, repo)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Root != realRoot {
		t.Errorf("Root = %q, want the symlink-resolved %q", audit.Root, realRoot)
	}
	for _, r := range audit.Rows {
		if r.OrphanDir != nil {
			t.Errorf("row %s is an orphan, but git lists it as a live worktree", r.Name)
		}
	}
	got := View{Policy: audit.Policy, Alive: PidAlive, Root: audit.Root}.KPI(audit.Rows)
	if !strings.HasPrefix(got, "worktrees=1 ") {
		t.Errorf("KPI %q, want worktrees=1", got)
	}
}
