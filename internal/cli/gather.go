package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
)

func newGatherCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "gather <repo-dir>",
		Short: "Print the snapshot of one repo as JSON",
		Long: "Gather the snapshot of one repo, as tick would, and print it as JSON: the board items in\n" +
			"Controller or On hold and the To do items the tick parked, with their PRs and trusted\n" +
			"comments, and the open runs. It reads the same token, host.toml and database as tick, but\n" +
			"takes no tick.lock, runs no precheck and saves no tick state.",
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
	rf, err := newForgeOpener(env.Getenv, env.ForgeURL).forRepo(ctx, repo)
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

	pre, err := prepare(ctx, st.DB, cfg, rf, repo, false, env.now())
	if err != nil {
		return err
	}
	who, err := rf.who()
	if err != nil {
		return err
	}
	snap, err := dispatch.Complete(ctx, rf.Forge, pre, dispatch.NewTrust(rf.Forge, cfg.AnswerPermissions), who, errOut, env.now())
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
