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
	root := NewRoot(os.Stdin, os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

// NewRoot builds the root command with its subcommands.
func NewRoot(in io.Reader, out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "lighthouse",
		Short:         "Install and upgrade the lighthouse packages in a repository",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
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
