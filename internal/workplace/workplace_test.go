package workplace

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// skipOnWindows skips t on native Windows, where the behaviour under test does not exist.
func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why + "; see docs/host-assumptions.md")
	}
}

func TestStand(t *testing.T) {
	cases := []struct {
		state string
		want  Stand
	}{
		{"working", Working},
		{"done", Done},
		{"idle", Waiting},
		{"", Waiting},
	}
	for _, c := range cases {
		if got := (Session{State: c.state}).Stand(); got != c.want {
			t.Errorf("state %q: got %v, want %v", c.state, got, c.want)
		}
	}
}

func TestExecRunner(t *testing.T) {
	run := ExecRunner()
	ctx := context.Background()

	out, code, err := run(ctx, 5*time.Second, "sh", "-c", "echo hi; exit 0")
	if err != nil || code != 0 || out != "hi\n" {
		t.Errorf("success: out=%q code=%d err=%v", out, code, err)
	}
	_, code, err = run(ctx, 5*time.Second, "sh", "-c", "exit 3")
	if err != nil || code != 3 {
		t.Errorf("non-zero exit: code=%d err=%v, want code 3 and no error", code, err)
	}
	if _, _, err = run(ctx, 5*time.Second, "lighthouse-no-such-binary"); err == nil {
		t.Error("missing binary: want error")
	}
	if _, _, err = run(ctx, 100*time.Millisecond, "sleep", "5"); err == nil {
		t.Error("timeout: want error")
	}
}

func TestExecRunnerTimeoutWithGrandchildHoldingStdout(t *testing.T) {
	// The right outcome takes the 200 ms timeout plus the 2 s WaitDelay; waiting for the
	// grandchild takes 120 s. The bound is a quarter of the wrong outcome (#338).
	start := time.Now()
	_, _, err := ExecRunner()(context.Background(), 200*time.Millisecond, "sh", "-c", "sleep 120 & wait")
	if err == nil {
		t.Fatal("want timeout error")
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("returned after %v, want near the 200ms timeout", d)
	}
}

func TestExecRunnerSignalKilledIsError(t *testing.T) {
	skipOnWindows(t, "Windows has no POSIX signals: kill -9 $$ in Git Bash does not end the process by a signal")
	_, code, err := ExecRunner()(context.Background(), 5*time.Second, "sh", "-c", "kill -9 $$")
	if err == nil {
		t.Errorf("killed by signal: want error, got code %d and no error", code)
	}
}

func TestExecRunnerErrorCarriesStderr(t *testing.T) {
	run := ExecRunner()
	_, _, err := run(context.Background(), 300*time.Millisecond, "sh", "-c", "echo late >&2; sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "stderr: late") {
		t.Errorf("timeout: err = %v, want DeadlineExceeded with stderr late", err)
	}
	_, code, err := run(context.Background(), 5*time.Second, "sh", "-c", "echo fine >&2; exit 2")
	if err != nil || code != 2 {
		t.Errorf("non-zero exit: code=%d err=%v, want code 2 and no error", code, err)
	}
	skipOnWindows(t, "Windows has no POSIX signals: kill -9 $$ in Git Bash does not end the process by a signal")
	_, _, err = run(context.Background(), 5*time.Second, "sh", "-c", "echo boom >&2; kill -9 $$")
	if err == nil || !strings.Contains(err.Error(), "stderr: boom") {
		t.Errorf("signal: err = %v, want stderr boom in it", err)
	}
}

func TestExecRunnerStderrIsClipped(t *testing.T) {
	skipOnWindows(t, "Windows has no POSIX signals: kill -9 $$ in Git Bash does not end the process by a signal")
	_, _, err := ExecRunner()(context.Background(), 5*time.Second, "sh", "-c", "printf '%0300d' 0 >&2; kill -9 $$")
	if err == nil {
		t.Fatal("want error")
	}
	if got := strings.Count(err.Error(), "0"); got != 200 {
		t.Errorf("stderr zeros in error = %d, want 200: %v", got, err)
	}
}

func TestClip(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"  short \n", "short"},
		{strings.Repeat("a", 200), strings.Repeat("a", 200)},
		{strings.Repeat("a", 201), strings.Repeat("a", 200)},
		// é is two bytes at 199-200: cutting at 200 would split it.
		{strings.Repeat("a", 199) + "é" + "z", strings.Repeat("a", 199)},
	}
	for _, c := range cases {
		if got := clip(c.in, 200); got != c.want {
			t.Errorf("clip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStandString(t *testing.T) {
	for s, want := range map[Stand]string{Working: "working", Waiting: "waiting", Done: "done"} {
		if got := s.String(); got != want {
			t.Errorf("Stand(%d).String() = %q, want %q", s, got, want)
		}
	}
}
