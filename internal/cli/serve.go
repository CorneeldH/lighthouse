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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
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
			repos, err := forgeRepos(hostconfig.Path(), repo, forgeURL, os.Getenv, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			cfg, err := hostconfig.LoadDispatch(hostconfig.Path())
			if err != nil {
				return err
			}
			meta := server.RunsMeta{WorkspaceURL: strings.TrimRight(cfg.Forge.URL, "/"), Night: cfg.Night}
			return serve(ctx, addr, dbPath, repos, meta, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	cmd.Flags().StringVar(&repo, "repo", os.Getenv("LIGHTHOUSE_FORGE_REPO"),
		"GitHub repository to read questions and returned issues from, as owner/name (default: $LIGHTHOUSE_FORGE_REPO); the token comes from GH_TOKEN or GITHUB_TOKEN; ignored when host.toml has [dispatch] repos")
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

// forgeRepos returns the repos serve shows. With [dispatch] repos in the host.toml at hostToml,
// those that load, in that order, each narrowed to the issues in Controller when its board has
// that option; repo is then ignored, with one line on errOut when it is not among them. Without
// [dispatch] repos, repo alone without a filter, as in M1, and none without repo. A host.toml
// that does not load is an error; a served repo that does not load gets one line on errOut and
// is skipped.
func forgeRepos(hostToml, repo, forgeURL string, getenv func(string) string, errOut io.Writer) ([]overview.RepoSource, error) {
	cfg, err := hostconfig.LoadDispatch(hostToml)
	if err != nil {
		return nil, err
	}
	if len(cfg.Repos) == 0 {
		if repo == "" {
			return nil, nil
		}
		return []overview.RepoSource{{Slug: repo, Src: newForge(repo, forgeURL, getenv)}}, nil
	}
	var out []overview.RepoSource
	for _, path := range cfg.Repos {
		r, err := repoconfig.Load(path)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
			continue
		}
		c := newForge(r.Slug, forgeURL, getenv)
		src := overview.RepoSource{Slug: r.Slug, Src: c}
		switch {
		case r.Board == nil:
		case r.Board.StatusOptions["Controller"] == "":
			_, _ = fmt.Fprintf(errOut, "lighthouse: %s: missing forge.board.status_options.Controller: overview shows every open issue\n", path)
		default:
			src.InController = overview.ControllerIssues(c, r.Slug, *r.Board)
		}
		out = append(out, src)
	}
	if repo != "" && !slices.ContainsFunc(out, func(s overview.RepoSource) bool { return s.Slug == repo }) {
		_, _ = fmt.Fprintf(errOut, "lighthouse: --repo %s ignored: serving the repos of [dispatch]\n", repo)
	}
	return out, nil
}

// serve opens the store, listens on addr and serves until ctx is done, then shuts down
// within shutdownTimeout. An empty dbPath means the default path. repos feed the forge endpoints,
// and their slugs pick the rows of the tick state that /api/runs shows.
func serve(ctx context.Context, addr, dbPath string, repos []overview.RepoSource, meta server.RunsMeta, errOut io.Writer) error {
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
	slugs := make([]string, len(repos))
	for i, r := range repos {
		slugs[i] = r.Slug
	}
	srv := &http.Server{Handler: server.New(web.FS(), server.Deps{
		Report: rep.Report,
		Forge:  repos,
		Leftovers: func(ctx context.Context) ([]workplace.Leftover, error) {
			return workplace.Leftovers(ctx, st.DB)
		},
		RunsMeta: meta,
		Runs: func(ctx context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error) {
			return dispatch.LoadView(ctx, st.DB, slugs)
		},
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
