// Package nodeapi is the wire contract between the tick and lighthouse serve on a kenn-forge
// node: the route paths, the version and the bodies both sides share.
package nodeapi

import (
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Version is the version of the node-API. The tick refuses a node that reports another one.
const Version = 1

// Prefix is the root of every node route.
const Prefix = "/api/node/v1"

// PathInfo is GET info: the binary version, the node-API version and the platform.
const PathInfo = Prefix + "/info"

// The statemap routes: they touch only the node's state dir and look nothing up at its forge.
// {id} is the workspace id at the node's forge, {session} the session id of the run.
const (
	// PathLaunch is PUT: write the run instructions and the carrier; the body is a
	// LaunchRequest, the answer the workplace.Carrier written.
	PathLaunch = Prefix + "/workspaces/{id}/launch"
	// PathCarrier is DELETE: remove the carrier; a missing one is no error.
	PathCarrier = Prefix + "/workspaces/{id}/carrier"
	// PathPermission is GET: the raw permission.json of the run, 404 no_permission without one.
	PathPermission = Prefix + "/runs/{session}/permission"
)

// The git routes: each looks up the worktree of {id} at the node's own forge and works on
// that path; the tick never sends one.
const (
	// PathFollow is POST: workplace.FollowRemote on the worktree; the body is a FollowRequest.
	// Own work is 409 with an OwnWork body.
	PathFollow = Prefix + "/workspaces/{id}/follow"
	// PathRelocate is POST: dispatch.Relocate of the start command onto the worktree; body and
	// answer are a Relocation. A command it cannot relocate is 422 with the reason as detail.
	PathRelocate = Prefix + "/workspaces/{id}/relocate"
	// PathGit is GET with the query merged_head (empty or 40 lowercase hex): the GitState.
	PathGit = Prefix + "/workspaces/{id}/git"
	// PathSessions is GET: the agent sessions of the workspace at the node's forge, []Session.
	PathSessions = Prefix + "/workspaces/{id}/sessions"
)

// PathTranscript is GET with the queries profile (a profile the node knows) and from
// ("<file>:<offset>,…", files relative to the transcript dir with slashes): a tar
// (application/x-tar) with one member per transcript file of {session}, holding its bytes from
// the offset. Each member header carries the PAX records PAXSize and PAXFrom. Without the main
// transcript it is 404 no_transcript.
const PathTranscript = Prefix + "/workspaces/{id}/transcript/{session}"

// The PAX records of a member of PathTranscript.
const (
	// PAXSize is the length of the file on the node when it was read; the member ends there.
	PAXSize = "LIGHTHOUSE.size"
	// PAXFrom is the offset the member starts at: the offset asked for, or 0 when the file on
	// the node is shorter than that and the member holds the whole file.
	PAXFrom = "LIGHTHOUSE.from"
)

// The machine-readable codes of a 404. The tick maps on the code, never on the status alone.
const (
	CodeWorkspaceGone = "workspace_gone" // the node's forge no longer knows the workspace
	CodeNoTranscript  = "no_transcript"  // the session has no transcript on the node
	CodeNoPermission  = "no_permission"  // the run has no permission note on the node
)

// CodeOwnWork is the code of the 409 of follow: the workspace holds work origin lacks.
const CodeOwnWork = "own_work"

// LaunchRequest is the body of PUT launch: the fields of a workplace.LaunchSpec, with the
// content of the run instructions in place of their path. MCPConfig is a path on the node
// and is passed on unchanged.
type LaunchRequest struct {
	Repo         string `json:"repo"`
	Issue        int    `json:"issue"`
	SessionID    string `json:"session_id"`
	Mode         string `json:"mode"` // "start" or "resume"
	Profile      string `json:"profile,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	Native       bool   `json:"native,omitempty"`
	MCPConfig    string `json:"mcp_config,omitempty"`
	Instructions string `json:"instructions"`
}

// Problem is the body of every error answer of the node-API.
type Problem struct {
	Status int    `json:"status"`
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail"`
}

func (p *Problem) Error() string {
	if p.Code != "" {
		return fmt.Sprintf("node answered %d (%s): %s", p.Status, p.Code, p.Detail)
	}
	return fmt.Sprintf("node answered %d: %s", p.Status, p.Detail)
}

// GetStatus is the HTTP status of the problem.
func (p *Problem) GetStatus() int { return p.Status }

// Info is the answer of GET info.
type Info struct {
	Version  int    `json:"version"`  // the node-API version, Version
	Binary   string `json:"binary"`   // the lighthouse version of the binary on the node
	Platform string `json:"platform"` // GOOS/GOARCH of the node
}

// FollowRequest is the body of POST follow: the branch Prepare asked for, and whether the
// hub made the workspace just now.
type FollowRequest struct {
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
}

// OwnWork is the 409 answer of follow: the fields of a *workplace.OwnWorkError, inside the
// problem shape (status, code own_work, detail) every error answer has.
type OwnWork struct {
	Status    int                 `json:"status"`
	Code      string              `json:"code"`
	Detail    string              `json:"detail"`
	Workspace workplace.Workspace `json:"workspace"`
	Commits   int                 `json:"commits"`
	Dirty     bool                `json:"dirty"`
}

func (o *OwnWork) Error() string { return o.Detail }

// GetStatus is the HTTP status of the answer, 409.
func (o *OwnWork) GetStatus() int { return o.Status }

// Relocation is the body and the answer of POST relocate: the start command.
type Relocation struct {
	Command string `json:"command"`
}

// GitState is the answer of GET git: workplace.Dirty and workplace.Unpushed of the worktree.
type GitState struct {
	Dirty    bool `json:"dirty"`
	Unpushed bool `json:"unpushed"`
}

// Session is one entry of GET sessions: a workplace.Session without its workspace id, with
// UpdatedAt as RFC 3339, or "" when the forge sent none that parsed.
type Session struct {
	Key       string `json:"key"`
	State     string `json:"state"`
	SessionID string `json:"session_id"`
	UpdatedAt string `json:"updated_at"`
}

// The process routes: each looks up the worktree of {id} at the node's own forge and runs the
// kenn-forge adapter of the node (Node "") with the profile of the run; the body is a
// StopRequest. A profile the node does not know, or an unsafe or empty session id, is 400.
const (
	// PathStop is POST: Stop. On a node without unit_prefix (no systemd) it is Kill.
	PathStop = Prefix + "/workspaces/{id}/stop"
	// PathKill is POST: Kill, only the claude processes of the given sessions.
	PathKill = Prefix + "/workspaces/{id}/kill"
)

// StopRequest is the body of POST stop and POST kill.
type StopRequest struct {
	Profile    string   `json:"profile"`
	SessionIDs []string `json:"session_ids,omitempty"`
}

// The usage routes: the tick's usage guard on the node. They touch only the directory of the
// pause flag and the alarm mirror (lighthouse/controller) and the marker directory.
const (
	// PathUsage is PUT: write the pause flag and the alarm mirror as the tick has them, or
	// remove them; the body is a UsageRequest.
	PathUsage = Prefix + "/usage"
	// PathMarkers is GET: the resume markers of the node, []park.Marker.
	PathMarkers = Prefix + "/markers"
	// PathMarker is DELETE with the query key: park.RemoveMarker of {session}, which leaves a
	// marker whose key differs. A missing marker is no error.
	PathMarker = Prefix + "/markers/{session}"
)

// UsageRequest is the body of PUT usage: the raw content of the tick's PAUSE and alarm mirror.
// nil removes the file on the node.
type UsageRequest struct {
	Pause *string `json:"pause,omitempty"`
	Alarm *string `json:"alarm,omitempty"`
}
