package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/turnroots"
)

// newTurnRootsCommand is `lighthouse turn-roots [--match PATTERN]...`: the checkouts the turn of
// the Stop payload on stdin wrote in. Exit 0: without --match the roots, sorted, one per line;
// with it `touched\t<root>` or `untouched\t<root>` per root. Exit 1: the checkout cannot be
// determined, the reason on stdout.
func newTurnRootsCommand() *cobra.Command {
	var patterns []string
	cmd := &cobra.Command{
		Use:   "turn-roots",
		Short: "Print the checkouts the turn of a Stop payload wrote in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			lines, err := turnRootsLines(raw, os.Getenv("CLAUDE_PROJECT_DIR"), patterns)
			var undetermined *turnroots.Undetermined
			if errors.As(err, &undetermined) {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), undetermined.Msg); err != nil {
					return err
				}
				return &exitError{code: 1, err: errSilent}
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
			return err
		},
	}
	cmd.Flags().StringArrayVar(&patterns, "match", nil, "judge each root on this pattern")
	return cmd
}

// turnRootsLines are the output lines: the roots, or with patterns one verdict per root.
func turnRootsLines(raw []byte, projectDir string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return turnroots.Resolve(raw, projectDir)
	}
	verdicts, err := turnroots.Classify(raw, projectDir, patterns)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		state := "untouched"
		if v.Touched {
			state = "touched"
		}
		lines = append(lines, state+"\t"+v.Root)
	}
	return lines, nil
}
