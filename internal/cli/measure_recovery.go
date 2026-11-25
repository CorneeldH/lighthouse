package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// recoveryForge is the part of a forge that measure-recovery reads.
type recoveryForge interface {
	MergedPRs(ctx context.Context, since, until time.Time) ([]forge.PRSummary, error)
	PullHeads(ctx context.Context) ([]forge.PullHead, error)
	SearchIssues(ctx context.Context, text string) ([]forge.Issue, error)
	Issue(ctx context.Context, number int) (forge.Issue, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
}

// measureRecoveryEnv is what `lighthouse measure-recovery` takes from its surroundings, so a
// test can replace it.
type measureRecoveryEnv struct {
	Getenv  func(string) string
	Now     func() time.Time
	GhToken func() string
	// Open opens the forge of repo; nil: the forge of the repo through forgeOpener.
	Open func(ctx context.Context, repo repoconfig.Repo) (recoveryForge, error)
}

func defaultMeasureRecoveryEnv() measureRecoveryEnv {
	return measureRecoveryEnv{Getenv: os.Getenv, Now: time.Now, GhToken: ghAuthToken}
}

func (e measureRecoveryEnv) open(ctx context.Context, repo repoconfig.Repo) (recoveryForge, error) {
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

var issueHeadRe = regexp.MustCompile(`^issue-(\d+)(-|$)`)

// recoveryJob is one PR to measure.
type recoveryJob struct {
	pr       int
	issue    int
	mergedAt time.Time
}

// runMeasureRecovery fetches the default branch, measures every PR that needs it and only then
// writes: a git or forge error leaves the database as it was.
func runMeasureRecovery(ctx context.Context, env measureRecoveryEnv, dir, dbPath string, days int, out io.Writer) error {
	repo, err := repoconfig.Load(dir)
	if err != nil {
		return err
	}
	if repo.Kind == "gitlab" {
		return errors.New("measure-recovery: GitLab is not supported: MergedPRs is not implemented for it")
	}
	if err := gitFetch(ctx, dir, repo.DefaultBranch); err != nil {
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

	now := env.Now()
	states, err := telemetry.RecoveryStates(ctx, st.DB, repo.Slug)
	if err != nil {
		return err
	}
	known := make(map[int]telemetry.RecoveryState, len(states))
	for _, s := range states {
		known[s.PR] = s
	}

	f, err := env.open(ctx, repo)
	if err != nil {
		return err
	}
	jobs, skipped, err := recoveryJobs(ctx, f, known, now, days)
	if err != nil {
		return err
	}
	var recs []telemetry.RecoveryRecord
	if len(jobs) > 0 {
		since := slices.MinFunc(jobs, func(a, b recoveryJob) int { return a.mergedAt.Compare(b.mergedAt) }).mergedAt
		src := telemetry.GitSource{Dir: dir, Branch: "origin/" + repo.DefaultBranch, Issues: telemetry.ForgeIssues(f, since)}
		for _, j := range jobs {
			rec, err := telemetry.MeasureRecovery(ctx, src, j.pr, j.mergedAt, now)
			if errors.Is(err, telemetry.ErrNoMergeCommit) {
				skipped = append(skipped, skip{j.pr, "no_merge_commit"})
				continue
			}
			if err != nil {
				return err
			}
			rounds, err := telemetry.CountFixPRRounds(ctx, st.DB, repo.Slug, j.pr, j.mergedAt)
			if err != nil {
				return err
			}
			recs = append(recs, telemetry.RecoveryRecord{
				Origin: telemetry.Origin{Source: "recovery", SourceID: fmt.Sprintf("%s#%d", repo.Slug, j.pr), RecordedAt: now},
				Repo:   repo.Slug, Issue: j.issue, Recovery: rec, FixPRRounds: rounds,
			})
		}
	}
	for _, r := range recs {
		if _, _, err := telemetry.RecordRecovery(ctx, st.DB, r); err != nil {
			return err
		}
	}
	if err := printRecoveries(out, recs, skipped); err != nil {
		return err
	}
	src := telemetry.GitSource{Dir: dir, Branch: "origin/" + repo.DefaultBranch}
	return runTraitPass(ctx, f, src, st.DB, repo, now, days, out)
}

// runTraitPass records route, area and size of every trajectory that still lacks one or has a PR
// in the window. A forge error, a missing marker or a missing label leaves the old value and
// writes one `traits #<issue>: <reason>` line; the exit code does not change.
func runTraitPass(ctx context.Context, f recoveryForge, src telemetry.GitSource, db *sql.DB, repo repoconfig.Repo, now time.Time, days int, out io.Writer) error {
	targets, err := telemetry.TraitTargets(ctx, db, repo.Slug, now, days, repo.Labels.AreaPrefix != "")
	if err != nil {
		return err
	}
	for _, tg := range targets {
		if !tg.NeedsFill && !tg.InWindow {
			continue
		}
		t := telemetry.Traits{Repo: repo.Slug, Issue: tg.Issue}
		var reasons []string
		before := tg.LastMergedAt
		if tg.FirstReviewAt != nil {
			before = *tg.FirstReviewAt
		}
		issue, err := f.Issue(ctx, tg.Issue)
		var comments []protocol.Comment
		if err != nil {
			reasons = append(reasons, "read issue: "+err.Error())
		} else if comments, err = f.Comments(ctx, tg.Issue); err != nil {
			reasons = append(reasons, "read comments: "+err.Error())
		}
		if err == nil {
			if repo.Labels.AreaPrefix != "" {
				if area, ok := telemetry.AreaFromLabels(issue.Labels, repo.Labels.AreaPrefix); ok {
					t.Area = &area
				} else {
					reasons = append(reasons, "no single area label")
				}
			}
			if route, ok := telemetry.RouteFromComments(comments, before); ok {
				t.Route = &route
			} else {
				reasons = append(reasons, "no triage marker")
			}
		}
		missing, err := prsWithoutCodeLines(ctx, db, repo.Slug, tg.Issue)
		if err != nil {
			return err
		}
		for _, pr := range missing {
			n, err := src.CodeLines(ctx, pr)
			if err != nil {
				reasons = append(reasons, fmt.Sprintf("code lines of #%d: %v", pr, err))
				continue
			}
			if t.CodeLines == nil {
				t.CodeLines = map[int]int{}
			}
			t.CodeLines[pr] = n
		}
		if err := telemetry.RecordTraits(ctx, db, t); err != nil {
			return err
		}
		for _, r := range reasons {
			if _, err := fmt.Fprintf(out, "traits #%d: %s\n", tg.Issue, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// prsWithoutCodeLines lists the PRs of a trajectory whose recovery row has no size yet.
func prsWithoutCodeLines(ctx context.Context, db *sql.DB, repo string, issue int) ([]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.pr FROM recoveries r JOIN trajectories t ON t.id = r.trajectory_id
		WHERE t.repo = ? AND t.issue = ? AND r.code_lines IS NULL ORDER BY r.pr`, repo, issue)
	if err != nil {
		return nil, fmt.Errorf("measure-recovery: PRs without code lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var prs []int
	for rows.Next() {
		var pr int
		if err := rows.Scan(&pr); err != nil {
			return nil, fmt.Errorf("measure-recovery: scan PR: %w", err)
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

// skip is a PR that was not measured, with the reason.
type skip struct {
	pr     int
	reason string
}

// recoveryJobs lists the PRs to measure: those merged in the last days without a closed row, and
// every open row outside that span. It calls the forge only for PRs that are not closed.
func recoveryJobs(ctx context.Context, f recoveryForge, known map[int]telemetry.RecoveryState, now time.Time, days int) ([]recoveryJob, []skip, error) {
	merged, err := f.MergedPRs(ctx, now.AddDate(0, 0, -days), now.Add(time.Second))
	if err != nil {
		return nil, nil, fmt.Errorf("measure-recovery: list merged PRs: %w", err)
	}
	var jobs []recoveryJob
	var skipped []skip
	seen := map[int]bool{}
	var heads map[int]string
	for _, pr := range merged {
		if seen[pr.Number] {
			continue
		}
		seen[pr.Number] = true
		s, hasRow := known[pr.Number]
		if hasRow && s.Status != telemetry.RecoveryOpen {
			continue
		}
		if hasRow {
			// An open row carries its issue; the head of a merged PR may be gone or renamed.
			jobs = append(jobs, recoveryJob{pr: pr.Number, issue: s.Issue, mergedAt: pr.MergedAt})
			continue
		}
		if heads == nil {
			hs, err := f.PullHeads(ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("measure-recovery: list pull heads: %w", err)
			}
			heads = make(map[int]string, len(hs))
			for _, h := range hs {
				heads[h.Number] = h.HeadRef
			}
		}
		m := issueHeadRe.FindStringSubmatch(heads[pr.Number])
		if m == nil {
			skipped = append(skipped, skip{pr.Number, "no_issue_branch"})
			continue
		}
		issue, err := strconv.Atoi(m[1])
		if err != nil || issue < 1 {
			skipped = append(skipped, skip{pr.Number, "no_issue_branch"})
			continue
		}
		jobs = append(jobs, recoveryJob{pr: pr.Number, issue: issue, mergedAt: pr.MergedAt})
	}
	for _, s := range known {
		if s.Status == telemetry.RecoveryOpen && !seen[s.PR] {
			jobs = append(jobs, recoveryJob{pr: s.PR, issue: s.Issue, mergedAt: s.MergedAt})
		}
	}
	slices.SortFunc(jobs, func(a, b recoveryJob) int { return a.pr - b.pr })
	return jobs, skipped, nil
}

func printRecoveries(out io.Writer, recs []telemetry.RecoveryRecord, skipped []skip) error {
	slices.SortFunc(recs, func(a, b telemetry.RecoveryRecord) int { return a.PR - b.PR })
	slices.SortFunc(skipped, func(a, b skip) int { return a.pr - b.pr })
	var b strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&b, "#%d %s %d items, %d fix-pr rounds\n", r.PR, r.Status, len(r.Items), r.FixPRRounds)
	}
	for _, s := range skipped {
		fmt.Fprintf(&b, "#%d skipped: %s\n", s.pr, s.reason)
	}
	_, err := io.WriteString(out, b.String())
	return err
}

// gitFetch brings origin/<branch> up to date, so a stale clone never freezes a wrong "none".
func gitFetch(ctx context.Context, dir, branch string) error {
	spec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--quiet", "origin", spec)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("measure-recovery: git fetch origin %s: %w: %s", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}
