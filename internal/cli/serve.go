package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/server"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/web"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	defaultAddr     = "127.0.0.1:8127"
	shutdownTimeout = 10 * time.Second
)

func newServeCommand() *cobra.Command {
	var addr, dbPath, repo, forgeURL string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the lighthouse server",
		Long: "Run the lighthouse server: the API and the web UI on one address. SIGINT and SIGTERM\n" +
			"stop it cleanly; only one server runs per database.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			c := newForge(repo, forgeURL, os.Getenv)
			inController, err := controllerFilter(hostconfig.Path(), repo, c, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return serve(ctx, addr, dbPath, c, inController, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	cmd.Flags().StringVar(&repo, "repo", os.Getenv("LIGHTHOUSE_FORGE_REPO"),
		"GitHub repository to read questions and returned issues from, as owner/name (default: $LIGHTHOUSE_FORGE_REPO); the token comes from GH_TOKEN or GITHUB_TOKEN")
	cmd.Flags().StringVar(&forgeURL, "forge-url", envOr("LIGHTHOUSE_FORGE_URL", forge.DefaultBaseURL),
		"GitHub API base URL (default: $LIGHTHOUSE_FORGE_URL, else https://api.github.com); the token is sent there")
	return cmd
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// newForge builds the GitHub client for repo against baseURL (trailing slashes dropped), with
// the token from the environment.
func newForge(repo, baseURL string, getenv func(string) string) *forge.Client {
	c := forge.New(repo, githubToken(getenv))
	c.BaseURL = strings.TrimRight(baseURL, "/")
	return c
}

// githubToken returns GH_TOKEN, else GITHUB_TOKEN, else "".
func githubToken(getenv func(string) string) string {
	if t := getenv("GH_TOKEN"); t != "" {
		return t
	}
	return getenv("GITHUB_TOKEN")
}

// controllerFilter narrows the overview to the issues in Controller when repo is served by
// [dispatch] of the host.toml at hostToml and has a [forge.board]. Otherwise it returns nil:
// without [dispatch] the overview shows every open issue, as in M1. A host.toml that does not
// load is an error; a served repo that does not load gets one line on errOut and is skipped.
func controllerFilter(hostToml, repo string, c *forge.Client, errOut io.Writer) (overview.Filter, error) {
	cfg, err := hostconfig.LoadDispatch(hostToml)
	if err != nil {
		return nil, err
	}
	for _, path := range cfg.Repos {
		r, err := repoconfig.Load(path)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
			continue
		}
		if r.Slug != repo || r.Board == nil {
			continue
		}
		if r.Board.StatusOptions["Controller"] == "" {
			_, _ = fmt.Fprintf(errOut, "lighthouse: %s: missing forge.board.status_options.Controller: overview shows every open issue\n", path)
			return nil, nil
		}
		return overview.ControllerIssues(c, repo, *r.Board), nil
	}
	return nil, nil
}

// serve opens the store, listens on addr and serves until ctx is done, then shuts down
// within shutdownTimeout. An empty dbPath means the default path. src feeds the forge endpoints,
// narrowed by inController; nil shows every open issue.
func serve(ctx context.Context, addr, dbPath string, src overview.Source, inController overview.Filter,
	errOut io.Writer) error {
	if dbPath == "" {
		p, err := store.DefaultPath()
		if err != nil {
			return err
		}
		dbPath = p
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = st.Close()
		return err
	}
	_, _ = fmt.Fprintf(errOut, "lighthouse: listening on %s\n", ln.Addr())

	rep := &usage.Reporter{ReadCodex: usage.ReadCodex, Now: time.Now}
	if paths, err := usage.DefaultPaths(); err == nil {
		rep.Cache = paths.Cache()
	}
	srv := &http.Server{Handler: server.New(web.FS(), rep.Report, src, inController, func(ctx context.Context) ([]workplace.Leftover, error) {
		return workplace.Leftovers(ctx, st.DB)
	}), ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	var runErr error
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutCtx); errors.Is(err, context.DeadlineExceeded) {
			_ = srv.Close()
		}
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}
	if err := st.Close(); err != nil && runErr == nil {
		runErr = err
	}
	return runErr
}
