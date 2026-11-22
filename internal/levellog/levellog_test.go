package levellog

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repoWithWorktree makes a main checkout and a linked worktree of it, and points the state
// directory at a fresh temp dir.
func repoWithWorktree(t *testing.T) (main, linked string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	base := t.TempDir()
	main = filepath.Join(base, "main")
	linked = filepath.Join(base, "linked")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, main, "init", "-q", "-b", "main")
	git(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, main, "worktree", "add", "-q", "-b", "other", linked)
	return main, linked
}

// Fails when Append keys the file on the worktree path (--show-toplevel) instead of the
// absolute --git-common-dir: the two roots would then count separately.
func TestWorktreesShareOneLog(t *testing.T) {
	main, linked := repoWithWorktree(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := Append(main, "main-commit-gate", "blocked", at); err != nil {
		t.Fatal(err)
	}
	if err := Append(linked, "main-commit-gate", "blocked again", at); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{main, linked} {
		got, err := Count(root, at.Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if got["main-commit-gate"] != 2 || len(got) != 1 {
			t.Errorf("Count(%s) = %v, want 2 for main-commit-gate", root, got)
		}
	}
}

// Fails when Count ignores since, or compares with > instead of >= on the boundary.
func TestCountIgnoresBeforeSince(t *testing.T) {
	main, _ := repoWithWorktree(t)
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{since.Add(-time.Second), since, since.Add(time.Hour)} {
		if err := Append(main, "a", "x", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := Append(main, "b", "x", since.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := Count(main, since)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 2 || got["b"] != 0 {
		t.Errorf("Count = %v, want a=2 and no b", got)
	}
}

// Fails when Append swallows a write failure: the hook then claims a measurement it did not make.
func TestAppendWithoutWritableDirIsAnError(t *testing.T) {
	main, _ := repoWithWorktree(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker) // a file, so no directory can be made below it
	if err := Append(main, "a", "x", time.Now()); err == nil {
		t.Error("Append into an unwritable state dir returned nil")
	}
}

// Fails when Dir accepts a relative base: a log would then land in whatever cwd a hook has.
func TestDirNeedsAnAbsoluteBase(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative/state")
	t.Setenv("HOME", "relative/home")
	t.Setenv("LOCALAPPDATA", "relative/local")
	t.Setenv("USERPROFILE", "relative/home")
	if dir, err := Dir(); err == nil {
		t.Errorf("Dir() = %q, want an error", dir)
	}
}

// Fails when Dir ignores XDG_STATE_HOME or drops the lighthouse/level-log suffix.
func TestDirUsesXDGStateHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "lighthouse", "level-log"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

// Fails when the key depends on anything but the repository, e.g. on time or a counter.
func TestKeyIsStableAcrossCalls(t *testing.T) {
	main, linked := repoWithWorktree(t)
	first, err := logPath(main)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{main, linked, main} {
		got, err := logPath(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Errorf("logPath(%s) = %q, want %q", root, got, first)
		}
	}
	if runtime.GOOS != "windows" && len(filepath.Base(first)) < 16 {
		t.Errorf("key too short: %q", first)
	}
}
