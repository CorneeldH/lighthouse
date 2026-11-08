//go:build linux

package local

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// fakeClaude writes its argv (NUL-separated), cwd and umask, then leaves a grandchild in a session
// of its own that outlives it, as a dev server started by a run would.
const fakeClaude = `#!/bin/sh
printf '%s\0' "$@" >"$LH_TEST_ARGV"
pwd >"$LH_TEST_CWD"
umask >"$LH_TEST_UMASK"
setsid -f sh -c 'echo $$ >"$LH_TEST_GRANDCHILD.tmp" && mv "$LH_TEST_GRANDCHILD.tmp" "$LH_TEST_GRANDCHILD"; exec sleep 600'
exit 0
`

// launchOutcome is what the Runner returned for the launch command.
type launchOutcome struct {
	mu   sync.Mutex
	seen bool
	code int
	err  error
}

// recordingRunner is ExecRunner that keeps the outcome of the launch command.
func recordingRunner(o *launchOutcome) workplace.Runner {
	run := workplace.ExecRunner()
	return func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
		out, code, err := run(ctx, timeout, name, args...)
		if name == "sh" && len(args) > 1 && args[1] == launchScript {
			o.mu.Lock()
			o.seen, o.code, o.err = true, code, err
			o.mu.Unlock()
		}
		return out, code, err
	}
}

func randomUUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// waitFor polls cond every 50 ms until it holds or d has passed.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// readPid reads a pid written by echo; ok is false while the file does not exist or is still
// empty: `echo $$ >file` creates the file before it writes.
func readPid(t *testing.T, path string) (int, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(text)
	if err != nil || pid <= 0 {
		t.Fatalf("pid file %s = %q, want a pid", path, b)
	}
	return pid, true
}

// procState is the state field of /proc/<pid>/stat; ok is false when the process is gone.
func procState(t *testing.T, pid int) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	// A process that goes between open and read answers ESRCH.
	if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || len(b) < i+3 {
		t.Fatalf("/proc/%d/stat = %q, want <pid> (<comm>) <state> ...", pid, b)
	}
	return string(b[i+2]), true
}

// killPidFile sends SIGKILL to the pid in path, if there is one.
func killPidFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// TestRealRunKillReachesGrandchild runs the launch script in a real shell, against real /proc,
// from a repo whose path holds a space: claude gets the prompt byte for byte, runs in the
// worktree with the umask of the tick, the launch frees the Runner's pipes, and Kill reaches
// what claude left behind.
func TestRealRunKillReachesGrandchild(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("no setsid")
	}
	// A umask that is neither the usual 022 nor the 077 of the pid and log files.
	old := syscall.Umask(0o027)
	t.Cleanup(func() { syscall.Umask(old) })
	repo, _ := newRepoNamed(t, "repo met spatie")
	bin, out := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaude), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	argvFile, cwdFile, grandchildFile := filepath.Join(out, "argv"), filepath.Join(out, "cwd"), filepath.Join(out, "grandchild")
	umaskFile := filepath.Join(out, "umask")
	t.Setenv("LH_TEST_ARGV", argvFile)
	t.Setenv("LH_TEST_UMASK", umaskFile)
	t.Setenv("LH_TEST_CWD", cwdFile)
	t.Setenv("LH_TEST_GRANDCHILD", grandchildFile)
	t.Cleanup(func() { killPidFile(grandchildFile) })

	launch := &launchOutcome{}
	a := &Adapter{
		RepoDir: repo, DefaultBranch: "main", Repo: "o/r",
		StateDir: t.TempDir(), Home: t.TempDir(),
		Run: recordingRunner(launch),
	}
	ws := prepare(t, a, "issue-1-x")
	const prompt = "it's \"$HOME\" $(id); echo\nnext"
	sid := randomUUID(t)
	spec := workplace.LaunchSpec{Profile: "infra", Mode: "start", Prompt: prompt, InstructionsFile: "/abs/instr.md", SessionID: sid}
	sess, err := a.Spawn(t.Context(), ws, spec)
	if err != nil {
		t.Fatalf("Spawn = %v, want accepted", err)
	}

	launch.mu.Lock()
	seen, code, launchErr := launch.seen, launch.code, launch.err
	launch.mu.Unlock()
	if !seen || code != 0 || launchErr != nil {
		t.Fatalf("launch outcome: seen %v, code %d, err %v; want exit 0 without error", seen, code, launchErr)
	}

	var grandchild int
	if !waitFor(10*time.Second, func() bool {
		var ok bool
		grandchild, ok = readPid(t, grandchildFile)
		return ok
	}) {
		t.Fatalf("no grandchild pid in %s after 10 s", grandchildFile)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if argv[0] != "-p" {
		t.Errorf("argv[0] = %q, want -p (argv %q)", argv[0], argv)
	}
	if got := argv[len(argv)-1]; got != prompt {
		t.Errorf("last argument = %q, want the prompt %q byte for byte", got, prompt)
	}
	cwd, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSuffix(string(cwd), "\n"); got != ws.Path {
		t.Errorf("claude cwd = %q, want the worktree %q", got, ws.Path)
	}
	mask, err := os.ReadFile(umaskFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(mask)); got != "0027" {
		t.Errorf("claude umask = %q, want the tick's 0027", got)
	}
	s, err := a.store()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{s.logPath(sess.Key), s.pidPath(ws.ID, sess.Key)} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v; want mode 0600", p, fi, err)
		}
	}

	var sessions []workplace.Session
	if !waitFor(10*time.Second, func() bool {
		var err error
		sessions, err = a.Sessions(t.Context(), ws.ID)
		if err != nil {
			t.Fatalf("Sessions = %v", err)
		}
		return len(sessions) == 1 && sessions[0].Key == sess.Key && sessions[0].State == "done"
	}) {
		t.Fatalf("Sessions after 10 s = %+v, want [%s done]", sessions, sess.Key)
	}

	if st, ok := procState(t, grandchild); !ok || st == "Z" {
		t.Fatalf("grandchild %d before Kill: state %q, alive %v; want a live process", grandchild, st, ok)
	}
	if err := a.Kill(t.Context(), ws.ID, []string{sid}); err != nil {
		t.Fatalf("Kill = %v, want nil", err)
	}
	var st string
	if !waitFor(5*time.Second, func() bool {
		var ok bool
		st, ok = procState(t, grandchild)
		return !ok || st == "Z"
	}) {
		t.Errorf("grandchild %d after Kill: state %q, want gone or Z within 5 s", grandchild, st)
	}
}
