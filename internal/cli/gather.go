package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

func newGatherCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "gather <repo-dir>",
		Short: "Print the snapshot of one repo as JSON",
		Long: "Gather the snapshot of one repo, as tick would, and print it as JSON: the board items in\n" +
			"Controller or On hold with their PRs and trusted comments, and the open runs. It reads the\n" +
			"same token, host.toml and database as tick, but takes no tick.lock, runs no precheck and\n" +
			"saves no tick state.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := tickEnv{Getenv: os.Getenv, HostToml: hostconfig.Path()}
			return gather(cmd.Context(), env, args[0], dbPath, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	return cmd
}

// gather writes the snapshot of the repo in dir to out as JSON, indented by two spaces and
// ending in a newline. Ignored 🤖-comments go to errOut, as in tick.
func gather(ctx context.Context, env tickEnv, dir, dbPath string, out, errOut io.Writer) error {
	token := githubToken(env.Getenv)
	if token == "" {
		return errors.New("gather: no GitHub token: set GH_TOKEN or GITHUB_TOKEN")
	}
	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return err
	}
	repo, err := repoconfig.Load(dir)
	if err == nil {
		err = repoconfig.Dispatchable(repo)
	}
	if err != nil {
		return fmt.Errorf("gather: %w", err)
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

	open, err := runs.Open(ctx, st.DB, repo.Slug)
	if err != nil {
		return err
	}
	c := env.client(repo.Slug, token)
	pre, err := dispatch.Prepare(ctx, c, repo, open)
	if err != nil {
		return err
	}
	who, err := tokenLogin(ctx, c)
	if err != nil {
		return err
	}
	snap, err := dispatch.Complete(ctx, c, pre, dispatch.NewTrust(c, cfg.AnswerPermissions), who, errOut, env.now())
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("gather: encode snapshot: %w", err)
	}
	_, err = out.Write(append(data, '\n'))
	return err
}
