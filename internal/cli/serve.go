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
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/server"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/web"
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
			return serve(ctx, addr, dbPath, newForge(repo, forgeURL, os.Getenv), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	cmd.Flags().StringVar(&repo, "repo", os.Getenv("LIGHTHOUSE_FORGE_REPO"),
		"GitHub repository to read questions and returned issues from, as owner/name (default: $LIGHTHOUSE_FORGE_REPO); the token comes from GH_TOKEN or GITHUB_TOKEN")
	cmd.Flags().StringVar(&forgeURL, "forge-url", envOr("LIGHTHOUSE_FORGE_URL", "https://api.github.com"),
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

// serve opens the store, listens on addr and serves until ctx is done, then shuts down
// within shutdownTimeout. An empty dbPath means the default path. src feeds the forge endpoints.
func serve(ctx context.Context, addr, dbPath string, src overview.Source, errOut io.Writer) error {
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
	srv := &http.Server{Handler: server.New(web.FS(), rep.Report, src), ReadHeaderTimeout: 10 * time.Second}
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
