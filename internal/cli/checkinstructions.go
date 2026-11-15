package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/checkinstr"
	"github.com/CorneeldH/lighthouse/internal/config"
)

// newCheckInstructionsCommand is `lighthouse check-instructions`, the port of
// the former check-instructions.sh script.
func newCheckInstructionsCommand() *cobra.Command {
	var printExceptions bool
	cmd := scriptCommand(&cobra.Command{
		Use:   "check-instructions",
		Short: "Run the deterministic checks over the instruction layer of this repo",
		Long: "Checks 1, 2, 3, 4, 5, 7, 8, 10, 11 and 14, one line each: ok, FAIL or SKIPPED.\n" +
			"Exit: 0 no finding | 1 at least one FAIL | 2 COULD NOT CHECK (no repo root, broken\n" +
			"lighthouse.toml) or wrong usage | 4 --print-mirror-exceptions could not compare a pair.\n" +
			"The repo root is LIGHTHOUSE_REPO_ROOT, else git; the personal skills of check 5 are\n" +
			"CHECK_INSTRUCTIONS_PERSONAL, else $HOME/.claude/skills.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			root, err := config.RepoRoot()
			if err != nil {
				return &exitError{code: checkinstr.CouldNotCheck(errOut,
					"no repository root: LIGHTHOUSE_REPO_ROOT is not set and git rev-parse --show-toplevel failed")}
			}
			var code int
			if printExceptions {
				code = checkinstr.PrintMirrorExceptions(root, out, errOut)
			} else {
				code = checkinstr.Run(root, personalSkills(), out, errOut)
			}
			if code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	})
	cmd.Flags().BoolVar(&printExceptions, "print-mirror-exceptions", false,
		"print the current mirror differences in the form of the mirror exceptions file")
	return cmd
}

// personalSkills is CHECK_INSTRUCTIONS_PERSONAL, else $HOME/.claude/skills.
func personalSkills() string {
	if dir := os.Getenv("CHECK_INSTRUCTIONS_PERSONAL"); dir != "" {
		return dir
	}
	return os.Getenv("HOME") + "/.claude/skills"
}
