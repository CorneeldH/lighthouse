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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/server"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/web"
)

const (
	defaultAddr     = "127.0.0.1:8127"
	shutdownTimeout = 10 * time.Second
)

func newServeCommand() *cobra.Command {
	var addr, dbPath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the lighthouse server",
		Long: "Run the lighthouse server: the API and the web UI on one address. SIGINT and SIGTERM\n" +
			"stop it cleanly; only one server runs per database.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return serve(ctx, addr, dbPath, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	return cmd
}

// serve opens the store, listens on addr and serves until ctx is done, then shuts down
// within shutdownTimeout. An empty dbPath means the default path.
func serve(ctx context.Context, addr, dbPath string, errOut io.Writer) error {
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

	srv := &http.Server{Handler: server.New(web.FS()), ReadHeaderTimeout: 10 * time.Second}
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
