// Package gate holds the test-gate selectors: they map the files of a commit to what a test
// command should run, and run it. The commit-test-gate hook calls them through
// `lighthouse gate <name>`.
package gate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// Unchecked is the exit code for "nothing was tested". List it in
// `[test_gate] unchecked_exit_codes`; 2 cannot go there, pytest uses it.
const Unchecked = 3

// Env is what a gate reads besides its arguments. FilesPath is LIGHTHOUSE_GATE_FILES (empty
// is unset), Dir the directory to work in. The test command writes to Stdout and Stderr and
// inherits the stdin of the process.
type Env struct {
	Dir       string
	FilesPath string
	Stdout    io.Writer
	Stderr    io.Writer
}

// uncheckedError means nothing was tested; its text is the reason.
type uncheckedError string

func (e uncheckedError) Error() string { return string(e) }

func unchecked(format string, args ...any) error {
	return uncheckedError(fmt.Sprintf(format, args...))
}

// finish turns the outcome of a gate into its exit code. An unchecked error becomes
// `<name>: COULD NOT CHECK — <reason>` on stderr and Unchecked; any other error is not
// expected and treated the same, so a gate never goes green by accident.
func finish(name string, env Env, rc int, err error) int {
	if err == nil {
		return rc
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s: COULD NOT CHECK — %s\n", name, err)
	return Unchecked
}

// run starts cmd in env.Dir and returns its exit code. A command that stops on a signal is
// 128 plus the signal, never 0 or Unchecked. A command that cannot start is unchecked.
func run(cmd []string, env Env) (int, error) {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = env.Dir
	c.Stdin = os.Stdin
	c.Stdout = env.Stdout
	c.Stderr = env.Stderr
	err := c.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 0, unchecked("%s cannot start: %s", cmd[0], reason(err))
	}
	if code := exit.ExitCode(); code >= 0 {
		return code, nil
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return 1, nil
}

// reason is the innermost message of err: "no such file or directory", not the whole chain.
func reason(err error) string {
	var perr *os.PathError
	if errors.As(err, &perr) {
		return perr.Err.Error()
	}
	var eerr *exec.Error
	if errors.As(err, &eerr) {
		return eerr.Err.Error()
	}
	return err.Error()
}

// splitCommand returns what follows the first "--" in args, or an unchecked usage error.
func splitCommand(args []string, usage string) ([]string, error) {
	for i, a := range args {
		if a == "--" {
			if i+1 == len(args) {
				break
			}
			return args[i+1:], nil
		}
	}
	return nil, uncheckedError(usage)
}
