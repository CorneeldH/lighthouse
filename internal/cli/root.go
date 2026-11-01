// Package cli defines the lighthouse command line.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/version"
)

// Execute runs the command line on the process streams and returns the exit code.
func Execute() int {
	return execute(NewRoot(os.Stdin, os.Stdout, os.Stderr), os.Stderr)
}

// execute runs root and returns the exit code; an error other than errSilent goes to errOut.
func execute(root *cobra.Command, errOut io.Writer) int {
	if err := root.Execute(); err != nil {
		if err != errSilent {
			_, _ = fmt.Fprintf(errOut, "lighthouse: %v\n", err)
		}
		return 1
	}
	return 0
}

// NewRoot builds the root command with its subcommands.
func NewRoot(in io.Reader, out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "lighthouse",
		Short:         "Install the lighthouse packages and run the lighthouse server",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(newInitCommand())
	root.AddCommand(newUsageCommand())
	root.AddCommand(newServeCommand())
	root.AddCommand(newTickCommand())
	root.AddCommand(newGatherCommand())
	root.AddCommand(newRunCommand())
	root.AddCommand(newPauseCommand())
	root.AddCommand(newAskCommand(defaultAskEnv()))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version of this binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "lighthouse %s\n", version.String())
			return err
		},
	})
	return root
}
