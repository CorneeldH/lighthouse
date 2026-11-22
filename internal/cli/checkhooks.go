package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/hookcheck"
)

// hooksCheck is the hook check init runs after it wrote; a test replaces it.
var hooksCheck = func(root string, out, errOut io.Writer) int {
	return hookcheck.Run(hookcheck.DefaultEnv(root), out, errOut)
}

// checkHooksAfterInit runs the hook check on root with all output on errOut. It never changes
// the exit code of init: a failing check ends as one hint line.
func checkHooksAfterInit(root string, errOut io.Writer) {
	if hooksCheck(root, errOut, errOut) != 0 {
		_, _ = fmt.Fprintln(errOut, "check-hooks: some hooks do not run; see `lighthouse check-hooks` for details")
	}
}

// newCheckHooksCommand is `lighthouse check-hooks`.
func newCheckHooksCommand() *cobra.Command {
	return scriptCommand(&cobra.Command{
		Use:   "check-hooks",
		Short: "Check that the hooks registered in .claude/settings.json can run on this machine",
		Long: "Checks the hooks registered in .claude/settings.json: that the interpreter and the\n" +
			"scripts exist, that line endings are fit for the shell, and that each registered\n" +
			"hook starts in a smoketest.\n" +
			"Exit: 0 every hook can run | 1 at least one finding | 2 COULD NOT CHECK (no repo\n" +
			"root, unreadable settings) or wrong usage.\n" +
			"The repo root is LIGHTHOUSE_REPO_ROOT, else git. init runs this check after it wrote,\n" +
			"on stderr, and it never changes the exit code of init.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			root, err := config.RepoRoot()
			if err != nil {
				return &exitError{code: hookcheck.CouldNotCheck(errOut,
					"no repository root: LIGHTHOUSE_REPO_ROOT is not set and git rev-parse --show-toplevel failed")}
			}
			if code := hooksCheck(root, out, errOut); code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	})
}
