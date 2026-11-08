package cli

import (
	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// newHeadingsCommand is `lighthouse headings <key>`: the heading in the project language.
// Messages are written by config.RunHeadings and the exit code is returned silently.
func newHeadingsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "headings <key>",
		Short: "Print one heading of headings.toml in the project language",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := config.RunHeadings(args, cmd.OutOrStdout(), cmd.ErrOrStderr()); code != 0 {
				return &exitError{code: code, err: errSilent}
			}
			return nil
		},
	}
}
