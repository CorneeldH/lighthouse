package usage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Outcome says what a refresh or tick did.
type Outcome string

// The outcomes of Refresh and Tick.
const (
	Refreshed Outcome = "refreshed"
	Fresh     Outcome = "fresh"  // within the TTL, CLI not started
	Busy      Outcome = "busy"   // another refresh holds the lock
	Idle      Outcome = "idle"   // tick: no claude process
	Failed    Outcome = "failed" // old cache kept; the error says why
)

const refreshTimeout = 60 * time.Second // deadline of the context RunUsage receives
const maxStderr = 200                   // bytes of the claude stderr line kept in an error
const pgrepTimeout = 5 * time.Second    // a hanging pgrep counts as "cannot measure"
const waitDelay = 5 * time.Second       // cmd.WaitDelay: a grandchild holding stdout does not block Output after the kill

// Meter measures the usage windows and keeps the cache and the alarm file.
// Its fields are the seams the tests replace.
type Meter struct {
	Paths         Paths
	Config        Config
	Now           func() time.Time
	Loc           *time.Location
	RunUsage      func(ctx context.Context, workdir string) ([]byte, error)
	ClaudeRunning func() bool
}

// NewMeter returns a Meter on the real clock, time.Local, the claude CLI and pgrep.
func NewMeter(p Paths, c Config) Meter {
	return Meter{
		Paths:         p,
		Config:        c,
		Now:           time.Now,
		Loc:           time.Local,
		RunUsage:      runUsage,
		ClaudeRunning: func() bool { return claudeRunning(pgrep) },
	}
}

// Refresh measures the windows and writes the alarm, then the cache. Without
// force, a cache younger than the TTL is kept and the CLI is not started. On
// any error, a panic included, the old cache stays and the outcome is Failed.
func (m Meter) Refresh(force bool) (out Outcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = Failed, fmt.Errorf("usage refresh panicked: %v", r)
		}
	}()

	// The workdir lives in Paths.Dir, so this also creates the lock's directory.
	if err := os.MkdirAll(m.Paths.Workdir(), 0o755); err != nil {
		return Failed, err
	}
	unlock, held, err := tryLock(m.Paths.Lock())
	if err != nil {
		return Failed, err
	}
	if !held {
		return Busy, nil
	}
	defer unlock()

	now := m.Now()
	if !force {
		// A generated_at in the future is a skewed clock, not a fresh cache.
		if _, at, ok := ReadState(m.Paths.Cache()); ok {
			if age := now.Sub(at); age >= 0 && age < m.Config.TTL {
				return Fresh, nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	envelope, err := m.RunUsage(ctx, m.Paths.Workdir())
	if err != nil {
		return Failed, fmt.Errorf("claude /usage: %w", err)
	}
	w, err := Parse(envelope, now, m.Loc)
	if err != nil {
		return Failed, err
	}
	s := NewState(w, now, m.Loc)
	// The alarm before the cache: if the alarm fails, the cache is untouched.
	if err := WriteAlarm(m.Paths.Alarm(), s, m.Config); err != nil {
		return Failed, err
	}
	if err := WriteState(m.Paths.Cache(), s); err != nil {
		return Failed, err
	}
	return Refreshed, nil
}

// Tick prunes an expired alarm, then refreshes only while a claude process
// runs. Pruning comes before that gate and its error does not stop the tick.
func (m Meter) Tick() (Outcome, error) {
	_, _ = PruneAlarm(m.Paths.Alarm(), m.Now())
	if !m.ClaudeRunning() {
		return Idle, nil
	}
	return m.Refresh(false)
}

// usageCommand is `claude -p /usage --output-format json` in workdir, marked
// with LIGHTHOUSE_USAGE_REFRESH=1 and killed when ctx ends.
func usageCommand(ctx context.Context, workdir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "claude", "-p", "/usage", "--output-format", "json")
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "LIGHTHOUSE_USAGE_REFRESH=1")
	cmd.WaitDelay = waitDelay
	return cmd
}

// runUsage runs the usage command. A failed exit carries the first line of
// stderr in the error, so "not logged in" shows instead of "exit status 1".
func runUsage(ctx context.Context, workdir string) ([]byte, error) {
	out, err := usageCommand(ctx, workdir).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		line, _, _ := strings.Cut(strings.TrimSpace(string(exitErr.Stderr)), "\n")
		line = strings.TrimSpace(line)
		if len(line) > maxStderr {
			line = line[:maxStderr]
		}
		if line != "" {
			err = fmt.Errorf("%w: %s", err, line)
		}
	}
	return out, err
}

// claudeRunning reads the pgrep result. Only exit code 1 means no claude
// process: a failure to measure (no pgrep, exit 2 or 3) is not a "no".
func claudeRunning(pgrep func() (exitCode int, err error)) bool {
	code, err := pgrep()
	return err != nil || code != 1
}

// pgrep runs `pgrep -x claude` and returns its exit code. An error means
// pgrep did not run to an exit code.
func pgrep() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pgrepTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, "pgrep", "-x", "claude").Run()
	var exitErr *exec.ExitError
	// A killed pgrep also yields an ExitError, but its code is -1: not a "no".
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}
