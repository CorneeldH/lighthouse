// Package procrun is the exec seam of the skill commands: they run `gh` and `git` through a
// Func, so a test can replay the calls (internal/scriptcase) instead of starting a process.
//
// It does not use forge.Client: that speaks HTTP with a token and words its errors differently,
// while the scripts read `HTTP 404` from the stderr of `gh`.
package procrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/deadline"
)

// Result is what a finished process left: its output and exit code.
type Result struct {
	Stdout, Stderr string
	Code           int
}

// Func runs name with args. dir is the working directory (empty: the current one); stdin nil
// gives the process no input.
type Func func(ctx context.Context, dir string, stdin *string, name string, args ...string) (Result, error)

// Timeout is the error of a process that outran its timeout and was killed.
type Timeout struct{ After time.Duration }

func (e *Timeout) Error() string { return fmt.Sprintf("timed out after %v", e.After) }

// NotFound is the error of a program that does not exist.
type NotFound struct{ Name string }

func (e *NotFound) Error() string { return fmt.Sprintf("%s: not found", e.Name) }

// waitDelay bounds the wait for pipes a grandchild keeps open after the process was killed.
const waitDelay = 2 * time.Second

// Exec runs the real process. A timeout gives *Timeout (the whole process group is killed), a
// missing program *NotFound, and an exit code other than 0 no error: it is Result.Code.
func Exec(timeout time.Duration) Func {
	return func(ctx context.Context, dir string, stdin *string, name string, args ...string) (Result, error) {
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(runCtx, name, args...)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if stdin != nil {
			cmd.Stdin = strings.NewReader(*stdin)
		}
		deadline.InOwnGroup(cmd)
		cmd.Cancel = func() error { deadline.KillGroup(cmd); return nil }
		cmd.WaitDelay = waitDelay
		err := cmd.Run()
		res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
			return res, &Timeout{After: timeout}
		case ctx.Err() != nil:
			return res, ctx.Err()
		case err == nil:
			return res, nil
		case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
			return res, &NotFound{Name: name}
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			res.Code = exit.ExitCode()
			return res, nil
		}
		return res, err
	}
}
