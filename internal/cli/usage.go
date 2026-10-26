package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

func newUsageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Measure the Claude and Codex usage windows",
		Long: "Measure the Claude usage windows (session and week) and keep the cache and the\n" +
			"alarm file the shell guard reads, or print the Codex windows. refresh and tick\n" +
			"always exit 0; a failure is one line on stderr and the old cache stays. codex\n" +
			"exits 1 when Codex is not measurable.",
	}
	var force bool
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Measure now, unless the cache is younger than the TTL",
		Long: "Run `claude -p /usage`, write the alarm file and the cache. Without --force a\n" +
			"cache younger than cache_ttl_seconds is kept and claude is not started.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runMeter(cmd, func(m usage.Meter) (usage.Outcome, error) { return m.Refresh(force) })
			return nil
		},
	}
	refresh.Flags().BoolVar(&force, "force", false, "measure even when the cache is fresh")
	tick := &cobra.Command{
		Use:   "tick",
		Short: "Prune an expired alarm; refresh only while claude runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runMeter(cmd, func(m usage.Meter) (usage.Outcome, error) { return m.Tick() })
			return nil
		},
	}
	codex := &cobra.Command{
		Use:   "codex",
		Short: "Print the Codex usage windows as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printCodex(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), usage.ReadCodex)
		},
	}
	cmd.AddCommand(refresh, tick, codex)
	return cmd
}

// runMeter builds the meter from the environment and host.toml and reports op's outcome.
// A broken host.toml is reported but not fatal: the meter runs on the defaults.
func runMeter(cmd *cobra.Command, op func(usage.Meter) (usage.Outcome, error)) {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	paths, err := usage.DefaultPaths()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "usage: %s: %v\n", usage.Failed, err)
		return
	}
	cfg, err := usage.LoadConfig(hostconfig.Path())
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "usage: config: %v\n", err)
	}
	m := usage.NewMeter(paths, cfg)
	report(out, errOut, func() (usage.Outcome, error) { return op(m) })
}

// report runs measure and prints one line: the outcome on out, a failure on errOut.
// A panic counts as a failure, so the command still exits 0.
func report(out, errOut io.Writer, measure func() (usage.Outcome, error)) {
	outcome, err := func() (outcome usage.Outcome, err error) {
		defer func() {
			if r := recover(); r != nil {
				outcome, err = usage.Failed, fmt.Errorf("%v", r)
			}
		}()
		return measure()
	}()
	if err != nil || outcome == usage.Failed {
		_, _ = fmt.Fprintf(errOut, "usage: %s: %v\n", outcome, err)
		return
	}
	_, _ = fmt.Fprintf(out, "usage: %s\n", outcome)
}

// printCodex prints the state read by read as indented JSON on out. When Codex is
// not measurable it prints one line on errOut and returns errSilent (exit 1).
func printCodex(ctx context.Context, out, errOut io.Writer, read func(context.Context) (usage.CodexState, error)) error {
	state, err := read(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "not measurable: %v\n", err)
		return errSilent
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "not measurable: %v\n", err)
		return errSilent
	}
	_, err = fmt.Fprintf(out, "%s\n", data)
	return err
}
