//go:build e2e

// Black-box tests of pack/core/hooks/run.sh: how it finds the lighthouse binary, builds it from
// the checkout at version = "dev" without ever waiting for the build, and maps a missing binary
// or an unknown hook per the blocking list. Every test runs the script from a temporary repo with
// a PATH of symlinks to the tools run.sh calls, so no `go` or `lighthouse` of the host leaks in.
// The fake `go` logs each call (cwd and arguments), waits until FAKE_GO_GATE exists when that is
// set, and writes a fake binary to its -o path; the fake PATH `lighthouse` echoes its arguments
// and has its exit code, stdout and stderr set by the environment.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// runShTools is what run.sh and the fakes call (the dirname, setsid and git of run.sh included).
var runShTools = []string{
	"sh", "git", "grep", "sed", "awk", "cat", "mkdir", "rmdir", "rm", "mv", "find", "touch",
	"date", "mktemp", "sleep", "chmod", "dirname", "head", "nohup", "setsid",
}

const fakeGo = `#!/bin/sh
printf '%s|%s\n' "$(pwd -P)" "$*" >>"$FAKE_GO_LOG"
if [ "$1" != build ] || [ "$2" != -o ] || [ "$4" != ./cmd/lighthouse ]; then
  echo "fake go: unexpected arguments: $*" >&2
  exit 99
fi
# A build the test holds open until it creates the gate file: a run.sh that waits for the
# build hangs on the call's timeout, whatever the speed of the runner.
if [ -n "${FAKE_GO_GATE:-}" ]; then
  n=0
  while [ ! -e "$FAKE_GO_GATE" ] && [ "$n" -lt 2400 ]; do
    sleep 0.05
    n=$((n + 1))
  done
fi
if [ -n "${FAKE_GO_FAIL:-}" ]; then
  echo "compile error: boom"
  : >"$3" # a half-written output, like a linker that dies
  exit 1
fi
printf '#!/bin/sh\necho "built-binary $*"\n' >"$3"
chmod +x "$3"
`

const fakeLighthouse = `#!/bin/sh
cat >"$FAKE_LH_STDIN"
sleep "${FAKE_LH_SLEEP:-0}"
if [ -n "${FAKE_LH_SHOW_ROOT:-}" ]; then printf 'root=%s\n' "${LIGHTHOUSE_REPO_ROOT-unset}"; fi
if [ -n "${FAKE_LH_OUT+x}" ]; then printf '%s' "$FAKE_LH_OUT"; else echo "path-binary $*"; fi
printf '%s' "${FAKE_LH_ERR:-}" >&2
exit "${FAKE_LH_RC:-0}"
`

const (
	blockingHook    = "main-commit-gate"
	nonBlockingHook = "worktree-check"
	releaseVersion  = "v0.12.1"
)

// runShOpts is what differs per test in the repo run.sh runs in.
type runShOpts struct {
	version    string // manifest version; "" means dev
	noGo       bool   // no fake `go` on the PATH
	noLH       bool   // no fake `lighthouse` on the PATH
	noSetsid   bool   // no `setsid` on the PATH: the build detaches with nohup
	noSource   bool   // no cmd/lighthouse/main.go: not lighthouse's own checkout
	parentDir  string // directory between the temporary directory and the repo
	emptyTools bool   // a PATH directory without any tool
}

type runShRepo struct {
	t       *testing.T
	tmp     string
	root    string
	runSh   string
	bin     string // .lighthouse/bin of the repo
	pathDir string
	goLog   string
	stdin   string
}

type runShResult struct {
	code   int
	stdout string
	stderr string
}

func newRunShRepo(t *testing.T, o runShOpts) *runShRepo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("run.sh is a POSIX shell script")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &runShRepo{t: t, tmp: tmp, root: filepath.Join(tmp, o.parentDir, "repo")}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, r.root, "init", "-q", "-b", "main")
	writeFile(t, r.root, "README", "x\n")
	git(t, r.root, "add", "README")
	git(t, r.root, "commit", "-qm", "init")
	if !o.noSource {
		// The layout of lighthouse's own checkout: only there does `dev` build.
		writeFile(t, r.root, "cmd/lighthouse/main.go", "package main\n")
	}
	hooks := filepath.Join(r.root, ".claude", "hooks", "lighthouse")
	for _, name := range []string{"run.sh", "blocking"} {
		data, err := os.ReadFile(filepath.Join("..", "pack", "core", "hooks", name))
		if err != nil {
			t.Fatal(err)
		}
		writeExec(t, filepath.Join(hooks, name), string(data), 0o755)
	}
	r.runSh = filepath.Join(hooks, "run.sh")
	version := o.version
	if version == "" {
		version = "dev"
	}
	writeFile(t, r.root, ".lighthouse/manifest.toml",
		"# Managed by lighthouse init. Do not edit by hand.\n\nversion = \""+version+"\"\n")
	r.bin = filepath.Join(r.root, ".lighthouse", "bin")

	r.pathDir = filepath.Join(tmp, "path")
	if err := os.MkdirAll(r.pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !o.emptyTools {
		for _, tool := range runShTools {
			if tool == "setsid" && o.noSetsid {
				continue
			}
			if found, err := exec.LookPath(tool); err == nil {
				if err := os.Symlink(found, filepath.Join(r.pathDir, tool)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if !o.noGo {
		writeExec(t, filepath.Join(r.pathDir, "go"), fakeGo, 0o755)
	}
	if !o.noLH {
		writeExec(t, filepath.Join(r.pathDir, "lighthouse"), fakeLighthouse, 0o755)
	}
	r.goLog = filepath.Join(tmp, "go.log")
	r.stdin = filepath.Join(tmp, "lh-stdin")
	return r
}

func writeExec(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// env is the whole environment of a call: nothing of the host, so a controller run cannot leak in.
func (r *runShRepo) env(extra map[string]string) []string {
	env := []string{
		"PATH=" + r.pathDir, "HOME=" + r.tmp, "TMPDIR=" + r.tmp,
		"FAKE_GO_LOG=" + r.goLog, "FAKE_LH_STDIN=" + r.stdin,
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env // a later duplicate wins in os/exec
}

type callOpts struct {
	stdin  string
	cwd    string
	script string
	env    map[string]string
}

func (r *runShRepo) cmd(ctx context.Context, o callOpts, hook string, args ...string) *exec.Cmd {
	script := o.script
	if script == "" {
		script = r.runSh
	}
	cmd := exec.CommandContext(ctx, script, append([]string{hook}, args...)...)
	cmd.Dir = r.root
	if o.cwd != "" {
		cmd.Dir = o.cwd
	}
	cmd.Env = r.env(o.env)
	return cmd
}

func (r *runShRepo) call(o callOpts, hook string, args ...string) runShResult {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := r.cmd(ctx, o, hook, args...)
	// A build that keeps the pipes of the call open must not hang the test forever.
	cmd.WaitDelay = 3 * time.Second
	stdin := o.stdin
	if stdin == "" {
		stdin = "{}"
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := runShResult{stdout: stdout.String(), stderr: stderr.String()}
	if exit, ok := err.(*exec.ExitError); ok {
		res.code = exit.ExitCode()
	} else if err != nil {
		r.t.Fatalf("run.sh did not run: %v", err)
	}
	return res
}

func (r *runShRepo) head() string {
	r.t.Helper()
	out, err := exec.Command("git", "-C", r.root, "rev-parse", "HEAD").Output()
	if err != nil {
		r.t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (r *runShRepo) commit() {
	r.t.Helper()
	writeFile(r.t, r.root, "README", strconv.FormatInt(time.Now().UnixNano(), 10)+"\n")
	git(r.t, r.root, "commit", "-qam", "next")
}

func (r *runShRepo) goCalls() []string {
	data, err := os.ReadFile(r.goLog)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func (r *runShRepo) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(r.bin, rel))
	return err == nil
}

func (r *runShRepo) read(rel string) string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.bin, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

// waitBuilt waits until the go log has calls entries and the lock is gone. The deadline only
// catches a build that never ends.
func (r *runShRepo) waitBuilt(calls int) {
	r.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.goCalls()) >= calls && !r.exists("build.lock") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatalf("build not done: go calls %q, lock %v", r.goCalls(), r.exists("build.lock"))
}

// settle gives a build that should not start the time it would need to show up in the go log.
func (r *runShRepo) settle() {
	time.Sleep(300 * time.Millisecond)
}

func (r *runShRepo) tmpFiles() []string {
	entries, err := os.ReadDir(r.tmp)
	if err != nil {
		r.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lighthouse-run.") {
			names = append(names, e.Name())
		}
	}
	return names
}

func (r *runShRepo) leftoverTmpBinaries() []string {
	entries, err := os.ReadDir(r.bin)
	if err != nil {
		r.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lighthouse.tmp") {
			names = append(names, e.Name())
		}
	}
	return names
}

// age gives path the mtime of now minus d.
func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	at := time.Now().Add(-d)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// oldCheckoutBinary is a checkout binary built for another HEAD.
func (r *runShRepo) oldCheckoutBinary() {
	r.t.Helper()
	writeExec(r.t, filepath.Join(r.bin, "lighthouse"), "#!/bin/sh\necho \"old-binary $*\"\n", 0o755)
	writeFile(r.t, r.bin, "stamp", "0000000000000000000000000000000000000000\n")
}

func checkRunSh(t *testing.T, got runShResult, wantCode int, wantStdout, wantStderr string) {
	t.Helper()
	if got.code != wantCode || got.stdout != wantStdout || got.stderr != wantStderr {
		t.Errorf("run.sh = rc %d, stdout %q, stderr %q\nwant rc %d, stdout %q, stderr %q",
			got.code, got.stdout, got.stderr, wantCode, wantStdout, wantStderr)
	}
}

const installRelease = "COULD NOT CHECK: lighthouse not on PATH — install v0.12.1: go install " +
	"github.com/CorneeldH/lighthouse/cmd/lighthouse@v0.12.1, or download it from the release"

func TestRunShMissingBinary(t *testing.T) {
	cases := []struct {
		name       string
		opts       runShOpts
		hook       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"release blocks a blocking hook", runShOpts{version: releaseVersion, noLH: true}, blockingHook, []string{"pre"},
			2, "", installRelease + "\n"},
		{"release warns for another hook", runShOpts{version: releaseVersion, noLH: true}, nonBlockingHook, nil,
			0, `{"systemMessage": "` + installRelease + `"}` + "\n", ""},
		{"dev names the checkout build and use-path", runShOpts{noGo: true, noLH: true}, blockingHook, nil,
			2, "", "COULD NOT CHECK: lighthouse not on PATH — this checkout has version = \"dev\": run go install ./cmd/lighthouse here " +
				"(a build into .lighthouse/bin/ starts by itself when go is on the PATH), " +
				"or create .lighthouse/bin/use-path to use the lighthouse on the PATH\n"},
		{"dev without source names the release", runShOpts{noLH: true, noSource: true}, blockingHook, nil,
			2, "", "COULD NOT CHECK: lighthouse not on PATH — install it: go install " +
				"github.com/CorneeldH/lighthouse/cmd/lighthouse@latest, or download it from the release\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunShRepo(t, tc.opts)
			checkRunSh(t, r.call(callOpts{}, tc.hook, tc.args...), tc.wantCode, tc.wantStdout, tc.wantStderr)
		})
	}
}

func TestRunShPassesTheBinaryThrough(t *testing.T) {
	cases := []struct {
		name       string
		hook       string
		args       []string
		stdin      string
		env        map[string]string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"output and exit code untouched", blockingHook, []string{"pre"}, `{"tool_name":"Bash"}`,
			map[string]string{"FAKE_LH_OUT": `{"decision":"block"}` + "\n", "FAKE_LH_ERR": "reason: on main\n", "FAKE_LH_RC": "2"},
			2, `{"decision":"block"}` + "\n", "reason: on main\n"},
		{"hook and arguments reach the binary", blockingHook, []string{"pre"}, "{}", nil,
			0, "path-binary hook main-commit-gate pre\n", ""},
		{"another failure passes untouched", blockingHook, nil, "{}",
			map[string]string{"FAKE_LH_ERR": "Error: something else\n", "FAKE_LH_RC": "1", "FAKE_LH_OUT": ""},
			1, "", "Error: something else\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunShRepo(t, runShOpts{version: releaseVersion})
			checkRunSh(t, r.call(callOpts{stdin: tc.stdin, env: tc.env}, tc.hook, tc.args...), tc.wantCode, tc.wantStdout, tc.wantStderr)
			got, err := os.ReadFile(r.stdin)
			if err != nil || string(got) != tc.stdin {
				t.Errorf("stdin of the binary = %q, %v; want %q", got, err, tc.stdin)
			}
		})
	}
}

const unknownHook = "COULD NOT CHECK: unknown hook nope — update lighthouse to v0.12.1\n"

func TestRunShUnknownHook(t *testing.T) {
	cases := []struct {
		name       string
		opts       runShOpts
		hook       string
		binErr     string
		wantCode   int
		wantStdout string // exact
		wantMsg    string // or: stdout is JSON with this systemMessage
		wantStderr string
	}{
		{"release blocks a blocking hook", runShOpts{version: releaseVersion}, blockingHook, unknownHook,
			2, "", "", unknownHook},
		{"release warns for another hook", runShOpts{version: releaseVersion}, nonBlockingHook, unknownHook,
			0, "", strings.TrimSuffix(unknownHook, "\n"), ""},
		{"dev never blocks", runShOpts{noGo: true}, blockingHook, unknownHook,
			0, "", strings.TrimSuffix(unknownHook, "\n"), ""},
		{"dev without source blocks like a release", runShOpts{noSource: true}, blockingHook, unknownHook,
			2, "", "", unknownHook},
		{"the message is valid JSON with quote, backslash, tab and newline", runShOpts{version: releaseVersion}, nonBlockingHook,
			"COULD NOT CHECK: unknown hook \"x\" in C:\\path\twith tab\nsecond line\n",
			0, "", "COULD NOT CHECK: unknown hook \"x\" in C:\\path\twith tab\nsecond line", ""},
		{"the message is valid JSON with a carriage return", runShOpts{version: releaseVersion}, nonBlockingHook,
			"COULD NOT CHECK: line one\r\nline two\n",
			0, "", "COULD NOT CHECK: line one\r\nline two", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunShRepo(t, tc.opts)
			got := r.call(callOpts{env: map[string]string{"FAKE_LH_ERR": tc.binErr, "FAKE_LH_RC": "3", "FAKE_LH_OUT": ""}}, tc.hook)
			if tc.wantMsg == "" {
				checkRunSh(t, got, tc.wantCode, tc.wantStdout, tc.wantStderr)
				return
			}
			var msg struct {
				SystemMessage string `json:"systemMessage"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &msg); err != nil {
				t.Fatalf("stdout %q is no JSON: %v", got.stdout, err)
			}
			if got.code != tc.wantCode || msg.SystemMessage != tc.wantMsg || got.stderr != tc.wantStderr {
				t.Errorf("run.sh = rc %d, systemMessage %q, stderr %q\nwant rc %d, systemMessage %q, stderr %q",
					got.code, msg.SystemMessage, got.stderr, tc.wantCode, tc.wantMsg, tc.wantStderr)
			}
		})
	}
}

func TestRunShPathBinaryWithoutHookCommand(t *testing.T) {
	const oldBinary = "Error: unknown command \"hook\" for \"lighthouse\"\nRun 'lighthouse --help' for usage.\n"
	r := newRunShRepo(t, runShOpts{version: releaseVersion})
	env := map[string]string{"FAKE_LH_ERR": oldBinary, "FAKE_LH_RC": "1", "FAKE_LH_OUT": ""}
	msg := "COULD NOT CHECK: lighthouse has no hook command (" + filepath.Join(r.pathDir, "lighthouse") +
		" is older than this repo) — install v0.12.1: go install github.com/CorneeldH/lighthouse/cmd/lighthouse@v0.12.1, " +
		"or download it from the release"
	checkRunSh(t, r.call(callOpts{env: env}, blockingHook), 2, "", msg+"\n")
	checkRunSh(t, r.call(callOpts{env: env}, nonBlockingHook), 0, `{"systemMessage": "`+msg+`"}`+"\n", "")
}

func TestRunShWithoutATempFileForStderr(t *testing.T) {
	const noTmp = "COULD NOT CHECK: hook main-commit-gate exited 3 (no temporary file for its stderr)"
	cases := []struct {
		name       string
		hook       string
		binErr     string
		binRC      string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"exit 3 still blocks a blocking hook", blockingHook, unknownHook, "3", 2, "", unknownHook + noTmp + "\n"},
		{"exit 3 warns for another hook", nonBlockingHook, unknownHook, "3", 0,
			`{"systemMessage": "` + strings.Replace(noTmp, blockingHook, nonBlockingHook, 1) + `"}` + "\n", unknownHook},
		{"another exit passes untouched", blockingHook, "blocked\n", "2", 2, "", "blocked\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunShRepo(t, runShOpts{version: releaseVersion})
			env := map[string]string{
				"TMPDIR": filepath.Join(r.tmp, "missing"), "FAKE_LH_ERR": tc.binErr, "FAKE_LH_RC": tc.binRC, "FAKE_LH_OUT": "",
			}
			checkRunSh(t, r.call(callOpts{env: env}, tc.hook), tc.wantCode, tc.wantStdout, tc.wantStderr)
		})
	}
}

func TestRunShTerminatedRunRemovesItsTempFile(t *testing.T) {
	r := newRunShRepo(t, runShOpts{version: releaseVersion})
	cmd := r.cmd(context.Background(), callOpts{env: map[string]string{"FAKE_LH_SLEEP": "1"}}, nonBlockingHook)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for len(r.tmpFiles()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(r.tmpFiles()) == 0 {
		t.Fatal("run.sh made no temporary file")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// run.sh runs its TERM trap once the binary (1 s) has returned.
	deadline = time.Now().Add(10 * time.Second)
	for len(r.tmpFiles()) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if left := r.tmpFiles(); len(left) != 0 {
		t.Errorf("temporary files left after SIGTERM: %q", left)
	}
}

func TestRunShBuildsOnceAndNeverWaits(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts runShOpts
	}{
		{"setsid", runShOpts{}},
		{"nohup without setsid", runShOpts{noSetsid: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath("setsid"); err != nil && !tc.opts.noSetsid {
				t.Skip("no setsid on this host: the build would detach with nohup")
			}
			r := newRunShRepo(t, tc.opts)
			if _, err := os.Lstat(filepath.Join(r.pathDir, "setsid")); tc.opts.noSetsid && err == nil {
				t.Fatal("setsid is on the PATH")
			}
			// The build stays blocked until every call has returned: a call that waited for it
			// hits the timeout instead of a wall-clock bound.
			gate := filepath.Join(r.tmp, "go-gate")
			var wg sync.WaitGroup
			results := make([]runShResult, 8)
			for i := range results {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results[i] = r.call(callOpts{env: map[string]string{"FAKE_GO_GATE": gate}}, nonBlockingHook)
				}()
			}
			wg.Wait()
			writeFile(t, r.tmp, "go-gate", "")
			for i, got := range results {
				if got.code != 0 || got.stdout != "path-binary hook worktree-check\n" {
					t.Errorf("call %d = rc %d, stdout %q", i, got.code, got.stdout)
				}
			}
			r.waitBuilt(1)
			if n := len(r.goCalls()); n != 1 {
				t.Errorf("go calls = %d, want 1: %q", n, r.goCalls())
			}
			if st, err := os.Stat(filepath.Join(r.bin, "lighthouse")); err != nil || st.Mode()&0o100 == 0 {
				t.Errorf("no executable binary after the build: %v", err)
			}
			if got := strings.TrimSpace(r.read("stamp")); got != r.head() {
				t.Errorf("stamp = %q, want HEAD %q", got, r.head())
			}
			if left := r.leftoverTmpBinaries(); len(left) != 0 {
				t.Errorf("temporary binaries left: %q", left)
			}
			checkRunSh(t, r.call(callOpts{}, nonBlockingHook), 0, "built-binary hook worktree-check\n", "")
			if n := len(r.goCalls()); n != 1 {
				t.Errorf("go calls after the second run = %d, want 1: %q", n, r.goCalls())
			}
		})
	}
}

func TestRunShRunsThePreviousBinaryDuringTheBuild(t *testing.T) {
	r := newRunShRepo(t, runShOpts{})
	r.oldCheckoutBinary()
	gate := filepath.Join(r.tmp, "go-gate")
	got := r.call(callOpts{env: map[string]string{"FAKE_GO_GATE": gate}}, nonBlockingHook)
	writeFile(t, r.tmp, "go-gate", "")
	checkRunSh(t, got, 0, "old-binary hook worktree-check\n", "")
	r.waitBuilt(1)
	checkRunSh(t, r.call(callOpts{}, nonBlockingHook), 0, "built-binary hook worktree-check\n", "")
}

func TestRunShStartsNoBuild(t *testing.T) {
	cases := []struct {
		name       string
		opts       runShOpts
		setup      func(r *runShRepo)
		wantStdout string
	}{
		{"the stamp is the current HEAD", runShOpts{}, func(r *runShRepo) {
			r.oldCheckoutBinary()
			writeFile(t, r.bin, "stamp", r.head()+"\n")
		}, "old-binary hook worktree-check\n"},
		{"use-path exists", runShOpts{}, func(r *runShRepo) {
			r.oldCheckoutBinary()
			writeFile(t, r.bin, "use-path", "")
		}, "path-binary hook worktree-check\n"},
		{"no go on the PATH", runShOpts{noGo: true}, func(r *runShRepo) {
			r.oldCheckoutBinary()
		}, "path-binary hook worktree-check\n"},
		{"a release version", runShOpts{version: releaseVersion}, func(r *runShRepo) {
			r.oldCheckoutBinary()
		}, "path-binary hook worktree-check\n"},
		{"dev without source", runShOpts{noSource: true}, func(r *runShRepo) {
			r.oldCheckoutBinary()
		}, "path-binary hook worktree-check\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunShRepo(t, tc.opts)
			tc.setup(r)
			checkRunSh(t, r.call(callOpts{}, nonBlockingHook), 0, tc.wantStdout, "")
			r.settle()
			if calls := r.goCalls(); len(calls) != 0 {
				t.Errorf("go was called: %q", calls)
			}
			if r.exists("build.lock") || r.exists("build.failed") {
				t.Error("a build was started")
			}
		})
	}
}

func TestRunShRebuildsOnANewCommit(t *testing.T) {
	r := newRunShRepo(t, runShOpts{})
	r.call(callOpts{}, nonBlockingHook)
	r.waitBuilt(1)
	r.commit()
	r.call(callOpts{}, nonBlockingHook)
	r.waitBuilt(2)
	if got := strings.TrimSpace(r.read("stamp")); got != r.head() {
		t.Errorf("stamp = %q, want HEAD %q", got, r.head())
	}
}

func TestRunShBuildLock(t *testing.T) {
	t.Run("a stale lock is removed and the build runs", func(t *testing.T) {
		r := newRunShRepo(t, runShOpts{})
		if err := os.MkdirAll(filepath.Join(r.bin, "build.lock"), 0o755); err != nil {
			t.Fatal(err)
		}
		age(t, filepath.Join(r.bin, "build.lock"), 150*time.Second)
		r.call(callOpts{}, nonBlockingHook)
		r.waitBuilt(1)
		if got := strings.TrimSpace(r.read("stamp")); got != r.head() {
			t.Errorf("stamp = %q, want HEAD %q", got, r.head())
		}
	})
	t.Run("a fresh lock starts nothing", func(t *testing.T) {
		r := newRunShRepo(t, runShOpts{})
		if err := os.MkdirAll(filepath.Join(r.bin, "build.lock"), 0o755); err != nil {
			t.Fatal(err)
		}
		age(t, filepath.Join(r.bin, "build.lock"), 100*time.Second)
		checkRunSh(t, r.call(callOpts{}, nonBlockingHook), 0, "path-binary hook worktree-check\n", "")
		r.settle()
		if calls := r.goCalls(); len(calls) != 0 {
			t.Errorf("go was called: %q", calls)
		}
		if !r.exists("build.lock") {
			t.Error("the fresh lock was removed")
		}
	})
}

func TestRunShFailedBuildPausesSixtySeconds(t *testing.T) {
	r := newRunShRepo(t, runShOpts{})
	fail := callOpts{env: map[string]string{"FAKE_GO_FAIL": "1"}}
	r.call(fail, nonBlockingHook)
	r.waitBuilt(1)
	if got := strings.TrimSpace(r.read("build.failed")); got != r.head() {
		t.Errorf("build.failed = %q, want HEAD %q", got, r.head())
	}
	if got := r.read("build.log"); got != "compile error: boom\n" {
		t.Errorf("build.log = %q, want the go output", got)
	}
	if r.exists("lighthouse") {
		t.Error("a failed build left a binary")
	}
	if left := r.leftoverTmpBinaries(); len(left) != 0 {
		t.Errorf("temporary binaries left: %q", left)
	}

	age(t, filepath.Join(r.bin, "build.failed"), 30*time.Second)
	r.call(fail, nonBlockingHook)
	r.settle()
	if n := len(r.goCalls()); n != 1 {
		t.Fatalf("go calls 30 s after the failure = %d, want 1", n)
	}

	age(t, filepath.Join(r.bin, "build.failed"), 90*time.Second)
	r.call(fail, nonBlockingHook)
	r.waitBuilt(2)

	r.commit()
	r.call(callOpts{}, nonBlockingHook)
	r.waitBuilt(3)
	if r.exists("build.failed") {
		t.Error("build.failed stays after a good build")
	}
	if got := strings.TrimSpace(r.read("stamp")); got != r.head() {
		t.Errorf("stamp = %q, want HEAD %q", got, r.head())
	}
}

func TestRunShRepoRootForTheBinary(t *testing.T) {
	show := map[string]string{"FAKE_LH_SHOW_ROOT": "1"}
	t.Run("from a cwd outside the repo", func(t *testing.T) {
		r := newRunShRepo(t, runShOpts{version: releaseVersion})
		elsewhere := filepath.Join(r.tmp, "elsewhere")
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatal(err)
		}
		checkRunSh(t, r.call(callOpts{cwd: elsewhere, env: show}, nonBlockingHook), 0,
			"root="+r.root+"\npath-binary hook worktree-check\n", "")
	})
	t.Run("a set root is kept", func(t *testing.T) {
		r := newRunShRepo(t, runShOpts{version: releaseVersion})
		checkRunSh(t, r.call(callOpts{env: map[string]string{"FAKE_LH_SHOW_ROOT": "1", "LIGHTHOUSE_REPO_ROOT": "/given"}}, nonBlockingHook), 0,
			"root=/given\npath-binary hook worktree-check\n", "")
	})
}

func TestRunShFromASubdirWithASpace(t *testing.T) {
	r := newRunShRepo(t, runShOpts{parentDir: "a b"})
	sub := filepath.Join(r.root, "sub dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join("..", ".claude", "hooks", "lighthouse", "run.sh")
	checkRunSh(t, r.call(callOpts{cwd: sub, script: script}, nonBlockingHook), 0, "path-binary hook worktree-check\n", "")
	r.waitBuilt(1)
	cwd, args, _ := strings.Cut(r.goCalls()[0], "|")
	if cwd != r.root {
		t.Errorf("go ran in %q, want %q", cwd, r.root)
	}
	if want := "build -o " + r.bin + "/lighthouse.tmp."; !strings.HasPrefix(args, want) {
		t.Errorf("go arguments %q do not start with %q", args, want)
	}
	checkRunSh(t, r.call(callOpts{cwd: sub, script: script}, nonBlockingHook), 0, "built-binary hook worktree-check\n", "")
}

// With CDPATH set, `cd` on a relative path prints the directory: `here` must not take it.
func TestRunShRelativePathWithCDPATH(t *testing.T) {
	r := newRunShRepo(t, runShOpts{version: releaseVersion})
	script := filepath.Join(".claude", "hooks", "lighthouse", "run.sh")
	checkRunSh(t, r.call(callOpts{script: script, env: map[string]string{"CDPATH": r.root}}, nonBlockingHook),
		0, "path-binary hook worktree-check\n", "")
}

// Without grep or without the list it cannot be read, without dirname run.sh used to lose itself, and
// without any tool nothing can run: all fail closed for a blocking hook.
func TestRunShMissingToolStillBlocksABlockingHook(t *testing.T) {
	cases := []struct {
		name string
		opts runShOpts
		drop string
	}{
		{"no grep", runShOpts{version: releaseVersion, noLH: true}, "grep"},
		// Only protects against a dirname that is needed again and fails open: run.sh itself uses none.
		{"no dirname", runShOpts{version: releaseVersion, noLH: true}, "dirname"},
		{"an empty PATH", runShOpts{version: releaseVersion, noLH: true, emptyTools: true}, ""},
		{"no list of blocking hooks", runShOpts{version: releaseVersion, noLH: true}, "blocking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunShRepo(t, tc.opts)
			if tc.drop != "" {
				dir := r.pathDir
				if tc.drop == "blocking" {
					dir = filepath.Dir(r.runSh)
				}
				if err := os.Remove(filepath.Join(dir, tc.drop)); err != nil {
					t.Fatal(err)
				}
			}
			got := r.call(callOpts{}, blockingHook)
			if got.code != 2 || !strings.Contains(got.stderr, "COULD NOT CHECK") {
				t.Errorf("run.sh = rc %d, stdout %q, stderr %q; want rc 2 and COULD NOT CHECK", got.code, got.stdout, got.stderr)
			}
		})
	}
}

// The run.sh installed in one repository passes a hook through for another repository, with the
// real binary on the PATH and no LIGHTHOUSE_REPO_ROOT in the environment (what a controller run sets).
func TestRunShInstalledFindsItsRootWithoutEnv(t *testing.T) {
	bin := buildLighthouse(t)
	home := freshRepo(t)
	run(t, home, bin, "init", "--yes")
	feature := freshRepo(t)
	git(t, feature, "checkout", "-q", "-b", "issue-1-x")
	payload, _ := json.Marshal(map[string]any{
		"tool_name": "Bash", "cwd": feature, "tool_input": map[string]string{"command": "git commit -m x"},
	})
	runSh := filepath.Join(home, ".claude", "hooks", "lighthouse", "run.sh")
	cmd := exec.Command(runSh, "main-commit-gate")
	if runtime.GOOS == "windows" {
		// Windows cannot exec a shell script; Claude Code runs the hook through Git Bash there (#559).
		cmd = exec.Command("bash", filepath.ToSlash(runSh), "main-commit-gate")
	}
	cmd.Dir = feature
	cmd.Env = hookEnv(bin)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.String() != "" || stderr.String() != "" {
		t.Errorf("guard from a feature repo: %v, stdout %q, stderr %q; want success, silent", err, stdout.String(), stderr.String())
	}
}

// The pinned shellcheck of the shell package, else the one on the PATH; skipped when neither
// exists. A CI run installs it.
func TestRunShShellcheckClean(t *testing.T) {
	lib, _ := filepath.Abs(filepath.Join("..", "pack", "shell", "scripts", "lib", "shellcheck-bin.sh"))
	resolve := exec.Command("bash", "-c", `. "`+lib+`"`+"\nresolve_shellcheck\n")
	resolve.Env = cleanEnv()
	shellcheck := ""
	if out, err := resolve.Output(); err == nil {
		shellcheck = strings.TrimSpace(string(out))
	}
	if shellcheck == "" {
		found, err := exec.LookPath("shellcheck")
		if err != nil {
			t.Skip("shellcheck not installed")
		}
		shellcheck = found
	}
	runSh, _ := filepath.Abs(filepath.Join("..", "pack", "core", "hooks", "run.sh"))
	check := exec.Command(shellcheck, "-s", "sh", "-S", "style", runSh)
	check.Env = cleanEnv()
	out, err := check.CombinedOutput()
	if err != nil {
		t.Errorf("shellcheck run.sh: %v\n%s", err, out)
	}
}
