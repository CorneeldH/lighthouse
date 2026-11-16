package pins

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Runner runs a command and returns its combined stdout and stderr.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// execWaitDelay is how long ExecRunner waits for the output pipe to close after the command
// exited or its context ended: a child the command leaves behind (a daemon it starts) can hold
// the pipe open far longer.
const execWaitDelay = 2 * time.Second

// ExecRunner is the real Runner: exec.CommandContext, no shell, stdout and stderr combined. It
// returns at most execWaitDelay after the command exits, even when a child keeps its output open.
func ExecRunner(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = execWaitDelay
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// MeasureTool runs `<command> <version_args>` through run, within timeout, and compares the
// first semantic version in its output with the pin. Lower is behind (a prerelease of the pinned
// version too), higher is ahead (not judged, so never current), equal is current. A pin that
// did not load, a command not on PATH, a time-out, a failing command or output without a version
// is an error.
func MeasureTool(ctx context.Context, run Runner, p Pin, timeout time.Duration) Result {
	if p.Err != nil {
		return pluginError("%v", p.Err)
	}
	pinned, err := ParseVersion(p.Version)
	if err != nil {
		return pluginError("pin %s: %v", p.Name, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(ctx, p.Command, p.VersionArgs...)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return pluginError("%s is not on PATH", p.Command)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return pluginError("%s %v: time-out after %s", p.Command, p.VersionArgs, timeout)
	case err != nil:
		return pluginError("%s %v failed: %v", p.Command, p.VersionArgs, err)
	}
	installed, ok := FirstVersionIn(out)
	if !ok {
		return pluginError("no version found in the output of %s %v", p.Command, p.VersionArgs)
	}
	status := StatusCurrent
	switch installed.Compare(pinned) {
	case -1:
		status = StatusBehind
	case 1:
		status = StatusAhead
	}
	return Result{Status: status, Detail: fmt.Sprintf("installed %s (%s), pinned %s", installed, p.Command, pinned)}
}
