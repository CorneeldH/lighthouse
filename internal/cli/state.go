package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/boardstart"
	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/issuestate"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// newStateCommand is `lighthouse state show|label|return`: the state of an issue, its blocking
// label with the rank rule, and giving it back to To do.
func newStateCommand() *cobra.Command {
	return newStateCommandWith(newDefaultForgeOpener(), dbRunState(""))
}

// newStateCommandWith opens the forge with o (the repo, board and token_env of lighthouse.toml)
// and reads the run state of an issue with run.
func newStateCommandWith(o *forgeOpener, run func(context.Context, string, int) (string, error)) *cobra.Command {
	env := issuestate.Env{Config: boardstart.LoadConfig, Forge: forgeOpenWith(o, loadRepoAtConfigPath), Run: run}
	exit := func(code int) error {
		if code == 0 {
			return nil
		}
		return &exitError{code: code, err: errSilent}
	}
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Show the state of an issue, set its blocking label, or give it back to To do",
		Long: "Keys: needs_shaping, needs_decision, blocked_issue, blocked_external, needs_hands, needs_testing (rank order).\n" +
			"Exit 1 on a wrong number or key (no forge call) and on a forge error; exit 2 on a config error or a missing token.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show <nr>",
			Short: "Print the state of issue <nr>",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				return exit(issuestate.Show(c.Context(), env, args[0], c.OutOrStdout()))
			},
		},
		&cobra.Command{
			Use:   "label <nr> <key>",
			Short: "Set blocking label <key> on issue <nr>; the lowest rank wins, the others go",
			Args:  cobra.ExactArgs(2),
			RunE: func(c *cobra.Command, args []string) error {
				return exit(issuestate.Label(c.Context(), env, args[0], args[1], c.OutOrStdout()))
			},
		},
		&cobra.Command{
			Use:   "return <nr> <key>",
			Short: "Give issue <nr> back: blocking label <key> and board status To do; Priority stays",
			Args:  cobra.ExactArgs(2),
			RunE: func(c *cobra.Command, args []string) error {
				return exit(issuestate.GiveBack(c.Context(), env, args[0], args[1], c.OutOrStdout()))
			},
		},
	)
	return cmd
}

// dbRunState reads the state of the run of an issue from the database at path ("" is
// store.DefaultPath): "open", "parked", or "" without a run. It opens the database without the
// serve lock, so it works next to a running `lighthouse serve`. `state show` only reads: a
// missing database counts as no run and is neither created nor migrated (store.OpenShared does
// both), so the file is stat'ed first.
func dbRunState(path string) func(ctx context.Context, repo string, nr int) (string, error) {
	return func(ctx context.Context, repo string, nr int) (string, error) {
		p := path
		if p == "" {
			var err error
			if p, err = store.DefaultPath(); err != nil {
				return "", err
			}
		}
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			return "", nil
		} else if err != nil {
			return "", err
		}
		st, err := store.OpenShared(ctx, p)
		if err != nil {
			return "", err
		}
		defer func() { _ = st.Close() }()
		runs, _, err := dispatch.LoadView(ctx, st.DB, []string{repo})
		if err != nil {
			return "", err
		}
		for _, r := range runs {
			if r.Issue == nr {
				return r.State, nil
			}
		}
		return "", nil
	}
}
