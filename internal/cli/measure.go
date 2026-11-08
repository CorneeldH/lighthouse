package cli

import (
	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/kpi"
)

// newMeasureStartIssueKPICommand is `lighthouse measure-start-issue-kpi [metric]`. The package
// kpi writes the table and the messages; the exit code is returned silently.
func newMeasureStartIssueKPICommand() *cobra.Command {
	var dir, since string
	cmd := &cobra.Command{
		Use:   "measure-start-issue-kpi [leadin|worktree-first|mistrigger|paths|gate|scout]",
		Short: "Measure the lighthouse-start-issue skill from the Claude Code transcripts",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			errOut := cmd.ErrOrStderr()
			opts := kpi.Options{Dir: dir}
			if len(args) > 1 {
				return &exitError{code: kpi.RejectMetric(args[1], kpi.StartIssueMetrics, errOut), err: errSilent}
			}
			if len(args) == 1 {
				if args[0] == "" {
					return &exitError{code: kpi.RejectMetric("", kpi.StartIssueMetrics, errOut), err: errSilent}
				}
				opts.Metric = args[0]
			}
			if cmd.Flags().Changed("since") {
				opts.Since = &since
			}
			if code := kpi.RunStartIssue(opts, cmd.OutOrStdout(), errOut); code != 0 {
				return &exitError{code: code, err: errSilent}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "transcript directory (default: this repo's Claude Code projects directory)")
	cmd.Flags().StringVar(&since, "since", "", "YYYY-MM-DD; only records from this day on")
	return cmd
}
