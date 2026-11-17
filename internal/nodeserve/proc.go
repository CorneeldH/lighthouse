package nodeserve

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

type stopInput struct {
	ID   string `path:"id"`
	Body nodeapi.StopRequest
}

func (h *handler) registerProc() {
	huma.Post(h.api, nodeapi.PathStop, h.stop, func(op *huma.Operation) {
		op.OperationID = "post-stop"
		op.DefaultStatus = http.StatusNoContent
	})
	huma.Post(h.api, nodeapi.PathKill, h.kill, func(op *huma.Operation) {
		op.OperationID = "post-kill"
		op.DefaultStatus = http.StatusNoContent
	})
}

// stop runs Stop of the node's adapter with the profile of the run. A node without unit_prefix
// has no systemd unit (a Mac): there stop is Kill. That fallback lives here only; on the adapter
// an empty unit_prefix stays an error of Stop, so a misconfigured tick host never drops to Kill.
func (h *handler) stop(ctx context.Context, in *stopInput) (*struct{}, error) {
	a, err := h.procAdapter(ctx, in)
	if err != nil {
		return nil, err
	}
	if h.d.Forge.UnitPrefix == "" {
		return nil, procError("kill", in.ID, a.Kill(ctx, in.ID, in.Body.SessionIDs))
	}
	return nil, procError("stop", in.ID, a.Stop(ctx, in.ID, in.Body.SessionIDs))
}

// kill runs Kill of the node's adapter with the profile of the run: with software in the
// devcontainers, never on the host.
func (h *handler) kill(ctx context.Context, in *stopInput) (*struct{}, error) {
	a, err := h.procAdapter(ctx, in)
	if err != nil {
		return nil, err
	}
	return nil, procError("kill", in.ID, a.Kill(ctx, in.ID, in.Body.SessionIDs))
}

// procAdapter checks the request before any call (workspace id, profile, every session id) and
// that the workspace resolves, and returns a copy of the node's adapter with the run's profile.
func (h *handler) procAdapter(ctx context.Context, in *stopInput) (*kennforge.Adapter, error) {
	if err := checkWorkspaceID(in.ID); err != nil {
		return nil, err
	}
	if !h.d.Profiles[in.Body.Profile] {
		return nil, badRequest("profile %q is not a profile of this node", in.Body.Profile)
	}
	for _, sid := range in.Body.SessionIDs {
		if err := checkSession(sid); err != nil {
			return nil, err
		}
	}
	if _, err := h.resolve(ctx, in.ID); err != nil {
		return nil, err
	}
	a := *h.adapter // a copy per request: the profile is the run's, not the node's
	a.Profile = in.Body.Profile
	return &a, nil
}

// checkSession is 400 for a session id that is not safe, and for one that pgrep would take as
// an option. An empty id would match every process.
func checkSession(sid string) error {
	if !workplace.SafeSession.MatchString(sid) || strings.HasPrefix(sid, "-") {
		return badRequest("session id %q is not safe", sid)
	}
	return nil
}

// procError maps an error of Stop or Kill: a workspace the forge no longer knows is 404
// workspace_gone, anything else 500, never a stopped answer.
func procError(what, id string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workplace.ErrGone):
		return &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeWorkspaceGone, Detail: err.Error()}
	}
	return internal("%s in %s: %v", what, id, err)
}
