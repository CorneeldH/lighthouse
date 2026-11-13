//go:build !windows

package deadline

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func in(d time.Duration) time.Time { return time.Now().Add(d) }

func sh(script string) []string { return []string{"sh", "-c", script} }

func goneWithin(pid int, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func pidWithin(t *testing.T, file string, d time.Duration) int {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if b, err := os.ReadFile(file); err == nil && strings.TrimSpace(string(b)) != "" {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no pid written")
	return 0
}

func TestPassesExitCodeThrough(t *testing.T) {
	code, _, err := RunCapture(in(30*time.Second), sh("exit 5"), Options{})
	if err != nil || code != 5 {
		t.Fatalf("code, err = %d, %v; want 5", code, err)
	}
}

func TestExpiredDeadlineReturns124AndKillsTheGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	code, _, err := RunCapture(in(time.Second), sh("sleep 30 & echo $! > "+pidfile+"; wait"), Options{Grace: 10 * time.Second})
	if err != nil || code != TimeoutExit || time.Since(start) > 10*time.Second {
		t.Fatalf("code, err = %d, %v after %v", code, err, time.Since(start))
	}
	if !goneWithin(pidWithin(t, pidfile, time.Second), 5*time.Second) {
		t.Fatal("grandchild survived the deadline")
	}
}

func TestSigtermIgnorerIsKilledAfterGrace(t *testing.T) {
	start := time.Now()
	code, _, err := RunCapture(in(time.Second), sh("trap '' TERM; sleep 30"), Options{Grace: time.Second})
	if err != nil || code != TimeoutExit || time.Since(start) > 6*time.Second {
		t.Fatalf("code, err = %d, %v after %v", code, err, time.Since(start))
	}
}

func TestPastDeadlineDoesNotStart(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	code, out, err := RunCapture(in(-time.Second), []string{"touch", marker}, Options{})
	if err != nil || code != TimeoutExit || out != nil {
		t.Fatalf("got %d, %q, %v", code, out, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("command ran")
	}
}

func TestMissingCommandReturns127AndNoOutput(t *testing.T) {
	code, out, err := RunCapture(in(30*time.Second), []string{"lighthouse-does-not-exist-xyz"}, Options{})
	if err != nil || code != NotFoundExit || len(out) != 0 {
		t.Fatalf("got %d, %q, %v", code, out, err)
	}
}

func TestNotExecutableIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gate.sh")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, err := RunCapture(in(30*time.Second), []string{file}, Options{})
	if err == nil || code == NotFoundExit {
		t.Fatalf("got %d, %v; want a start error, not 127", code, err)
	}
}

func TestCaptureReturnsStdoutAndStderrTogether(t *testing.T) {
	code, out, err := RunCapture(in(30*time.Second), sh("echo a; echo b >&2; exit 3"), Options{})
	if err != nil || code != 3 || strings.Join(strings.Fields(string(out)), " ") != "a b" {
		t.Fatalf("got %d, %q, %v", code, out, err)
	}
}

func TestCaptureSurvivesMoreThanAPipeBuffer(t *testing.T) {
	code, out, err := RunCapture(in(30*time.Second), sh("head -c 200000 /dev/zero | tr '\\0' x"), Options{})
	if err != nil || code != 0 || len(out) != 200000 {
		t.Fatalf("got %d, %d bytes, %v", code, len(out), err)
	}
}

func TestStdinDirAndEnvReachTheChild(t *testing.T) {
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	code, out, err := RunCapture(in(30*time.Second), sh("pwd -P; cat; echo $X"),
		Options{Dir: dir, Stdin: []byte("payload"), Env: append(os.Environ(), "X=y")})
	if err != nil || code != 0 {
		t.Fatalf("got %d, %v", code, err)
	}
	if got := strings.Fields(string(out)); len(got) != 2 || got[0] != real || got[1] != "payloady" {
		t.Fatalf("out = %q", out)
	}
	if os.Getenv("X") != "" {
		t.Fatal("the child's env leaked into this process")
	}
}

func TestCaptureOnTimeoutReturns124AndWhatWasWritten(t *testing.T) {
	code, out, err := RunCapture(in(time.Second), sh("echo vroeg; sleep 30"), Options{Grace: time.Second})
	if err != nil || code != TimeoutExit || !strings.Contains(string(out), "vroeg") {
		t.Fatalf("got %d, %q, %v", code, out, err)
	}
}

func TestChildExitOf128PlusNPassesThrough(t *testing.T) {
	for _, rc := range []int{129, 130, 143} {
		code, _, err := RunCapture(in(30*time.Second), sh("exit "+strconv.Itoa(rc)), Options{})
		if err != nil || code != rc {
			t.Fatalf("exit %d: got %d, %v", rc, code, err)
		}
	}
}

func TestSignalReturnsInterruptedWithTheOutput(t *testing.T) {
	stop := make(chan os.Signal, 1)
	go func() {
		time.Sleep(500 * time.Millisecond)
		stop <- syscall.SIGTERM
	}()
	code, _, err := RunCapture(in(30*time.Second), sh("echo vroeg; sleep 30"), Options{Grace: time.Second, Stop: stop})
	var intr *Interrupted
	if !errors.As(err, &intr) {
		t.Fatalf("got %d, %v; want *Interrupted", code, err)
	}
	if intr.Signal != syscall.SIGTERM || !strings.Contains(string(intr.Output), "vroeg") {
		t.Fatalf("Interrupted = %v, %q", intr.Signal, intr.Output)
	}
}

func TestRunCaptureStopsTheGroupOnASignal(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		_, _, err := RunCapture(in(60*time.Second), sh("sleep 30 & echo $! > "+pidfile+"; wait"), Options{Grace: 5 * time.Second, Stop: stop})
		done <- err
	}()
	pid := pidWithin(t, pidfile, 5*time.Second)
	stop <- syscall.SIGINT
	select {
	case err := <-done:
		var intr *Interrupted
		if !errors.As(err, &intr) || intr.Signal != syscall.SIGINT {
			t.Fatalf("err = %v, want *Interrupted(SIGINT)", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunCapture did not return after the signal")
	}
	if !goneWithin(pid, 5*time.Second) {
		t.Fatal("grandchild survived the signal")
	}
}

func TestRunCaptureDoesNotWaitForAGrandchild(t *testing.T) {
	start := time.Now()
	code, out, err := RunCapture(in(30*time.Second), sh("sleep 60 & echo ok"), Options{})
	if err != nil || code != 0 || string(out) != "ok\n" || time.Since(start) > 2*time.Second {
		t.Fatalf("got %d, %q, %v after %v", code, out, err, time.Since(start))
	}
}
