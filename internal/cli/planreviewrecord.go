package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/datadir"
	"github.com/CorneeldH/lighthouse/internal/review"
)

var (
	recordOutcomes    = []string{"converged", "round_limit", "aborted"}
	recordReviewModes = []string{"codex", "codex_same_family", "fallback"}
)

// planReviewExit is the exit code of the command, with every message already printed.
func planReviewExit(code int) error {
	if code == 0 {
		return nil
	}
	return &exitError{code: code, err: errSilent}
}

// oneOf is the usage error for a flag value outside its choices.
func oneOf(flag, value string, choices []string) error {
	for _, c := range choices {
		if value == c {
			return nil
		}
	}
	return usageErr(fmt.Errorf("argument %s: invalid choice: '%s' (choose from %s)", flag, value, strings.Join(choices, ", ")))
}

// exactArgs makes the wrong number of arguments a usage error.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usageErr(fmt.Errorf("%s: expected %d argument(s), got %d", cmd.CommandPath(), n, len(args)))
		}
		return nil
	}
}

// withArgs sets the positional-argument check after scriptCommand, which sets its own.
func withArgs(cmd *cobra.Command, args cobra.PositionalArgs) *cobra.Command {
	cmd.Args = args
	return cmd
}

// atLeastOneArg is the usage error for a missing document.
func atLeastOneArg(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return usageErr(fmt.Errorf("%s: the following arguments are required: docs", cmd.CommandPath()))
	}
	return nil
}

// newPlanReviewRecordCommand is `lighthouse plan-review-record`, the port of
// plan_review_record.py: start, write, accept-open and pr-open-points.
func newPlanReviewRecordCommand(deps review.Deps) *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "plan-review-record",
		Short: "Attempt directory and measurement record of the adversarial plan and spec review",
		Long: "Exit: 0 ok | 2 wrong usage | 3 not the host from host.toml, or host unknown/not measurable:\n" +
			"no record | 4 write or validation error (only the staging directory stays). On 3 and 4 the\n" +
			"skill carries on with the handoff: the review is the product, the record measurement material.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageErr(fmt.Errorf("%s: a subcommand is required: start, write, accept-open or pr-open-points", cmd.CommandPath()))
		},
	})
	cmd.AddCommand(newRecordStartCommand(deps), newRecordWriteCommand(deps),
		newAcceptOpenCommand(), newPROpenPointsCommand())
	return cmd
}

// recordRepoRoot is the repo root, or the usage-style failure of the script (exit 2).
func recordRepoRoot(cmd *cobra.Command) (string, error) {
	repo, err := config.RepoRoot()
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "plan_review_record: %v\n", err)
		return "", planReviewExit(2)
	}
	return repo, nil
}

func newRecordStartCommand(deps review.Deps) *cobra.Command {
	kind := "plan"
	cmd := withArgs(scriptCommand(&cobra.Command{
		Use:   "start [--kind plan|spec] <DOC>",
		Short: "Reserve the ephemeral attempt directory and record the commit that round 1 reads",
		Long: "Reserves `${TMPDIR:-/tmp}/<kind>-review/<chain-key>/<attempt>/` and records the commit that\n" +
			"round 1 reads. Prints the path. The document must be committed. On the host from host.toml\n" +
			"the attempt also appears at once as `.<attempt>.tmp/` in the data directory.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("--kind", kind, review.Kinds); err != nil {
				return err
			}
			repo, err := recordRepoRoot(cmd)
			if err != nil {
				return err
			}
			tmpRoot := os.Getenv("TMPDIR")
			if tmpRoot == "" {
				tmpRoot = "/tmp"
			}
			attempt, err := review.Start(args[0], repo, tmpRoot, kind, deps)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "start failed: %v\n", err)
				var invalid *review.InvalidError
				if errors.As(err, &invalid) {
					return planReviewExit(2)
				}
				return planReviewExit(1)
			}
			review.Reserve(attempt, deps, cmd.ErrOrStderr())
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), attempt)
			return nil
		},
	}), exactArgs(1))
	cmd.Flags().StringVar(&kind, "kind", "plan", "which review: plan or spec")
	return cmd
}

func newAcceptOpenCommand() *cobra.Command {
	var doc string
	cmd := withArgs(scriptCommand(&cobra.Command{
		Use:   "accept-open <ATTEMPT_DIR> --doc <DOCPATH>",
		Short: "At the round limit, accept every open BLOCKER/MAJOR of the last round",
		Long: "Sets every still-open BLOCKER/MAJOR of the last round to `accepted_open` in\n" +
			"`round-<last>.decisions.json` and appends a block `heading:open_after_review` to DOCPATH\n" +
			"(replaces an existing block; every variant counts). Prints the block, in `[project] language`.\n" +
			"Runs on any host.\n\n" +
			"Exit 0 ok | 2 no open point | 4 read, parse or write error (ACCEPT-OPEN FAILED).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("doc") {
				return usageErr(errors.New("the following arguments are required: --doc"))
			}
			if _, err := recordRepoRoot(cmd); err != nil {
				return err
			}
			block, err := review.AcceptOpen(args[0], doc)
			if errors.Is(err, review.ErrNoOpenPoints) {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err)
				return planReviewExit(2)
			}
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "ACCEPT-OPEN FAILED: %v\n", err)
				return planReviewExit(4)
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), block)
			return nil
		},
	}), exactArgs(1))
	cmd.Flags().StringVar(&doc, "doc", "", "the document that gets the block")
	return cmd
}

func newPROpenPointsCommand() *cobra.Command {
	return withArgs(scriptCommand(&cobra.Command{
		Use:   "pr-open-points <DOCPATH>...",
		Short: "Print the open review points of documents for the PR body",
		Long: "Prints the `heading:open_after_review` blocks of the documents (and of the specs that the\n" +
			"`**Spec:**` line of a plan names) under `## Open review points`; no block, then nothing. A\n" +
			"missing path is skipped. Runs on any host.\n\n" +
			"Exit 0 ok | 4 other read or config error (PR-OPEN-POINTS FAILED).",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := recordRepoRoot(cmd)
			if err != nil {
				return err
			}
			section, err := review.PROpenPoints(args, repo)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "PR-OPEN-POINTS FAILED: %v\n", err)
				return planReviewExit(4)
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), section)
			return nil
		},
	}), atLeastOneArg)
}

// recordFlags are the flags of `write`.
type recordFlags struct {
	outcome, reviewMode, issueBody, issueFetchedAt, finalSHA string
	level, reviewerModel, shadowModel, skillDir              string
	specs                                                    []string
	issue                                                    int
}

func newRecordWriteCommand(deps review.Deps) *cobra.Command {
	var f recordFlags
	cmd := withArgs(scriptCommand(&cobra.Command{
		Use:   "write <ATTEMPT_DIR> --outcome ... --review-mode ... [flags]",
		Short: "Write the round JSONs, their decisions sidecars and one record.json to the data directory",
		Long: "Writes the round JSONs, their decisions sidecars and one `record.json` to\n" +
			"`<data-dir>/<kind>-review/<chain-key>/<attempt>/`, through a staging directory and one\n" +
			"rename. `kind` comes from `attempt.json` (if it is missing, it is `plan`). Only on the host\n" +
			"from host.toml; elsewhere no record and a loud message (exit 3).",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecordWrite(cmd, args[0], &f, deps)
		},
	}), exactArgs(1))
	fl := cmd.Flags()
	fl.StringVar(&f.outcome, "outcome", "", "converged, round_limit or aborted (required)")
	fl.StringVar(&f.reviewMode, "review-mode", "", "codex, codex_same_family or fallback (required)")
	fl.StringArrayVar(&f.specs, "spec", nil, "a spec of the plan, repeatable")
	fl.IntVar(&f.issue, "issue", 0, "the issue that stands in for the specs")
	fl.StringVar(&f.issueBody, "issue-body", "", "file with the body of that issue")
	fl.StringVar(&f.issueFetchedAt, "issue-fetched-at", "", "when the body was fetched")
	fl.StringVar(&f.finalSHA, "final-sha", "", "commit of the final document (default HEAD)")
	fl.StringVar(&f.level, "level", "", "reasoning effort of the review")
	fl.StringVar(&f.reviewerModel, "reviewer-model", "", "model of the fallback reviewer")
	fl.StringVar(&f.shadowModel, "shadow-model", "", "model of the paired shadow run")
	fl.StringVar(&f.skillDir, "skill-dir", "", "skill directory (default .claude/skills/lighthouse-review-<kind>)")
	return cmd
}

// checkWriteFlags is the argparse part of `write`: required flags, choices, and the rule that
// --issue, --issue-body and --issue-fetched-at go together.
func checkWriteFlags(cmd *cobra.Command, f *recordFlags) error {
	fl := cmd.Flags()
	for _, name := range []string{"outcome", "review-mode"} {
		if !fl.Changed(name) {
			return usageErr(fmt.Errorf("the following arguments are required: --%s", name))
		}
	}
	if err := oneOf("--outcome", f.outcome, recordOutcomes); err != nil {
		return err
	}
	if err := oneOf("--review-mode", f.reviewMode, recordReviewModes); err != nil {
		return err
	}
	given := 0
	for _, name := range []string{"issue", "issue-body", "issue-fetched-at"} {
		if fl.Changed(name) {
			given++
		}
	}
	if given != 0 && given != 3 {
		return usageErr(errors.New("--issue, --issue-body and --issue-fetched-at go together"))
	}
	return nil
}

func runRecordWrite(cmd *cobra.Command, attemptDir string, f *recordFlags, deps review.Deps) error {
	if err := checkWriteFlags(cmd, f); err != nil {
		return err
	}
	stderr := cmd.ErrOrStderr()
	repo, err := recordRepoRoot(cmd)
	if err != nil {
		return err
	}
	kind := "plan"
	if meta, err := review.ReadMeta(attemptDir); err == nil {
		if k, ok := meta["kind"].(string); ok {
			kind = k
		}
	}
	hasIssue := cmd.Flags().Changed("issue")
	if kind == "plan" {
		if len(f.specs) == 0 && !hasIssue {
			_, _ = fmt.Fprintln(stderr, "give at least one --spec or --issue")
			return planReviewExit(2)
		}
	} else if len(f.specs) > 0 || hasIssue {
		_, _ = fmt.Fprintf(stderr, "--spec/--issue do not belong to --kind spec: %s is a spec review, "+
			"which has no separate specs or issue\n", attemptDir)
		return planReviewExit(2)
	}
	if reason := deps.HostRefusal(kind); reason != nil {
		_, _ = fmt.Fprintln(stderr, *reason)
		return planReviewExit(3)
	}
	final, err := writeRecord(attemptDir, repo, kind, hasIssue, f, deps)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s-REVIEW-RECORD FAILED: %v. Only the staging directory remains; "+
			"the handoff carries on.\n", strings.ToUpper(kind), err)
		return planReviewExit(4)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), final)
	return nil
}

// writeRecord builds the record and writes it to the data directory.
func writeRecord(attemptDir, repo, kind string, hasIssue bool, f *recordFlags, deps review.Deps) (string, error) {
	in := review.RecordInput{
		Repo: repo, Outcome: f.outcome, ReviewMode: f.reviewMode, Specs: f.specs,
		FinalSHA: f.finalSHA, Level: f.level, ReviewerModel: f.reviewerModel,
		SkillDir: f.skillDir, ShadowModel: f.shadowModel,
	}
	if hasIssue {
		body, err := os.ReadFile(f.issueBody)
		if err != nil {
			return "", err
		}
		in.Issue = &review.Issue{Number: f.issue, FetchedAt: f.issueFetchedAt, Sum: review.Sum(body)}
	}
	if in.FinalSHA == "" {
		head, err := review.Git(repo, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		in.FinalSHA = strings.TrimSpace(string(head))
	}
	if in.SkillDir == "" {
		in.SkillDir = review.DefaultSkillDirs[kind]
	}
	in.CodexHome = os.Getenv("CODEX_SPECREVIEW_HOME")
	if in.CodexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		in.CodexHome = filepath.Join(home, ".codex-specreview")
	}
	record, err := review.BuildRecord(attemptDir, in, deps)
	if err != nil {
		return "", err
	}
	root, err := datadir.Dir(os.Getenv)
	if err != nil {
		return "", err
	}
	return review.Write(attemptDir, record, root, f.issueBody)
}
