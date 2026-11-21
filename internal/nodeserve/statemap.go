package nodeserve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The statemap routes touch only the node's state dir, by workspace id or session id. They look
// nothing up at the node's forge, so they also work after the forge forgot the workspace.

type launchInput struct {
	ID   string `path:"id"`
	Body nodeapi.LaunchRequest
}

type launchOutput struct{ Body workplace.Carrier }

type carrierInput struct {
	ID string `path:"id"`
}

type permissionInput struct {
	Session string `path:"session"`
}

type permissionOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

type postFailedInput = permissionInput

type postFailedOutput struct {
	ContentType string `header:"Content-Type"`
	ModTime     string `header:"X-Lighthouse-Mod-Time"` // nodeapi.HeaderModTime
	Body        []byte
}

func (h *handler) registerStatemap() {
	huma.Put(h.api, nodeapi.PathLaunch, h.launch, func(op *huma.Operation) { op.OperationID = "put-launch" })
	huma.Delete(h.api, nodeapi.PathCarrier, h.deleteCarrier, func(op *huma.Operation) {
		op.OperationID = "delete-carrier"
		op.DefaultStatus = http.StatusNoContent
	})
	huma.Get(h.api, nodeapi.PathPermission, h.permission, func(op *huma.Operation) { op.OperationID = "get-permission" })
	huma.Get(h.api, nodeapi.PathPostFailed, h.postFailed, func(op *huma.Operation) { op.OperationID = "get-post-failed" })
}

// launch writes the run instructions the tick sent and the carrier that names them, both in the
// node's state dir, and answers the carrier. Every check on the request comes before the first
// write: an invalid request leaves nothing behind.
func (h *handler) launch(_ context.Context, in *launchInput) (*launchOutput, error) {
	if err := checkWorkspaceID(in.ID); err != nil {
		return nil, err
	}
	r := in.Body
	if r.Instructions == "" {
		return nil, badRequest("launch of %s carries no run instructions", in.ID)
	}
	state := h.d.StateDir
	if !filepath.IsAbs(state) {
		return nil, internal("node state dir %q is not absolute", state)
	}
	spec := workplace.LaunchSpec{
		Repo: r.Repo, Issue: r.Issue, SessionID: r.SessionID, Mode: r.Mode,
		Profile: r.Profile, Prompt: r.Prompt, Native: r.Native, MCPConfig: r.MCPConfig,
		InstructionsFile: dispatch.InstructionsPath(state, r.SessionID),
	}
	// NewCarrier checks the session (a UUID, so a safe path element), mode, paths and prompt.
	c, err := workplace.NewCarrier(spec, state, h.d.Now())
	if err != nil {
		return nil, badRequest("%v", err)
	}
	if _, err := dispatch.WriteInstructionsContent(state, r.SessionID, []byte(r.Instructions)); err != nil {
		return nil, internal("run instructions of %s: %v", r.SessionID, err)
	}
	if _, err := workplace.WriteCarrier(state, "", in.ID, c); err != nil {
		return nil, internal("carrier of %s: %v", in.ID, err)
	}
	return &launchOutput{Body: c}, nil
}

// deleteCarrier removes the carrier of the workspace; a missing one is no error.
func (h *handler) deleteCarrier(_ context.Context, in *carrierInput) (*struct{}, error) {
	if err := checkWorkspaceID(in.ID); err != nil {
		return nil, err
	}
	if err := workplace.RemoveCarrier(h.d.StateDir, "", in.ID); err != nil {
		return nil, internal("carrier of %s: %v", in.ID, err)
	}
	return nil, nil
}

// permission answers the raw permission.json of the run: the tick parses it, so a node of
// another version never measures differently. Without a note it is 404 no_permission.
func (h *handler) permission(_ context.Context, in *permissionInput) (*permissionOutput, error) {
	if !workplace.SafeSession.MatchString(in.Session) {
		return nil, badRequest("session id %q is not safe", in.Session)
	}
	data, err := os.ReadFile(dispatch.PermissionPath(h.d.StateDir, in.Session))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeNoPermission, Detail: fmt.Sprintf("run %s has no permission note", in.Session)}
	}
	if err != nil {
		return nil, internal("permission note of %s: %v", in.Session, err)
	}
	return &permissionOutput{ContentType: "application/json", Body: data}, nil
}

// postFailed answers the raw post-failed.json of the run, for the same reason as permission,
// with its modification time in nodeapi.HeaderModTime: the tick dates an unreadable note by it.
// Without a note it is 404 no_post_failed.
func (h *handler) postFailed(_ context.Context, in *postFailedInput) (*postFailedOutput, error) {
	if !workplace.SafeSession.MatchString(in.Session) {
		return nil, badRequest("session id %q is not safe", in.Session)
	}
	f, err := os.Open(dispatch.PostFailedPath(h.d.StateDir, in.Session))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeNoPostFailed, Detail: fmt.Sprintf("run %s has no post-failed note", in.Session)}
	}
	if err != nil {
		return nil, internal("post-failed note of %s: %v", in.Session, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, internal("post-failed note of %s: %v", in.Session, err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, internal("post-failed note of %s: %v", in.Session, err)
	}
	return &postFailedOutput{
		ContentType: "application/json",
		ModTime:     info.ModTime().UTC().Format(time.RFC3339Nano),
		Body:        data,
	}, nil
}
