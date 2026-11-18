package kennforge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	defaultReadyWait    = 60 * time.Second
	defaultSpawnTimeout = 30 * time.Second
	gitTimeout          = 60 * time.Second
	readyPoll           = time.Second
)

// Adapter is the kenn-forge implementation of the workplace interface.
type Adapter struct {
	Client     *Client
	Kind, Repo string            // [forge] kind and repo of the repository
	StateDir   string            // where carriers go
	Targets    map[string]string // profile -> forge target_key
	Profile    string            // the profile of the run: Stop, Kill and Transcript pick the container by it
	Node       string            // forge node_id of the workspace; "": self, the forge at Client
	Serve      *NodeClient       // lighthouse serve on Node; nil: self
	UnitPrefix string            // [dispatch.forge] unit_prefix, no default: "" makes Stop fail
	Run        workplace.Runner
	Home       string              // where ~/.claude/projects lives; "": os.UserHomeDir
	Sleep      func(time.Duration) // nil: time.Sleep
	ReadyWait  time.Duration       // 0: 60s

	spawnTimeout time.Duration // 0: 30s
}

func (a *Adapter) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

func wsPath(id string) string { return "/api/v1/workspaces/" + url.PathEscape(id) }

type wireWorkspace struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	WorktreeDir string `json:"worktree_path"`
	GitHeadRef  string `json:"git_head_ref"`
	Repo        struct {
		Provider string `json:"provider"`
		RepoPath string `json:"repo_path"`
	} `json:"repo"`
}

// Prepare returns the ready workspace for branch: an existing one, or a new one, and follows the
// remote branch in it (workplace.FollowRemote). On another node serve follows it there: the
// worktree path is the node's, not this host's (#327). Own work is a *workplace.OwnWorkError on
// both.
func (a *Adapter) Prepare(ctx context.Context, branch string) (workplace.Workspace, error) {
	n, err := a.serve()
	if err != nil {
		return workplace.Workspace{}, err
	}
	var list struct {
		Workspaces []wireWorkspace `json:"workspaces"`
	}
	if err := a.Client.do(ctx, http.MethodGet, a.path("/api/v1/workspaces"), nil, 0, &list); err != nil {
		return workplace.Workspace{}, fmt.Errorf("list workspaces: %w", err)
	}
	id, created := "", false
	for _, w := range list.Workspaces {
		// Forge lists the workspaces of every repository; repo_path is lower case.
		if w.Repo.Provider != a.Kind || !strings.EqualFold(w.Repo.RepoPath, a.Repo) {
			continue
		}
		if w.GitHeadRef == branch || w.GitHeadRef == "refs/heads/"+branch {
			id = w.ID
			break
		}
	}
	if id == "" {
		var w wireWorkspace
		path := a.path("/api/v1/repo/" + a.Kind + "/" + a.Repo + "/workspaces")
		if err := a.Client.do(ctx, http.MethodPost, path, map[string]string{"branch": branch}, 0, &w); err != nil {
			return workplace.Workspace{}, fmt.Errorf("create workspace: %w", err)
		}
		if w.ID == "" {
			return workplace.Workspace{}, errors.New("create workspace: answer has no id")
		}
		id, created = w.ID, true
	}
	w, err := a.waitReady(ctx, id)
	if err != nil {
		return workplace.Workspace{}, err
	}
	ws := workplace.Workspace{ID: id, Path: w.WorktreeDir, Branch: branch}
	if n != nil {
		if err := n.Follow(ctx, id, branch, created); err != nil {
			var own *workplace.OwnWorkError
			if errors.As(err, &own) {
				return workplace.Workspace{}, own
			}
			return workplace.Workspace{}, fmt.Errorf("follow %s on node %s: %w", branch, a.Node, err)
		}
		return ws, nil
	}
	if err := workplace.FollowRemote(ctx, a.Run, gitTimeout, ws, created); err != nil {
		return workplace.Workspace{}, err
	}
	return ws, nil
}

// waitReady polls the workspace until it is ready; a found workspace can still be creating.
func (a *Adapter) waitReady(ctx context.Context, id string) (wireWorkspace, error) {
	wait := a.ReadyWait
	if wait == 0 {
		wait = defaultReadyWait
	}
	polls := int(wait / readyPoll)
	for i := 0; ; i++ {
		var w wireWorkspace
		if err := a.Client.do(ctx, http.MethodGet, a.path(wsPath(id)), nil, 0, &w); err != nil {
			return w, fmt.Errorf("read workspace %s: %w", id, err)
		}
		if w.Status == "ready" {
			if w.WorktreeDir == "" {
				return w, fmt.Errorf("workspace %s is ready but has no worktree_path", id)
			}
			return w, nil
		}
		if i >= polls {
			return w, fmt.Errorf("workspace %s not ready after %s (status %q)", id, wait, w.Status)
		}
		a.sleep(readyPoll)
	}
}

// Spawn starts the agent through a forge handoff. It has three outcomes: accepted
// (a session), workplace.ErrNotAccepted (no agent runs, the carrier is gone) and
// workplace.ErrUncertain (an agent may run, the carrier stays). It never sends a second POST.
// On another node serve writes the carrier and the run instructions there before the handoff
// (see launch); a launch that fails sends no handoff.
//
// After a 5xx, a timeout, a broken connection or a 2xx without session.key it reads the
// sessions once more and takes a new key of this run. After a 5xx, no such key is
// ErrNotAccepted; a failed re-read is ErrUncertain.
func (a *Adapter) Spawn(ctx context.Context, ws workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	if err := notForge(ws.ID); err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", err, workplace.ErrNotAccepted)
	}
	target, ok := a.Targets[l.Profile]
	if !ok {
		return workplace.Session{}, fmt.Errorf("no forge target for profile %q: %w", l.Profile, workplace.ErrNotAccepted)
	}
	n, err := a.serve()
	if err != nil {
		return workplace.Session{}, err
	}
	if n != nil {
		if err := a.launch(ctx, n, ws.ID, l); err != nil {
			return workplace.Session{}, err
		}
	} else {
		c, err := workplace.NewCarrier(l, a.StateDir, time.Now())
		if err != nil {
			return workplace.Session{}, fmt.Errorf("%w: %w", err, workplace.ErrNotAccepted)
		}
		if _, err := workplace.WriteCarrier(a.StateDir, "", ws.ID, c); err != nil {
			return workplace.Session{}, fmt.Errorf("write carrier: %w: %w", err, workplace.ErrNotAccepted)
		}
	}
	notAccepted := func(err error) (workplace.Session, error) {
		if !errors.Is(err, workplace.ErrNotAccepted) {
			err = fmt.Errorf("%v: %w", err, workplace.ErrNotAccepted)
		}
		if rmErr := a.removeCarrier(ctx, ws.ID); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("remove carrier: %w", rmErr))
		}
		return workplace.Session{}, err
	}
	before, err := a.Sessions(ctx, ws.ID)
	if err != nil {
		return notAccepted(fmt.Errorf("read sessions before spawn: %w", err))
	}
	known := map[string]bool{}
	for _, s := range before {
		known[s.Key] = true
	}
	timeout := a.spawnTimeout
	if timeout == 0 {
		timeout = defaultSpawnTimeout
	}
	var out struct {
		Session struct {
			Key string `json:"key"`
		} `json:"session"`
	}
	body := map[string]string{"target_key": target, "message": l.Prompt}
	path := a.path(wsPath(ws.ID)) + "/runtime/agent-handoffs"
	err = a.Client.do(ctx, http.MethodPost, path, body, timeout, &out)
	if err == nil && out.Session.Key != "" {
		return workplace.Session{Key: out.Session.Key, WorkspaceID: ws.ID, SessionID: l.SessionID}, nil
	}
	if err == nil {
		err = fmt.Errorf("handoff answer has no session.key: %w", workplace.ErrUncertain)
	}
	var se *StatusError
	isStatus := errors.As(err, &se)
	switch {
	case isStatus && se.Status >= 500:
		// The agent may have started before the 5xx: look once more.
		s, ok, rerr := a.appeared(ctx, ws, l, known)
		if rerr != nil {
			// Whether an agent started is not measurable: uncertain, never "not accepted".
			// %v keeps the 5xx from matching ErrNotAccepted.
			return workplace.Session{}, fmt.Errorf("%v; re-read sessions: %v: %w", err, rerr, workplace.ErrUncertain)
		}
		if ok {
			return s, nil
		}
		return notAccepted(err)
	case isStatus || errors.Is(err, workplace.ErrNotAccepted):
		return notAccepted(err)
	}
	// Uncertain: a timeout, a broken connection or an answer without a key; an agent may
	// be running.
	s, ok, rerr := a.appeared(ctx, ws, l, known)
	if rerr != nil {
		return workplace.Session{}, fmt.Errorf("%w; re-read sessions: %v", err, rerr)
	}
	if ok {
		return s, nil
	}
	return workplace.Session{}, err
}

// appeared reads the sessions once and looks for one that was not there before the POST
// and is this run's: its session_id is ours (it wins), or still empty. A new key with
// another session_id is provably not ours, since the carrier passes --session-id. A failed
// read is an error, never "nothing appeared".
func (a *Adapter) appeared(ctx context.Context, ws workplace.Workspace, l workplace.LaunchSpec, known map[string]bool) (workplace.Session, bool, error) {
	after, err := a.Sessions(ctx, ws.ID)
	if err != nil {
		return workplace.Session{}, false, err
	}
	fallback := ""
	for _, s := range after {
		if known[s.Key] {
			continue
		}
		if s.SessionID == l.SessionID {
			return workplace.Session{Key: s.Key, WorkspaceID: ws.ID, SessionID: l.SessionID}, true, nil
		}
		if s.SessionID == "" && fallback == "" {
			fallback = s.Key
		}
	}
	if fallback != "" {
		return workplace.Session{Key: fallback, WorkspaceID: ws.ID, SessionID: l.SessionID}, true, nil
	}
	return workplace.Session{}, false, nil
}

// notForge refuses the ID of a workspace the local adapter made: kenn-forge would answer 404
// and the caller would read that as workplace.ErrGone.
func notForge(id string) error {
	if strings.HasPrefix(id, workplace.LocalIDPrefix) {
		return fmt.Errorf("workspace %s is a local workspace, not a kenn-forge one", id)
	}
	return nil
}

// Sessions lists the agent sessions of a workspace. A 404 is workplace.ErrGone. On another
// node serve reads them at the node's forge, with the same fields (see nodeSessions).
func (a *Adapter) Sessions(ctx context.Context, workspaceID string) ([]workplace.Session, error) {
	if err := notForge(workspaceID); err != nil {
		return nil, err
	}
	n, err := a.serve()
	if err != nil {
		return nil, err
	}
	if n != nil {
		return a.nodeSessions(ctx, n, workspaceID)
	}
	var out struct {
		Sessions *[]struct {
			Key       string  `json:"runtime_session_key"`
			State     *string `json:"state"`
			SessionID string  `json:"session_id"`
			UpdatedAt string  `json:"updated_at"`
		} `json:"sessions"`
	}
	if err := a.Client.do(ctx, http.MethodGet, a.path(wsPath(workspaceID))+"/agent-sessions", nil, 0, &out); err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Status == http.StatusNotFound {
			return nil, fmt.Errorf("read sessions of %s: %v: %w", workspaceID, se, workplace.ErrGone)
		}
		return nil, fmt.Errorf("read sessions of %s: %w", workspaceID, err)
	}
	if out.Sessions == nil {
		return nil, fmt.Errorf("sessions of %s: answer has no sessions list: %w", workspaceID, workplace.ErrUncertain)
	}
	res := make([]workplace.Session, 0, len(*out.Sessions))
	for _, e := range *out.Sessions {
		if e.Key == "" {
			continue
		}
		s := workplace.Session{Key: e.Key, WorkspaceID: workspaceID, SessionID: e.SessionID}
		if e.State != nil {
			s.State = *e.State
		}
		if t, err := time.Parse(time.RFC3339, e.UpdatedAt); err == nil {
			s.UpdatedAt = t
		}
		res = append(res, s)
	}
	return res, nil
}

// Lookup reads the workspace: Path from worktree_path, Branch from git_head_ref. A 404 is
// workplace.ErrGone (on another node only as confirmGone allows); a workspace without
// worktree_path is an error.
func (a *Adapter) Lookup(ctx context.Context, workspaceID string) (workplace.Workspace, error) {
	if err := notForge(workspaceID); err != nil {
		return workplace.Workspace{}, err
	}
	w, err := a.readWorkspace(ctx, workspaceID)
	if err != nil {
		return workplace.Workspace{}, err
	}
	return workplace.Workspace{ID: workspaceID, Path: w.WorktreeDir, Branch: w.GitHeadRef}, nil
}

// readWorkspace reads one workspace that has a worktree_path. A 404 is workplace.ErrGone,
// as notFound decides.
func (a *Adapter) readWorkspace(ctx context.Context, workspaceID string) (wireWorkspace, error) {
	var w wireWorkspace
	if err := a.Client.do(ctx, http.MethodGet, a.path(wsPath(workspaceID)), nil, 0, &w); err != nil {
		if nf := a.notFound(ctx, err, "read workspace "+workspaceID); nf != nil {
			return w, nf
		}
		return w, fmt.Errorf("read workspace %s: %w", workspaceID, err)
	}
	if w.WorktreeDir == "" {
		return w, fmt.Errorf("workspace %s has no worktree_path", workspaceID)
	}
	return w, nil
}

// Remove deletes the workspace and then its carrier. A workspace that is already gone is
// no error; one with work in it is workplace.ErrDirty. A carrier it cannot wipe is an error.
// On another node a 404 is "already gone" only as confirmGone allows; otherwise the carrier
// stays and the error wraps workplace.ErrUncertain.
//
// kenn-forge refuses (409) to delete a dirty worktree and has no force parameter, so with
// o.Force the worktree is first reset to HEAD and cleaned of every untracked and ignored file,
// with local git. That needs the worktree on this host: on another node a forced Remove is an
// error, before anything is changed.
func (a *Adapter) Remove(ctx context.Context, workspaceID string, o workplace.RemoveOptions) error {
	if err := notForge(workspaceID); err != nil {
		return err
	}
	if o.Force {
		if err := a.discardWork(ctx, workspaceID); err != nil {
			return fmt.Errorf("remove workspace %s: %w", workspaceID, err)
		}
	}
	err := a.Client.do(ctx, http.MethodDelete, a.path(wsPath(workspaceID)), nil, 0, nil)
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusConflict {
		return fmt.Errorf("remove workspace %s: %v: %w", workspaceID, se, workplace.ErrDirty)
	}
	if err != nil && (se == nil || se.Status != http.StatusNotFound) {
		return fmt.Errorf("remove workspace %s: %w", workspaceID, err)
	}
	if err != nil {
		if cerr := a.confirmGone(ctx, se); cerr != nil {
			return fmt.Errorf("remove workspace %s: %v: %w", workspaceID, se, cerr)
		}
	}
	return a.Discard(ctx, workspaceID)
}

// forceGit are the git options of discardWork: the worktree's own config may come from an agent,
// so it starts no fsmonitor and no hook.
var forceGit = []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}

// discardWork throws away the uncommitted work in the worktree of a workspace: reset --hard to
// HEAD, then clean -ffdx (nested repositories and ignored files too). A workspace that is already
// gone leaves nothing to discard: the DELETE that follows finds it gone as well.
func (a *Adapter) discardWork(ctx context.Context, workspaceID string) error {
	if a.Node != "" {
		return fmt.Errorf("forced remove needs local git, and the workspace is on node %s", a.Node)
	}
	w, err := a.readWorkspace(ctx, workspaceID)
	if errors.Is(err, workplace.ErrGone) {
		return nil
	}
	if err != nil {
		return err
	}
	if !filepath.IsAbs(w.WorktreeDir) {
		return fmt.Errorf("worktree_path %q is not absolute", w.WorktreeDir)
	}
	if err := a.checkDiscardable(ctx, w); err != nil {
		return err
	}
	for _, args := range [][]string{{"reset", "--hard", "--quiet"}, {"clean", "-ffdxq"}} {
		argv := append(append(append([]string{}, forceGit...), "-C", w.WorktreeDir), args...)
		_, code, err := a.Run(ctx, gitTimeout, "git", argv...)
		if err != nil {
			return fmt.Errorf("git %s: %w", args[0], err)
		}
		if code != 0 {
			return fmt.Errorf("git %s: exit code %d", args[0], code)
		}
	}
	return nil
}

// scheduledBranchPrefix is the prefix of the branch of a scheduled run, the only workspace a forced
// remove may throw work away from.
const scheduledBranchPrefix = "scheduled/"

// checkDiscardable refuses a discard unless w is a scheduled run's branch checked out in a linked
// worktree whose real HEAD is that branch. reset --hard and clean -ffdx in the main checkout, or
// in a worktree of someone's work, would destroy what is not the run's; a bad worktree_path from
// the forge must not become that.
func (a *Adapter) checkDiscardable(ctx context.Context, w wireWorkspace) error {
	branch := strings.TrimPrefix(w.GitHeadRef, "refs/heads/")
	if !strings.HasPrefix(branch, scheduledBranchPrefix) {
		return fmt.Errorf("worktree %q is on branch %q, not a scheduled run's: nothing discarded", w.WorktreeDir, branch)
	}
	// The branch the forge names is not trusted on its own: it comes from the same answer as the
	// path. The worktree's real HEAD must be that branch.
	argv := append(append([]string{}, forceGit...), "-C", w.WorktreeDir, "symbolic-ref", "--quiet", "HEAD")
	head, code, err := a.Run(ctx, gitTimeout, "git", argv...)
	if err != nil {
		return fmt.Errorf("git symbolic-ref: %w", err)
	}
	if real := strings.TrimPrefix(strings.TrimSpace(head), "refs/heads/"); code != 0 || real != branch {
		return fmt.Errorf("worktree %q is on branch %q, not %q as the forge says: nothing discarded", w.WorktreeDir, real, branch)
	}
	dirs := make([]string, 0, 2)
	for _, flag := range []string{"--git-dir", "--git-common-dir"} {
		argv := append(append([]string{}, forceGit...), "-C", w.WorktreeDir, "rev-parse", "--path-format=absolute", flag)
		out, code, err := a.Run(ctx, gitTimeout, "git", argv...)
		if err != nil {
			return fmt.Errorf("git rev-parse: %w", err)
		}
		if code != 0 {
			return fmt.Errorf("git rev-parse %s: exit code %d", flag, code)
		}
		dirs = append(dirs, filepath.Clean(strings.TrimSpace(out)))
	}
	if dirs[0] == dirs[1] {
		return fmt.Errorf("worktree %q is the main checkout, not a linked worktree: nothing discarded", w.WorktreeDir)
	}
	return nil
}

// Discard wipes the carrier of the workspace, on another node through serve; a missing carrier
// is no error.
func (a *Adapter) Discard(ctx context.Context, workspaceID string) error {
	if err := notForge(workspaceID); err != nil {
		return err
	}
	if err := a.removeCarrier(ctx, workspaceID); err != nil {
		return fmt.Errorf("remove carrier of %s: %w", workspaceID, err)
	}
	return nil
}
