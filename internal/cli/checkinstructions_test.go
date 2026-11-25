package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/testpath"
)

// checkInstructionsEnv blanks what a controller run or the host would leak into the command and
// returns an empty lighthouse.toml outside any repo, so no config of the machine is read.
func checkInstructionsEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"LIGHTHOUSE_CONTROLLER_RUN", "LIGHTHOUSE_REPO_ROOT"} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", filepath.Join(t.TempDir(), "personal"))
}

// Classification: cli-output. Fails when the command no longer finds the repo root through git
// from the cwd, which is how every hook and skill calls it, or runs its checks on the cwd instead.
func TestCheckInstructionsRootFromGit(t *testing.T) {
	checkInstructionsEnv(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "-C", repo, "-c", "commit.gpgsign=false", "init", "-q")
	init.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(strings.Repeat("woord ", 5)), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"check-instructions"})
	if code := execute(root, &errOut); code != 0 {
		t.Errorf("code %d, want 0; stderr %q", code, errOut.String())
	}
	if want := "ok    CLAUDE.md counts 5 words, within the ceiling of 3000\n"; !strings.Contains(out.String(), want) {
		t.Errorf("no %q in stdout:\n%s", want, out.String())
	}
}

// Classification: cli-output. Fails when a cwd outside any repository, without
// LIGHTHOUSE_REPO_ROOT, stops giving exit 2 with COULD NOT CHECK on stderr: it would then check
// the wrong tree or pass.
func TestCheckInstructionsNoRootCouldNotCheck(t *testing.T) {
	checkInstructionsEnv(t)
	plain := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
	t.Chdir(plain)

	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"check-instructions"})
	code := execute(root, &errOut)
	want := "check-instructions: COULD NOT CHECK — no repository root: LIGHTHOUSE_REPO_ROOT is not set and git rev-parse --show-toplevel failed\n"
	if code != 2 || errOut.String() != want || out.Len() != 0 {
		t.Errorf("code %d stderr %q stdout %q, want 2 %q \"\"", code, errOut.String(), out.String(), want)
	}
}

func TestPersonalSkills(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		dir := testpath.Abs("some", "dir")
		t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", dir)
		testpath.SetHome(t, testpath.Abs("home", "x"))
		if got := personalSkills(); got != dir {
			t.Errorf("got %q", got)
		}
	})
	t.Run("home", func(t *testing.T) {
		t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", "")
		home := testpath.Abs("home", "x")
		testpath.SetHome(t, home)
		if got, want := personalSkills(), filepath.Join(home, ".claude", "skills"); got != want {
			t.Errorf("got %q", got)
		}
	})
	// Red when an unset HOME gives "/.claude/skills", a path that can exist on a machine.
	t.Run("no home", func(t *testing.T) {
		t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", "")
		testpath.SetHome(t, "")
		if got := personalSkills(); got != "$HOME/.claude/skills" {
			t.Errorf("got %q", got)
		}
	})
}
