//go:build !windows

package evidence

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/prbody"
)

// The run_row tests are the Go twins of tests/pack/test_evidence.py (run_row), with the same
// asserts; the CLI tests there stay pytest and call `lighthouse evidence`.

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// withConfig writes a lighthouse.toml with extra appended and points the config at it.
func withConfig(t *testing.T, extra string) {
	t.Helper()
	useHeadings(t)
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	writeFile(t, path, "[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"+extra, 0o644)
	t.Setenv("LIGHTHOUSE_TOML", path)
}

// stubBin is a directory with one executable script per name, put first on PATH.
func stubBin(t *testing.T, stubs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, script := range stubs {
		writeFile(t, filepath.Join(dir, name), script, 0o755)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func dirWithF(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f"), "hallo wereld\n", 0o644)
	return dir
}

func runOne(t *testing.T, command, result, cwd string, ghOK bool, timeout time.Duration) (string, string) {
	t.Helper()
	return RunRow(prbody.Row{AC: "1", Command: command, Result: result}, cwd, ghOK, timeout)
}

func want(t *testing.T, gotOutcome, gotDetail, outcome, detail string) {
	t.Helper()
	if gotOutcome != outcome || gotDetail != detail {
		t.Errorf("got (%q, %q), want (%q, %q)", gotOutcome, gotDetail, outcome, detail)
	}
}

const defaultTimeout = 300 * time.Second

func TestRunRowHolds(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`grep -F hallo f`", "wereld", dirWithF(t), true, defaultTimeout)
	want(t, o, d, Holds, "exit code 0, `wereld` found")
}

func TestRunRowSubstringMissing(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`grep -F hallo f`", "nope", dirWithF(t), true, defaultTimeout)
	want(t, o, d, Fails, "missing from output: 'nope'")
}

func TestRunRowExitCode(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`test -f bestaatniet`", "x", t.TempDir(), true, defaultTimeout)
	want(t, o, d, Fails, "exit code 1; missing from output: 'x'")
}

func TestRunRowAfterPRPrefix(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`grep -F hallo f`", "na PR: wereld", dirWithF(t), true, defaultTimeout)
	want(t, o, d, Holds, "exit code 0, `wereld` found")
}

func TestRunRowResultWithBackticks(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`grep -F hallo f`", "`wereld`", dirWithF(t), true, defaultTimeout)
	want(t, o, d, Holds, "exit code 0, `wereld` found")
}

func TestRunRowEmptyExpectationIsInvalid(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`ls`", "``", t.TempDir(), true, defaultTimeout)
	want(t, o, d, Invalid, "Evidence outside the allowlist: empty Result cell")
}

func TestRunRowOutsideAllowlistDoesNotRun(t *testing.T) {
	withConfig(t, "")
	dir := t.TempDir()
	o, d := runOne(t, "`git show --output=x HEAD`", "x", dir, true, defaultTimeout)
	want(t, o, d, Invalid, "Evidence outside the allowlist: option `--output=x` is not in the allowlist")
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Error("x exists: the row ran")
	}
}

func TestRunRowGHNotReachable(t *testing.T) {
	withConfig(t, "")
	marker := filepath.Join(t.TempDir(), "gh-called")
	stubBin(t, map[string]string{"gh": "#!/bin/sh\ntouch " + marker + "\necho x\n"})
	o, d := runOne(t, "`gh pr view 1`", "x", t.TempDir(), false, defaultTimeout)
	want(t, o, d, Unmeasurable, "gh not reachable")
	if _, err := os.Stat(marker); err == nil {
		t.Error("gh ran although it is not reachable")
	}
}

func TestRunRowBinaryMissing(t *testing.T) {
	withConfig(t, "")
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	o, d := runOne(t, "`grep -F a f`", "a", dir, true, defaultTimeout)
	want(t, o, d, Unmeasurable, "not found: grep")
}

// A grep on PATH that is not executable: Python's exec gives EACCES, not ENOENT.
func TestRunRowBinaryNotExecutable(t *testing.T) {
	withConfig(t, "")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "grep"), "#!/bin/sh\n", 0o644)
	t.Setenv("PATH", dir)
	o, d := runOne(t, "`grep -F a f`", "a", dir, true, defaultTimeout)
	want(t, o, d, Unmeasurable, "not executable: [Errno 13] Permission denied: 'grep'")
}

func TestRunRowTimeout(t *testing.T) {
	withConfig(t, "")
	// A deadline of 0 s has already passed before the first read: deterministic.
	o, d := runOne(t, "`ls`", "x", t.TempDir(), true, 0)
	want(t, o, d, Unmeasurable, "timeout after 0 s")
}

func TestRunRowOutputCapped(t *testing.T) {
	withConfig(t, "")
	for _, cmd := range []string{"cat /dev/zero", "cat zeros"} {
		t.Run(cmd, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Symlink("/dev/zero", filepath.Join(dir, "zeros")); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			o, d := runOne(t, "`"+cmd+"`", "x", dir, true, defaultTimeout)
			if elapsed := time.Since(start); elapsed >= 20*time.Second {
				t.Errorf("took %v", elapsed)
			}
			want(t, o, d, Unmeasurable, "output larger than 4194304 bytes")
		})
	}
}

func TestRunRowOutputCapSmaller(t *testing.T) {
	withConfig(t, "")
	old := maxOutput
	maxOutput = 10
	t.Cleanup(func() { maxOutput = old })
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f"), "een hele lange regel van meer dan tien bytes\n", 0o644)
	o, d := runOne(t, "`cat f`", "regel", dir, true, defaultTimeout)
	want(t, o, d, Unmeasurable, "output larger than 10 bytes")
}

func TestRunRowOutputUnderCapHolds(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`cat f`", "wereld", dirWithF(t), true, defaultTimeout)
	want(t, o, d, Holds, "exit code 0, `wereld` found")
}

// Stdout and stderr share the one pipe: a fragment on stderr is found too.
func TestRunRowReadsStderr(t *testing.T) {
	withConfig(t, "")
	o, d := runOne(t, "`cat nothere`", "nothere", t.TempDir(), true, defaultTimeout)
	want(t, o, d, Fails, "exit code 1")
}

// The subprocess gets _SUB_ENV on top of the environment, and /dev/null on stdin.
func TestRunRowEnvironmentAndStdin(t *testing.T) {
	withConfig(t, "")
	t.Setenv("GH_PAGER", "less")
	stubBin(t, map[string]string{"cat": "#!/bin/sh\nprintf 'pager=%s|%s|%s prompt=%s color=%s stdin=' \"$GIT_PAGER\" \"$PAGER\" \"$GH_PAGER\" \"$GH_PROMPT_DISABLED\" \"$NO_COLOR\"\n/bin/cat\necho end\n"})
	o, d := runOne(t, "`cat`", "pager=cat|cat|cat prompt=1 color=1 stdin=end", t.TempDir(), true, defaultTimeout)
	want(t, o, d, Holds, "exit code 0, `pager=cat|cat|cat prompt=1 color=1 stdin=end` found")
}

func TestRunRowEmptyOptionWritesNothing(t *testing.T) {
	withConfig(t, "")
	for _, cmd := range []string{
		"git log -n1 --format= --output={path}",
		"git show --pretty= --output={path} HEAD",
		"git log --pretty --output={path}",
	} {
		t.Run(cmd, func(t *testing.T) {
			dir := t.TempDir()
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
				git := exec.Command("git", args...)
				git.Dir = dir
				if out, err := git.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			pwn := filepath.Join(dir, "pwn")
			o, _ := runOne(t, "`"+strings.ReplaceAll(cmd, "{path}", pwn)+"`", "x", dir, true, defaultTimeout)
			if o != Invalid {
				t.Errorf("outcome %q, want %q", o, Invalid)
			}
			if _, err := os.Stat(pwn); err == nil {
				t.Error("pwn exists: the row ran")
			}
		})
	}
}

// --- Review-focus 4: a child in the background that keeps the pipe open --------------------

func readPid(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(raw)))
}

// alive reports a process that exists and is no zombie (a container's init may not reap it).
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // no /proc (darwin): kill -0 is the answer
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	return len(fields) == 0 || fields[0] != "Z"
}

// A `grep` stub starts `sleep 60 &`, which holds the pipe, and then sleeps itself. At the
// deadline the row is `timeout after 1 s`, the run does not wait for the child, and the child
// is gone: the kill reached the whole group, not only grep.
func TestRunRowKillsTheGroupOnTimeout(t *testing.T) {
	withConfig(t, "")
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	stubBin(t, map[string]string{"grep": "#!/bin/sh\nsleep 60 &\necho $! > \"" + pidFile + "\"\nsleep 60\n"})
	t.Cleanup(func() {
		if pid, err := readPid(pidFile); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	start := time.Now()
	o, d := runOne(t, "`grep -F a f`", "a", t.TempDir(), true, time.Second)
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Errorf("took %v: the run waited past the deadline", elapsed)
	}
	want(t, o, d, Unmeasurable, "timeout after 1 s")
	pid, err := readPid(pidFile)
	if err != nil {
		t.Fatalf("pid of the background child: %v", err)
	}
	for end := time.Now().Add(500 * time.Millisecond); alive(pid) && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(pid) {
		t.Errorf("background child %d of grep still runs: the group was not killed", pid)
	}
}

// After EOF the group goes too (Python's `_stop` in `finally`): a child that closed the pipe
// and keeps running in the background does not outlive the row.
func TestRunRowKillsTheGroupAfterEOF(t *testing.T) {
	withConfig(t, "")
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	stubBin(t, map[string]string{"grep": "#!/bin/sh\nsleep 60 >/dev/null 2>&1 &\necho $! > \"" + pidFile + "\"\necho a\n"})
	t.Cleanup(func() {
		if pid, err := readPid(pidFile); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	o, d := runOne(t, "`grep -F a f`", "a", t.TempDir(), true, 10*time.Second)
	want(t, o, d, Holds, "exit code 0, `a` found")
	pid, err := readPid(pidFile)
	if err != nil {
		t.Fatalf("pid of the background child: %v", err)
	}
	for end := time.Now().Add(500 * time.Millisecond); alive(pid) && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(pid) {
		t.Errorf("background child %d of grep still runs after EOF: the group was not killed", pid)
	}
}

// A command that closes its output and then hangs: EOF comes early, the wait for the exit still
// ends at the deadline (Python's proc.wait(timeout=…)).
func TestRunRowTimeoutAfterEOF(t *testing.T) {
	withConfig(t, "")
	stubBin(t, map[string]string{"grep": "#!/bin/sh\necho a\nexec sleep 60 >/dev/null 2>&1\n"})
	start := time.Now()
	o, d := runOne(t, "`grep -F a f`", "a", t.TempDir(), true, time.Second)
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Errorf("took %v: the run waited past the deadline", elapsed)
	}
	want(t, o, d, Unmeasurable, "timeout after 1 s")
}

// --- Run and CheckCell ------------------------------------------------------------------------

const ghStub = "#!/bin/sh\ntouch \"$GH_MARKER\"\ncase \"$1 $2\" in\n  \"api rate_limit\") echo 4999; exit 0 ;;\nesac\necho \"${GH_STUB_OUT:-y}\"\n"

type row3 struct{ ac, cmd, res string }

func body(nr int, rows []row3) string {
	var table []string
	for _, r := range rows {
		table = append(table, "| "+r.ac+" | "+r.cmd+" | "+r.res+" |")
	}
	n := strconv.Itoa(nr)
	return "Closes #" + n + "\n\n### Evidence #" + n + "\n\n| AC | Command | Result |\n|---|---|---|\n" + strings.Join(table, "\n") + "\n"
}

// runCLI is `run` over a body with rows, cwd a directory with f; it returns rc, stdout and
// the marker the gh stub touches.
func runCLI(t *testing.T, text string, beforePR bool, ghOut string) (int, string, string) {
	t.Helper()
	dir := dirWithF(t)
	marker := filepath.Join(t.TempDir(), "gh-called")
	t.Setenv("GH_MARKER", marker)
	t.Setenv("GH_STUB_OUT", ghOut)
	stubBin(t, map[string]string{"gh": ghStub})
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	writeFile(t, bodyFile, text, 0o644)
	var out bytes.Buffer
	rc := Run(5, bodyFile, dir, beforePR, &out)
	return rc, out.String(), marker
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

var naPRRows = []row3{
	{"1. een", "`grep -F hallo f`", "wereld"},
	{"2. twee", "`gh api graphql -f 'query={viewer{login}}'`", "`na PR: x`"},
}

func TestRunBeforePRSkipsAfterPR(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, marker := runCLI(t, body(5, naPRRows), true, "y")
	wantOut := "| AC | Outcome | Detail |\n|---|---|---|\n" +
		"| 1. een | holds | exit code 0, `wereld` found |\n" +
		"| 2. twee | after PR, not measured | runs only after the PR |\n"
	if rc != 0 || out != wantOut {
		t.Errorf("rc %d, stdout:\n%s\nwant rc 0, stdout:\n%s", rc, out, wantOut)
	}
	if exists(marker) {
		t.Error("gh ran before the PR")
	}
}

func TestRunWithoutBeforePRRunsAfterPR(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, marker := runCLI(t, body(5, naPRRows), false, "y")
	if rc != 1 || !strings.Contains(out, "| 2. twee | does not hold | missing from output: 'x' |") {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
	if !exists(marker) {
		t.Error("gh did not run")
	}
}

func TestRunWithoutBeforePRHolds(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	if rc, out, _ := runCLI(t, body(5, naPRRows), false, "x"); rc != 0 {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
}

func TestRunBeforePRAfterPROutsideAllowlistIsInvalid(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, _ := runCLI(t, body(5, []row3{{"1. een", "`rm -rf x`", "na PR: x"}}), true, "y")
	if rc != 1 || !strings.Contains(out, "| 1. een | invalid | Evidence outside the allowlist: `rm` is not in the allowlist |") {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
}

func TestRunBeforePREmptyAfterPRResultIsInvalid(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, _ := runCLI(t, body(5, []row3{{"1. een", "`grep -F hallo f`", "`na PR: `"}}), true, "y")
	if rc != 1 || !strings.Contains(out, "| 1. een | invalid | Evidence outside the allowlist: empty Result cell |") ||
		strings.Contains(out, "after PR, not measured") {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
}

func TestRunGHNotReachableIsExit4(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	dir := t.TempDir()
	stubBin(t, map[string]string{"gh": "#!/bin/sh\nexit 1\n"})
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	writeFile(t, bodyFile, body(5, []row3{{"1. een", "`gh pr view 1`", "x"}}), 0o644)
	var out bytes.Buffer
	if rc := Run(5, bodyFile, dir, false, &out); rc != 4 || !strings.Contains(out.String(), "| 1. een | not measurable | gh not reachable |") {
		t.Errorf("rc %d, stdout:\n%s", rc, out.String())
	}
}

func TestRunNoTable(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, marker := runCLI(t, "Closes #5\n\ngeen bewijs\n", false, "y")
	if rc != 2 || out != "Closes without evidence block: heading `### Evidence #5` is missing\n" {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
	if exists(marker) {
		t.Error("gh ran without a table")
	}
}

func TestRunMix(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, _ := runCLI(t, body(5, []row3{{"1. een", "`grep -F hallo f`", "wereld"}, {"2. twee", "`rm -rf x`", "x"}}), false, "y")
	if rc != 1 || !strings.Contains(out, "Evidence outside the allowlist") || !strings.Contains(out, "| holds |") {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
}

func TestRunDetailDoesNotBreakTheTable(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	rc, out, _ := runCLI(t, body(5, []row3{{"1. een", "`grep -F hallo f`", `a\|b`}}), false, "y")
	if rc != 1 || !strings.Contains(out, `| 1. een | does not hold | missing from output: 'a\|b' |`) {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
}

func TestRunOutcomesFollowLanguage(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"nl\"\n")
	rc, out, _ := runCLI(t, body(5, []row3{{"1. een", "`grep -F hallo f`", "wereld"}}), false, "y")
	if rc != 0 || out != "| AC | Uitkomst | Detail |\n|---|---|---|\n| 1. een | klopt | exit code 0, `wereld` found |\n" {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
}

func TestRunCouldNotCheck(t *testing.T) {
	withConfig(t, "")
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.md")
	var out bytes.Buffer
	if rc := Run(5, missing, dir, false, &out); rc != 3 ||
		out.String() != "COULD NOT CHECK: body file unreadable ([Errno 2] No such file or directory: '"+missing+"')\n" {
		t.Errorf("missing body: rc %d, stdout %q", rc, out.String())
	}
	bodyFile := filepath.Join(dir, "body.md")
	writeFile(t, bodyFile, "Closes #5\n", 0o644)
	out.Reset()
	if rc := Run(5, bodyFile, missing, false, &out); rc != 3 || out.String() != "COULD NOT CHECK: --cwd "+missing+" is not a directory\n" {
		t.Errorf("cwd not a dir: rc %d, stdout %q", rc, out.String())
	}
	t.Setenv("LIGHTHOUSE_TOML", missing)
	out.Reset()
	if rc := Run(5, bodyFile, dir, false, &out); rc != 3 || out.String() != "COULD NOT CHECK: "+missing+": file is missing\n" {
		t.Errorf("config missing: rc %d, stdout %q", rc, out.String())
	}
}

func TestCheckCell(t *testing.T) {
	withConfig(t, "[test_gate]\nevidence_test_dir = \"t/\"\n")
	var out bytes.Buffer
	if rc := CheckCell("`grep -F x f`", &out); rc != 0 || out.String() != "" {
		t.Errorf("allowed: rc %d, stdout %q", rc, out.String())
	}
	out.Reset()
	if rc := CheckCell("`rm -rf x`", &out); rc != 1 || out.String() != "`rm` is not in the allowlist\n" {
		t.Errorf("refused: rc %d, stdout %q", rc, out.String())
	}
	out.Reset()
	if rc := CheckCell("`uv run pytest tests/x.py`", &out); rc != 1 || out.String() != "path `tests/x.py` is not under `t/`\n" {
		t.Errorf("pytest dir from config: rc %d, stdout %q", rc, out.String())
	}
}

// The config is read only for a pytest row, as in Python: a broken config blocks only there.
func TestCheckCellReadsConfigOnlyForPytest(t *testing.T) {
	useHeadings(t)
	missing := filepath.Join(t.TempDir(), "lighthouse.toml")
	t.Setenv("LIGHTHOUSE_TOML", missing)
	var out bytes.Buffer
	if rc := CheckCell("`grep -F x f`", &out); rc != 0 || out.String() != "" {
		t.Errorf("grep: rc %d, stdout %q", rc, out.String())
	}
	out.Reset()
	if rc := CheckCell("`uv run pytest tests/x.py`", &out); rc != 3 || out.String() != "COULD NOT CHECK: "+missing+": file is missing\n" {
		t.Errorf("pytest: rc %d, stdout %q", rc, out.String())
	}
}

// A body file that is not valid UTF-8: Python's strict read raises (a traceback, rc 1); Go says
// COULD NOT CHECK with rc 3, a deliberate deviation. Nothing runs, and no gh probe either.
func TestRunBodyFileNotUTF8(t *testing.T) {
	withConfig(t, "[project]\nlanguage = \"en\"\n")
	text := body(5, []row3{{"1. een", "`gh pr view 1`", "x"}}) + "\xff\n"
	rc, out, marker := runCLI(t, text, false, "x")
	if rc != 3 || out != "COULD NOT CHECK: body file unreadable (not valid UTF-8)\n" {
		t.Errorf("rc %d, stdout %q", rc, out)
	}
	if exists(marker) {
		t.Error("gh ran for a body that could not be read")
	}
}
