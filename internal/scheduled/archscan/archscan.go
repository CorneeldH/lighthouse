// Package archscan is the scheduled job architecture-scan: a periodic, unattended scan of the
// architecture of a repository that leaves one decision issue. This file holds the registration,
// the gate, the launch and the staging of the corpus; the checks and publishing follow in a later task,
// and their stubs report not implemented.
package archscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/safefs"
)

// Name is the job name a [[schedule]] block names in `job`.
const Name = "architecture-scan"

func init() { scheduled.Register(New()) }

// Job is the architecture-scan job. The registered instance has no params: Configure returns
// the instance for one schedule.
type Job struct {
	cfg   config
	bound bool
}

var (
	_ scheduled.Job          = (*Job)(nil)
	_ scheduled.Configurable = (*Job)(nil)
)

var errNotImplemented = errors.New("archscan: not implemented")

// New returns the unconfigured job, as the registry holds it.
func New() scheduled.Job { return &Job{cfg: defaults()} }

// Configure implements scheduled.Configurable: it validates the params of s.
func (j *Job) Configure(s repoconfig.Schedule) (scheduled.Job, error) {
	c, err := parse(s.Params)
	if err != nil {
		return nil, err
	}
	return &Job{cfg: c, bound: true}, nil
}

// Name implements scheduled.Job.
func (*Job) Name() string { return Name }

var errUnbound = errors.New("archscan: job is not configured for a schedule (scheduled.Bind)")

// skipNoCommits is the skip of a slot whose origin/<default> is the base of the open scan issue.
const skipNoCommits = "no commits since last scan"

func defaultBranch(env scheduled.Env) string {
	if env.Repo.DefaultBranch == "" {
		return "main"
	}
	return env.Repo.DefaultBranch
}

// Gate implements scheduled.Job: after a fetch, HEAD of origin/<default> against the base in the
// marker of the open scan issue decides. Every failure is an error, never a skip: the tick then
// keeps the slot and the next tick tries again.
func (j *Job) Gate(ctx context.Context, env scheduled.Env) (scheduled.Verdict, error) {
	if !j.bound {
		return scheduled.Verdict{}, errUnbound
	}
	if env.Forge == nil {
		return scheduled.Verdict{}, fmt.Errorf("%s: the repo has no forge", Name)
	}
	label := env.Repo.Labels.NeedsDecision
	if label == "" {
		return scheduled.Verdict{}, fmt.Errorf("%s: labels.needs_decision is not set", Name)
	}
	branch := defaultBranch(env)
	if _, err := git(ctx, env, env.Repo.Dir, fetchLimit, "fetch", "--quiet", "origin", branch); err != nil {
		return scheduled.Verdict{}, err
	}
	head, err := revParse(ctx, env, env.Repo.Dir, "origin/"+branch)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	open, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "open")
	if err != nil {
		return scheduled.Verdict{}, fmt.Errorf("%s: %w", Name, err)
	}
	if len(open) > 0 {
		if m, ok := scheduled.ParseMarker(open[0].Body); ok && m.Base == head {
			return scheduled.Verdict{Skip: skipNoCommits}, nil
		}
	}
	return scheduled.Verdict{RunID: "scan-" + env.Now.UTC().Format("20060102T1504")}, nil
}

// Launch implements scheduled.Job.
func (j *Job) Launch(scheduled.Verdict, scheduled.Env) (scheduled.LaunchPlan, error) {
	if !j.bound {
		return scheduled.LaunchPlan{}, errUnbound
	}
	return scheduled.LaunchPlan{
		Command:      j.cfg.command + " --corpus " + corpusPath + " --out " + resultPath,
		Instructions: instructions,
		Tools:        slices.Clone(j.cfg.tools),
		MaxRuntime:   j.cfg.maxRuntime,
	}, nil
}

const instructions = "You are running unattended from a schedule. There is no human to answer questions: ask none.\n" +
	"Read the issues, the hotspots and the earlier scans only from the corpus file you get with --corpus (`" + corpusPath + "`), " +
	"not from the forge.\n" +
	"Write exactly one file, at the path you get with --out (`" + resultPath + "`). Create no issues. " +
	"Do NOT create a git commit and do NOT push: the controller checks the result and publishes it itself.\n" +
	"A file anywhere else, or a symlink, fails the run.\n"

// Stage implements scheduled.Job: it writes the corpus of the workplace into the staging
// directory.
func (j *Job) Stage(ctx context.Context, env scheduled.Env, _ scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	c, err := j.buildCorpus(ctx, env)
	if err != nil {
		return err
	}
	b, err := fit(c, maxCorpus)
	if err != nil {
		return err
	}
	// Look before creating: MkdirAll would follow a symlink among the existing parents.
	for _, rel := range []string{".lighthouse", stagingDir} {
		_, err := safefs.Stat(env.Dir, rel)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(filepath.Join(env.Dir, filepath.FromSlash(rel)), 0o755); err != nil {
				return safefs.FileErr(rel, err)
			}
		} else if err != nil {
			return err
		}
	}
	return safefs.WriteFresh(filepath.Join(env.Dir, filepath.FromSlash(corpusPath)), corpusPath, b)
}

// Cleanup implements scheduled.Job (stub).
func (*Job) Cleanup(context.Context, scheduled.Env, string) error { return errNotImplemented }

// Allowed implements scheduled.Job (stub): nothing may be written yet.
func (*Job) Allowed(string) bool { return false }

// Check implements scheduled.Job (stub).
func (*Job) Check(context.Context, scheduled.Env, string) error { return errNotImplemented }

// Finish implements scheduled.Job (stub).
func (*Job) Finish(context.Context, scheduled.Env, string, scheduled.Verdict) error {
	return errNotImplemented
}
