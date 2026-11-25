package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/initcmd"
)

func newUninstallCommand() *cobra.Command {
	var o initcmd.Options
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove what lighthouse init installed from this repository",
		Long: "Remove what lighthouse init installed from this repository. Shows a diff per file and\n" +
			"removes only after approval (Remove lighthouse? [y/N]), or directly with --yes.\n" +
			"A unit you changed locally makes the whole run refuse, and nothing is removed, unless\n" +
			"--force is given.\n\n" +
			"What init created is undone: files it created and sections it added. Project files\n" +
			"that were seeded (labels file, direction documents), .lighthouse/run, labels on the\n" +
			"forge, host.toml and the binary stay; the closing report names them on stderr.\n" +
			"An install from before this version has no record of what init created: lighthouse.toml\n" +
			"and the rest stay for you to remove.",
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
			if initcmd.Uninstall(o) != 0 {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&o.Yes, "yes", false, "approve the removal without asking")
	cmd.Flags().BoolVar(&o.Force, "force", false, "remove locally changed units too")
	return cmd
}
