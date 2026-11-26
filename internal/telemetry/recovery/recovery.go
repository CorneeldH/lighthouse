// Package recovery measures the recovery after merge of the PRs of a repo and records the traits
// of their trajectories. It is the core of `lighthouse measure-recovery` and of the scheduled job
// measure-recovery; both open the database and the forge and call Measure.
package recovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// Forge is the part of a forge that Measure reads.
type Forge interface {
	MergedPRs(ctx context.Context, since, until time.Time) ([]forge.PRSummary, error)
	PullHeads(ctx context.Context) ([]forge.PullHead, error)
	SearchIssues(ctx context.Context, text string) ([]forge.Issue, error)
	Issue(ctx context.Context, number int) (forge.Issue, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
}

var issueHeadRe = regexp.MustCompile(`^issue-(\d+)(-|$)`)

// job is one PR to measure.
type job struct {
	pr       int
	issue    int
	mergedAt time.Time
}

// skip is a PR that was not measured, with the reason.
type skip struct {
	pr     int
	reason string
}

// Measure measures, for every PR of repo that merged in the last days and for every open
// measurement outside that span, which fixes, reverts and bug issues followed it, and records the
// result in db. The clone in repo.Dir is fetched first and measured as origin/<default branch>.
// A closed measurement (recovered or none) is not touched again. After that, per trajectory that
// still lacks a route, an area or the size of a PR, or has a PR in that span, the route, area
// and code lines are recorded. Out gets one line per measured or skipped PR and one
// `traits #<issue>: <reason>` line per trait that could not be read; that is no error.
//
// All measuring happens before the first write, so a git or forge error writes nothing; each PR
// is then written in its own transaction, so a database error can leave the PRs before it
// written. GitLab is refused: it cannot list merged PRs yet.
func Measure(ctx context.Context, db *sql.DB, f Forge, repo repoconfig.Repo, days int, now time.Time, out io.Writer) error {
	if days < 1 {
		return fmt.Errorf("measure-recovery: days %d is below 1", days)
	}
	if repo.Kind == "gitlab" {
		return errors.New("measure-recovery: GitLab is not supported: MergedPRs is not implemented for it")
	}
	dir := repo.Dir
	if err := gitFetch(ctx, dir, repo.DefaultBranch); err != nil {
		return err
	}
	states, err := telemetry.RecoveryStates(ctx, db, repo.Slug)
	if err != nil {
		return err
	}
	known := make(map[int]telemetry.RecoveryState, len(states))
	for _, s := range states {
		known[s.PR] = s
	}

	jobs, skipped, err := jobsToMeasure(ctx, f, known, now, days)
	if err != nil {
		return err
	}
	var recs []telemetry.RecoveryRecord
	if len(jobs) > 0 {
		since := slices.MinFunc(jobs, func(a, b job) int { return a.mergedAt.Compare(b.mergedAt) }).mergedAt
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
			rounds, err := telemetry.CountFixPRRounds(ctx, db, repo.Slug, j.pr, j.mergedAt)
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
		if _, _, err := telemetry.RecordRecovery(ctx, db, r); err != nil {
			return err
		}
	}
	if err := printRecoveries(out, recs, skipped); err != nil {
		return err
	}
	src := telemetry.GitSource{Dir: dir, Branch: "origin/" + repo.DefaultBranch}
	return traitPass(ctx, f, src, db, repo, now, days, out)
}

// traitPass records route, area and size of every trajectory that still lacks one or has a PR
// in the window. A forge error, a missing marker or a missing label leaves the old value and
// writes one `traits #<issue>: <reason>` line; it is no error.
func traitPass(ctx context.Context, f Forge, src telemetry.GitSource, db *sql.DB, repo repoconfig.Repo, now time.Time, days int, out io.Writer) error {
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

// jobsToMeasure lists the PRs to measure: those merged in the last days without a closed row, and
// every open row outside that span. It calls the forge only for PRs that are not closed.
func jobsToMeasure(ctx context.Context, f Forge, known map[int]telemetry.RecoveryState, now time.Time, days int) ([]job, []skip, error) {
	merged, err := f.MergedPRs(ctx, now.AddDate(0, 0, -days), now.Add(time.Second))
	if err != nil {
		return nil, nil, fmt.Errorf("measure-recovery: list merged PRs: %w", err)
	}
	var jobs []job
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
			jobs = append(jobs, job{pr: pr.Number, issue: s.Issue, mergedAt: pr.MergedAt})
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
		jobs = append(jobs, job{pr: pr.Number, issue: issue, mergedAt: pr.MergedAt})
	}
	for _, s := range known {
		if s.Status == telemetry.RecoveryOpen && !seen[s.PR] {
			jobs = append(jobs, job{pr: s.PR, issue: s.Issue, mergedAt: s.MergedAt})
		}
	}
	slices.SortFunc(jobs, func(a, b job) int { return a.pr - b.pr })
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
