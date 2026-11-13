package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

// newCodexUsageCommand is `lighthouse codex-usage read|budget`, the port of codex_usage.py.
// read gets the time per home from the config.
func newCodexUsageCommand(read func(context.Context, time.Duration) (usage.CodexState, error)) *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "codex-usage",
		Short: "Read the Codex usage window, or decide if Codex fits a review round",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageErr(fmt.Errorf("%s: a subcommand is required: read or budget", cmd.CommandPath()))
		},
	})
	cmd.AddCommand(scriptCommand(&cobra.Command{
		Use:   "read",
		Short: "Print the reading as JSON",
		Long: "Print the reading as JSON, indented by two spaces (exit 0). Not measurable: one line\n" +
			"`not measurable: <reason>` on stderr, nothing on stdout, exit 1. A reset_credits object\n" +
			"that cannot be read fails that Codex home.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printCodex(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				func(ctx context.Context) (usage.CodexState, error) {
					cfg, err := usage.LoadBudgetConfig()
					if err != nil {
						return usage.CodexState{}, fmt.Errorf("config: %w", err)
					}
					return read(ctx, cfg.Timeout)
				})
		},
	}))
	cmd.AddCommand(scriptCommand(&cobra.Command{
		Use:   "budget",
		Short: "Decide before a review round: codex or fallback (exit 0/6)",
		Long: "Print one JSON object (decision, reason, window, reset_at, session_pct, week_pct, source,\n" +
			"lower_bound, thresholds) and exit 6 on `fallback`, else 0. It writes no marker and asks no\n" +
			"question. A broken config is not_measurable, with a line on stderr.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			outcome, code, stderr := usage.ReadBudget(cmd.Context(), read)
			if stderr != "" {
				_, _ = io.WriteString(cmd.ErrOrStderr(), stderr)
			}
			data, err := marshalNoEscape(outcome, "")
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
			if code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}))
	return cmd
}

// marshalNoEscape is json.Marshal (indent "" gives one line) or json.MarshalIndent with two
// spaces, without HTML escaping, ending in a newline.
func marshalNoEscape(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
