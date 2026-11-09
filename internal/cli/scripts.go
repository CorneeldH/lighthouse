package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// usageErr marks err as a usage error: exit 2, as argparse does. Cobra's default is 1.
func usageErr(err error) error { return &exitError{code: 2, err: err} }

// scriptCommand makes cmd a script port: a bad flag is a usage error (exit 2), and so is any
// argument that no subcommand takes. Run it on every command of the port.
func scriptCommand(cmd *cobra.Command) *cobra.Command {
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usageErr(fmt.Errorf("%s: unrecognized arguments: %v", cmd.CommandPath(), args))
		}
		return nil
	}
	return cmd
}
