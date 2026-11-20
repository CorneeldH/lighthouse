package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/boardstart"
	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/issuestate"
)

// newStateCommand is `lighthouse state show|label|return`: the state of an issue, its blocking
// label with the rank rule, and giving it back to To do.
func newStateCommand() *cobra.Command {
	return newStateCommandWith(newDefaultForgeOpener(), issuestate.DBRuns(""))
}

// newStateCommandWith opens the forge with o (the repo, board and token_env of lighthouse.toml)
// and reads the runs with runs.
func newStateCommandWith(o *forgeOpener, runs func(context.Context, string) ([]dispatch.RunView, error)) *cobra.Command {
	env := issuestate.Env{Config: boardstart.LoadConfig, Forge: forgeOpenWith(o, loadRepoAtConfigPath), Runs: runs}
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
			"Exit 1 on a wrong number or key (no forge call) and on a forge error; exit 2 on a config error.",
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
