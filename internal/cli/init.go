package cli

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/initcmd"
)

func newInitCommand() *cobra.Command {
	var o initcmd.Options
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Install the lighthouse packages into this repository",
		Long: "Install the lighthouse packages into this repository. Shows a diff per file and\n" +
			"writes only after approval, or directly with --yes. --upgrade replaces what an\n" +
			"earlier init wrote; a locally changed unit is refused unless --force is given.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			o.Dir = dir
			o.In = cmd.InOrStdin()
			o.Out = cmd.OutOrStdout()
			o.Err = cmd.ErrOrStderr()
			if initcmd.Run(o) != 0 {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&o.Upgrade, "upgrade", false, "replace what an earlier init installed")
	cmd.Flags().BoolVar(&o.Force, "force", false, "overwrite locally changed units")
	cmd.Flags().BoolVar(&o.Yes, "yes", false, "approve every change without asking")
	return cmd
}

// errSilent makes the command fail without printing again; initcmd already reported.
var errSilent = errors.New("")
