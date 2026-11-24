package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// trajectoryForge is the part of a forge that trajectory reads.
type trajectoryForge interface {
	Issue(ctx context.Context, number int) (forge.Issue, error)
}

// trajectoryEnv is what `lighthouse trajectory` takes from its surroundings, so a test can
// replace it.
type trajectoryEnv struct {
	Getenv  func(string) string
	Now     func() time.Time
	GhToken func() string
	// Open opens the forge of repo; nil: the forge of the repo through forgeOpener.
	Open func(ctx context.Context, repo repoconfig.Repo) (trajectoryForge, error)
}

func defaultTrajectoryEnv() trajectoryEnv {
	return trajectoryEnv{Getenv: os.Getenv, Now: time.Now, GhToken: ghAuthToken}
}

func (e trajectoryEnv) open(ctx context.Context, repo repoconfig.Repo) (trajectoryForge, error) {
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

// newTrajectoryCommand is `lighthouse trajectory <issue> [--repo-dir <path>] [--db <path>]`.
func newTrajectoryCommand(env trajectoryEnv) *cobra.Command {
	var repoDir, dbPath string
	cmd := &cobra.Command{
		Use:   "trajectory <issue> [--repo-dir <path>] [--db <path>]",
		Short: "Show the questions, answers, takeovers and wait time of one trajectory",
		Long: "Show, for the trajectory of an issue, when each question, answer and takeover was and how\n" +
			"the time of the trajectory divides over its causes: active, human, usage, ci_review,\n" +
			"pickup and, when present, unexplained. Each cause line ends in measured, estimate or\n" +
			"running; an active line that may include CI and review time says so. The repo comes from\n" +
			"the lighthouse.toml in --repo-dir (default .); the issue is read from the forge for its\n" +
			"closing time.\n\n" +
			"Exit: 0 printed | 1 a config, forge or database error, or no trajectory for the issue |\n" +
			"2 wrong usage.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErr(fmt.Errorf("%s: takes one issue number, got %d arguments", cmd.CommandPath(), len(args)))
			}
			if n, err := strconv.Atoi(args[0]); err != nil || n < 1 {
				return usageErr(fmt.Errorf("%s: %q is not an issue number", cmd.CommandPath(), args[0]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			issue, _ := strconv.Atoi(args[0])
			if err := runTrajectory(cmd.Context(), env, repoDir, dbPath, issue, cmd.OutOrStdout()); err != nil {
				return &exitError{code: 1, err: err}
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	cmd.Flags().StringVar(&repoDir, "repo-dir", ".", "the clone of the repo")
	cmd.Flags().StringVar(&dbPath, "db", "", "the database; default the lighthouse.db of the state directory")
	return cmd
}

func runTrajectory(ctx context.Context, env trajectoryEnv, dir, dbPath string, issue int, out io.Writer) error {
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
	id, ok, err := telemetry.FindTrajectoryID(ctx, st.DB, repo.Slug, issue)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no trajectory for issue %d", issue)
	}
	f, err := env.open(ctx, repo)
	if err != nil {
		return err
	}
	is, err := f.Issue(ctx, issue)
	if err != nil {
		return fmt.Errorf("trajectory: read issue %d: %w", issue, err)
	}
	tl, err := telemetry.LoadTimeline(ctx, st.DB, id, is.ClosedAt, env.Now())
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, formatTimeline(tl))
	return err
}

// causeOrder is the fixed order of the cause lines: the precedence of the timeline.
var causeOrder = []telemetry.Cause{
	telemetry.CauseActive, telemetry.CauseHuman, telemetry.CauseUsage,
	telemetry.CauseCIReview, telemetry.CausePickup, telemetry.CauseUnexplained,
}

func formatTimeline(tl telemetry.TrajectoryTimeline) string {
	var b strings.Builder
	moments := func(name string, ms []telemetry.Moment) {
		for _, m := range ms {
			fmt.Fprintf(&b, "%s %s", name, m.At.UTC().Format(time.RFC3339))
			if m.Reason != "" {
				fmt.Fprintf(&b, " %s", m.Reason)
			}
			b.WriteByte('\n')
		}
	}
	moments("question", tl.Questions)
	moments("answer", tl.Answers)
	moments("takeover", tl.Takeovers)
	for _, c := range causeOrder {
		t, ok := tl.ByCause[c]
		if !ok {
			continue
		}
		kind := "measured"
		switch {
		case t.Running:
			kind = "running"
		case t.Estimate:
			kind = "estimate"
		}
		fmt.Fprintf(&b, "%s %s %s", c, t.Duration.Round(time.Second), kind)
		if t.InclusiveCIReview {
			b.WriteString(" incl. ci_review")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
