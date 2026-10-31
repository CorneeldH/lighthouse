// Package workplace is the dispatcher's view of a forge workspace: prepare one,
// start an agent in it, read its sessions, stop it and remove the workspace.
package workplace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Workspace is one forge workspace.
type Workspace struct{ ID, Path, Branch string }

// Session is one agent session in a workspace.
type Session struct {
	Key, WorkspaceID, State, SessionID string
	UpdatedAt                          time.Time // zero when forge sent none or it did not parse
}

// Stand is the coarse state of a session.
type Stand int

// The stands a session can have.
const (
	Working Stand = iota
	Waiting
	Done
)

// Stand maps the forge state: "working" is Working, "done" is Done, anything
// else (idle, null) is Waiting.
func (s Session) Stand() Stand {
	switch s.State {
	case "working":
		return Working
	case "done":
		return Done
	default:
		return Waiting
	}
}

// LaunchSpec describes the agent a Spawn starts.
type LaunchSpec struct {
	Repo      string
	Issue     int
	SessionID string
	Mode      string // "start" or "resume"
	Profile   string
	Prompt    string
	// Native is the native lane: --model sonnet.
	Native bool
	// InstructionsFile is absolute; passed as --append-system-prompt-file.
	InstructionsFile string
	// MCPConfig is absolute or empty; it comes from the profile.
	MCPConfig string
}

// Workplace is the forge side of a run.
type Workplace interface {
	Prepare(ctx context.Context, branch string) (Workspace, error)
	Spawn(ctx context.Context, ws Workspace, l LaunchSpec) (Session, error)
	Sessions(ctx context.Context, workspaceID string) ([]Session, error)
	Stop(ctx context.Context, workspaceID string, sessionIDs []string) error
	Remove(ctx context.Context, workspaceID string) error
	// Discard forgets a launch whose agent never showed up: a target that
	// starts late must find nothing to start. Nothing left to forget is no error.
	Discard(ctx context.Context, workspaceID string) error
}

// Sentinels; adapters wrap them with %w.
var (
	// ErrNotAccepted: the spawn was refused, so no agent started.
	ErrNotAccepted = errors.New("workplace: spawn not accepted")
	// ErrUncertain: the outcome is unknown (timeout, broken connection).
	ErrUncertain = errors.New("workplace: outcome uncertain")
	// ErrDirty: the workspace holds uncommitted or unpushed work.
	ErrDirty = errors.New("workplace: workspace is dirty")
	// ErrGone: the forge no longer knows the workspace (a 404).
	ErrGone = errors.New("workplace: workspace is gone")
)

// Runner runs one subprocess. err is non-nil only when the command could not
// run or timed out; otherwise code is the exit code and a non-zero code is no
// error.
type Runner func(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout string, code int, err error)

// ExecRunner runs commands with os/exec. An error it produces carries the
// first 200 bytes of stderr. A non-zero exit is no error, so its stderr is not
// passed on: the Runner contract has no place for it.
func ExecRunner() Runner {
	return func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		var out bytes.Buffer
		var stderr headBuffer
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		// A grandchild that outlives the kill may hold the stdout pipe open.
		cmd.WaitDelay = 2 * time.Second
		err := cmd.Run()
		if ctx.Err() != nil {
			return out.String(), -1, withStderr(ctx.Err(), stderr.String())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() < 0 {
				return out.String(), -1, withStderr(fmt.Errorf("command ended without an exit code: %w", err), stderr.String())
			}
			return out.String(), exitErr.ExitCode(), nil
		}
		if err != nil {
			return out.String(), -1, withStderr(err, stderr.String())
		}
		return out.String(), 0, nil
	}
}

const stderrLimit = 200

func withStderr(err error, stderr string) error {
	if s := clip(stderr, stderrLimit); s != "" {
		return fmt.Errorf("%w (stderr: %s)", err, s)
	}
	return err
}

// headBuffer keeps the first bytes written to it and drops the rest, so a
// chatty command cannot fill memory.
type headBuffer struct{ b []byte }

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := 4*stderrLimit - len(h.b); room > 0 {
		h.b = append(h.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (h *headBuffer) String() string { return string(h.b) }

// clip trims s and cuts it to at most n bytes, never inside a UTF-8 character.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
