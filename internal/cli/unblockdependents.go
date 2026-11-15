package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/unblock"
)

// blockedLabel is the blocking label from `labels.blocked_issue`, with the config reader's default.
func blockedLabel() (string, error) {
	path, err := config.Path()
	if err != nil {
		return "", err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return "", err
	}
	label, err := config.Lookup(cfg, "labels.blocked_issue")
	if err != nil {
		return "", err
	}
	return fmt.Sprint(label), nil
}

func newUnblockDependentsCommand() *cobra.Command {
	return newUnblockCommand(procrun.Exec(unblock.GhTimeout))
}

// newUnblockCommand is `lighthouse unblock-dependents --issue <nr> [--dry-run]`, the port of
// unblock_dependents.py. run starts `gh`; a test hands in a replay.
func newUnblockCommand(run procrun.Func) *cobra.Command {
	var issue int
	var dryRun bool
	cmd := scriptCommand(&cobra.Command{
		Use:   "unblock-dependents --issue <nr> [--dry-run]",
		Short: "Release the issues that waited on a just-closed issue",
		Long: "Release the issues that waited on a just-closed issue, when it was their last open blocker.\n\n" +
			"Only a close as `completed` releases. A dependent that carries `[labels] blocked_issue` and has no\n" +
			"other open blocker loses the label and gets a comment; the closed issue gets one comment back.\n" +
			"The summary goes to stdout and is appended to $GITHUB_STEP_SUMMARY. Exit: 0 ok | 1 something\n" +
			"could not be read or written | 2 wrong usage or a broken config.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("issue") {
				return usageErr(fmt.Errorf("%s: the following arguments are required: --issue", cmd.CommandPath()))
			}
			label, err := blockedLabel()
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "unblock-dependents: %v\n", err)
				return &exitError{code: 2, err: errSilent}
			}
			code, lines := unblock.Run(cmd.Context(), run, issue, label, dryRun)
			if err := unblock.Write(cmd.OutOrStdout(), lines); err != nil {
				return err
			}
			if err := appendSummary(lines); err != nil {
				return err
			}
			if code != 0 {
				return &exitError{code: code, err: errSilent}
			}
			return nil
		},
	})
	cmd.Flags().IntVar(&issue, "issue", 0, "the closed issue")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "read only, write nothing")
	return cmd
}

// appendSummary adds the lines to $GITHUB_STEP_SUMMARY, when it is set.
func appendSummary(lines []string) (err error) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // the runner names this file
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return unblock.Write(file, lines)
}
