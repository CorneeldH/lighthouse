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
	"github.com/CorneeldH/lighthouse/internal/nodeserve"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/priorities"
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
	var overviewInterval time.Duration
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the lighthouse server",
		Long: "Run the lighthouse server: the API and the web UI on one address. SIGINT and SIGTERM\n" +
			"stop it cleanly; only one server runs per database.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if overviewInterval <= 0 {
				return fmt.Errorf("--overview-interval must be positive, got %s", overviewInterval)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			repos, slugs, err := forgeRepos(hostconfig.Path(), repo, forgeURL, os.Getenv, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			cfg, err := hostconfig.LoadDispatch(hostconfig.Path())
			if err != nil {
				return err
			}
			meta := server.RunsMeta{WorkspaceURL: strings.TrimRight(cfg.Forge.LinkURL(), "/"), Night: cfg.Night}
			node, err := nodeRoutes(hostconfig.Path(), cfg, os.Getenv)
			if err != nil {
				return err
			}
			return serve(ctx, addr, dbPath, repos, slugs, meta, node, overviewInterval, server.PrioritiesInterval, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	cmd.Flags().StringVar(&repo, "repo", os.Getenv("LIGHTHOUSE_FORGE_REPO"),
		"GitHub repository to read questions and returned issues from, as owner/name (default: $LIGHTHOUSE_FORGE_REPO); the token comes from GH_TOKEN or GITHUB_TOKEN; ignored when host.toml has [dispatch] repos")
	cmd.Flags().StringVar(&forgeURL, "forge-url", envOr("LIGHTHOUSE_FORGE_URL", ""),
		"GitHub API base URL for GitHub repos only (default: $LIGHTHOUSE_FORGE_URL, else https://api.github.com); GitLab repos use their [forge] host, and the GitHub token is sent only to this URL")
	// Hidden: only the e2e tests shorten it, so Refresh shows a forge change within a second.
	cmd.Flags().DurationVar(&overviewInterval, "overview-interval", server.RefreshInterval,
		"time from the start of one forge overview build to the next")
	_ = cmd.Flags().MarkHidden("overview-interval")
	return cmd
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// forgeRepos returns the repos serve shows the forge of, and the slugs whose runs /api/runs
// shows. With [dispatch] repos in the host.toml at hostToml, the sources are those that load
// and open, in that order, each narrowed to the issues in Controller when its board has that
// option; slugs holds every repo that loads, also one without a token, each slug once. repo is
// then ignored, with one line on errOut when it is not among them. Without [dispatch] repos,
// repo alone as a GitHub repo without a filter, as in M1, and nothing without repo. A host.toml
// that does not load is an error; a served repo that does not load, has a duplicate slug or
// has no forge (no token, an Open error) gets one line on errOut and is skipped.
func forgeRepos(hostToml, repo, forgeURL string, getenv func(string) string, errOut io.Writer) (sources []overview.RepoSource, slugs []string, err error) {
	return forgeReposWith(hostToml, repo, newForgeOpener(getenv, forgeURL), errOut)
}

// forgeReposWith is forgeRepos with the opener given, so a test can point the GitLab repos at
// a fake through gitlabURL. The GitHub URL is o.githubURL, for GitHub repos only; production leaves gitlabURL empty.
func forgeReposWith(hostToml, repo string, o *forgeOpener, errOut io.Writer) (sources []overview.RepoSource, slugs []string, err error) {
	cfg, err := hostconfig.LoadDispatch(hostToml)
	if err != nil {
		return nil, nil, err
	}
	// serve never asks the token login, so no ctx of a request reaches the opener.
	ctx := context.Background()
	skip := func(path string, err error) {
		_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
	}
	if len(cfg.Repos) == 0 {
		if repo == "" {
			return nil, nil, nil
		}
		rf, err := o.forRepo(ctx, repoconfig.Repo{Slug: repo, Kind: "github"})
		if err != nil {
			skip(repo, err)
			return nil, []string{repo}, nil
		}
		return []overview.RepoSource{{Slug: repo, Src: rf.Forge}}, []string{repo}, nil
	}
	for _, l := range repoconfig.LoadAll(cfg.Repos) {
		path, r := l.Path, l.Repo
		if r.Slug != "" && !slices.Contains(slugs, r.Slug) {
			slugs = append(slugs, r.Slug)
		}
		if l.Err != nil {
			skip(path, l.Err)
			continue
		}
		rf, err := o.forRepo(ctx, r)
		if err != nil {
			skip(path, err)
			continue
		}
		src := overview.RepoSource{Slug: r.Slug, Src: rf.Forge, Board: r.Board != nil,
			Controller: r.Board != nil && r.Board.StatusOptions["Controller"] != ""}
		if src.Board && !src.Controller {
			_, _ = fmt.Fprintf(errOut, "lighthouse: %s: missing forge.board.status_options.Controller: overview shows every open issue\n", path)
		}
		sources = append(sources, src)
	}
	if repo != "" && !slices.Contains(slugs, repo) {
		_, _ = fmt.Fprintf(errOut, "lighthouse: --repo %s ignored: serving the repos of [dispatch]\n", repo)
	}
	return sources, slugs, nil
}

// nodeRoutes returns the node-API handler when the host.toml at hostToml has [node], and nil
// without it. It fails when the forge token of [dispatch.forge] is missing or empty, so a node
// never starts serving routes it cannot answer. The state dir is where the tick keeps its own,
// and the profiles are the names of [dispatch.profiles] on this node.
func nodeRoutes(hostToml string, cfg hostconfig.Dispatch, getenv func(string) string) (http.Handler, error) {
	n, present, err := hostconfig.LoadNode(hostToml)
	if err != nil || !present {
		return nil, err
	}
	if err := hostconfig.ValidateNodeForge(cfg); err != nil {
		return nil, err
	}
	stateDir, err := tickEnv{Getenv: getenv}.stateDir()
	if err != nil {
		return nil, err
	}
	profiles := make(map[string]bool, len(cfg.Profiles))
	for name := range cfg.Profiles {
		profiles[name] = true
	}
	return nodeserve.New(nodeserve.Deps{
		StateDir:  stateDir,
		TokenFile: n.TokenFile,
		Forge:     cfg.Forge,
		Profiles:  profiles,
	}), nil
}

// serve opens the store, listens on addr and serves until ctx is done, then shuts down
// within shutdownTimeout. An empty dbPath means the default path. repos feed the forge endpoints,
// and slugs pick the rows of the tick state that /api/runs shows. node, when not nil, is the
// node-API under /api/node/.
func serve(ctx context.Context, addr, dbPath string, repos []overview.RepoSource, slugs []string, meta server.RunsMeta, node http.Handler,
	interval, prioInterval time.Duration, errOut io.Writer) error {
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
	refreshCtx, cancelRefresh := context.WithCancel(ctx)
	defer cancelRefresh()
	read, _, stopped := startOverview(refreshCtx, repos, interval)
	prioSrcs := make([]priorities.Source, 0, len(repos))
	prioForges := make(map[string]forge.Forge, len(repos))
	for _, r := range repos {
		prioSrcs = append(prioSrcs, priorities.Source{Slug: r.Slug, Src: r.Src})
		prioForges[r.Slug] = r.Src
	}
	prioRead, prioPatch, prioStopped := startPriorities(refreshCtx, prioSrcs, read, prioInterval)
	srv := &http.Server{Handler: server.New(web.FS(), server.Deps{
		Report:     rep.Report,
		Forge:      repos,
		Overview:   read,
		Priorities: prioRead,
		// Both are set together: the rank endpoint needs the forges and the cache patch.
		PriorityForges:  prioForges,
		PatchPriorities: prioPatch,
		Leftovers: func(ctx context.Context) ([]workplace.Leftover, error) {
			return workplace.Leftovers(ctx, st.DB)
		},
		RunsMeta: meta,
		Node:     node,
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
	// The refresher stops before the store closes, also when serve ended on a serve error.
	cancelRefresh()
	stopped()
	prioStopped()
	if err := st.Close(); err != nil && runErr == nil {
		runErr = err
	}
	return runErr
}

// startOverview returns the reader of the forge overview. Without repos it builds on every read
// (which reports forge.ErrNotConfigured), starts nothing and stopped is a no-op. With repos it
// starts a Refresher on ctx that builds every interval, and retries a failed build after
// server.RefreshRetry or interval, whichever is shorter; stopped blocks until its goroutine has
// returned. patch is the Refresher's Modify; the overview has no use for it.
func startOverview(ctx context.Context, repos []overview.RepoSource, interval time.Duration) (read func(context.Context) (overview.Overview, error), patch func(func(overview.Overview) overview.Overview) bool, stopped func()) {
	build := func(ctx context.Context) (overview.Overview, error) { return overview.BuildAll(ctx, repos) }
	return startRefresher(ctx, len(repos) > 0, build, interval, server.RefreshRetry)
}

// startPriorities returns the reader of the prioritisation lists, built by its own Refresher
// so a slow planning read never delays the overview. Each build reads the overview through
// overviewRead for the board statuses and passes its error on, and then calls priorities.Build.
// Without srcs it builds on every read (which reports forge.ErrNotConfigured), starts nothing
// and stopped is a no-op; otherwise it behaves as startOverview. patch changes the cached
// lists in place, and does nothing (false) without a refresher.
func startPriorities(ctx context.Context, srcs []priorities.Source, overviewRead func(context.Context) (overview.Overview, error),
	interval time.Duration) (read func(context.Context) ([]priorities.Repo, error), patch func(func([]priorities.Repo) []priorities.Repo) bool, stopped func()) {
	build := func(ctx context.Context) ([]priorities.Repo, error) {
		if len(srcs) == 0 {
			return nil, forge.ErrNotConfigured
		}
		ov, err := overviewRead(ctx)
		if err != nil {
			return nil, err
		}
		return priorities.Build(ctx, srcs, ov.Statuses)
	}
	// A failed planning read costs API points, so it retries at PrioritiesRetry, not RefreshRetry.
	return startRefresher(ctx, len(srcs) > 0, build, interval, server.PrioritiesRetry)
}

// startRefresher starts a Refresher of build on ctx when enabled, and returns its reader, its
// patch (Modify; a no-op returning false when disabled) and a stopped that blocks until its goroutine has returned. Disabled, the reader builds on every
// read and stopped is a no-op.
func startRefresher[T any](ctx context.Context, enabled bool, build server.BuildFunc[T], interval, retry time.Duration) (read func(context.Context) (T, error), patch func(func(T) T) bool, stopped func()) {
	if !enabled {
		return build, func(func(T) T) bool { return false }, func() {}
	}
	r := server.NewRefresher(build, interval, min(retry, interval), server.BuildTimeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	return r.Latest, r.Modify, func() { <-done }
}
