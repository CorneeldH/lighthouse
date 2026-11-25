package commitgate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/prbody"
	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

// The tests here run Pre against real git repositories: which root a commit lands in,
// whether that root belongs to this project, and what Pre does per branch. The test run itself is
// the fake one of the harness.

const (
	widgetsOrigin = "https://github.com/Acme/widgets.git"
	gadgetsOrigin = "https://github.com/Acme/gadgets.git"
)

// gitIn runs git in dir with a fixed identity and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitEnv is the environment without the variables that would point git at another repository.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_INDEX_FILE=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// newRepo is a repository named name under a temporary directory, on branch, with one commit.
func newRepo(t *testing.T, name, branch string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, name)
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q", "-b", branch)
	write(t, filepath.Join(repo, "README.md"), "# repo\n")
	gitIn(t, repo, "add", "README.md")
	gitIn(t, repo, "commit", "-qm", "init")
	return repo
}

// projectHarness is a pre gate for the project repository `main` returned next to it, in a clean
// environment: the state dir of the markers and the config files are the test's own.
func projectHarness(t *testing.T, gate string) (*harness, string) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	h := newHarness(t, "pre", gate, "")
	repo := newRepo(t, "project", "main")
	h.g.Env = append(h.g.Env, "CLAUDE_PROJECT_DIR="+repo)
	return h, repo
}

// stubGit puts a `git` in front of PATH that fails with message when its arguments match pattern
// (a shell case pattern), and otherwise runs the real one.
func stubGit(t *testing.T, pattern, message string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	stubbin.RequirePOSIX(t, "case on $* and exec of the real git")
	dir := t.TempDir()
	script := "case \"$*\" in\n" +
		"  " + pattern + ") echo '" + message + "' >&2; exit 128 ;;\n" +
		"esac\n" +
		"exec '" + real + "' \"$@\"\n"
	stubbin.MakeScript(t, dir, "git", script)
	stubbin.Prepend(t, dir)
}

// markerHeads is the `heads` of the marker that Pre recorded for session s1; ok is false without one.
func markerHeads(t *testing.T) (heads map[string]string, ok bool) {
	t.Helper()
	dir, err := StateDir("s1", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "t1.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var marker struct{ Heads map[string]string }
	if err := json.Unmarshal(raw, &marker); err != nil {
		t.Fatal(err)
	}
	return marker.Heads, true
}

func (h *harness) pre(command, cwd string) Result {
	h.t.Helper()
	return Pre(context.Background(), h.g, payload(command), cwd)
}

// ranIn are the roots the test command ran in.
func (h *harness) ranIn() []string {
	var dirs []string
	for _, c := range h.calls {
		dirs = append(dirs, c.opts.Dir)
	}
	return dirs
}

func passLine(root string) string { return plainNotes + "commit-test-gate: " + root + " — 3 passed" }

// The root that is tested is the target of the commit, not the directory the call starts in, and
// once per root however many commits the call holds.
func TestPreTestsTheRootOfTheCommit(t *testing.T) {
	spaced := newRepo(t, "a b", "main")
	for _, tc := range []struct {
		name    string
		repo    func(project string) string
		command func(repo string) string
		cwd     func(repo string) string
	}{
		{"cd", nil, func(r string) string { return "cd " + r + " && git commit -m x" }, func(string) string { return t.TempDir() }},
		{"git -C", nil, func(r string) string { return "git -C " + r + " commit -m x" }, func(string) string { return t.TempDir() }},
		{"two commits in one root", nil, func(string) string { return "git commit -m a && git commit -m b" }, func(r string) string { return r }},
		{"a path with a space", func(string) string { return spaced },
			func(r string) string { return `cd "` + r + `" && git commit -m x` }, func(string) string { return t.TempDir() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := projectHarness(t, testGate)
			h.reply(runReply{0, "3 passed\n", nil})
			if tc.repo != nil {
				repo = tc.repo(repo)
				h.g.Env = append(h.g.Env, "CLAUDE_PROJECT_DIR="+repo)
			}
			res := h.pre(tc.command(repo), tc.cwd(repo))
			if got := message(t, res, "PreToolUse"); got != passLine(repo) {
				t.Fatalf("message:\n%s\nwant:\n%s", got, passLine(repo))
			}
			if got := h.ranIn(); !reflect.DeepEqual(got, []string{repo}) {
				t.Fatalf("ran in %q, want only %q", got, repo)
			}
		})
	}
}

// A commit in a repository of another project is skipped with a note and takes no slot.
func TestPreSkipsAnotherProjectVisibly(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	foreign := newRepo(t, "foreign", "main")
	hold(t, h.slots, 1)
	hold(t, h.slots, 2)
	res := h.pre("cd "+foreign+" && git commit -m x", t.TempDir())
	want := "commit-test-gate: " + foreign + " skipped — other project (git-common-dir " + foreign + "/.git vs " + repo + "/.git; origin none vs none)"
	if got := message(t, res, "PreToolUse"); got != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got, want)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the test command ran in %q", h.ranIn())
	}
}

// forgeWorktree is a worktree of a separate bare clone of repo, the way forge sets it up: another
// git-common-dir, and the clone's origin set to origin.
func forgeWorktree(t *testing.T, repo, origin string) (worktree, clone string) {
	t.Helper()
	base := filepath.Dir(repo)
	clone = filepath.Join(base, "clone.git")
	gitIn(t, base, "clone", "-q", "--bare", repo, clone)
	gitIn(t, clone, "remote", "set-url", "origin", origin)
	worktree = filepath.Join(base, "work")
	gitIn(t, clone, "worktree", "add", "-q", worktree, "-b", "work")
	gitIn(t, worktree, "switch", "-q", "main")
	return worktree, clone
}

func TestPreTestsAWorktreeOfTheSameOrigin(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	h.reply(runReply{0, "3 passed\n", nil})
	gitIn(t, repo, "remote", "add", "origin", widgetsOrigin)
	work, _ := forgeWorktree(t, repo, widgetsOrigin)
	res := h.pre("git -C "+work+" commit -m x", t.TempDir())
	if got := message(t, res, "PreToolUse"); got != passLine(work) {
		t.Fatalf("message:\n%s\nwant:\n%s", got, passLine(work))
	}
	if got := h.ranIn(); !reflect.DeepEqual(got, []string{work}) {
		t.Fatalf("ran in %q, want only %q", got, work)
	}
}

func TestPreNamesTheCommonDirsAndOriginsWhenSkipping(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	gitIn(t, repo, "remote", "add", "origin", widgetsOrigin)
	work, clone := forgeWorktree(t, repo, gadgetsOrigin)
	res := h.pre("git -C "+work+" commit -m x", t.TempDir())
	want := "commit-test-gate: " + work + " skipped — other project (git-common-dir " + clone + " vs " + repo + "/.git; origin " +
		gadgetsOrigin + " vs " + widgetsOrigin + ")"
	if got := message(t, res, "PreToolUse"); got != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got, want)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the test command ran in %q", h.ranIn())
	}
}

// A git that breaks is not "no repository": nothing was tested, and that blocks.
func TestPreBlocksWhenGitBreaks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern func(repo, second string) string
		cwd     func(repo, second string) string
		reason  func(repo, second string) string
	}{
		{"toplevel", func(string, string) string { return "*--show-toplevel*" }, func(r, _ string) string { return r },
			func(r, _ string) string { return "git could not read '" + r + "': fatal: kapot." }},
		{"common dir of the target", func(_, s string) string { return "'-C " + s + " rev-parse --path-format=absolute --git-common-dir'" },
			func(_, s string) string { return s },
			func(_, s string) string {
				return "git could not determine the common git dir of '" + s + "': fatal: kapot."
			}},
		{"common dir of the project", func(r, _ string) string { return "'-C " + r + " rev-parse --path-format=absolute --git-common-dir'" },
			func(_, s string) string { return s },
			func(r, _ string) string {
				return "git could not determine the common git dir of '" + r + "': fatal: kapot."
			}},
		{"branch", func(string, string) string { return "*'branch --show-current'*" }, func(r, _ string) string { return r },
			func(r, _ string) string { return "git could not read the branch of '" + r + "': fatal: kapot" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := projectHarness(t, testGate)
			second := filepath.Join(filepath.Dir(repo), "second")
			gitIn(t, repo, "worktree", "add", "-q", "-b", "x2", second)
			stubGit(t, tc.pattern(repo, second), "fatal: kapot")
			res := h.pre("git commit -m x", tc.cwd(repo, second))
			blocked(t, res, "commit-test-gate: COULD NOT CHECK — "+tc.reason(repo, second)+"\n\n"+preTail)
			if len(h.calls) != 0 {
				t.Fatalf("the test command ran in %q", h.ranIn())
			}
			if _, ok := markerHeads(t); ok {
				t.Fatal("a marker was recorded")
			}
		})
	}
}

// git speaks the language of the user: without LC_ALL=C its "not a repository" would read as a
// git that breaks.
func TestPreReadsGitInTheCLocale(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	stubbin.RequirePOSIX(t, "$LC_ALL test, case on $* and exec of the real git")
	stub := t.TempDir()
	stubbin.MakeScript(t, stub, "git",
		"if [ \"$LC_ALL\" != C ]; then\n"+
			"  case \"$*\" in *--show-toplevel*) echo 'fatal: kein Git-Repository (oder irgendein Elternverzeichnis): .git' >&2; exit 128 ;; esac\n"+
			"fi\n"+
			"exec '"+real+"' \"$@\"\n")
	stubbin.Prepend(t, stub)
	h, _ := projectHarness(t, testGate)
	plain := t.TempDir()
	res := h.pre("cd "+plain+" && git commit -m x", plain)
	if res.Code != 0 || res.Output != nil || res.Stderr != "" {
		t.Fatalf("got %d %v\n%s, want a silent pass", res.Code, res.Output, res.Stderr)
	}
}

// Any other branch, and a detached HEAD, is recorded for Post instead of tested.
func TestPreRecordsACommitOffTheDefaultBranch(t *testing.T) {
	for _, tc := range []struct {
		name string
		move []string
	}{
		{"feature branch", []string{"checkout", "-q", "-b", "issue-1-x"}},
		{"detached HEAD", []string{"checkout", "-q", "--detach"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := projectHarness(t, testGate)
			gitIn(t, repo, tc.move...)
			res := h.pre("git commit -m x", repo)
			if res.Code != 0 || res.Output != nil || res.Stderr != "" {
				t.Fatalf("got %d %v\n%s, want a silent pass", res.Code, res.Output, res.Stderr)
			}
			if len(h.calls) != 0 {
				t.Fatalf("the test command ran in %q", h.ranIn())
			}
			heads, ok := markerHeads(t)
			if want := (map[string]string{repo: gitIn(t, repo, "rev-parse", "HEAD")}); !ok || !reflect.DeepEqual(heads, want) {
				t.Fatalf("marker heads = %v (%v), want %v", heads, ok, want)
			}
		})
	}
}

func TestPreFollowsTheConfiguredDefaultBranch(t *testing.T) {
	trunk := func(h *harness) {
		write(t, h.toml, minimalTOML+"default_branch = \"trunk\"\n\n"+testGate)
	}
	t.Run("on trunk", func(t *testing.T) {
		h, repo := projectHarness(t, testGate)
		trunk(h)
		h.reply(runReply{0, "3 passed\n", nil})
		gitIn(t, repo, "branch", "-m", "main", "trunk")
		res := h.pre("git commit -m x", repo)
		if got := message(t, res, "PreToolUse"); got != passLine(repo) {
			t.Fatalf("message:\n%s\nwant:\n%s", got, passLine(repo))
		}
		if _, ok := markerHeads(t); ok {
			t.Fatal("a marker was recorded for trunk")
		}
	})
	t.Run("on main", func(t *testing.T) {
		h, repo := projectHarness(t, testGate)
		trunk(h)
		res := h.pre("git commit -m x", repo)
		if res.Code != 0 || res.Output != nil || res.Stderr != "" {
			t.Fatalf("got %d %v\n%s, want a silent pass", res.Code, res.Output, res.Stderr)
		}
		if len(h.calls) != 0 {
			t.Fatalf("the test command ran in %q", h.ranIn())
		}
		if _, ok := markerHeads(t); !ok {
			t.Fatal("main is not the default branch here, so its HEAD should be recorded")
		}
	})
}

func TestPreBlocksOnAnUnreadableDefaultBranch(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	write(t, h.toml, minimalTOML+"default_branch = 3\n\n"+testGate)
	res := h.pre("git commit -m x", repo)
	// test_gate.on is the first key Pre reads, so the error names it; the broken key is the cause.
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the configuration cannot be read (test_gate.on): config: "+h.toml+": key forge.default_branch must be of type str\n\n"+preTail)
	if len(h.calls) != 0 {
		t.Fatalf("the test command ran in %q", h.ranIn())
	}
}

// A background call returns before the commit lands, so Post would see nothing: no marker, but a
// visible note.
func TestPreNotesABackgroundCommit(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	gitIn(t, repo, "checkout", "-q", "-b", "issue-1-x")
	raw := []byte(`{"session_id":"s1","tool_use_id":"t1","tool_input":{"command":"git commit -m x","run_in_background":true}}`)
	res := Pre(context.Background(), h.g, raw, repo)
	want := "commit-test-gate: " + repo + " — commit in a background call is not tested; commit in the foreground."
	if got := message(t, res, "PreToolUse"); got != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got, want)
	}
	if _, ok := markerHeads(t); ok {
		t.Fatal("a marker was recorded for a background call")
	}
}

func TestPreBlocksWhenTheHeadCannotBeRecorded(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	gitIn(t, repo, "checkout", "-q", "-b", "issue-1-x")
	// A file where the state dir must go.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	write(t, blocker, "x")
	t.Setenv("XDG_RUNTIME_DIR", blocker)
	res := h.pre("git commit -m x", repo)
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — could not record HEAD: commit_base: mkdir "+blocker+": not a directory\n\n"+preTail)
}

// A call with a commit to the default branch and one to a feature worktree: the first is tested,
// the second recorded.
func TestPreTestsTheDefaultBranchAndRecordsTheRest(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	h.reply(runReply{0, "3 passed\n", nil})
	feature := filepath.Join(filepath.Dir(repo), "feature")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "issue-1-x", feature)
	res := h.pre("git commit -m a && git -C "+feature+" commit -m b", repo)
	if got := message(t, res, "PreToolUse"); got != passLine(repo) {
		t.Fatalf("message:\n%s\nwant:\n%s", got, passLine(repo))
	}
	if got := h.ranIn(); !reflect.DeepEqual(got, []string{repo}) {
		t.Fatalf("ran in %q, want only %q", got, repo)
	}
	heads, ok := markerHeads(t)
	if want := (map[string]string{feature: gitIn(t, feature, "rev-parse", "HEAD")}); !ok || !reflect.DeepEqual(heads, want) {
		t.Fatalf("marker heads = %v (%v), want %v", heads, ok, want)
	}
}

// fakeRootGit puts a `git` on PATH that prints a messy top level: a doubled and a trailing slash.
func fakeRootGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a sh script")
	}
	bin := t.TempDir()
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\nprintf '/repo/root//\\n'\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestCommitRootsAreNative(t *testing.T) {
	fakeRootGit(t)
	dir := t.TempDir()

	roots, block := (&Gate{}).commitRoots(context.Background(), []gitinvocations.Invocation{{Target: dir}})

	if block != nil {
		t.Fatalf("blocked: %+v", block)
	}
	if want := []string{filepath.Clean("/repo/root")}; !reflect.DeepEqual(roots, want) {
		t.Errorf("roots %q; want %q", roots, want)
	}
}

func TestCreateRootsAreNative(t *testing.T) {
	fakeRootGit(t)
	dir := t.TempDir()

	roots, block := (&Gate{}).createRoots(context.Background(), []prbody.PrCreate{{Cwd: &dir}}, dir)

	if block != nil {
		t.Fatalf("blocked: %+v", block)
	}
	if want := []string{filepath.Clean("/repo/root")}; !reflect.DeepEqual(roots, want) {
		t.Errorf("roots %q; want %q", roots, want)
	}
}
