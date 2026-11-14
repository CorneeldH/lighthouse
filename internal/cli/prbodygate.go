package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/prbody"
)

// prBodyGateUsage is the usage line of pr_body_gate.py, under the name of this command.
const prBodyGateUsage = "usage: lighthouse pr-body-gate <cwd>  (command on stdin)"

// newPrBodyGateCommand is `lighthouse pr-body-gate <cwd>`: the check of pr-create-gate on the
// command on stdin, for a harness without the hook. Exit 0 (stdout: the pass lines), 2 (stderr:
// the blocker), 3 (an open quote), 1 (a configuration error) or 64 (not exactly one argument).
// Every argument is the cwd, as in pr_body_gate.py: no flags, so `--help` is a cwd too.
func newPrBodyGateCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "pr-body-gate <cwd>",
		Short:              "Check the gh pr create in the command on stdin, as pr-create-gate does",
		DisableFlagParsing: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), prBodyGateUsage)
				return &exitError{code: 64, err: errSilent}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			code := prbody.Main(cmd.Context(), args[0], cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), prbody.DefaultEnv())
			if code == 0 {
				return nil
			}
			return &exitError{code: code, err: errSilent}
		},
	}
}
