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

// The machine-readable codes of a 404. The tick maps on the code, never on the status alone.
const (
	CodeWorkspaceGone = "workspace_gone" // the node's forge no longer knows the workspace
	CodeNoTranscript  = "no_transcript"  // the session has no transcript on the node
)

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
