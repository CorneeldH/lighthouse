package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// launchScript detaches claude from the tick: setsid -f gives it a session of its own, and the
// redirections free the Runner's pipes. The child writes its own pid and start time (field 22
// of /proc/<pid>/stat) before exec, so the pid file describes claude. $$ inside $(...) is the
// pid of the shell that execs. umask 077 covers only the log and the pid file: the caller's
// umask, passed in as data, is back in place before claude starts.
const launchScript = `cd "$1" || exit 97
log=$2; pidf=$3; shift 3
mask=$(umask) || exit 97
umask 077
exec setsid -f sh -c '
st=$(sed "s/.*) //" /proc/$$/stat | cut -d" " -f20) && [ -n "$st" ] || exit 98
printf "%s %s\n" "$$" "$st" >"$0.tmp" && mv "$0.tmp" "$0" || exit 98
umask "$1" && shift || exit 98
exec "$@"' "$pidf" "$mask" "$@" </dev/null >>"$log" 2>&1`

const (
	pidWait       = 10 * time.Second
	pidPoll       = 200 * time.Millisecond
	launchTimeout = 30 * time.Second
)

func (a *Adapter) sleep(d time.Duration) {
	if a.Sleep == nil {
		time.Sleep(d)
		return
	}
	a.Sleep(d)
}

func notAccepted(format string, args ...any) error {
	return fmt.Errorf("%w: %w", workplace.ErrNotAccepted, fmt.Errorf(format, args...))
}

// Spawn starts claude -p detached in the worktree. Every step before the launch that fails is
// ErrNotAccepted and sends no launch; the launch is sent once, never again.
func (a *Adapter) Spawn(ctx context.Context, ws workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	if err := checkLocal(ws.ID); err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}
	carrier, err := workplace.NewCarrier(l, a.StateDir, a.now())
	if err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}
	s, err := a.store()
	if err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}
	if _, code, err := a.Run(ctx, gitTimeout, "sh", "-c", "command -v claude"); err != nil || code != 0 {
		return workplace.Session{}, notAccepted("claude is not on the PATH (exit code %d, err %v)", code, err)
	}
	pids, err := a.procs().Marked(l.SessionID)
	if err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}
	if len(pids) > 0 {
		return workplace.Session{}, notAccepted("session %s already has processes %v", l.SessionID, pids)
	}
	if err := s.ensureLogs(); err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix) // crypto/rand.Read never fails
	key := l.SessionID + "." + hex.EncodeToString(suffix)
	if err := s.putSession(ws.ID, sessRecord{Key: key, SessionID: l.SessionID, Mode: l.Mode, StartedAt: a.now().UTC()}); err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}

	// The environment of the run is the carrier's, sorted by key so the argv is stable; its
	// markVar is the mark Marked and Kill find the processes by.
	argv := []string{"-c", launchScript, "sh", ws.Path, s.logPath(key), s.pidPath(ws.ID, key), "env"}
	for _, k := range slices.Sorted(maps.Keys(carrier.Env)) {
		argv = append(argv, k+"="+carrier.Env[k])
	}
	argv = append(argv, "claude", "-p")
	argv = append(argv, carrier.Args...)
	_, code, runErr := a.Run(ctx, launchTimeout, "sh", argv...)
	if runErr == nil && code != 0 {
		if err := s.dropSession(ws.ID, key); err != nil {
			// Deliberately not the spec's ErrNotAccepted: the session record that could not be
			// dropped would be left orphaned. On ErrUncertain, rule 15 of decide (start uncertain)
			// Discards it.
			return workplace.Session{}, fmt.Errorf("%w: launch exit code %d; dropping its record: %w", workplace.ErrUncertain, code, err)
		}
		return workplace.Session{}, notAccepted("launch exit code %d", code)
	}
	appeared, err := a.waitPid(s, ws.ID, key)
	if err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrUncertain, err)
	}
	if !appeared {
		return workplace.Session{}, fmt.Errorf("%w: no pid file for %s after %s (launch error %v)", workplace.ErrUncertain, key, pidWait, runErr)
	}
	return workplace.Session{Key: key, WorkspaceID: ws.ID, SessionID: l.SessionID}, nil
}

// waitPid looks for the pid file, then up to pidWait/pidPoll times sleeps and looks again.
func (a *Adapter) waitPid(s store, id, key string) (bool, error) {
	for i := 0; ; i++ {
		_, _, ok, err := s.pid(id, key)
		if err != nil || ok {
			return ok, err
		}
		if i >= int(pidWait/pidPoll) {
			return false, nil
		}
		a.sleep(pidPoll)
	}
}

// Sessions lists the launches that have a pid file. A launch is working while the process of
// the pid file lives with the same start time; a process that cannot be measured fails the list.
func (a *Adapter) Sessions(ctx context.Context, workspaceID string) ([]workplace.Session, error) {
	if err := checkLocal(workspaceID); err != nil {
		return nil, err
	}
	if _, _, err := a.resolve(ctx, workspaceID); err != nil {
		return nil, err
	}
	s, err := a.store()
	if err != nil {
		return nil, err
	}
	recs, err := s.sessions(workspaceID)
	if err != nil {
		return nil, err
	}
	out := []workplace.Session{}
	for _, r := range recs {
		pid, start, ok, err := s.pid(workspaceID, r.Key)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		cur, alive, err := a.procs().StartTime(pid)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", r.Key, err)
		}
		state := "done"
		if alive && cur == start {
			state = "working"
		}
		out = append(out, workplace.Session{Key: r.Key, WorkspaceID: workspaceID, State: state, SessionID: r.SessionID, UpdatedAt: r.StartedAt})
	}
	return out, nil
}
