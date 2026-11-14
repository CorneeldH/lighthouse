// Package deadline runs a command until an absolute deadline, then stops its process group. It
// ports pack/core/lib/deadline.py (`run_until` with capture and raise_on_signal).
//
// A PreToolUse hook that hits its own timeout is fail-open: Claude sees silence, and the gate is
// skipped unnoticed. By ending the gate itself on a deadline, well within the hook timeout, and
// stopping the whole child, grandchildren included, it fails loudly instead.
package deadline

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const (
	// TimeoutExit is the code of a command the deadline ended.
	TimeoutExit = 124
	// NotFoundExit is the code of a command that does not exist.
	NotFoundExit = 127
)

// defaultGrace is the wait between SIGTERM and SIGKILL when Options.Grace is zero.
const defaultGrace = 10 * time.Second

// Options are the optional parts of RunCapture.
type Options struct {
	// Grace is the wait between SIGTERM and SIGKILL (default 10 s).
	Grace time.Duration
	// Dir is the child's working directory (default: this process's).
	Dir string
	// Stdin is fed to the child through a temporary file; nil gives it no input.
	Stdin []byte
	// Env is the child's whole environment; nil inherits this process's.
	Env []string
	// Stop delivers the signals that end the run. With nil, RunCapture subscribes to SIGTERM,
	// SIGINT and SIGHUP (what the platform has) itself, and unsubscribes on return.
	Stop <-chan os.Signal
}

// Interrupted is the error of a run that a signal stopped, after the group was stopped.
type Interrupted struct {
	Signal os.Signal
	// Output is what the child wrote up to then.
	Output []byte
}

func (e *Interrupted) Error() string { return fmt.Sprintf("interrupted by signal: %v", e.Signal) }

// RunCapture runs argv until deadline. Stdout and stderr go together to one temporary file, never
// a pipe: a pipe fills up at 64 KiB, and a Wait on a pipe also waits for every grandchild that
// keeps it open.
//
// A deadline already passed returns (TimeoutExit, nil, nil) without a start; a missing command
// (NotFoundExit, nil, nil); a command that exists but cannot start (EACCES, say) is an error. After
// the deadline the group gets SIGTERM, then SIGKILL after Grace, and the code is TimeoutExit with
// what was written. A signal stops the group the same way and gives an *Interrupted; a second one
// during the grace skips it. A child killed by a signal has code 128 + the signal.
func RunCapture(deadline time.Time, argv []string, o Options) (code int, output []byte, err error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return TimeoutExit, nil, nil
	}
	grace := o.Grace
	if grace <= 0 {
		grace = defaultGrace
	}
	stop := o.Stop
	if stop == nil {
		ch := make(chan os.Signal, 2)
		signal.Notify(ch, forwarded...)
		defer signal.Stop(ch)
		stop = ch
	}

	out, err := os.CreateTemp("", "deadline-out-")
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = out.Close(); _ = os.Remove(out.Name()) }()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = o.Dir, o.Env
	cmd.Stdout, cmd.Stderr = out, out
	if o.Stdin != nil {
		in, err := os.CreateTemp("", "deadline-in-")
		if err != nil {
			return 0, nil, err
		}
		defer func() { _ = in.Close(); _ = os.Remove(in.Name()) }()
		if _, err := in.Write(o.Stdin); err != nil {
			return 0, nil, err
		}
		if _, err := in.Seek(0, io.SeekStart); err != nil {
			return 0, nil, err
		}
		cmd.Stdin = in
	}
	InOwnGroup(cmd)
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return NotFoundExit, nil, nil
		}
		return 0, nil, err
	}

	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	timer := time.NewTimer(remaining)
	defer timer.Stop()

	var got os.Signal
	select {
	case <-done:
		return exitCode(cmd), readAll(out), nil
	case <-timer.C:
		code = TimeoutExit
	case got = <-stop:
	}
	signalGroup(cmd, syscall.SIGTERM)
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case <-done:
	case <-graceTimer.C:
		KillGroup(cmd)
		<-done
	case <-stop:
		KillGroup(cmd)
		<-done
	}
	if got != nil {
		return 0, nil, &Interrupted{Signal: got, Output: readAll(out)}
	}
	return code, readAll(out), nil
}

// exitCode is the child's exit code, 128 + the signal when a signal ended it.
func exitCode(cmd *exec.Cmd) int {
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return cmd.ProcessState.ExitCode()
}

// readAll reads everything back from the shared file, wherever the write position is.
func readAll(f *os.File) []byte {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	b, _ := io.ReadAll(f)
	return b
}
