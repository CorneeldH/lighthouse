package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

// nodeServes holds one client of lighthouse serve per node of host.toml, for one tick or one
// lighthouse run: every adapter on a node, readHostFacts and the usage push share it, so GET info
// goes once per tick and node (#327). The local workplace has no nodes and gets none.
type nodeServes struct {
	clients map[string]*kennforge.NodeClient // per node_id
	names   map[string]string                // node_id -> name in host.toml
}

// newNodeServes builds the clients of the nodes of cfg. A 401 or 403 of serve on any route is a
// configuration error: it writes one line per node to errOut, the first time it meets it.
func newNodeServes(cfg hostconfig.Dispatch, errOut io.Writer) *nodeServes {
	s := &nodeServes{clients: map[string]*kennforge.NodeClient{}, names: map[string]string{}}
	if cfg.Workplace == hostconfig.WorkplaceLocal {
		return s
	}
	seen := &authSeen{w: errOut, logged: map[string]bool{}}
	for name, n := range cfg.Nodes {
		c := kennforge.NewNodeClient(n.ServeURL, n.ServeTokenFile)
		if c.HTTP != nil {
			hc := *c.HTTP
			hc.Transport = authWatch{base: transport(hc.Transport), name: name, id: n.NodeID, seen: seen}
			c.HTTP = &hc
		}
		s.clients[n.NodeID] = c
		s.names[n.NodeID] = name
	}
	return s
}

func transport(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return http.DefaultTransport
	}
	return rt
}

// ids are the node_ids of s, ordered by their name in host.toml.
func (s *nodeServes) ids() []string {
	out := make([]string, 0, len(s.clients))
	for id := range s.clients {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(s.names[a], s.names[b]) })
	return out
}

// serve is the serve client of node (a node_id) for this tick; nil on self and for a node that is
// not in host.toml. Without serves (a test that builds workplaces by hand) it builds a client of
// its own.
func (e tickEnv) serve(cfg hostconfig.Dispatch, node string) *kennforge.NodeClient {
	if node == "" {
		return nil
	}
	if e.serves != nil {
		return e.serves.clients[node]
	}
	for _, n := range cfg.Nodes {
		if n.NodeID == node {
			return kennforge.NewNodeClient(n.ServeURL, n.ServeTokenFile)
		}
	}
	return nil
}

// withServes is env with the serve clients of cfg for this tick or run.
func (e tickEnv) withServes(cfg hostconfig.Dispatch, errOut io.Writer) tickEnv {
	e.serves = newNodeServes(cfg, errOut)
	return e
}

// authSeen writes the configuration-error line of a node once.
type authSeen struct {
	mu     sync.Mutex
	logged map[string]bool
	w      io.Writer
}

// authWatch sees every answer of serve on one node and reports a 401 or 403 through seen.
type authWatch struct {
	base     http.RoundTripper
	name, id string
	seen     *authSeen
}

func (a authWatch) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := a.base.RoundTrip(r)
	if err == nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		a.seen.mu.Lock()
		defer a.seen.mu.Unlock()
		if !a.seen.logged[a.id] {
			a.seen.logged[a.id] = true
			_, _ = fmt.Fprintf(a.seen.w, "lighthouse: node %s (%s): serve answered %d: configuration error: "+
				"check serve_token_file of the node against [node] token_file on it\n", a.name, a.id, resp.StatusCode)
		}
	}
	return resp, err
}

// nodeMarkers reads the resume markers of every node (GET markers), in the order of their names.
// from maps the session of each marker to its node_id; unread holds the error of each node whose
// markers did not read.
func (e tickEnv) nodeMarkers(ctx context.Context) (markers []park.Marker, from map[string]string, unread map[string]error) {
	from, unread = map[string]string{}, map[string]error{}
	if e.serves == nil {
		return nil, from, unread
	}
	for _, id := range e.serves.ids() {
		ms, err := e.serves.clients[id].Markers(ctx)
		if err != nil {
			unread[id] = err
			continue
		}
		for _, m := range ms {
			if m.SessionID == "" {
				continue
			}
			markers = append(markers, m)
			from[m.SessionID] = id
		}
	}
	return markers, from, unread
}

// pushUsage sends the tick's PAUSE and alarm mirror to every node (PUT usage), as they are, or
// none for a file that is not there. A node that does not take it keeps what it had: one line on
// errOut, no tick error. A file that does not read sends nothing, so a node never loses a pause
// the tick could not read.
func pushUsage(ctx context.Context, env tickEnv, errOut io.Writer) {
	if env.serves == nil || len(env.serves.clients) == 0 {
		return
	}
	req, err := env.usageRequest()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: usage to nodes: %s\n", oneLine(err))
		return
	}
	for _, id := range env.serves.ids() {
		if err := env.serves.clients[id].PutUsage(ctx, req); err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: usage to node %s: %s\n", env.serves.names[id], oneLine(err))
		}
	}
}

// usageRequest is the content of the tick's PAUSE and alarm mirror; nil for a file that is not
// there, or for the mirror when the tick has no place for it.
func (e tickEnv) usageRequest() (nodeapi.UsageRequest, error) {
	var req nodeapi.UsageRequest
	pauseFile, err := park.PauseFile(e.Getenv)
	if err != nil {
		return req, err
	}
	if req.Pause, err = readOptional(pauseFile); err != nil {
		return req, err
	}
	meter, err := e.meter()
	if err != nil {
		return req, fmt.Errorf("usage meter: %w", err)
	}
	if meter.Paths.Mirror != "" {
		req.Alarm, err = readOptional(meter.Paths.Mirror)
	}
	return req, err
}

// readOptional is the content of path, nil when it does not exist.
func readOptional(path string) (*string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

// sweepNodeLeftovers removes what node runs that are no longer open left in stateDir: their
// transcript mirrors (kennforge.SweepMirrors) and the tick-side carriers of #187
// (carriers/<node>/<id>.json), which nothing removes since Discard goes to serve. A carrier on
// self (carriers/<id>.json) is not its.
func sweepNodeLeftovers(ctx context.Context, db *sql.DB, stateDir string) error {
	open, err := runs.OpenAll(ctx, db)
	if err != nil {
		return err
	}
	sessions, workspaces := map[string]bool{}, map[string]bool{}
	for _, r := range open {
		if r.Node == "" {
			continue
		}
		sessions[r.Node+"/"+r.SessionID] = true
		if r.WorkspaceID != "" {
			workspaces[r.Node+"/"+r.WorkspaceID] = true
		}
	}
	errs := []error{kennforge.SweepMirrors(stateDir, func(node, session string) bool { return sessions[node+"/"+session] })}
	root := filepath.Join(stateDir, "carriers")
	nodes, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("sweep carriers: %w", err))
	}
	for _, n := range nodes {
		if !n.IsDir() || !workplace.SafeID.MatchString(n.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, n.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("sweep carriers: %w", err))
			continue
		}
		for _, f := range files {
			id, ok := strings.CutSuffix(f.Name(), ".json")
			if f.IsDir() || !ok || !workplace.SafeID.MatchString(id) || workspaces[n.Name()+"/"+id] {
				continue
			}
			if err := workplace.RemoveCarrier(stateDir, n.Name(), id); err != nil {
				errs = append(errs, fmt.Errorf("sweep carriers: %w", err))
			}
		}
		_ = os.Remove(filepath.Join(root, n.Name())) // only when empty
	}
	return errors.Join(errs...)
}
