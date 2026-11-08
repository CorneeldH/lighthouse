// Package cli defines the lighthouse command line.
package cli

import (
	"errors"
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

// exitError makes execute return code; a nil or errSilent err prints nothing.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// execute runs root and returns the exit code: the code of an exitError, else 1.
// An error that is nil or wraps errSilent is not printed; any other goes to errOut.
func execute(root *cobra.Command, errOut io.Writer) int {
	err := root.Execute()
	if err == nil {
		return 0
	}
	code := 1
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
		if ee.err == nil {
			return code
		}
	}
	if !errors.Is(err, errSilent) {
		_, _ = fmt.Fprintf(errOut, "lighthouse: %v\n", err)
	}
	return code
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
	root.AddCommand(newConfigCommand())
	root.AddCommand(newHeadingsCommand())
	root.AddCommand(newMeasureStartIssueKPICommand())
	root.AddCommand(newProtocolCommand())
	root.AddCommand(newHookCommand())
	root.AddCommand(newUsageCommand())
	root.AddCommand(newServeCommand())
	root.AddCommand(newTickCommand())
	root.AddCommand(newGatherCommand())
	root.AddCommand(newRunCommand())
	root.AddCommand(newTriageCommand(defaultTriageEnv))
	root.AddCommand(newPauseCommand())
	root.AddCommand(newAskCommand(defaultAskEnv()))
	root.AddCommand(newSelfUpdateCommand())
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
