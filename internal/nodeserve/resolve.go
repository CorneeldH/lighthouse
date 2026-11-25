package nodeserve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/gitpath"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const toplevelTimeout = 30 * time.Second

// resolve looks up the worktree of workspace id at the node's own forge and checks it: an
// absolute path that is, after filepath.EvalSymlinks, the toplevel git reports for it. The
// returned Path is the evaluated one. Every error is a *nodeapi.Problem: an unsafe id is 400
// before any call, a forge 404 is 404 workspace_gone, a forge that fails or reports a relative
// path is 502, and a path that is not a toplevel is 500.
func (h *handler) resolve(ctx context.Context, id string) (workplace.Workspace, error) {
	if err := checkWorkspaceID(id); err != nil {
		return workplace.Workspace{}, err
	}
	ws, err := h.adapter.Lookup(ctx, id)
	if errors.Is(err, workplace.ErrGone) {
		return workplace.Workspace{}, &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeWorkspaceGone, Detail: err.Error()}
	}
	if err != nil {
		return workplace.Workspace{}, &nodeapi.Problem{Status: http.StatusBadGateway, Detail: err.Error()}
	}
	if !filepath.IsAbs(ws.Path) {
		return workplace.Workspace{}, &nodeapi.Problem{Status: http.StatusBadGateway, Detail: fmt.Sprintf("forge reports worktree_path %q of %s, not an absolute path", ws.Path, id)}
	}
	path, err := filepath.EvalSymlinks(ws.Path)
	if err != nil {
		return workplace.Workspace{}, internal("worktree of %s: %v", id, err)
	}
	out, code, err := h.d.Run(ctx, toplevelTimeout, "git", "-C", path, "rev-parse", "--show-toplevel")
	if err != nil || code != 0 {
		return workplace.Workspace{}, internal("worktree %s of %s is not a git worktree (exit %d): %v", path, id, code, err)
	}
	top, err := filepath.EvalSymlinks(gitpath.Native(out))
	if err != nil {
		return workplace.Workspace{}, internal("toplevel of %s: %v", path, err)
	}
	if top != path {
		return workplace.Workspace{}, internal("worktree %s of %s is not its toplevel %s", path, id, top)
	}
	ws.Path = path
	return ws, nil
}

// checkWorkspaceID is 400 for an id that is no safe file name or no kenn-forge id.
func checkWorkspaceID(id string) error {
	if !workplace.SafeID.MatchString(id) || strings.HasPrefix(id, workplace.LocalIDPrefix) {
		return badRequest("workspace id %q is not a kenn-forge id", id)
	}
	return nil
}

func internal(format string, args ...any) *nodeapi.Problem {
	return &nodeapi.Problem{Status: http.StatusInternalServerError, Detail: fmt.Sprintf(format, args...)}
}

func badRequest(format string, args ...any) *nodeapi.Problem {
	return &nodeapi.Problem{Status: http.StatusBadRequest, Detail: fmt.Sprintf(format, args...)}
}
