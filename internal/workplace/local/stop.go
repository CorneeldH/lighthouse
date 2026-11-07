package local

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
)

// Stop ends every process that carries the mark of the given sessions.
func (a *Adapter) Stop(ctx context.Context, workspaceID string, sessionIDs []string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	return a.end(ctx, workspaceID, sessionIDs)
}

// Kill ends the same processes as Stop; a local run has no gentler way to stop.
func (a *Adapter) Kill(ctx context.Context, workspaceID string, sessionIDs []string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	return a.end(ctx, workspaceID, sessionIDs)
}

// end sends SIGTERM to every process marked with a session, waits up to 5 s for them to go,
// then sends SIGKILL to the rest and waits once more. It needs no workspace record: the mark
// also finds what outlives claude, such as a dev server. An empty sid fails before any signal.
func (a *Adapter) end(_ context.Context, _ string, sessionIDs []string) error {
	for _, sid := range sessionIDs {
		if sid == "" {
			return errors.New("local workplace: empty session id")
		}
	}
	for _, sid := range sessionIDs {
		if err := a.endSession(sid); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) endSession(sid string) error {
	pids, err := a.procs().Marked(sid)
	if err != nil {
		return fmt.Errorf("session %s: %w", sid, err)
	}
	if len(pids) == 0 {
		return nil
	}
	if err := a.signalAll(sid, pids, syscall.SIGTERM); err != nil {
		return err
	}
	for range 5 {
		a.sleep(time.Second)
		if pids, err = a.procs().Marked(sid); err != nil {
			return fmt.Errorf("session %s: %w", sid, err)
		}
		if len(pids) == 0 {
			return nil
		}
	}
	if err := a.signalAll(sid, pids, syscall.SIGKILL); err != nil {
		return err
	}
	a.sleep(time.Second)
	if pids, err = a.procs().Marked(sid); err != nil {
		return fmt.Errorf("session %s: %w", sid, err)
	}
	if len(pids) > 0 {
		return fmt.Errorf("session %s: processes %v still run after SIGKILL", sid, pids)
	}
	return nil
}

func (a *Adapter) signalAll(sid string, pids []int, sig syscall.Signal) error {
	for _, pid := range pids {
		if err := a.procs().Signal(pid, sig); err != nil {
			return fmt.Errorf("session %s: %w", sid, err)
		}
	}
	return nil
}
