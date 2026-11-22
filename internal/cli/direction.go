package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/direction"
)

// newDirectionCommand is `lighthouse direction`, the commands on the direction documents.
func newDirectionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "direction",
		Short: "Work with the direction documents of this repo",
	}
	cmd.AddCommand(newDirectionCheckCommand())
	return cmd
}

func newDirectionCheckCommand() *cobra.Command {
	return scriptCommand(&cobra.Command{
		Use:   "check",
		Short: "Check the shape of the strategy, the tenets and the area files",
		Long: "Prints each finding as path:line: message. A repo that sets none of the strategy,\n" +
			"tenets and areas paths of lighthouse.toml opted out: nothing is printed.\n" +
			"Exit: 0 no finding or opted out | 1 at least one finding | 2 COULD NOT CHECK (no repo\n" +
			"root, unparsable lighthouse.toml).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			couldNot := func(reason string) error {
				_, _ = fmt.Fprintf(errOut, "direction check: COULD NOT CHECK — %s\n", reason)
				return &exitError{code: 2}
			}
			root, err := config.RepoRoot()
			if err != nil {
				return couldNot("no repository root: LIGHTHOUSE_REPO_ROOT is not set and git rev-parse --show-toplevel failed")
			}
			if err := direction.CheckConfig(root); err != nil {
				return couldNot(fmt.Sprintf("lighthouse.toml: %v", err))
			}
			findings := direction.Check(root)
			for _, f := range findings {
				_, _ = fmt.Fprintln(out, f)
			}
			if len(findings) > 0 {
				return &exitError{code: 1}
			}
			return nil
		},
	})
}
