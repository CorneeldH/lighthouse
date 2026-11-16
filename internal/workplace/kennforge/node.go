package kennforge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// On a node other than self the adapter keeps the hub for the life cycle of the workspace (list,
// create, handoff, DELETE) and leaves everything it does on this host on self to lighthouse serve
// on that node, through Serve (#327): carrier and run instructions, follow, sessions, stop, kill
// and the measurements below. It never writes to StateDir/carriers/<node>/.

var (
	_ workplace.GitMeter         = (*Adapter)(nil)
	_ workplace.Relocator        = (*Adapter)(nil)
	_ workplace.PermissionReader = (*Adapter)(nil)
)

// serve returns the serve client of a.Node, or nil on self. An adapter on a node without one
// cannot reach the node: that is an error, never a fall-back to this host.
func (a *Adapter) serve() (*NodeClient, error) {
	if a.Node == "" {
		return nil, nil
	}
	if a.Serve == nil {
		return nil, fmt.Errorf("node %s: no lighthouse serve configured: %w: %w", a.Node, workplace.ErrNotAccepted, workplace.ErrNotMeasurable)
	}
	return a.Serve, nil
}

// nodeOnly is the serve client for a measurement that exists only on a node: on self it is
// ErrNotMeasurable, so a caller that asks it of self never reads a clean answer.
func (a *Adapter) nodeOnly(what string) (*NodeClient, error) {
	n, err := a.serve()
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, fmt.Errorf("%s: only on a node, the adapter is on self: %w", what, workplace.ErrNotMeasurable)
	}
	return n, nil
}

// serveGone decides a 404 workspace_gone of serve on workspace id. It is ErrGone only when the
// hub does not know the workspace either and reports the node reachable (the rule of #187, in
// confirmGone); a hub that still knows it, or cannot confirm, makes it ErrUncertain. It returns
// nil when err is no workspace_gone.
func (a *Adapter) serveGone(ctx context.Context, err error, id, what string) error {
	var ne *NodeError
	if !errors.As(err, &ne) || ne.Status != http.StatusNotFound || ne.Code != nodeapi.CodeWorkspaceGone {
		return nil
	}
	herr := a.Client.do(ctx, http.MethodGet, a.path(wsPath(id)), nil, 0, nil)
	if herr == nil {
		return fmt.Errorf("%s: %v, but the hub still knows workspace %s: %w", what, ne, id, workplace.ErrUncertain)
	}
	if nf := a.notFound(ctx, herr, what+": "+ne.Error()); nf != nil {
		return nf
	}
	return fmt.Errorf("%s: %v; hub: %v: %w", what, ne, herr, workplace.ErrUncertain)
}

// launch sends the carrier and the content of the run instructions to the node, before any
// handoff. Every failure is ErrNotAccepted (and never ErrUncertain: no handoff went out). A
// launch that may have written the carrier (5xx, or no answer) has it deleted again.
func (a *Adapter) launch(ctx context.Context, n *NodeClient, id string, l workplace.LaunchSpec) error {
	instr, err := os.ReadFile(l.InstructionsFile)
	if err != nil {
		return fmt.Errorf("run instructions: %v: %w", err, workplace.ErrNotAccepted)
	}
	_, err = n.Launch(ctx, id, nodeapi.LaunchRequest{
		Repo: l.Repo, Issue: l.Issue, SessionID: l.SessionID, Mode: l.Mode, Profile: l.Profile,
		Prompt: l.Prompt, Native: l.Native, MCPConfig: l.MCPConfig, Instructions: string(instr),
	})
	if err == nil {
		return nil
	}
	out := fmt.Errorf("launch on node %s: %v: %w", a.Node, err, workplace.ErrNotAccepted)
	var ne *NodeError
	if (errors.As(err, &ne) && ne.Status >= 500) || errors.Is(err, workplace.ErrUncertain) {
		if rmErr := n.DeleteCarrier(ctx, id); rmErr != nil {
			out = errors.Join(out, fmt.Errorf("remove carrier on node %s: %w", a.Node, rmErr))
		}
	}
	return out
}

// removeCarrier wipes the carrier of the workspace: on the node through serve, on self in
// StateDir. A missing carrier is no error.
func (a *Adapter) removeCarrier(ctx context.Context, id string) error {
	n, err := a.serve()
	if err != nil {
		return err
	}
	if n != nil {
		return n.DeleteCarrier(ctx, id)
	}
	return workplace.RemoveCarrier(a.StateDir, "", id)
}

// nodeSessions reads the sessions at the node's forge through serve; a workspace_gone counts
// as ErrGone only as serveGone allows.
func (a *Adapter) nodeSessions(ctx context.Context, n *NodeClient, id string) ([]workplace.Session, error) {
	ss, err := n.Sessions(ctx, id)
	if err == nil {
		return ss, nil
	}
	what := "read sessions of " + id + " on node " + a.Node
	if g := a.serveGone(ctx, err, id, what); g != nil {
		return nil, g
	}
	return nil, fmt.Errorf("%s: %w", what, err)
}

// nodeProc runs Stop or Kill on the node through serve, with the run's profile. A workspace the
// node's forge forgot leaves serve nothing to stop: no error once the hub confirms it is gone,
// ErrUncertain while it does not.
func (a *Adapter) nodeProc(ctx context.Context, what, id string, sessionIDs []string,
	call func(ctx context.Context, id, profile string, sessionIDs []string) error) error {
	err := call(ctx, id, a.Profile, sessionIDs)
	if err == nil {
		return nil
	}
	label := what + " in workspace " + id + " on node " + a.Node
	if g := a.serveGone(ctx, err, id, label); g != nil {
		if errors.Is(g, workplace.ErrGone) {
			return nil
		}
		return g
	}
	return fmt.Errorf("%s: %w", label, err)
}

// Dirty is workplace.Dirty on the worktree on the node, measured there by serve.
func (a *Adapter) Dirty(ctx context.Context, workspaceID string) (bool, error) {
	n, err := a.nodeOnly("dirty of " + workspaceID)
	if err != nil {
		return false, err
	}
	dirty, _, err := n.Git(ctx, workspaceID, "")
	if err != nil {
		return false, fmt.Errorf("dirty of %s on node %s: %w", workspaceID, a.Node, notMeasurable(err))
	}
	return dirty, nil
}

// Unpushed is workplace.Unpushed on the worktree on the node, with mergedHead empty or the full
// SHA of a merged PR's head.
func (a *Adapter) Unpushed(ctx context.Context, workspaceID, mergedHead string) (bool, error) {
	n, err := a.nodeOnly("unpushed of " + workspaceID)
	if err != nil {
		return false, err
	}
	_, unpushed, err := n.Git(ctx, workspaceID, mergedHead)
	if err != nil {
		return false, fmt.Errorf("unpushed of %s on node %s: %w", workspaceID, a.Node, notMeasurable(err))
	}
	return unpushed, nil
}

// Relocate is dispatch.Relocate of the start command onto the worktree on the node.
func (a *Adapter) Relocate(ctx context.Context, workspaceID, command string) (string, error) {
	n, err := a.nodeOnly("relocate in " + workspaceID)
	if err != nil {
		return "", err
	}
	out, err := n.Relocate(ctx, workspaceID, command)
	if err != nil {
		return "", fmt.Errorf("relocate in %s on node %s: %w", workspaceID, a.Node, err)
	}
	return out, nil
}

// Permission is the raw permission note of the run on the node; nil, nil without one.
func (a *Adapter) Permission(ctx context.Context, sessionID string) ([]byte, error) {
	n, err := a.nodeOnly("permission of " + sessionID)
	if err != nil {
		return nil, err
	}
	data, err := n.Permission(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("permission of %s on node %s: %w", sessionID, a.Node, notMeasurable(err))
	}
	return data, nil
}

// notMeasurable makes any failure of a measurement on the node not measurable: a 4xx of serve
// carries no sentinel of its own, and a measurement that failed is never an answer.
func notMeasurable(err error) error {
	if errors.Is(err, workplace.ErrNotMeasurable) {
		return err
	}
	return fmt.Errorf("%w: %w", err, workplace.ErrNotMeasurable)
}
