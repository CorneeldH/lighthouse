package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/selfupdate"
	"github.com/CorneeldH/lighthouse/internal/version"
)

// newVersionCommand prints the binary version; its child compare orders two tags.
func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of this binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "lighthouse %s\n", version.String())
			return err
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "compare <a> <b>",
		Short: "Print -1, 0 or 1 when version a is older than, equal to or newer than b",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := selfupdate.CompareTags(withV(args[0]), withV(args[1]))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), n)
			return err
		},
	})
	return cmd
}

// withV adds the v prefix CompareTags requires when the caller left it out.
func withV(s string) string {
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}
