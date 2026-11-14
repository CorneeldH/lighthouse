package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/review"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// newReviewRunCommand is `lighthouse review-run`, the port of review.sh and verify.sh. read is
// the Codex reading for the budget check.
func newReviewRunCommand(read func(context.Context, time.Duration) (usage.CodexState, error)) *cobra.Command {
	o := review.Options{}
	cmd := scriptCommand(&cobra.Command{
		Use:   "review-run [flags] <DOC> <ROUND> <OUT> [PRIOR] [LEVEL]",
		Short: "Run one adversarial review round through an external Codex session",
		Long: "Run one review (or, with --verify, one verification) round through Codex.\n\n" +
			"  DOC    path to the document under review, relative to the repo root\n" +
			"  ROUND  round number\n" +
			"  OUT    where the JSON result lands (.events, .err and .budget.json land next to it)\n" +
			"  PRIOR  path to the previous round, or \"none\" (default). A sidecar\n" +
			"         `<name without .json>.decisions.json` next to it is named in the prompt.\n" +
			"  LEVEL  reasoning effort; empty = from config.toml\n\n" +
			"Exit: 0 ok | 1 prompt not buildable or incomplete result | 2 usage error | 3 codex missing |\n" +
			"4 not logged in | 5 run failed | 6 budget exhausted beforehand (CODEX_BUDGET: fallback — <reason>)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.Kind != "spec" && o.Kind != "plan" {
				return usageErr(fmt.Errorf("--kind: %q is not spec or plan", o.Kind))
			}
			if len(args) > 5 {
				return usageErr(fmt.Errorf("%s: unrecognized arguments: %v", cmd.CommandPath(), args[5:]))
			}
			for i, dst := range []*string{&o.Doc, &o.Round, &o.Out, &o.Prior, &o.Level} {
				*dst = ""
				if i < len(args) {
					*dst = args[i]
				}
			}
			o.Stdout, o.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
			o.ReadBudget = func(ctx context.Context) (usage.BudgetOutcome, int, string) {
				return usage.ReadBudget(ctx, read)
			}
			if code := review.RunRound(cmd.Context(), o); code != 0 {
				return &exitError{code: code, err: errSilent}
			}
			return nil
		},
	})
	cmd.Args = func(*cobra.Command, []string) error { return nil } // RunE counts them, as a usage error
	cmd.Flags().SetInterspersed(true)
	cmd.Flags().StringVar(&o.Kind, "kind", "spec", "which review: spec or plan")
	cmd.Flags().BoolVar(&o.Verify, "verify", false, "run a verification round: verifier prompt and verification schema")
	cmd.Flags().StringVar(&o.SkillDir, "skill-dir", "", "skill directory with the templates (default .claude/skills/lighthouse-review-<kind>, repo-relative)")
	cmd.Flags().StringVar(&o.Prompt, "prompt", "", "prompt template (default: reviewer-prompt.md, or verifier-prompt.md with --verify, in the skill directory)")
	cmd.Flags().StringVar(&o.Schema, "schema", "", "output schema (default: findings.schema.json, or verification.schema.json with --verify, in the skill directory)")
	cmd.Flags().StringArrayVar(&o.Contexts, "context", nil, "extra context path for {{CONTEXT_PATHS}}, repeatable; repo-relative or absolute, must exist")
	return cmd
}
