package kennforge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// A node other than self is reached through the fleet routes of the hub at Client. Measured
// against kenn-io/forge tag v0.7.0:
//
//   - internal/server/fleetapi/fleet_proxy.go registers /api/v1/fleet/hosts/{host_key}/... for
//     GET and POST .../workspaces, POST .../repo/{provider}/{owner}/{name}/workspaces, GET and
//     DELETE .../workspaces/{id}, GET .../workspaces/{id}/runtime, POST
//     .../workspaces/{id}/runtime/agent-handoffs and DELETE
//     .../workspaces/{id}/runtime/sessions/{session_key}. host_key is the member's node_id
//     (resolveFleetHostTarget). The hub sends the node's answer through unchanged.
//   - The hub answers 404 "fleet host not found" itself for a host_key it does not know, and
//     for an enrolled spoke it cannot reach (no tunnel, outbound disabled; memberReachable).
//     Its body is fleetHostNotFoundProblem through writeProblemResponse:
//     {"title":"Not Found","status":404,"detail":"fleet host not found","code":"notFound",
//     "details":{"hostKey":"node-a"}}. See confirmGone.
//   - There is no /workspaces/{id}/agent-sessions route on the hub: Sessions reads /runtime.

// path routes p, an /api/v1 path of forge, to a.Node: unchanged for self.
func (a *Adapter) path(p string) string {
	if a.Node == "" {
		return p
	}
	return "/api/v1/fleet/hosts/" + url.PathEscape(a.Node) + strings.TrimPrefix(p, "/api/v1")
}

// snapshotPath is the hub's snapshot with its members. Measured against forge v0.7.0: the
// route is GET /api/v1/snapshot (internal/server/fleetapi/fleet_routes.go, registered on the
// /api/v1 group in internal/server/huma_routes.go; frontend/openapi/openapi.yaml lists
// /snapshot and no /fleet/snapshot). Without include_peers=true a hub lists only itself
// (buildFleetSnapshot in internal/server/fleetapi/fleet_hub.go).
const snapshotPath = "/api/v1/snapshot?include_peers=true"

// wireSnapshot is the part of fleet.Snapshot (internal/fleet/types.go, v0.7.0) lighthouse
// reads. hosts[] is HostSummary; nodeID is the enrolled node_id, the host_key of the fleet
// routes. reachable is false for a member whose raw snapshot the hub could not fetch; a member
// the hub cannot reach at all (fetchMemberResults skips it) is not listed. Example:
//
//	{"protocolVersion":1,"generation":7,"hosts":[
//	  {"id":"…","configKey":"node-a","nodeID":"node-a","name":"node-a","kind":"remote",
//	   "federationRole":"spoke","platform":"macos","preferredTransport":"direct",
//	   "reachable":true}],
//	 "projects":[],"worktrees":[],"sessions":[],"workspaces":[]}
type wireSnapshot struct {
	Hosts *[]struct {
		NodeID    string `json:"nodeID"`
		Reachable bool   `json:"reachable"`
	} `json:"hosts"`
}

// nodeReachable asks the hub whether it reaches a.Node now. A node the snapshot does not list
// is not reachable. It never goes through path: the snapshot is the hub's own.
func (a *Adapter) nodeReachable(ctx context.Context) (bool, error) {
	var snap wireSnapshot
	if err := a.Client.do(ctx, http.MethodGet, snapshotPath, nil, 0, &snap); err != nil {
		return false, fmt.Errorf("read fleet snapshot: %w", err)
	}
	if snap.Hosts == nil {
		return false, errors.New("fleet snapshot has no hosts list")
	}
	for _, h := range *snap.Hosts {
		if h.NodeID == a.Node {
			return h.Reachable, nil
		}
	}
	return false, nil
}

// hubNotFound is the detail of the hub's own 404 for a host it does not know or reach.
const hubNotFound = "fleet host not found"

// confirmGone decides what the 404 se means. For self it is "gone". For another node the hub
// answers 404 also when it does not reach the node. Its own 404 is never "gone": the tunnel
// may be back by the time the snapshot is read. Any other 404 is "gone" only while the
// snapshot reports the node reachable. Otherwise confirmGone returns an error wrapping
// ErrUncertain.
func (a *Adapter) confirmGone(ctx context.Context, se *StatusError) error {
	if a.Node == "" {
		return nil
	}
	if strings.Contains(se.Detail, hubNotFound) {
		return fmt.Errorf("node %s: 404 not confirmed: %w", a.Node, workplace.ErrUncertain)
	}
	ok, err := a.nodeReachable(ctx)
	if err != nil {
		// %v: a failed snapshot read must not carry its own sentinel (ErrNotAccepted).
		return fmt.Errorf("node %s: 404 not confirmed: %v: %w", a.Node, err, workplace.ErrUncertain)
	}
	if !ok {
		return fmt.Errorf("node %s: 404 not confirmed: %w", a.Node, workplace.ErrUncertain)
	}
	return nil
}

// notFound turns a 404 in err into ErrGone, when confirmGone allows it, under the message
// what. It returns nil when err is no 404.
func (a *Adapter) notFound(ctx context.Context, err error, what string) error {
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusNotFound {
		return nil
	}
	if cerr := a.confirmGone(ctx, se); cerr != nil {
		return fmt.Errorf("%s: %v: %w", what, se, cerr)
	}
	return fmt.Errorf("%s: %v: %w", what, se, workplace.ErrGone)
}

// wireRuntime is the answer of GET /workspaces/{id}/runtime in forge v0.7.0:
// workspaceRuntimeResponse in internal/server/workspaceapi/types.go, with sessions of type
// localruntime.SessionInfo (internal/workspace/localruntime/manager.go), handler
// getWorkspaceRuntime in internal/server/workspaceapi/routes_handlers.go. A session has key,
// workspace_id, target_key, label, kind (agent, acp, shell, plain_shell, command), status
// (starting, running, exited, error, parked), display_region, created_at, and exited_at and
// exit_code once it exited.
//
// It has NO session_id: forge's runtime does not know claude's --session-id. Sessions on a
// node therefore gives every session an empty SessionID, and lighthouse sees its own agent
// there as foreign until #327. Example:
//
//	{"launch_targets":[…],"sessions":[
//	  {"key":"k1","workspace_id":"ws1","target_key":"paia-software","label":"Claude",
//	   "kind":"agent","status":"running","display_region":"main",
//	   "created_at":"2026-11-13T10:00:00Z"}]}
type wireRuntime struct {
	Sessions *[]struct {
		Key    string `json:"key"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	} `json:"sessions"`
}

// remoteSessions reads the sessions of a workspace on a.Node from /runtime. A shell is no
// agent session and is left out. State is the runtime status, with "exited" as "done"; every
// other status is a process that may still work, never done. A 404 is ErrGone only once
// confirmGone allows it.
func (a *Adapter) remoteSessions(ctx context.Context, workspaceID string) ([]workplace.Session, error) {
	var out wireRuntime
	if err := a.Client.do(ctx, http.MethodGet, a.path(wsPath(workspaceID))+"/runtime", nil, 0, &out); err != nil {
		if nf := a.notFound(ctx, err, "read sessions of "+workspaceID); nf != nil {
			return nil, nf
		}
		return nil, fmt.Errorf("read sessions of %s: %w", workspaceID, err)
	}
	if out.Sessions == nil {
		return nil, fmt.Errorf("sessions of %s: answer has no sessions list: %w", workspaceID, workplace.ErrUncertain)
	}
	res := make([]workplace.Session, 0, len(*out.Sessions))
	for _, e := range *out.Sessions {
		if e.Key == "" || e.Kind == "shell" || e.Kind == "plain_shell" {
			continue
		}
		state := e.Status
		if state == "exited" {
			state = "done"
		}
		res = append(res, workplace.Session{Key: e.Key, WorkspaceID: workspaceID, State: state})
	}
	return res, nil
}

// remoteStop is Stop and Kill on a.Node: no systemctl and no podman, those run on this host.
// The sessions must be readable; a failed read is an error, never "gone". Since /runtime has no
// session_id, no session of the node carries one of sessionIDs: each is already gone for
// lighthouse, and no DELETE .../runtime/sessions/{key} goes out (spec: until #327 Stop finds
// nothing to stop on another node).
func (a *Adapter) remoteStop(ctx context.Context, workspaceID string, sessionIDs []string) error {
	for _, sid := range sessionIDs {
		if sid == "" {
			return fmt.Errorf("stop in workspace %s on node %s: empty session id", workspaceID, a.Node)
		}
	}
	if _, err := a.remoteSessions(ctx, workspaceID); err != nil {
		return fmt.Errorf("stop in workspace %s on node %s: %w", workspaceID, a.Node, err)
	}
	return nil
}
