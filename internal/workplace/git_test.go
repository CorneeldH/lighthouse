package workplace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo returns a clone with one pushed commit on main and a bare origin.
func newRepo(t *testing.T) (repo, bare string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "origin.git")
	repo = filepath.Join(root, "work")
	git(t, root, "init", "--bare", "-b", "main", bare)
	git(t, root, "init", "-b", "main", repo)
	git(t, repo, "remote", "add", "origin", bare)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "a.txt")
	git(t, repo, "commit", "-m", "init")
	git(t, repo, "push", "origin", "main")
	return repo, bare
}

func commitFile(t *testing.T, repo, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", name)
	git(t, repo, "commit", "-m", name)
}

func TestDirtyClean(t *testing.T) {
	t.Parallel()
	repo, _ := newRepo(t)
	got, err := Dirty(context.Background(), ExecRunner(), repo)
	if err != nil || got {
		t.Fatalf("got %v %v, want false nil", got, err)
	}
}

func TestDirtyUntracked(t *testing.T) {
	t.Parallel()
	repo, _ := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Dirty(context.Background(), ExecRunner(), repo)
	if err != nil || !got {
		t.Fatalf("got %v %v, want true nil", got, err)
	}
}

func TestUnpushedAfterPush(t *testing.T) {
	t.Parallel()
	repo, _ := newRepo(t)
	got, err := Unpushed(context.Background(), ExecRunner(), repo, "")
	if err != nil || got {
		t.Fatalf("got %v %v, want false nil", got, err)
	}
}

func TestUnpushedLocalCommit(t *testing.T) {
	t.Parallel()
	repo, _ := newRepo(t)
	commitFile(t, repo, "b.txt")
	got, err := Unpushed(context.Background(), ExecRunner(), repo, "")
	if err != nil || !got {
		t.Fatalf("got %v %v, want true nil", got, err)
	}
}

func TestUnpushedWithoutRemoteBranch(t *testing.T) {
	t.Parallel()
	repo, _ := newRepo(t)
	git(t, repo, "checkout", "-b", "feature")
	commitFile(t, repo, "b.txt")
	got, err := Unpushed(context.Background(), ExecRunner(), repo, "")
	if err != nil || !got {
		t.Fatalf("got %v %v, want true nil", got, err)
	}
}

func TestUnpushedSquashMergedPruned(t *testing.T) {
	t.Parallel()
	repo, bare := newRepo(t)
	git(t, repo, "checkout", "-b", "feature")
	commitFile(t, repo, "b.txt")
	git(t, repo, "push", "origin", "feature")
	head := git(t, repo, "rev-parse", "HEAD")
	git(t, bare, "branch", "-D", "feature")
	git(t, repo, "fetch", "--prune", "origin")

	ctx := context.Background()
	got, err := Unpushed(ctx, ExecRunner(), repo, head)
	if err != nil || got {
		t.Errorf("with mergedHead: got %v %v, want false nil", got, err)
	}
	got, err = Unpushed(ctx, ExecRunner(), repo, "")
	if err != nil || !got {
		t.Errorf("without mergedHead: got %v %v, want true nil", got, err)
	}
	got, err = Unpushed(ctx, ExecRunner(), repo, strings.Repeat("0", 40))
	if err == nil {
		t.Errorf("unfetchable mergedHead: got %v nil, want error", got)
	}
}

// mergedElsewhere gives repo a pushed branch feature with one commit, then a commit on that
// branch from another clone, as a fix from another session does. The PR head is kept under
// refs/pull/1/head, as GitHub does, and the branch is deleted and pruned. fetched says whether
// repo fetched the PR head before the prune. It returns the PR head.
func mergedElsewhere(t *testing.T, fetched bool) (repo, mergedHead string) {
	t.Helper()
	repo, bare := newRepo(t)
	git(t, repo, "checkout", "-b", "feature")
	commitFile(t, repo, "b.txt")
	git(t, repo, "push", "origin", "feature")
	other := filepath.Join(t.TempDir(), "other")
	git(t, repo, "clone", "-b", "feature", bare, other)
	commitFile(t, other, "c.txt")
	git(t, other, "push", "origin", "feature")
	mergedHead = git(t, other, "rev-parse", "HEAD")
	if fetched {
		git(t, repo, "fetch", "origin")
	}
	git(t, bare, "update-ref", "refs/pull/1/head", mergedHead)
	git(t, bare, "branch", "-D", "feature")
	git(t, repo, "fetch", "--prune", "origin")
	return repo, mergedHead
}

func TestUnpushedBehindMergedHead(t *testing.T) {
	t.Parallel()
	for _, fetched := range []bool{true, false} {
		repo, mergedHead := mergedElsewhere(t, fetched)
		got, err := Unpushed(context.Background(), ExecRunner(), repo, mergedHead)
		if err != nil || got {
			t.Errorf("fetched=%v: got %v %v, want false nil", fetched, got, err)
		}
	}
}

func TestUnpushedOwnCommitAfterMergedHead(t *testing.T) {
	t.Parallel()
	repo, mergedHead := mergedElsewhere(t, true)
	git(t, repo, "merge", "--ff-only", mergedHead)
	commitFile(t, repo, "d.txt")
	got, err := Unpushed(context.Background(), ExecRunner(), repo, mergedHead)
	if err != nil || !got {
		t.Errorf("got %v %v, want true nil", got, err)
	}
}

func TestUnpushedOwnCommitBesideMergedHead(t *testing.T) {
	t.Parallel()
	for _, fetched := range []bool{true, false} {
		repo, mergedHead := mergedElsewhere(t, fetched)
		commitFile(t, repo, "d.txt")
		got, err := Unpushed(context.Background(), ExecRunner(), repo, mergedHead)
		if err != nil || !got {
			t.Errorf("fetched=%v: got %v %v, want true nil", fetched, got, err)
		}
	}
}

func TestGitUnmeasurable(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nope")
	if got, err := Dirty(context.Background(), ExecRunner(), missing); err == nil {
		t.Errorf("Dirty: got %v nil, want error", got)
	}
	if got, err := Unpushed(context.Background(), ExecRunner(), missing, ""); err == nil {
		t.Errorf("Unpushed: got %v nil, want error", got)
	}
}

func TestGitRunnerErrorPropagates(t *testing.T) {
	t.Parallel()
	run := func(context.Context, time.Duration, string, ...string) (string, int, error) {
		return "", -1, context.DeadlineExceeded
	}
	if _, err := Dirty(context.Background(), run, "/x"); err == nil {
		t.Error("Dirty: want error")
	}
	if _, err := Unpushed(context.Background(), run, "/x", ""); err == nil {
		t.Error("Unpushed: want error")
	}
}
