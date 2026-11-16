package nodeserve

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The git routes work on the worktree resolve looks up at the node's own forge, never on a
// path from the tick. sessions reads only the forge.

// followTimeout is the timeout of every git command of follow, as on the kenn-forge adapter.
const followTimeout = 60 * time.Second

var (
	// safeBranch is a branch name that git takes as one ref argument, never as an option.
	safeBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	// mergedHeadPattern is a full lowercase commit SHA: it goes to git as an argument.
	mergedHeadPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type followInput struct {
	ID   string `path:"id"`
	Body nodeapi.FollowRequest
}

type relocateInput struct {
	ID   string `path:"id"`
	Body nodeapi.Relocation
}

type relocateOutput struct{ Body nodeapi.Relocation }

type gitInput struct {
	ID         string `path:"id"`
	MergedHead string `query:"merged_head"`
}

type gitOutput struct{ Body nodeapi.GitState }

type sessionsInput struct {
	ID string `path:"id"`
}

type sessionsOutput struct{ Body []nodeapi.Session }

func (h *handler) registerGit() {
	huma.Post(h.api, nodeapi.PathFollow, h.follow, func(op *huma.Operation) {
		op.OperationID = "post-follow"
		op.DefaultStatus = http.StatusNoContent
	})
	huma.Post(h.api, nodeapi.PathRelocate, h.relocate, func(op *huma.Operation) {
		op.OperationID = "post-relocate"
		op.DefaultStatus = http.StatusOK
	})
	huma.Get(h.api, nodeapi.PathGit, h.git, func(op *huma.Operation) { op.OperationID = "get-git" })
	huma.Get(h.api, nodeapi.PathSessions, h.sessions, func(op *huma.Operation) { op.OperationID = "get-sessions" })
}

// follow runs workplace.FollowRemote on the worktree, for the branch the tick prepared. Own
// work is 409 with the fields of the *workplace.OwnWorkError; any other failure is 500.
func (h *handler) follow(ctx context.Context, in *followInput) (*struct{}, error) {
	b := in.Body.Branch
	if !safeBranch.MatchString(b) || strings.Contains(b, "..") {
		return nil, badRequest("branch %q is not a safe branch name", b)
	}
	ws, err := h.resolve(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	ws.Branch = b
	err = workplace.FollowRemote(ctx, h.d.Run, followTimeout, ws, in.Body.Created)
	var own *workplace.OwnWorkError
	if errors.As(err, &own) {
		return nil, &nodeapi.OwnWork{
			Status: http.StatusConflict, Code: nodeapi.CodeOwnWork, Detail: own.Error(),
			Workspace: own.Workspace, Commits: own.Commits, Dirty: own.Dirty,
		}
	}
	if err != nil {
		return nil, internal("follow %s of %s: %v", b, in.ID, err)
	}
	return nil, nil
}

// relocate runs dispatch.Relocate of the start command onto the worktree. A command it cannot
// relocate is 422 with the reason as detail: the tick makes it a relocErr.
func (h *handler) relocate(ctx context.Context, in *relocateInput) (*relocateOutput, error) {
	ws, err := h.resolve(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	cmd, err := dispatch.Relocate(in.Body.Command, ws.Path)
	if err != nil {
		return nil, &nodeapi.Problem{Status: http.StatusUnprocessableEntity, Detail: err.Error()}
	}
	return &relocateOutput{Body: nodeapi.Relocation{Command: cmd}}, nil
}

// git measures workplace.Dirty and workplace.Unpushed on the worktree. merged_head is checked
// before anything else; a failing git is 500, never a clean answer.
func (h *handler) git(ctx context.Context, in *gitInput) (*gitOutput, error) {
	if in.MergedHead != "" && !mergedHeadPattern.MatchString(in.MergedHead) {
		return nil, badRequest("merged_head %q is not a full lowercase commit SHA", in.MergedHead)
	}
	ws, err := h.resolve(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	dirty, err := workplace.Dirty(ctx, h.d.Run, ws.Path)
	if err != nil {
		return nil, internal("dirty of %s: %v", in.ID, err)
	}
	unpushed, err := workplace.Unpushed(ctx, h.d.Run, ws.Path, in.MergedHead)
	if err != nil {
		return nil, internal("unpushed of %s: %v", in.ID, err)
	}
	return &gitOutput{Body: nodeapi.GitState{Dirty: dirty, Unpushed: unpushed}}, nil
}

// sessions answers the agent sessions of the workspace as the node's forge reports them,
// read by the kenn-forge adapter of the node itself (Node ""). It needs no worktree, so it
// does not resolve: a forge 404 is 404 workspace_gone, any other forge failure 502.
func (h *handler) sessions(ctx context.Context, in *sessionsInput) (*sessionsOutput, error) {
	if err := checkWorkspaceID(in.ID); err != nil {
		return nil, err
	}
	list, err := h.adapter.Sessions(ctx, in.ID)
	if errors.Is(err, workplace.ErrGone) {
		return nil, &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeWorkspaceGone, Detail: err.Error()}
	}
	if err != nil {
		return nil, &nodeapi.Problem{Status: http.StatusBadGateway, Detail: err.Error()}
	}
	out := make([]nodeapi.Session, 0, len(list))
	for _, s := range list {
		e := nodeapi.Session{Key: s.Key, State: s.State, SessionID: s.SessionID}
		if !s.UpdatedAt.IsZero() {
			e.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
		}
		out = append(out, e)
	}
	return &sessionsOutput{Body: out}, nil
}
