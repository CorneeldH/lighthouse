// Package nodeserve is the node-API of lighthouse serve on a kenn-forge node: what the tick
// does locally on self, serve does here for a run on this node.
package nodeserve

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/version"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

// Deps are what the node-API reads from.
type Deps struct {
	StateDir  string                   // the node's state dir: carriers, runs, markers
	TokenFile string                   // [node] token_file: the bearer token, read per request
	Forge     hostconfig.DispatchForge // the node's own forge
	Profiles  map[string]bool          // the names of [dispatch.profiles] on the node
	Home      string                   // where ~/.claude/projects lives; "": os.UserHomeDir
	Run       workplace.Runner         // nil: workplace.ExecRunner()
	Now       func() time.Time         // nil: time.Now
}

type handler struct {
	d       Deps
	api     huma.API
	mux     *http.ServeMux
	adapter *kennforge.Adapter // the adapter of the node's own forge, Node ""
}

// New returns the node-API handler. Every route sits under nodeapi.Prefix and needs the token.
func New(d Deps) http.Handler { return newHandler(d) }

func newHandler(d Deps) *handler {
	if d.Run == nil {
		d.Run = workplace.ExecRunner()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("lighthouse node", version.String())
	cfg.OpenAPIPath = "" // the node-API is for the tick only: no description, no docs
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil
	h := &handler{
		d:   d,
		api: humago.New(mux, cfg),
		mux: mux,
		adapter: &kennforge.Adapter{
			Client:     kennforge.New(d.Forge.URL, d.Forge.TokenFile),
			StateDir:   d.StateDir,
			UnitPrefix: d.Forge.UnitPrefix,
			Run:        d.Run,
			Home:       d.Home,
		},
	}
	huma.Get(h.api, nodeapi.PathInfo, func(context.Context, *struct{}) (*infoOutput, error) {
		return &infoOutput{Body: nodeapi.Info{
			Version:  nodeapi.Version,
			Binary:   version.String(),
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		}}, nil
	}, func(op *huma.Operation) { op.OperationID = "get-node-info" })
	h.registerStatemap()
	h.registerGit()
	h.registerTranscript()
	return h
}

type infoOutput struct{ Body nodeapi.Info }

// ServeHTTP checks the bearer token, read from TokenFile on every request, and then serves the
// route. No header is 401, a wrong token 403, and a token file that is missing or empty fails
// the request with 500: it never lets an empty bearer in.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeProblem(w, &nodeapi.Problem{Status: http.StatusUnauthorized, Detail: "missing bearer token"})
		return
	}
	raw, err := os.ReadFile(h.d.TokenFile)
	want := strings.TrimSpace(string(raw))
	if err != nil || want == "" {
		writeProblem(w, &nodeapi.Problem{Status: http.StatusInternalServerError, Detail: "node token file unreadable or empty"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(want)) != 1 {
		writeProblem(w, &nodeapi.Problem{Status: http.StatusForbidden, Detail: "wrong bearer token"})
		return
	}
	h.mux.ServeHTTP(w, r)
}

func writeProblem(w http.ResponseWriter, p *nodeapi.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
