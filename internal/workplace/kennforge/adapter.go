package kennforge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	Client       *Client
	Kind, Repo   string            // [forge] kind and repo of the repository
	MainCheckout string            // the repo's main checkout, for the devcontainer label
	StateDir     string            // where carriers go
	Targets      map[string]string // profile -> forge target_key
	Profile      string            // the repo profile ([controller] profile)
	Run          workplace.Runner
	Sleep        func(time.Duration) // nil: time.Sleep
	ReadyWait    time.Duration       // 0: 60s

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

// Prepare returns the ready workspace for branch: an existing one, or a new one.
func (a *Adapter) Prepare(ctx context.Context, branch string) (workplace.Workspace, error) {
	var list struct {
		Workspaces []wireWorkspace `json:"workspaces"`
	}
	if err := a.Client.do(ctx, http.MethodGet, "/api/v1/workspaces", nil, 0, &list); err != nil {
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
		path := "/api/v1/repo/" + a.Kind + "/" + a.Repo + "/workspaces"
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
	if err := a.followRemote(ctx, ws, created); err != nil {
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
		if err := a.Client.do(ctx, http.MethodGet, wsPath(id), nil, 0, &w); err != nil {
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

// git runs one git command in dir and returns trimmed stdout and the exit code. A
// command that could not run or timed out is an error, never a negative answer.
func (a *Adapter) git(ctx context.Context, dir string, args ...string) (string, int, error) {
	out, code, err := a.Run(ctx, gitTimeout, "git", append([]string{"-C", dir}, args...)...)
	if err != nil {
		return "", code, fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(out), code, nil
}

func (a *Adapter) gitOK(ctx context.Context, dir string, args ...string) (string, error) {
	out, code, err := a.git(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", strings.Join(args, " "), code)
	}
	return out, nil
}

// followRemote moves the workspace onto origin/<branch> when that branch exists, without
// ever dropping commits of its own.
func (a *Adapter) followRemote(ctx context.Context, ws workplace.Workspace, created bool) error {
	ref := "refs/heads/" + ws.Branch
	remote := "origin/" + ws.Branch
	out, err := a.gitOK(ctx, ws.Path, "ls-remote", "origin", ref)
	if err != nil {
		return err
	}
	if out == "" {
		return nil
	}
	if _, err := a.gitOK(ctx, ws.Path, "fetch", "origin", ws.Branch); err != nil {
		return err
	}
	head, err := a.gitOK(ctx, ws.Path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	want, err := a.gitOK(ctx, ws.Path, "rev-parse", remote)
	if err != nil {
		return err
	}
	if head == want {
		return nil
	}
	if created {
		// Forge puts a new workspace on an existing remote branch at origin/HEAD, not at the
		// branch (measured in paia, controller-forge-verification.md row (i)).
		return a.checkoutRemote(ctx, ws, remote, want)
	}
	// A found workspace can hold work of an earlier run: never checkout -B over it.
	cur, err := a.gitOK(ctx, ws.Path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if cur == ws.Branch {
		_, code, err := a.git(ctx, ws.Path, "merge-base", "--is-ancestor", "HEAD", remote)
		if err != nil {
			return err
		}
		switch code {
		case 0:
			_, err := a.gitOK(ctx, ws.Path, "merge", "--ff-only", remote)
			return err
		case 1:
		default:
			return fmt.Errorf("git merge-base --is-ancestor: exit code %d", code)
		}
	}
	// On another branch (main, say) a fast-forward would move that branch: only switch
	// when HEAD holds nothing that is not on origin.
	n, err := a.gitOK(ctx, ws.Path, "rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	if err != nil {
		return err
	}
	own, err := strconv.Atoi(n)
	if err != nil {
		return fmt.Errorf("git rev-list --count: unreadable answer %q", n)
	}
	if own > 0 {
		return nil
	}
	return a.checkoutRemote(ctx, ws, remote, want)
}

func (a *Adapter) checkoutRemote(ctx context.Context, ws workplace.Workspace, remote, want string) error {
	if _, err := a.gitOK(ctx, ws.Path, "checkout", "-B", ws.Branch, "--track", remote); err != nil {
		return err
	}
	head, err := a.gitOK(ctx, ws.Path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != want {
		return fmt.Errorf("workspace %s: HEAD %s is not %s after checkout", ws.ID, head, remote)
	}
	return nil
}

// Spawn starts the agent through a forge handoff. It has three outcomes: accepted
// (a session), workplace.ErrNotAccepted (no agent runs, the carrier is gone) and
// workplace.ErrUncertain (an agent may run, the carrier stays). It never sends a second POST.
//
// After a 5xx, a timeout, a broken connection or a 2xx without session.key it reads the
// sessions once more and takes a new key of this run. After a 5xx, no such key is
// ErrNotAccepted; a failed re-read is ErrUncertain.
func (a *Adapter) Spawn(ctx context.Context, ws workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	target, ok := a.Targets[l.Profile]
	if !ok {
		return workplace.Session{}, fmt.Errorf("no forge target for profile %q: %w", l.Profile, workplace.ErrNotAccepted)
	}
	c, err := workplace.NewCarrier(l, a.StateDir, time.Now())
	if err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", err, workplace.ErrNotAccepted)
	}
	if _, err := workplace.WriteCarrier(a.StateDir, ws.ID, c); err != nil {
		return workplace.Session{}, fmt.Errorf("write carrier: %w: %w", err, workplace.ErrNotAccepted)
	}
	notAccepted := func(err error) (workplace.Session, error) {
		if !errors.Is(err, workplace.ErrNotAccepted) {
			err = fmt.Errorf("%v: %w", err, workplace.ErrNotAccepted)
		}
		if rmErr := workplace.RemoveCarrier(a.StateDir, ws.ID); rmErr != nil {
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
	path := wsPath(ws.ID) + "/runtime/agent-handoffs"
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

// Sessions lists the agent sessions of a workspace.
func (a *Adapter) Sessions(ctx context.Context, workspaceID string) ([]workplace.Session, error) {
	var out struct {
		Sessions *[]struct {
			Key       string  `json:"runtime_session_key"`
			State     *string `json:"state"`
			SessionID string  `json:"session_id"`
			UpdatedAt string  `json:"updated_at"`
		} `json:"sessions"`
	}
	if err := a.Client.do(ctx, http.MethodGet, wsPath(workspaceID)+"/agent-sessions", nil, 0, &out); err != nil {
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

// Remove deletes the workspace and then its carrier. A workspace that is already gone is
// no error; one with work in it is workplace.ErrDirty. A carrier it cannot wipe is an error.
func (a *Adapter) Remove(ctx context.Context, workspaceID string) error {
	err := a.Client.do(ctx, http.MethodDelete, wsPath(workspaceID), nil, 0, nil)
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusConflict {
		return fmt.Errorf("remove workspace %s: %v: %w", workspaceID, se, workplace.ErrDirty)
	}
	if err != nil && (se == nil || se.Status != http.StatusNotFound) {
		return fmt.Errorf("remove workspace %s: %w", workspaceID, err)
	}
	return a.Discard(ctx, workspaceID)
}

// Discard wipes the carrier of the workspace; a missing carrier is no error.
func (a *Adapter) Discard(_ context.Context, workspaceID string) error {
	if err := workplace.RemoveCarrier(a.StateDir, workspaceID); err != nil {
		return fmt.Errorf("remove carrier of %s: %w", workspaceID, err)
	}
	return nil
}
