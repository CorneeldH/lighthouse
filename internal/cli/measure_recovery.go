package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery"
)

// measureRecoveryEnv is what `lighthouse measure-recovery` takes from its surroundings, so a
// test can replace it.
type measureRecoveryEnv struct {
	Getenv  func(string) string
	Now     func() time.Time
	GhToken func() string
	// Open opens the forge of repo; nil: the forge of the repo through forgeOpener.
	Open func(ctx context.Context, repo repoconfig.Repo) (recovery.Forge, error)
}

func defaultMeasureRecoveryEnv() measureRecoveryEnv {
	return measureRecoveryEnv{Getenv: os.Getenv, Now: time.Now, GhToken: ghAuthToken}
}

func (e measureRecoveryEnv) open(ctx context.Context, repo repoconfig.Repo) (recovery.Forge, error) {
	if e.Open != nil {
		return e.Open(ctx, repo)
	}
	o := newForgeOpener(e.Getenv, "")
	o.ghToken = e.GhToken
	rf, err := o.forRepo(ctx, repo)
	if err != nil {
		return nil, err
	}
	return rf.Forge, nil
}

// newMeasureRecoveryCommand is `lighthouse measure-recovery --repo-dir <path> [--days N] [--db <path>]`.
func newMeasureRecoveryCommand(env measureRecoveryEnv) *cobra.Command {
	var repoDir, dbPath string
	var days int
	cmd := &cobra.Command{
		Use:   "measure-recovery --repo-dir <path> [--days N] [--db <path>]",
		Short: "Measure the recovery after merge of the PRs of a repo and store it",
		Long: "Measure, for every PR that merged in the last N days and for every open measurement\n" +
			"outside that span, which fixes, reverts and bug issues followed it within 7 days, and\n" +
			"store the result in lighthouse.db. The repo and its default branch come from the\n" +
			"lighthouse.toml in --repo-dir; the clone there is fetched first and measured as\n" +
			"origin/<default branch>. A measurement that is closed (recovered or none) is not touched\n" +
			"again. After that, per trajectory that still lacks a route, an area or the size of a PR, or\n" +
			"has a PR in that span, the route (triage marker), area (label) and code lines are\n" +
			"recorded; a forge error or missing marker or label keeps the old value and prints one\n" +
			"`traits #<issue>: <reason>` line. GitHub only: GitLab cannot list merged PRs yet.\n\n" +
			"Exit: 0 measured, also when PRs were skipped | 1 a config, git, forge or database error |\n" +
			"2 wrong usage. All measuring happens before the first write, so a git or forge error\n" +
			"writes nothing; each PR is then written in its own transaction, so a database error\n" +
			"can leave the PRs before it written.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usageErr(fmt.Errorf("%s: takes no arguments, got %d", cmd.CommandPath(), len(args)))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if repoDir == "" {
				return usageErr(fmt.Errorf("%s: argument --repo-dir is required", cmd.CommandPath()))
			}
			if days < 1 {
				return usageErr(fmt.Errorf("argument --days: %d is below 1", days))
			}
			if err := runMeasureRecovery(cmd.Context(), env, repoDir, dbPath, days, cmd.OutOrStdout()); err != nil {
				return &exitError{code: 1, err: err}
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	cmd.Flags().StringVar(&repoDir, "repo-dir", "", "the clone of the repo to measure (required)")
	cmd.Flags().IntVar(&days, "days", 14, "measure the PRs that merged in the last N days")
	cmd.Flags().StringVar(&dbPath, "db", "", "the database; default the lighthouse.db of the state directory")
	return cmd
}

// runMeasureRecovery loads the repo in dir, opens the database and the forge, and measures
// (recovery.Measure).
func runMeasureRecovery(ctx context.Context, env measureRecoveryEnv, dir, dbPath string, days int, out io.Writer) error {
	repo, err := repoconfig.Load(dir)
	if err != nil {
		return err
	}
	if dbPath == "" {
		if dbPath, err = store.DefaultPath(); err != nil {
			return err
		}
	}
	st, err := store.OpenShared(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	f, err := env.open(ctx, repo)
	if err != nil {
		return err
	}
	return recovery.Measure(ctx, st.DB, f, repo, days, env.Now(), out)
}
