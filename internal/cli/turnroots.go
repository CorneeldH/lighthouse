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

// newTurnRootsCommand is `lighthouse turn-roots [--match PATTERN]... [--files]`: the checkouts the turn of
// the Stop payload on stdin wrote in. Exit 0: without --match the roots, sorted, one per line;
// with it `touched\t<root>` or `untouched\t<root>` per root, and with --files after those a
// `file\t<root>\t<path>` line per touched path (none for a root in doubt). Exit 1: the checkout cannot be
// determined, the reason on stdout.
func newTurnRootsCommand() *cobra.Command {
	var patterns []string
	var files bool
	cmd := &cobra.Command{
		Use:   "turn-roots",
		Short: "Print the checkouts the turn of a Stop payload wrote in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			lines, err := turnRootsLines(raw, os.Getenv("CLAUDE_PROJECT_DIR"), patterns, files)
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
	cmd.Flags().BoolVar(&files, "files", false, "with --match: after the verdicts, a `file\\t<root>\\t<path>` line per touched path")
	return cmd
}

// turnRootsLines are the output lines: the roots, or with patterns one verdict per root.
func turnRootsLines(raw []byte, projectDir string, patterns []string, files bool) ([]string, error) {
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
	if files {
		for _, v := range verdicts {
			for _, f := range v.Files {
				lines = append(lines, "file\t"+v.Root+"\t"+f)
			}
		}
	}
	return lines, nil
}
