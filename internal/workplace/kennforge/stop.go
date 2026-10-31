package kennforge

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

var _ workplace.Workplace = (*Adapter)(nil)

const (
	stopTimeout  = 30 * time.Second
	killRetries  = 5
	killRetryGap = time.Second
)

// UnitName is the systemd unit forge_agent.sh gives a worktree: every byte outside
// [A-Za-z0-9_-] becomes a dash, so a multibyte character gives several.
func UnitName(profile, worktreePath string) string {
	base := filepath.Base(worktreePath)
	b := []byte(base)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			b[i] = '-'
		}
	}
	return "paia-forge-" + profile + "-" + string(b)
}

// Stop ends the processes of a workspace: its systemd unit and, for the software
// profile, the claude processes of the given sessions in the shared devcontainer.
// Anything it cannot measure is an error, never "gone".
func (a *Adapter) Stop(ctx context.Context, workspaceID string, sessionIDs []string) error {
	var w wireWorkspace
	if err := a.Client.do(ctx, http.MethodGet, wsPath(workspaceID), nil, 0, &w); err != nil {
		return fmt.Errorf("read workspace %s: %w", workspaceID, err)
	}
	if w.WorktreeDir == "" {
		return fmt.Errorf("workspace %s has no worktree_path", workspaceID)
	}
	unit := UnitName(a.Profile, w.WorktreeDir)
	// The exit code of stop is ignored: a unit that is not there fails, and is-active decides.
	if _, _, err := a.Run(ctx, stopTimeout, "systemctl", "--user", "stop", unit); err != nil {
		return fmt.Errorf("systemctl stop %s: %w", unit, err)
	}
	out, _, err := a.Run(ctx, stopTimeout, "systemctl", "--user", "is-active", unit)
	if err != nil {
		return fmt.Errorf("systemctl is-active %s: %w", unit, err)
	}
	if state := strings.TrimSpace(out); state != "inactive" && state != "failed" {
		return fmt.Errorf("unit %s is not stopped (is-active %q)", unit, state)
	}
	// With the software profile claude runs in the shared devcontainer, outside the cgroup
	// of the unit, and forge has no stop endpoint (spec section 6): kill it by session id.
	if a.Profile != "software" || len(sessionIDs) == 0 {
		return nil
	}
	return a.killInContainers(ctx, sessionIDs)
}

func (a *Adapter) killInContainers(ctx context.Context, sessionIDs []string) error {
	out, code, err := a.Run(ctx, stopTimeout, "podman", "ps",
		"--filter", "label=devcontainer.local_folder="+a.MainCheckout, "--format", "{{.ID}}")
	if err != nil {
		return fmt.Errorf("podman ps: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("podman ps: exit code %d", code)
	}
	for _, c := range strings.Fields(out) {
		for _, sid := range sessionIDs {
			if err := a.killSession(ctx, c, sid); err != nil {
				return err
			}
		}
	}
	return nil
}

// alive asks whether a process matching sid runs in container c: rc 0 yes, rc 1 no.
func (a *Adapter) alive(ctx context.Context, c, sid string) (bool, error) {
	_, code, err := a.Run(ctx, stopTimeout, "podman", "exec", c, "pgrep", "-f", sid)
	if err != nil {
		return false, fmt.Errorf("pgrep %s in %s: %w", sid, c, err)
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("pgrep %s in %s: exit code %d", sid, c, code)
}

func (a *Adapter) killSession(ctx context.Context, c, sid string) error {
	live, err := a.alive(ctx, c, sid)
	if err != nil || !live {
		return err
	}
	if _, _, err := a.Run(ctx, stopTimeout, "podman", "exec", c, "pkill", "-f", sid); err != nil {
		return fmt.Errorf("pkill %s in %s: %w", sid, c, err)
	}
	for i := 0; i < killRetries; i++ {
		a.sleep(killRetryGap)
		live, err := a.alive(ctx, c, sid)
		if err != nil {
			return err
		}
		if !live {
			return nil
		}
	}
	return fmt.Errorf("session %s still runs in container %s after pkill", sid, c)
}
