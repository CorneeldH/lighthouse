// Package nodeapi is the wire contract between the tick and lighthouse serve on a kenn-forge
// node: the route paths, the version and the bodies both sides share.
package nodeapi

import "fmt"

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

// The machine-readable codes of a 404. The tick maps on the code, never on the status alone.
const (
	CodeWorkspaceGone = "workspace_gone" // the node's forge no longer knows the workspace
	CodeNoTranscript  = "no_transcript"  // the session has no transcript on the node
	CodeNoPermission  = "no_permission"  // the run has no permission note on the node
)

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
