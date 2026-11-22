package gitenvscrub

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestScrubUnsetsEveryVariable(t *testing.T) {
	for _, name := range Vars {
		t.Setenv(name, "/elsewhere")
	}
	Scrub()
	for _, name := range Vars {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s still set to %q after Scrub", name, v)
		}
	}
}

// TestFixtureCommitStaysOutOfTheRealRepo is the reproduction of #463: a child process that starts
// with GIT_DIR pointing at a victim repository, and imports this package, must commit in its own
// fixture, not in the victim.
func TestFixtureCommitStaysOutOfTheRealRepo(t *testing.T) {
	victim := t.TempDir()
	run(t, victim, nil, "git", "init", "-q", "-b", "main")
	count := func() string {
		return run(t, victim, nil, "git", "rev-list", "--all", "--count")
	}
	before := count()

	fixture := t.TempDir()
	// The child is this very test binary, in a mode that makes one fixture commit after the init.
	run(t, fixture, []string{"GIT_DIR=" + filepath.Join(victim, ".git"), "GITENVSCRUB_CHILD=" + fixture},
		os.Args[0], "-test.run=^TestChildFixtureCommit$")

	if after := count(); after != before {
		t.Errorf("victim repo has %s commits after the fixture ran, had %s", after, before)
	}
	if got := strings.TrimSpace(run(t, fixture, nil, "git", "rev-list", "--count", "HEAD")); got != "1" {
		t.Errorf("fixture has %s commits, want 1", got)
	}
}

func TestChildFixtureCommit(t *testing.T) {
	dir := os.Getenv("GITENVSCRUB_CHILD")
	if dir == "" {
		t.Skip("only the child of TestFixtureCommitStaysOutOfTheRealRepo runs this")
	}
	env := []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	run(t, dir, env, "git", "-C", dir, "init", "-q", "-b", "main")
	run(t, dir, env, "git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "init")
}

var (
	gitUse    = regexp.MustCompile(`"git"|internal/fixturesetup"`)
	scrubUse  = regexp.MustCompile(`_\s+"github\.com/CorneeldH/lighthouse/internal/gitenvscrub"`)
	exemptPkg = map[string]bool{"internal/gitenvscrub": true}
)

// TestEveryPackageThatRunsGitInATestScrubsTheEnvironment keeps the fix whole: a package whose tests
// start git, and that does not import this package, leaks fixture commits when GIT_DIR is set.
func TestEveryPackageThatRunsGitInATestScrubsTheEnvironment(t *testing.T) {
	root := filepath.Join("..", "..")
	usesGit, scrubs := map[string]bool{}, map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "worktrees", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if gitUse.Match(data) {
			usesGit[rel] = true
		}
		if scrubUse.Match(data) {
			scrubs[rel] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for pkg := range usesGit {
		if !scrubs[pkg] && !exemptPkg[pkg] {
			missing = append(missing, pkg)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("tests start git but do not import internal/gitenvscrub (add a gitenv_test.go):\n  %s", strings.Join(missing, "\n  "))
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}
