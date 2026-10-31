package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// tickEnv is what tick reads from the machine: the environment and the path of host.toml.
type tickEnv struct {
	Getenv   func(string) string
	HostToml string
}

func newTickCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one dispatcher tick",
		Long: "Run one dispatcher tick over the repos in host.toml [dispatch]. A repo that cannot be\n" +
			"dispatched is skipped loudly and tick exits 1; a tick that is still running makes this one exit 0.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env := tickEnv{Getenv: os.Getenv, HostToml: hostconfig.Path()}
			return tick(cmd.Context(), env, dbPath, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	return cmd
}

// tick takes tick.lock, loads the repos of host.toml and skips, loudly, each one that is not
// dispatchable. A busy lock is not an error. At least one skip returns errSilent: the reasons
// are already on errOut.
func tick(ctx context.Context, env tickEnv, dbPath string, errOut io.Writer) error {
	runtime := env.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtime) {
		return errors.New("tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock")
	}
	dir := filepath.Join(runtime, "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tick: %w", err)
	}
	lock, err := flock.TryLock(filepath.Join(dir, "tick.lock"))
	if errors.Is(err, flock.ErrHeld) {
		_, _ = fmt.Fprintln(errOut, "lighthouse: previous tick still running")
		return nil
	}
	if err != nil {
		return fmt.Errorf("tick: %w", err)
	}
	defer func() { _ = lock.Close() }()

	if githubToken(env.Getenv) == "" {
		return errors.New("tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN")
	}
	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return err
	}
	if dbPath == "" {
		if dbPath, err = store.DefaultPath(); err != nil {
			return err
		}
	}
	st, err := store.OpenShared(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	skipped := false
	for _, path := range cfg.Repos {
		repo, err := repoconfig.Load(path)
		if err == nil {
			err = repoconfig.Dispatchable(repo)
		}
		if err != nil {
			skipped = true
			reason := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ")
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, reason)
		}
	}
	if skipped {
		return errSilent
	}
	return nil
}
