package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/datadir"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/review"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// indexDataRoot indexes every review record in the data directory into the shared database.
func indexDataRoot(ctx context.Context, db *sql.DB) (review.IndexResult, error) {
	root, err := datadir.Dir(os.Getenv)
	if err != nil {
		return review.IndexResult{}, err
	}
	return review.IndexRecords(ctx, db, root, time.Now, servedRepos(hostconfig.Path()))
}

// servedRepos lists the repos of the host's dispatch config with their forge.repo slug, for
// tracing records without trajectory. Fail closed: a host config that cannot be read, or any
// listed repo that does not resolve (load error, duplicate slug), gives nil, so no tracing: a
// sha in a served clone and in an omitted one must not link to the wrong repo.
func servedRepos(hostToml string) []review.ServedRepo {
	cfg, err := hostconfig.LoadDispatch(hostToml)
	if err != nil {
		return nil
	}
	var out []review.ServedRepo
	for _, l := range repoconfig.LoadAll(cfg.Repos) {
		if l.Err != nil || l.Repo.Slug == "" {
			return nil
		}
		out = append(out, review.ServedRepo{Slug: l.Repo.Slug, Dir: l.Path})
	}
	return out
}

func newReviewIndexCommand() *cobra.Command {
	var ledgers []string
	var issue int
	cmd := &cobra.Command{
		Use:   "review-index [--ledger <path>]... [--issue <n>]",
		Short: "Index review records and SDD ledgers as findings of their trajectory",
		Long: "Walks every plan and spec review chain in the data directory and indexes the findings of\n" +
			"the newest attempt of each chain into lighthouse.db; a changed record.json updates its rows.\n" +
			"A ledger given with --ledger adds the Ruling: lines of its tasks as task_review findings.\n" +
			"The issue of a ledger is --issue, else the issue-<n> in the plan path on its first line.\n" +
			"Run it before the SDD run removes the ledger.\n\n" +
			"Prints `indexed: new=<n> updated=<n> unchanged=<n> removed=<n>` and one `skipped: <path>: <reason>`\n" +
			"per item it did not index. Exit 0 ok, also with skipped items | 1 database or read error | 2 wrong usage.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usageErr(fmt.Errorf("%s: takes no arguments, got %d", cmd.CommandPath(), len(args)))
			}
			if cmd.Flags().Changed("issue") && issue < 1 {
				return usageErr(errors.New("--issue must be a positive issue number"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			dbPath, err := store.DefaultPath()
			if err != nil {
				return err
			}
			st, err := store.OpenShared(ctx, dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			total, err := indexDataRoot(ctx, st.DB)
			if err != nil {
				return err
			}
			for _, path := range ledgers {
				res, err := indexLedgerFile(ctx, st.DB, path, issue)
				if err != nil {
					return err
				}
				total.New += res.New
				total.Updated += res.Updated
				total.Unchanged += res.Unchanged
				total.Removed += res.Removed
				total.Skipped = append(total.Skipped, res.Skipped...)
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "indexed: new=%d updated=%d unchanged=%d removed=%d\n",
				total.New, total.Updated, total.Unchanged, total.Removed)
			for _, s := range total.Skipped {
				_, _ = fmt.Fprintf(out, "skipped: %s: %s\n", s.Path, s.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&ledgers, "ledger", nil, "an SDD ledger to index, repeatable")
	cmd.Flags().IntVar(&issue, "issue", 0, "the issue of the ledgers (default: from the plan path in the ledger)")
	return cmd
}

// indexLedgerFile indexes one ledger. Its trajectory is --issue, else the issue in the plan
// path on the first line, with the repo from forge.repo (Repo.Slug); a ledger without one is skipped.
func indexLedgerFile(ctx context.Context, db *sql.DB, path string, issue int) (review.IndexResult, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return review.IndexResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return review.IndexResult{}, err
	}
	if issue < 1 {
		planPath, _ := review.ParseLedger(data)
		issue, _ = review.LedgerIssue(planPath)
	}
	var tr *review.Trajectory
	if issue >= 1 {
		if cfg, err := loadRepoAtConfigPath(); err == nil && cfg.Slug != "" {
			tr = &review.Trajectory{Repo: cfg.Slug, Issue: issue}
		}
	}
	return review.IndexLedger(ctx, db, path, tr, time.Now)
}
