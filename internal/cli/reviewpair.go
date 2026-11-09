package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/review"
)

// newReviewPairCommand is `lighthouse review-pair`, the port of review_pair.py: the paired
// round-1 shadow run of the adversarial plan and spec review.
func newReviewPairCommand(deps review.PairDeps) *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "review-pair",
		Short: "The paired round-1 shadow run of the adversarial plan and spec review",
		Long: "In round 1 a shadow reviewer from the same model family runs alongside Codex, on the same\n" +
			"SHA and with the same prompt. The main thread decides blind on an A/B table; only `merge`\n" +
			"unblinds.\n\n" +
			"Exit: 0 ok | 2 wrong usage | 4 `SHADOW-PAIR FAILED: <reason>` - a changed work tree, a\n" +
			"missing or invalid file, or blind decisions that do not match. On 4 the subcommand writes no\n" +
			"output file. The review then carries on without a pair.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageErr(fmt.Errorf("%s: a subcommand is required: blind or merge", cmd.CommandPath()))
		},
	})
	cmd.AddCommand(
		newPairSubcommand("blind", "Check the tree and the raw files, draw who is A once, print the blind A/B table",
			"First checks that the work tree has not changed since `start` (`HEAD` is `reviewed_sha` from\n"+
				"`attempt.json`, `git status --porcelain` empty), then that `round-1.codex.json` and\n"+
				"`round-1.fallback.json` match the `findings.schema.json` of the skill for `kind`. Draws once\n"+
				"who is A (`pair.json`, reused afterwards) and writes and prints `round-1.blind.md`: the\n"+
				"findings of both as `A-f<i>`/`B-f<i>`, without source.", deps),
		newPairSubcommand("merge", "Unblind: write round-1.json with its decisions and sources sidecars",
			"Reads `pair.json`, the two raw files and the blind decisions `round-1.blind-decisions.json`\n"+
				"(`[{\"finding_id\", \"also\"?, \"decision\", \"reason\"}]`), and writes the canonical\n"+
				"`round-1.json` with `round-1.decisions.json` and `round-1.sources.json`: the sidecars that\n"+
				"`plan-review-record write` takes along to the record. Prints the unblinded table.", deps),
	)
	return cmd
}

func newPairSubcommand(name, short, long string, deps review.PairDeps) *cobra.Command {
	return withArgs(scriptCommand(&cobra.Command{
		Use:   name + " <ATTEMPT_DIR>",
		Short: short,
		Long:  long,
		RunE: func(cmd *cobra.Command, args []string) error {
			code := review.RunPair(name, args[0], config.RepoRoot, deps, cmd.OutOrStdout(), cmd.ErrOrStderr())
			return planReviewExit(code)
		},
	}), exactArgs(1))
}
