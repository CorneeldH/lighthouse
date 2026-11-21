//go:build !windows

package bashwrites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// End-to-end scenarios of bash-write-check: Pre, a real `bash -c` call and Post against a real git
// repo, with stub gates in a lighthouse root of their own (the gates of pack/shell and pack/python
// come with their packs, so the stubs report one fixed finding and then quote the file, the way
// shellcheck and ruff quote source lines). They are the twins of the _Session tests in the former
// tests/pack/test_bash_write_check.py.

const (
	cleanSh = "#!/usr/bin/env bash\nset -euo pipefail\necho \"hallo\"\n"
	// SC2034: `x` is never used. The stub blocks on `echo "hoi"`.
	brokenSh = "#!/usr/bin/env bash\nf() {\n  local x=1\n  echo \"hoi\"\n}\nf\n"
	// Clean, and as long as brokenSh: `"x$x"` has as many characters as `"hoi"`, so a `cp -p` over
	// it does not change the size.
	cleanSameSizeSh = "#!/usr/bin/env bash\nf() {\n  local x=1\n  echo \"x$x\"\n}\nf\n"
	bareExceptPy    = "try:\n    pass\nexcept:\n    pass\n"
)

// scenarioShellStub reads the payload like the real gate. The first SHELLCHECK_BIN that is not a
// file is a gate that cannot look; GATE_SLEEP slows the gate down so two posts overlap.
const scenarioShellStub = `#!/bin/sh
f=$(sed -n 's/.*"file_path":"\([^"]*\)".*/\1/p')
[ -f "$f" ] || exit 0
[ -z "${GATE_SLEEP:-}" ] || sleep "$GATE_SLEEP"
if grep -q 'echo "hoi"' "$f"; then
  echo "$f:3: SC2034 (stub): x appears unused." >&2
  cat "$f" >&2
  exit 2
fi
exit 0
`

const scenarioRuffStub = `#!/bin/sh
f=$(sed -n 's/.*"file_path":"\([^"]*\)".*/\1/p')
[ -f "$f" ] || exit 0
if grep -q '^except:' "$f"; then
  echo "$f:3:1: E722 (stub) Do not use bare except" >&2
  exit 2
fi
exit 0
`

// scenarioRunSh stands in for both run.sh gates; the link check blocks on a dead link target.
const scenarioRunSh = `#!/bin/sh
f=$(sed -n 's/.*"file_path":"\([^"]*\)".*/\1/p')
if [ "$1" = md-link-check ] && grep -q 'nergens.md' "$f"; then
  echo "Linkcheck: markdown link in $f: nergens.md does not exist" >&2
  exit 2
fi
exit 0
`

func heredoc(name, text string) string { return "cat > " + name + " <<'EOF'\n" + text + "EOF\n" }

// shFinding is the bundle part of one finding of the shell stub on brokenSh.
func shFinding(label, abs string) string {
	return "== " + label + " (shell-lint-check.sh) ==\n" + abs + ":3: SC2034 (stub): x appears unused.\n" +
		strings.TrimSuffix(brokenSh, "\n")
}

// pyFinding is the bundle part of one finding of the ruff stub on bareExceptPy.
func pyFinding(label, abs string) string {
	return "== " + label + " (ruff-format-check.sh) ==\n" + abs + ":3:1: E722 (stub) Do not use bare except"
}

type sess struct {
	t                    *testing.T
	root, repo, xdg, tmp string
	session              string
	calls                int
}

// newSess is a fresh repo (with one commit, or unborn), a lighthouse root with the stub gates and
// a clean environment: nothing of a controller run or of the host's git leaks in.
func newSess(t *testing.T, unborn bool) *sess {
	t.Helper()
	for _, key := range []string{"LIGHTHOUSE_CONTROLLER_RUN", "LIGHTHOUSE_REPO_ROOT", "LIGHTHOUSE_TOML",
		"LIGHTHOUSE_BASH_GATES_DEADLINE", "LIGHTHOUSE_BASH_GATES_START_NS", "SHELLCHECK_BIN", "GATE_SLEEP"} {
		t.Setenv(key, "")
	}
	for _, key := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	step := map[string]any{"repo": "repo", "branch": "main"}
	if unborn {
		step = map[string]any{"unborn": "repo"}
	}
	fixturesetup.Build(t, tmp, []map[string]any{step})
	s := &sess{t: t, tmp: tmp, repo: filepath.Join(tmp, "repo"), root: gateRoot(t),
		xdg: filepath.Join(tmp, "xdg"), session: "sess-scenario"}
	for name, stub := range map[string]string{
		"shell-lint-check.sh": scenarioShellStub, "ruff-format-check.sh": scenarioRuffStub, "run.sh": scenarioRunSh,
	} {
		writeFile(t, filepath.Join(s.root, hookDir, name), stub)
		if err := os.Chmod(filepath.Join(s.root, hookDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_PROJECT_DIR", s.repo)
	t.Setenv("XDG_RUNTIME_DIR", s.xdg)
	return s
}

func (s *sess) git(args ...string) { s.t.Helper(); fixturesetup.Git(s.t, s.repo, args...) }

func (s *sess) write(name, text string) {
	s.t.Helper()
	writeFile(s.t, filepath.Join(s.repo, name), text)
}

func (s *sess) commitAll(message string) {
	s.t.Helper()
	s.git("add", "-A")
	s.git("commit", "-qm", message)
}

func (s *sess) state() string {
	s.t.Helper()
	state, err := StateDir(s.session)
	if err != nil {
		s.t.Fatal(err)
	}
	return state
}

func (s *sess) ledger() *Ledger { return readLedger(filepath.Join(s.state(), "ledger.json")) }

func (s *sess) record(root string) *record {
	s.t.Helper()
	r := validRecord(s.ledger().Roots[root])
	if r == nil {
		s.t.Fatalf("no ledger row for %s", root)
	}
	return r
}

// worktree is `git worktree add` outside any Bash call; it returns the path.
func (s *sess) worktree(name string, args ...string) string {
	s.t.Helper()
	path := filepath.Join(s.tmp, name)
	s.git(append(append([]string{"worktree", "add", "-q"}, args...), path)...)
	return path
}

func (s *sess) payload(cmd, toolUse, cwd, event string) []byte {
	raw, err := json.Marshal(map[string]any{
		"session_id": s.session, "tool_use_id": toolUse, "hook_event_name": event, "tool_name": "Bash",
		"cwd": cwd, "tool_input": map[string]any{"command": cmd},
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return raw
}

type callOpts struct {
	cwd     string
	env     map[string]string
	event   string
	noPost  bool
	noBash  bool // the command is not run: the test does the write itself in between
	between func()
}

// setenv sets extra and returns the undo.
func setenv(t *testing.T, extra map[string]string) func() {
	t.Helper()
	type old struct {
		value string
		set   bool
	}
	saved := map[string]old{}
	for key, value := range extra {
		v, ok := os.LookupEnv(key)
		saved[key] = old{v, ok}
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		for key, o := range saved {
			if o.set {
				_ = os.Setenv(key, o.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
}

func (s *sess) toolUse() string {
	s.calls++
	return "toolu_" + strconv.Itoa(s.calls)
}

// pre runs the Pre hook for cmd and fails the test on anything but exit 0.
func (s *sess) pre(cmd, toolUse, cwd string) {
	s.t.Helper()
	eq(s.t, "pre exit code", Pre(s.payload(cmd, toolUse, cwd, "PreToolUse"), time.Now), 0)
}

// bash runs cmd in cwd, as the Bash tool would; its own exit code does not matter.
func (s *sess) bash(cmd, cwd string) {
	s.t.Helper()
	c := exec.Command("bash", "-c", cmd)
	c.Dir = cwd
	_ = c.Run()
}

// call is pre, the Bash call, between() and post, with one tool_use_id.
func (s *sess) call(cmd string, o callOpts) postResult {
	s.t.Helper()
	if o.cwd == "" {
		o.cwd = s.repo
	}
	if o.event == "" {
		o.event = "PostToolUse"
	}
	defer setenv(s.t, o.env)()
	toolUse := s.toolUse()
	s.pre(cmd, toolUse, o.cwd)
	if !o.noBash {
		s.bash(cmd, o.cwd)
	}
	if o.between != nil {
		o.between()
	}
	if o.noPost {
		return postResult{}
	}
	code, output, stderr := Post(s.payload(cmd, toolUse, o.cwd, o.event), s.root)
	return postResult{code, output, stderr}
}

func (s *sess) run(cmd string) postResult { s.t.Helper(); return s.call(cmd, callOpts{}) }

// ctx is the additionalContext of a result, "" without one.
func ctx(r postResult) string {
	hso, _ := r.output["hookSpecificOutput"].(map[string]any)
	text, _ := hso["additionalContext"].(string)
	return text
}

func eventOf(r postResult) string {
	hso, _ := r.output["hookSpecificOutput"].(map[string]any)
	text, _ := hso["hookEventName"].(string)
	return text
}

// blocked asserts exit 2 with exactly want as the bundle, on stderr and as the context.
func blocked(t *testing.T, r postResult, want string) {
	t.Helper()
	eq(t, "exit code", r.code, 2)
	eq(t, "stderr", r.stderr, want+"\n")
	eq(t, "context", ctx(r), want)
}

// quiet asserts a pass without a word.
func quiet(t *testing.T, r postResult) {
	t.Helper()
	eq(t, "result", r, postResult{})
}

func (s *sess) dropMarkers() {
	s.t.Helper()
	files, _ := filepath.Glob(filepath.Join(s.state(), "*.json"))
	dropped := 0
	for _, f := range files {
		if filepath.Base(f) != "ledger.json" {
			if err := os.Remove(f); err != nil {
				s.t.Fatal(err)
			}
			dropped++
		}
	}
	if dropped == 0 {
		s.t.Fatal("pre wrote no marker")
	}
}

func (s *sess) dropLedger() {
	s.t.Helper()
	if err := os.Remove(filepath.Join(s.state(), "ledger.json")); err != nil {
		s.t.Fatal(err)
	}
}

func rev(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// --- source 1: what is dirty ---------------------------------------------------------------

func TestScenarioFileDirtyBeforeTheFirstCallIsBaseline(t *testing.T) {
	s := newSess(t, false)
	s.write("x.sh", brokenSh)
	time.Sleep(50 * time.Millisecond)
	quiet(t, s.run("true"))
}

func TestScenarioFailingCommandStillChecks(t *testing.T) {
	s := newSess(t, false)
	r := s.call(heredoc("x.sh", brokenSh)+"false\n", callOpts{event: "PostToolUseFailure"})
	blocked(t, r, shFinding("x.sh", s.repo+"/x.sh"))
	eq(t, "event", eventOf(r), "PostToolUseFailure")
}

func TestScenarioCallWithoutPostIsCaughtNextTime(t *testing.T) {
	s := newSess(t, false)
	s.call(heredoc("x.sh", brokenSh), callOpts{noPost: true})
	blocked(t, s.run("true"), shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioSameBadFileIsReportedOnce(t *testing.T) {
	s := newSess(t, false)
	blocked(t, s.run(heredoc("x.sh", brokenSh)), shFinding("x.sh", s.repo+"/x.sh"))
	quiet(t, s.run("true"))
	quiet(t, s.run("true"))
}

func TestScenarioCpPOverDirtyFileOfTheSameSize(t *testing.T) {
	s := newSess(t, false)
	eq(t, "size", len(cleanSameSizeSh), len(brokenSh))
	s.write("a.sh", cleanSameSizeSh)
	src := filepath.Join(s.tmp, "broken-src.sh")
	writeFile(t, src, brokenSh)
	info, err := os.Stat(filepath.Join(s.repo, "a.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Same mtime AND size as the baseline: only ctime gives the copy away.
	if err := os.Chtimes(src, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	quiet(t, s.run("true"))
	blocked(t, s.run("cp -p "+src+" a.sh"), shFinding("a.sh", s.repo+"/a.sh"))
}

func TestScenarioSpaceInPath(t *testing.T) {
	s := newSess(t, false)
	blocked(t, s.run(heredoc("'my x.sh'", brokenSh)), shFinding("my x.sh", s.repo+"/my x.sh"))
}

func TestScenarioLatePreDoesNotBaselineTheWrite(t *testing.T) {
	s := newSess(t, false)
	start := time.Now().UnixNano()
	// A coarse ctime clock can lag a fraction behind: without this pause the file can get a
	// ctime just before start.
	time.Sleep(50 * time.Millisecond)
	s.write("x.sh", brokenSh)
	r := s.call("true", callOpts{env: map[string]string{"LIGHTHOUSE_BASH_GATES_START_NS": strconv.FormatInt(start, 10)}})
	blocked(t, r, shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioBrokenIndexIsLoud(t *testing.T) {
	s := newSess(t, false)
	r := s.run("echo garbage > .git/index")
	eq(t, "exit code", r.code, 2)
	if !strings.HasPrefix(r.stderr, "bash-write-check: COULD NOT CHECK: "+s.repo+" (") {
		t.Fatalf("stderr = %q", r.stderr)
	}
	eq(t, "context", ctx(r)+"\n", r.stderr)
}

func TestScenarioUnreadableManifestIsLoud(t *testing.T) {
	// Without a readable manifest the dispatcher cannot tell lighthouse's files from the
	// project's: running the gates could rewrite package code, skipping them is blind.
	s := newSess(t, false)
	r := s.run("mkdir -p .lighthouse && " + heredoc(".lighthouse/manifest.toml", "[[unit]\n") + heredoc("own.py", bareExceptPy))
	eq(t, "exit code", r.code, 2)
	_, wantErr := ManagedFiles(s.repo)
	if wantErr == nil {
		t.Fatal("the manifest reads")
	}
	eq(t, "stderr", r.stderr, "bash-write-check: COULD NOT CHECK: "+s.repo+" ("+wantErr.Error()+")\n")
	eq(t, "context", ctx(r)+"\n", r.stderr)
}

func TestScenarioChangedManagedFileStillGoesToItsGate(t *testing.T) {
	// Only the bytes `init` recorded are lighthouse's: a managed file that differs from its sha256
	// is checked as usual.
	s := newSess(t, false)
	sum := sha256.Sum256([]byte("x = 1\n"))
	manifest := "[[unit]]\nkind = \"file\"\npath = \".lighthouse/scripts/managed.py\"\npackage = \"python\"\n" +
		"version = \"dev\"\nsha256 = \"" + hex.EncodeToString(sum[:]) + "\"\n"
	r := s.run("mkdir -p .lighthouse/scripts && " + heredoc(".lighthouse/manifest.toml", manifest) +
		heredoc(".lighthouse/scripts/managed.py", bareExceptPy))
	blocked(t, r, pyFinding(".lighthouse/scripts/managed.py", s.repo+"/.lighthouse/scripts/managed.py"))
}

func TestScenarioTwoPostsOfOneCallReportOnce(t *testing.T) {
	s := newSess(t, false)
	cmd := heredoc("x.sh", brokenSh)
	ids := []string{"toolu_para_a", "toolu_para_b"}
	for _, id := range ids {
		s.pre(cmd, id, s.repo)
	}
	s.bash(cmd, s.repo)
	// The gate takes half a second: the second post meets the claim of the first.
	defer setenv(t, map[string]string{"GATE_SLEEP": "0.5"})()
	results := make([]postResult, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, output, stderr := Post(s.payload(cmd, id, s.repo, "PostToolUse"), s.root)
			results[i] = postResult{code, output, stderr}
		}()
	}
	wg.Wait()
	var stderrs []string
	var codes []int
	for _, r := range results {
		codes = append(codes, r.code)
		stderrs = append(stderrs, r.stderr)
	}
	sort.Ints(codes)
	sort.Strings(stderrs)
	eq(t, "codes", codes, []int{0, 2})
	eq(t, "stderrs", stderrs, []string{"", shFinding("x.sh", s.repo+"/x.sh") + "\n"})
}

func TestScenarioSymlinkedCwdIsOneRoot(t *testing.T) {
	s := newSess(t, false)
	link := filepath.Join(s.tmp, "link")
	if err := os.Symlink(s.repo, link); err != nil {
		t.Fatal(err)
	}
	r := s.call(heredoc("x.sh", brokenSh), callOpts{cwd: link})
	blocked(t, r, shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioRelativeWriteFromASubdir(t *testing.T) {
	s := newSess(t, false)
	if err := os.Mkdir(filepath.Join(s.repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := s.call(heredoc("y.sh", brokenSh), callOpts{cwd: filepath.Join(s.repo, "sub")})
	blocked(t, r, shFinding("sub/y.sh", s.repo+"/sub/y.sh"))
}

func TestScenarioCallOutsideAnyRepoIsSilent(t *testing.T) {
	// A bare `.git/` without HEAD above the cwd (on the Mac: `~/.git/` with only a tool cache)
	// made a root of every directory under `~`; git said "not a git repository" and post blocked
	// with COULD NOT CHECK on an `ls` outside any repo, where there is nothing to guard.
	for _, via := range []string{"cwd", "path"} {
		t.Run(via, func(t *testing.T) {
			s := newSess(t, false)
			home := filepath.Join(s.tmp, "home")
			sub := filepath.Join(home, ".claude", "skills")
			if err := os.MkdirAll(filepath.Join(home, ".git", "workflow-audit-tools"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if via == "cwd" {
				quiet(t, s.call("ls", callOpts{cwd: sub}))
			} else {
				quiet(t, s.run("ls "+sub))
			}
		})
	}
}

// --- source 2: what was committed in the call -----------------------------------------------

func TestScenarioWriteAndCommitInOneCall(t *testing.T) {
	s := newSess(t, false)
	blocked(t, s.run(heredoc("x.sh", brokenSh)+"git add x.sh && git commit -qm x\n"), shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioCommitWithoutPostIsCaughtNextTime(t *testing.T) {
	s := newSess(t, false)
	s.call(heredoc("x.sh", brokenSh)+"git add x.sh && git commit -qm x\n", callOpts{noPost: true})
	blocked(t, s.run("true"), shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioGitRmAndGitMvAreNotUnchecked(t *testing.T) {
	s := newSess(t, false)
	s.write("a.sh", cleanSh)
	s.write("b.sh", cleanSh)
	s.commitAll("a en b")
	quiet(t, s.run("git rm -q a.sh && git commit -qm rm"))
	quiet(t, s.run("git mv b.sh c.sh"))
}

func TestScenarioCommitThenMvThenCommit(t *testing.T) {
	s := newSess(t, false)
	s.write("a.py", "X = 1\n")
	s.commitAll("a")
	quiet(t, s.run("printf 'X = 2\\n' > a.py && git commit -qam wip && git mv a.py b.py && git commit -qm mv"))
	quiet(t, s.run("true"))
}

func TestScenarioCommitThenRm(t *testing.T) {
	s := newSess(t, false)
	quiet(t, s.run(heredoc("x.sh", brokenSh)+"git add x.sh && git commit -qm x && rm x.sh\n"))
	quiet(t, s.run("true"))
}

func TestScenarioConflictMergeChecksOnlyTheResolution(t *testing.T) {
	cases := []struct {
		name, resolution string
		want             string
	}{
		{"clean", "opgelost\n", ""},
		{"dead link", "[x](nergens.md)\n", "== c.md (md-link-check.sh) ==\nLinkcheck: markdown link in {repo}/c.md: nergens.md does not exist"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSess(t, false)
			s.write("c.md", "basis\n")
			s.commitAll("c")
			s.git("checkout", "-q", "-b", "side")
			s.write("s.sh", brokenSh)
			s.write("t.py", bareExceptPy)
			s.write("c.md", "side\n")
			s.commitAll("side")
			s.git("checkout", "-q", "main")
			s.write("c.md", "main\n")
			s.commitAll("main")
			pastThisSecond()
			quiet(t, s.run("true"))
			r := s.run("git merge -q side; printf '" + c.resolution + "' > c.md && git add c.md && git commit -qm merge")
			eq(t, "parents", len(strings.Fields(rev(t, s.repo, "log", "-1", "--format=%P"))), 2)
			if c.want == "" {
				quiet(t, r)
				return
			}
			// The combined diff names the conflict resolution: only c.md goes through a gate.
			blocked(t, r, strings.ReplaceAll(c.want, "{repo}", s.repo))
		})
	}
}

func TestScenarioCommittedPathMissingOnDiskIsUnchecked(t *testing.T) {
	// skip-worktree hides the deletion from `git status`: in HEAD, not on disk, not deleted in
	// source 1 — all three gates would give exit 0.
	s := newSess(t, false)
	r := s.run(heredoc("x.sh", brokenSh) + "git add x.sh && git commit -qm x" +
		" && git update-index --skip-worktree x.sh && rm x.sh\n")
	blocked(t, r, "bash-write-check: COULD NOT CHECK: "+s.repo+"/x.sh (in HEAD, not on disk)")
	eq(t, "pending", s.record(s.repo).pendingStrings(), []string{"x.sh"})
}

func TestScenarioCheckoutMovesHeadWithoutChecking(t *testing.T) {
	s := newSess(t, false)
	s.git("checkout", "-q", "-b", "side")
	// Both wrong, and the .py also unformatted: a dispatch would rewrite it, so byte-equal proves
	// that nothing ran.
	s.write("t.py", "x=1\n"+bareExceptPy)
	s.write("s.sh", brokenSh)
	s.commitAll("side")
	s.git("checkout", "-q", "main")
	pastThisSecond()
	quiet(t, s.run("true"))
	r := s.run("git checkout -q side")
	eq(t, "code", r.code, 0)
	eq(t, "stderr", r.stderr, "")
	got, err := os.ReadFile(filepath.Join(s.repo, "t.py"))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "t.py", string(got), "x=1\n"+bareExceptPy)
	got, err = os.ReadFile(filepath.Join(s.repo, "s.sh"))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "s.sh", string(got), brokenSh)
	eq(t, "context", ctx(r), "bash-write-check: "+s.repo+": HEAD moved without a commit (checkout: moving from main to side)")
}

func TestScenarioResetSoftMakesItDirty(t *testing.T) {
	// Source 1 already catches this: after `reset --soft` x.sh is dirty in the index.
	s := newSess(t, false)
	s.write("x.sh", brokenSh)
	s.commitAll("x")
	note := "bash-write-check: " + s.repo + ": HEAD moved without a commit (reset: moving to HEAD~1)"
	blocked(t, s.run("git reset -q --soft HEAD~1"), note+"\n\n"+shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioFirstCommitInAnUnbornRepo(t *testing.T) {
	s := newSess(t, true)
	quiet(t, s.run("true"))
	blocked(t, s.run(heredoc("x.sh", brokenSh)+"git add x.sh && git commit -qm x\n"), shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioOrphanCheckoutIsNotUnchecked(t *testing.T) {
	// HEAD becomes unborn while the ledger has a sha: `git reflog` would give rc 128 there, so
	// source 2 must not even call it.
	s := newSess(t, false)
	quiet(t, s.run("git checkout -q --orphan wees"))
}

func TestScenarioCommitThenStagedMvIsNotUnchecked(t *testing.T) {
	s := newSess(t, false)
	s.write("a.sh", cleanSh)
	s.commitAll("a")
	quiet(t, s.run(heredoc("a.sh", cleanSameSizeSh)+"git commit -qam wip && git mv a.sh b.sh\n"))
	quiet(t, s.run("true"))
	eq(t, "pending", s.record(s.repo).pendingStrings(), []string{})
}

func TestScenarioCommitThenSwitchBackIsNotSilent(t *testing.T) {
	// HEAD ends on the ledger `head` again: the stop walk sees nothing, the walk since the start of
	// the call finds the commit on `feat`. bad.sh is then not on disk and not in HEAD, but not
	// deleted: it cannot be looked at, so loud. Back on `feat` it is still in `pending` and goes
	// through the gate after all.
	s := newSess(t, false)
	quiet(t, s.run("true"))
	r := s.run("git switch -q -c feat && " + heredoc("bad.sh", brokenSh) +
		"git add bad.sh && git commit -qm x && git switch -q main\n")
	feat := rev(t, s.repo, "rev-parse", "feat")[:12]
	blocked(t, r, "bash-write-check: COULD NOT CHECK: "+s.repo+"/bad.sh (committed in "+feat+", not on disk and not in HEAD)")
	note := "bash-write-check: " + s.repo + ": HEAD moved without a commit (checkout: moving from main to feat)"
	blocked(t, s.run("git switch -q feat"), note+"\n\n"+shFinding("bad.sh", s.repo+"/bad.sh"))
}

func TestScenarioCommitThenDiscardIsNotUnchecked(t *testing.T) {
	// The call throws away its own commit again: the reflog still knows it, but no ref contains
	// it. bad.sh is then not committed elsewhere but gone.
	for name, discard := range map[string]string{
		"reset hard":     "git reset -q --hard HEAD~1",
		"amend after rm": "git rm -q bad.sh && git commit -q --amend --allow-empty -m x",
		"rebase drop":    "git rebase -q --onto HEAD~1 HEAD",
	} {
		t.Run(name, func(t *testing.T) {
			s := newSess(t, false)
			quiet(t, s.run("true"))
			r := s.run(heredoc("bad.sh", brokenSh) + "git add bad.sh && git commit -qm x && " + discard + "\n")
			if _, err := os.Stat(filepath.Join(s.repo, "bad.sh")); !os.IsNotExist(err) {
				t.Fatalf("bad.sh exists: %v", err)
			}
			quiet(t, r)
			eq(t, "pending", s.record(s.repo).pendingStrings(), []string{})
		})
	}
}

func TestScenarioCommitThenFastForwardMergeBackIsStillChecked(t *testing.T) {
	// The stop walk stops at the return to `main` (sha == ledger `head`) before the commit; the
	// fast-forward brings bad.sh onto main anyway.
	s := newSess(t, false)
	quiet(t, s.run("true"))
	r := s.run("git switch -q -c tmp && " + heredoc("bad.sh", brokenSh) +
		"git add bad.sh && git commit -qm x && git switch -q main && git merge -q --ff-only tmp\n")
	if _, err := os.Stat(filepath.Join(s.repo, "bad.sh")); err != nil {
		t.Fatal(err)
	}
	note := "bash-write-check: " + s.repo + ": HEAD moved without a commit (merge tmp: Fast-forward)"
	blocked(t, r, note+"\n\n"+shFinding("bad.sh", s.repo+"/bad.sh"))
}

// --- several roots and the fallback ------------------------------------------------------------

func TestScenarioCdIntoASecondWorktree(t *testing.T) {
	s := newSess(t, false)
	wt2 := s.worktree("wt2", "-b", "b2")
	// Two roots in one post: the label is the absolute path.
	blocked(t, s.run("cd "+wt2+" && "+heredoc("x.sh", brokenSh)), shFinding(wt2+"/x.sh", wt2+"/x.sh"))
}

func TestScenarioForeignRepoIsNotARoot(t *testing.T) {
	s := newSess(t, false)
	fixturesetup.Build(t, s.tmp, []map[string]any{{"repo": "los", "branch": "main"}})
	los := filepath.Join(s.tmp, "los")
	quiet(t, s.run("cd "+los+" && "+heredoc("x.sh", brokenSh)))
	if _, ok := s.ledger().Roots[los]; ok {
		t.Fatal("the foreign repo is in the ledger")
	}
}

func TestScenarioWorktreeAddThenCommitInside(t *testing.T) {
	s := newSess(t, false)
	r := s.run("git worktree add -q wt -b b && cd wt && " + heredoc("y.sh", brokenSh) + "git add y.sh && git commit -qm y\n")
	wt := s.repo + "/wt"
	blocked(t, r, shFinding(wt+"/y.sh", wt+"/y.sh"))
	eq(t, "head", headText(t, s.record(wt)), rev(t, wt, "rev-parse", "HEAD"))
}

func TestScenarioRecreatedWorktreeReflogGapStillWalksSinceTheCall(t *testing.T) {
	// The ledger still knows `wt` with the HEAD of the deleted worktree; the new worktree has a
	// fresh reflog without that sha (reflog gap). The walk since the start of the call finds the
	// commit anyway.
	s := newSess(t, false)
	quiet(t, s.run("git worktree add -q wt -b b1 && cd wt && echo a > a.txt && git add a.txt && git commit -qm a"))
	wt := s.repo + "/wt"
	eq(t, "in the worktree", s.call("true", callOpts{cwd: wt}).code, 0)
	old := headText(t, s.record(wt))
	eq(t, "remove", s.run("git worktree remove --force wt").code, 0)
	eq(t, "commit", s.run("echo m > m.txt && git add m.txt && git commit -qm m").code, 0)
	r := s.run("git worktree add -q wt -b b2 && cd wt && " + heredoc("y.sh", brokenSh) + "git add y.sh && git commit -qm y\n")
	blocked(t, r, "bash-write-check: "+wt+": reflog gap: ledger HEAD "+old+" not found\n\n"+shFinding(wt+"/y.sh", wt+"/y.sh"))
}

func TestScenarioRelativePathIntoAGitignoredNestedWorktree(t *testing.T) {
	s := newSess(t, false)
	s.write(".gitignore", "wt/\n")
	s.commitAll("ignore wt")
	wt := s.worktree("repo/wt")
	blocked(t, s.run(heredoc("wt/x.sh", brokenSh)), shFinding(wt+"/x.sh", wt+"/x.sh"))
}

func TestScenarioOpenQuoteWithAnAbsolutePath(t *testing.T) {
	s := newSess(t, false)
	wt2 := s.worktree("wt2", "-b", "b2")
	// Bash would refuse this whole line (open quote): pre and post by hand, the write itself here.
	cmd := "cat > " + wt2 + "/x.sh; echo 'open"
	r := s.call(cmd, callOpts{noBash: true, between: func() { writeFile(t, wt2+"/x.sh", brokenSh) }})
	blocked(t, r, shFinding(wt2+"/x.sh", wt2+"/x.sh"))
}

func TestScenarioMarkerGoneBeforePost(t *testing.T) {
	s := newSess(t, false)
	r := s.call(heredoc("x.sh", brokenSh), callOpts{between: s.dropMarkers})
	blocked(t, r, shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioLedgerGoneBeforePost(t *testing.T) {
	s := newSess(t, false)
	r := s.call(heredoc("x.sh", brokenSh), callOpts{between: s.dropLedger})
	blocked(t, r, "bash-write-check: fallback without ledger for "+s.repo+"\n\n"+shFinding("x.sh", s.repo+"/x.sh"))
}

func TestScenarioLedgerGoneCommitInTheCallIsFound(t *testing.T) {
	// Source 2 via reflog time: the commit usually falls in the same second as the start of pre —
	// `since` rounds down, otherwise it dropped out.
	s := newSess(t, false)
	r := s.call(heredoc("x.sh", brokenSh)+"git add x.sh && git commit -qm x\n", callOpts{between: s.dropLedger})
	blocked(t, r, "bash-write-check: fallback without ledger for "+s.repo+"\n\n"+shFinding("x.sh", s.repo+"/x.sh"))
	// The root is in the ledger again: the next call does not fall back.
	quiet(t, s.run("true"))
}

func TestScenarioNoteOnExitZeroNamesTheFailureEvent(t *testing.T) {
	s := newSess(t, false)
	r := s.call(heredoc("x.sh", cleanSh), callOpts{between: s.dropLedger, event: "PostToolUseFailure"})
	eq(t, "code", r.code, 0)
	eq(t, "stderr", r.stderr, "")
	eq(t, "event", eventOf(r), "PostToolUseFailure")
	eq(t, "context", ctx(r), "bash-write-check: fallback without ledger for "+s.repo)
}

func TestScenarioUnreadableLedgerFallsBackAndIsReplaced(t *testing.T) {
	s := newSess(t, false)
	garble := func() { writeFile(t, filepath.Join(s.state(), "ledger.json"), "{kapot") }
	r := s.call(heredoc("x.sh", brokenSh), callOpts{between: garble})
	blocked(t, r, "bash-write-check: ledger unreadable; fallback for every root\n\n"+
		"bash-write-check: fallback without ledger for "+s.repo+"\n\n"+
		"bash-write-check: unreadable ledger replaced; a root this call did not touch falls back at its next call\n\n"+
		shFinding("x.sh", s.repo+"/x.sh"))
	eq(t, "head", headText(t, s.record(s.repo)), rev(t, s.repo, "rev-parse", "HEAD"))
	if _, err := os.Stat(filepath.Join(s.state(), "ledger.reset")); err != nil {
		t.Fatal(err)
	}
	quiet(t, s.run("true"))
}

func TestScenarioReplacedLedgerDoesNotLetPreBaselineAnUnwalkedRoot(t *testing.T) {
	// wt2 is in the ledger and gets a wrong file outside a call (a background command); the
	// ledger breaks; a call in only the main repo replaces it. The next pre must then not seed
	// wt2 — otherwise y.sh silently becomes baseline.
	s := newSess(t, false)
	wt2 := s.worktree("wt2", "-b", "b2")
	quiet(t, s.run("cd "+wt2+" && true"))
	if _, ok := s.ledger().Roots[wt2]; !ok {
		t.Fatal("wt2 is not in the ledger")
	}
	writeFile(t, wt2+"/y.sh", brokenSh)
	writeFile(t, filepath.Join(s.state(), "ledger.json"), "{kapot")
	main := s.run("true")
	eq(t, "main only", main.code, 0)
	if _, ok := s.ledger().Roots[wt2]; ok {
		t.Fatal("wt2 is still in the replaced ledger")
	}
	r := s.run("cd " + wt2 + " && true")
	blocked(t, r, "bash-write-check: fallback without ledger for "+wt2+"\n\n"+shFinding(wt2+"/y.sh", wt2+"/y.sh"))
}

func TestScenarioNoMarkerAndNoLedgerSkipsSourceTwoWithANote(t *testing.T) {
	s := newSess(t, false)
	both := func() { s.dropMarkers(); s.dropLedger() }
	r := s.call(heredoc("x.sh", brokenSh)+"git add x.sh && git commit -qm x\n", callOpts{between: both})
	eq(t, "code", r.code, 0)
	eq(t, "stderr", r.stderr, "")
	// Without a marker the root demonstrably did not exist before pre: no fallback notice.
	eq(t, "context", ctx(r), "bash-write-check: "+s.repo+": source 2 skipped: no ledger HEAD and no marker with a start time to walk back to")
}

func TestScenarioRemovedWorktreeIsNamedNotSilent(t *testing.T) {
	s := newSess(t, false)
	wt2 := s.worktree("wt2", "-b", "b2")
	r := s.run("cd " + wt2 + " && " + heredoc("x.sh", brokenSh) + "git add x.sh && git commit -qm x && cd " + s.repo +
		" && git worktree remove --force " + wt2 + "\n")
	if _, err := os.Stat(wt2); !os.IsNotExist(err) {
		t.Fatalf("wt2 exists: %v", err)
	}
	eq(t, "code", r.code, 0)
	eq(t, "stderr", r.stderr, "")
	eq(t, "context", ctx(r), "bash-write-check: "+wt2+" no longer exists; not checked")
}

func TestScenarioTwoRootsWithTheSameRelPathGetDistinctHeaders(t *testing.T) {
	s := newSess(t, false)
	wt2 := s.worktree("wt2", "-b", "b2")
	r := s.run(heredoc("x.sh", brokenSh) + heredoc(wt2+"/x.sh", brokenSh))
	blocked(t, r, shFinding(s.repo+"/x.sh", s.repo+"/x.sh")+"\n\n"+shFinding(wt2+"/x.sh", wt2+"/x.sh"))
}

func TestScenarioOutsideAnyRepoIsSilentAndGitDoesNotFail(t *testing.T) {
	s := newSess(t, false)
	plain := filepath.Join(s.tmp, "plain")
	shim := filepath.Join(s.tmp, "shim")
	for _, dir := range []string{plain, shim} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	failures := filepath.Join(s.tmp, "git-failures.log")
	calls := filepath.Join(s.tmp, "git-calls.log")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(shim, "git"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+calls+"'\n'"+realGit+"' \"$@\"\nrc=$?\n"+
		"[ \"$rc\" -eq 0 ] || printf '%s %s\\n' \"$rc\" \"$*\" >> '"+failures+"'\nexit \"$rc\"\n")
	if err := os.Chmod(filepath.Join(shim, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := shim + string(os.PathListSeparator) + os.Getenv("PATH")
	quiet(t, s.call(heredoc("x.sh", brokenSh), callOpts{cwd: plain, env: map[string]string{"PATH": path}}))
	// The shim really was on the path: pre and post called git, none of them failed.
	logged, err := os.ReadFile(calls)
	if err != nil || !strings.Contains(string(logged), "rev-parse") {
		t.Fatalf("calls = %q, %v", logged, err)
	}
	if failed, err := os.ReadFile(failures); err == nil {
		t.Fatalf("git failed: %s", failed)
	}
}
