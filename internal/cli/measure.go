package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/kpi"
)

// newMeasureStartIssueKPICommand is `lighthouse measure-start-issue-kpi [metric]`. The package
// kpi writes the table and the messages; the exit code is returned silently.
func newMeasureStartIssueKPICommand() *cobra.Command {
	return newMeasureCommand("measure-start-issue-kpi", "lighthouse-start-issue", kpi.StartIssueMetrics, kpi.RunStartIssue)
}

// newMeasureFixPRKPICommand is `lighthouse measure-fix-pr-kpi [metric]`.
func newMeasureFixPRKPICommand() *cobra.Command {
	return newMeasureCommand("measure-fix-pr-kpi", "lighthouse-fix-pr", kpi.FixPRMetrics, kpi.RunFixPR)
}

func newMeasureCommand(name, skill string, metrics []string, run func(kpi.Options, io.Writer, io.Writer) int) *cobra.Command {
	var dir, since string
	cmd := &cobra.Command{
		Use:   name + " [" + strings.Join(metrics, "|") + "]",
		Short: "Measure the " + skill + " skill from the Claude Code transcripts",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			errOut := cmd.ErrOrStderr()
			opts := kpi.Options{Dir: dir}
			if len(args) > 1 {
				return &exitError{code: kpi.RejectMetric(args[1], metrics, errOut), err: errSilent}
			}
			if len(args) == 1 {
				if args[0] == "" {
					return &exitError{code: kpi.RejectMetric("", metrics, errOut), err: errSilent}
				}
				opts.Metric = args[0]
			}
			if cmd.Flags().Changed("since") {
				opts.Since = &since
			}
			if code := run(opts, cmd.OutOrStdout(), errOut); code != 0 {
				return &exitError{code: code, err: errSilent}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "transcript directory (default: this repo's Claude Code projects directory)")
	cmd.Flags().StringVar(&since, "since", "", "YYYY-MM-DD; only records from this day on")
	return cmd
}
