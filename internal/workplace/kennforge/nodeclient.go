package kennforge

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The deadlines of a node request. serve sets none on its git routes, so the client's deadline
// is the only one: follow runs several git commands of up to 60s each on the node.
const (
	nodeTimeout     = 30 * time.Second
	nodeGitTimeout  = 5 * time.Minute // follow, git, relocate
	nodeProcTimeout = 2 * time.Minute // stop, kill
	// transcript: serve resolves the workspace (up to 15s), may ask git (30s) and reads in the
	// container (30s); the client must wait at least as long.
	nodeTranscriptTimeout = 2 * time.Minute
	nodeHTTPHardLimit     = nodeGitTimeout + 30*time.Second
)

// NodeClient talks to lighthouse serve on a kenn-forge node: the node-API of nodeapi. The token
// is read from TokenFile on every request. Info is asked once per NodeClient; the tick builds a
// new client per tick, so that is once per tick and node.
//
// Errors: a node that is unreachable or answers 5xx, 401 or 403 is workplace.ErrNotMeasurable,
// never workplace.ErrGone; a request that never reached the node (dial refused, no token, Info
// failed or of another version) is also workplace.ErrNotAccepted. Every answer outside 2xx is a
// *NodeError; the adapter maps a 404 workspace_gone, the client does not.
type NodeClient struct {
	URL       string
	TokenFile string
	HTTP      *http.Client // nil: the client NewNodeClient builds

	mu   sync.Mutex
	info *nodeapi.Info // the Info of a node that speaks nodeapi.Version, once asked
	bad  error         // the failure of Info, once seen: kept for the life of the client
}

// NewNodeClient returns a client without proxy and without redirect following, with a hard
// timeout above the longest route deadline. The URL is checked by hostconfig (Task 1).
func NewNodeClient(url, tokenFile string) *NodeClient {
	hc := newHTTP()
	hc.Timeout = nodeHTTPHardLimit
	return &NodeClient{URL: strings.TrimRight(url, "/"), TokenFile: tokenFile, HTTP: hc}
}

// NodeError is an answer of the node outside 2xx: its status, the machine-readable code of a
// 404 (nodeapi.CodeWorkspaceGone, nodeapi.CodeNoTranscript, …) and the detail.
type NodeError struct {
	Node   string // the URL of the node
	Status int
	Code   string
	Detail string
}

func (e *NodeError) Error() string {
	what := fmt.Sprintf("node %s answered %d", e.Node, e.Status)
	if e.Code != "" {
		what += " (" + e.Code + ")"
	}
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return fmt.Sprintf("configuration error: %s: %s; check serve_token_file of the node against [node] token_file on it", what, e.Detail)
	}
	return what + ": " + e.Detail
}

// Is makes a 5xx, 401 and 403 not measurable, and a 5xx not accepted as on the forge client.
func (e *NodeError) Is(target error) bool {
	switch target {
	case workplace.ErrNotMeasurable:
		return e.Status >= 500 || e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	case workplace.ErrNotAccepted:
		return e.Status >= 500
	}
	return false
}

// notSent is an error of a request that never reached the node.
func (c *NodeClient) notSent(format string, args ...any) error {
	return fmt.Errorf("node %s: %s: %w: %w", c.URL, fmt.Sprintf(format, args...), workplace.ErrNotAccepted, workplace.ErrNotMeasurable)
}

// request is one call: method, route with its path values filled in, query and JSON body.
type request struct {
	method, path string
	query        url.Values
	body         any
	timeout      time.Duration // 0: nodeTimeout
	header       *http.Header  // when set, send stores the headers of the answer in it
}

// send sends one request and returns status and body of the answer. A transport failure is
// ErrNotMeasurable, and ErrNotAccepted when the dial failed, ErrUncertain otherwise.
func (c *NodeClient) send(ctx context.Context, r request) (int, []byte, error) {
	raw, err := os.ReadFile(c.TokenFile)
	token := strings.TrimSpace(string(raw))
	if err != nil || token == "" {
		return 0, nil, c.notSent("token file %s unreadable or empty", c.TokenFile)
	}
	var rd io.Reader
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return 0, nil, c.notSent("encode body: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = nodeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := c.URL + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, rd)
	if err != nil {
		return 0, nil, c.notSent("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = sharedHTTP()
	}
	resp, err := hc.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" && ctx.Err() == nil {
			return 0, nil, fmt.Errorf("node %s: %s %s: %v: %w: %w", c.URL, r.method, r.path, err, workplace.ErrNotAccepted, workplace.ErrNotMeasurable)
		}
		return 0, nil, fmt.Errorf("node %s: %s %s: %v: %w: %w", c.URL, r.method, r.path, err, workplace.ErrUncertain, workplace.ErrNotMeasurable)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("node %s: %s %s: read answer: %v: %w: %w", c.URL, r.method, r.path, err, workplace.ErrUncertain, workplace.ErrNotMeasurable)
	}
	if r.header != nil {
		*r.header = resp.Header
	}
	return resp.StatusCode, data, nil
}

// nodeError is the *NodeError of an answer outside 2xx, with code and detail from its problem
// body when it has one.
func (c *NodeClient) nodeError(status int, data []byte) *NodeError {
	e := &NodeError{Node: c.URL, Status: status, Detail: detail(data)}
	var p nodeapi.Problem
	if json.Unmarshal(data, &p) == nil && (p.Detail != "" || p.Code != "") {
		e.Code, e.Detail = p.Code, p.Detail
	}
	return e
}

func ok(status int) bool { return status >= 200 && status < 300 }

// call checks Info, sends the request, and decodes a 2xx answer into out (nil: ignore it).
func (c *NodeClient) call(ctx context.Context, r request, out any) error {
	if _, err := c.Info(ctx); err != nil {
		return err
	}
	status, data, err := c.send(ctx, r)
	if err != nil {
		return err
	}
	if !ok(status) {
		return c.nodeError(status, data)
	}
	return c.decode(r, data, out)
}

func (c *NodeClient) decode(r request, data []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("node %s: %s %s: answer is not JSON: %v: %w: %w", c.URL, r.method, r.path, err, workplace.ErrUncertain, workplace.ErrNotMeasurable)
	}
	return nil
}

// Info is GET info, asked once per client. A node of another node-API version is an error
// with both versions; that and every other failure (unreachable, no token, an answer outside
// 2xx or not JSON) is kept for every later call of the client, so a node that is down costs one
// timeout per tick, not one per route. Every error of Info is ErrNotAccepted and
// ErrNotMeasurable: the route it guards was not sent.
func (c *NodeClient) Info(ctx context.Context) (nodeapi.Info, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.info != nil {
		return *c.info, nil
	}
	if c.bad != nil {
		return nodeapi.Info{}, c.bad
	}
	r := request{method: http.MethodGet, path: nodeapi.PathInfo}
	status, data, err := c.send(ctx, r)
	if err != nil {
		c.bad = c.notSent("info: %v", err)
		return nodeapi.Info{}, c.bad
	}
	if !ok(status) {
		c.bad = fmt.Errorf("%w: %w: %w", c.nodeError(status, data), workplace.ErrNotAccepted, workplace.ErrNotMeasurable)
		return nodeapi.Info{}, c.bad
	}
	var info nodeapi.Info
	if err := json.Unmarshal(data, &info); err != nil {
		c.bad = c.notSent("info is not JSON: %v", err)
		return nodeapi.Info{}, c.bad
	}
	if info.Version != nodeapi.Version {
		c.bad = c.notSent("node-API version %d, this lighthouse speaks version %d", info.Version, nodeapi.Version)
		return nodeapi.Info{}, c.bad
	}
	c.info = &info
	return info, nil
}

func nodePath(route string, values ...string) string {
	for i := 0; i+1 < len(values); i += 2 {
		route = strings.Replace(route, "{"+values[i]+"}", url.PathEscape(values[i+1]), 1)
	}
	return route
}

// Launch is PUT launch: the node writes the run instructions and the carrier, and answers the
// carrier it wrote.
func (c *NodeClient) Launch(ctx context.Context, id string, req nodeapi.LaunchRequest) (workplace.Carrier, error) {
	var out workplace.Carrier
	err := c.call(ctx, request{method: http.MethodPut, path: nodePath(nodeapi.PathLaunch, "id", id), body: req}, &out)
	return out, err
}

// DeleteCarrier is DELETE carrier; a missing carrier is no error on the node.
func (c *NodeClient) DeleteCarrier(ctx context.Context, id string) error {
	return c.call(ctx, request{method: http.MethodDelete, path: nodePath(nodeapi.PathCarrier, "id", id)}, nil)
}

// Follow is POST follow. Own work (409) is a *workplace.OwnWorkError with the fields the node
// sent.
func (c *NodeClient) Follow(ctx context.Context, id, branch string, created bool) error {
	if _, err := c.Info(ctx); err != nil {
		return err
	}
	r := request{
		method: http.MethodPost, path: nodePath(nodeapi.PathFollow, "id", id),
		body: nodeapi.FollowRequest{Branch: branch, Created: created}, timeout: nodeGitTimeout,
	}
	status, data, err := c.send(ctx, r)
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		var own nodeapi.OwnWork
		if json.Unmarshal(data, &own) == nil && own.Code == nodeapi.CodeOwnWork {
			return &workplace.OwnWorkError{Workspace: own.Workspace, Commits: own.Commits, Dirty: own.Dirty}
		}
	}
	if !ok(status) {
		return c.nodeError(status, data)
	}
	return nil
}

// Sessions is GET sessions: the agent sessions of the workspace at the node's forge.
func (c *NodeClient) Sessions(ctx context.Context, id string) ([]workplace.Session, error) {
	var list []nodeapi.Session
	if err := c.call(ctx, request{method: http.MethodGet, path: nodePath(nodeapi.PathSessions, "id", id)}, &list); err != nil {
		return nil, err
	}
	out := make([]workplace.Session, 0, len(list))
	for _, s := range list {
		e := workplace.Session{Key: s.Key, WorkspaceID: id, State: s.State, SessionID: s.SessionID}
		if t, err := time.Parse(time.RFC3339, s.UpdatedAt); err == nil {
			e.UpdatedAt = t
		}
		out = append(out, e)
	}
	return out, nil
}

// Transcript is GET transcript: the transcript files of the session from the offsets in from
// (files relative to the transcript dir, with slashes), read where the node reads them for
// profile. A session without a transcript (404 no_transcript) is a *NodeError that is also
// fs.ErrNotExist, as on self.
func (c *NodeClient) Transcript(ctx context.Context, id, session, profile string, from map[string]int64) ([]workplace.FileDelta, error) {
	if _, err := c.Info(ctx); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(from))
	for name := range from {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+":"+strconv.FormatInt(from[name], 10))
	}
	q := url.Values{"profile": {profile}}
	if len(parts) > 0 {
		q.Set("from", strings.Join(parts, ","))
	}
	r := request{method: http.MethodGet, path: nodePath(nodeapi.PathTranscript, "id", id, "session", session), query: q,
		timeout: nodeTranscriptTimeout}
	status, data, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	if !ok(status) {
		e := c.nodeError(status, data)
		if e.Code == nodeapi.CodeNoTranscript {
			return nil, fmt.Errorf("%w: %w", e, fs.ErrNotExist)
		}
		return nil, e
	}
	deltas, err := untarDeltas(data, session)
	if err != nil {
		return nil, fmt.Errorf("node %s: transcript of %s: %v: %w", c.URL, session, err, workplace.ErrNotMeasurable)
	}
	return deltas, nil
}

// untarDeltas reads the tar of the transcript route: one delta per member, with the size and
// offset from its PAX records. The node is not trusted: every member must be a file of the
// session, its bytes must run from its offset to its size, and the main transcript must be
// there and come first, as ReadDelta answers it.
func untarDeltas(data []byte, sessionID string) ([]workplace.FileDelta, error) {
	var out []workplace.FileDelta
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if len(out) == 0 {
				return nil, fmt.Errorf("no main transcript %s.jsonl", sessionID)
			}
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		size, err := paxInt(hdr, nodeapi.PAXSize)
		if err != nil {
			return nil, err
		}
		from, err := paxInt(hdr, nodeapi.PAXFrom)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		if !workplace.SessionFile(hdr.Name, sessionID) {
			return nil, fmt.Errorf("member %q is no transcript file of session %s", hdr.Name, sessionID)
		}
		if len(out) == 0 && hdr.Name != sessionID+".jsonl" {
			return nil, fmt.Errorf("first member %s is not the main transcript %s.jsonl", hdr.Name, sessionID)
		}
		if from > size || int64(len(body)) != size-from {
			return nil, fmt.Errorf("member %s: %d bytes from offset %d do not end at size %d", hdr.Name, len(body), from, size)
		}
		out = append(out, workplace.FileDelta{Name: hdr.Name, Size: size, From: from, Data: body})
	}
}

func paxInt(hdr *tar.Header, key string) (int64, error) {
	raw, found := hdr.PAXRecords[key]
	if !found {
		return 0, fmt.Errorf("member %s has no %s", hdr.Name, key)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("member %s: %s %q is not a non-negative integer", hdr.Name, key, raw)
	}
	return n, nil
}

// Relocate is POST relocate: the start command relocated onto the worktree on the node. A
// command the node cannot relocate (422) is an error with its reason.
func (c *NodeClient) Relocate(ctx context.Context, id, command string) (string, error) {
	var out nodeapi.Relocation
	err := c.call(ctx, request{
		method: http.MethodPost, path: nodePath(nodeapi.PathRelocate, "id", id),
		body: nodeapi.Relocation{Command: command}, timeout: nodeGitTimeout,
	}, &out)
	return out.Command, err
}

// Permission is GET permission: the raw permission.json of the run, nil, nil without one (404
// no_permission). Any other 404 is an error: the node did not say the note is missing.
func (c *NodeClient) Permission(ctx context.Context, session string) ([]byte, error) {
	if _, err := c.Info(ctx); err != nil {
		return nil, err
	}
	status, data, err := c.send(ctx, request{method: http.MethodGet, path: nodePath(nodeapi.PathPermission, "session", session)})
	if err != nil {
		return nil, err
	}
	if !ok(status) {
		e := c.nodeError(status, data)
		if e.Status == http.StatusNotFound && e.Code == nodeapi.CodeNoPermission {
			return nil, nil
		}
		return nil, e
	}
	return data, nil
}

// PostFailed is GET post-failed: the raw post-failed.json of the run and its modification time
// on the node, nil, zero, nil without one. Both 404 no_post_failed and a 404 without a code (a
// node from before the route) mean no note; any other failure is an error. A missing or
// unparsable nodeapi.HeaderModTime (a node from before the header) is a zero time: unknown.
func (c *NodeClient) PostFailed(ctx context.Context, session string) ([]byte, time.Time, error) {
	if _, err := c.Info(ctx); err != nil {
		return nil, time.Time{}, err
	}
	var header http.Header
	status, data, err := c.send(ctx, request{method: http.MethodGet, path: nodePath(nodeapi.PathPostFailed, "session", session), header: &header})
	if err != nil {
		return nil, time.Time{}, err
	}
	if !ok(status) {
		e := c.nodeError(status, data)
		if e.Status == http.StatusNotFound && (e.Code == nodeapi.CodeNoPostFailed || e.Code == "") {
			return nil, time.Time{}, nil
		}
		return nil, time.Time{}, e
	}
	mod, err := time.Parse(time.RFC3339Nano, header.Get(nodeapi.HeaderModTime))
	if err != nil {
		mod = time.Time{}
	}
	return data, mod, nil
}

// Git is GET git: Dirty and Unpushed of the worktree on the node, with mergedHead empty or a
// full commit SHA.
func (c *NodeClient) Git(ctx context.Context, id, mergedHead string) (dirty, unpushed bool, err error) {
	var q url.Values
	if mergedHead != "" {
		q = url.Values{"merged_head": {mergedHead}}
	}
	var out nodeapi.GitState
	err = c.call(ctx, request{method: http.MethodGet, path: nodePath(nodeapi.PathGit, "id", id), query: q, timeout: nodeGitTimeout}, &out)
	if err != nil {
		return false, false, err
	}
	return out.Dirty, out.Unpushed, nil
}

// Stop is POST stop with the profile of the run.
func (c *NodeClient) Stop(ctx context.Context, id, profile string, sessionIDs []string) error {
	return c.call(ctx, request{
		method: http.MethodPost, path: nodePath(nodeapi.PathStop, "id", id),
		body: nodeapi.StopRequest{Profile: profile, SessionIDs: sessionIDs}, timeout: nodeProcTimeout,
	}, nil)
}

// Kill is POST kill with the profile of the run: only the claude processes of sessionIDs.
func (c *NodeClient) Kill(ctx context.Context, id, profile string, sessionIDs []string) error {
	return c.call(ctx, request{
		method: http.MethodPost, path: nodePath(nodeapi.PathKill, "id", id),
		body: nodeapi.StopRequest{Profile: profile, SessionIDs: sessionIDs}, timeout: nodeProcTimeout,
	}, nil)
}

// PutUsage is PUT usage: the tick's pause flag and alarm mirror on the node.
func (c *NodeClient) PutUsage(ctx context.Context, req nodeapi.UsageRequest) error {
	return c.call(ctx, request{method: http.MethodPut, path: nodeapi.PathUsage, body: req}, nil)
}

// Markers is GET markers: the resume markers of the node.
func (c *NodeClient) Markers(ctx context.Context) ([]park.Marker, error) {
	var out []park.Marker
	if err := c.call(ctx, request{method: http.MethodGet, path: nodeapi.PathMarkers}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteMarker is DELETE marker: the marker of session on the node, only while its key is key.
func (c *NodeClient) DeleteMarker(ctx context.Context, session, key string) error {
	return c.call(ctx, request{
		method: http.MethodDelete, path: nodePath(nodeapi.PathMarker, "session", session),
		query: url.Values{"key": {key}},
	}, nil)
}
