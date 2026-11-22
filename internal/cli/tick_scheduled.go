package cli

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/cron"
	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	_ "github.com/CorneeldH/lighthouse/internal/scheduled/mapplanned" // registers job map-planned
	_ "github.com/CorneeldH/lighthouse/internal/scheduled/maprefresh" // registers job map-refresh
	_ "github.com/CorneeldH/lighthouse/internal/scheduled/reflect"    // registers job reflect
)

// job returns the scheduled job called name: from Jobs when a test set it, else the registry.
func (e tickEnv) job(name string) (scheduled.Job, bool) {
	if e.Jobs != nil {
		return e.Jobs(name)
	}
	return scheduled.Lookup(name)
}

// planSchedules plans the schedules of every gathered repo, after the gather and before Decide
// (spec §2): scheduled.Plan on the repo's own history, then per due schedule its job's gate and
// launch plan. A gate that says skip marks the slot skipped with one line on errOut; a gate or
// launch plan that fails or an unreadable or unwritable slot leaves the slot open with one line
// on errOut, holds the repo's tick state back so the next tick gathers and gates again, and
// reports failed. An unknown job only gets its line: it holds and fails nothing. The schedules
// that are left go to Decide in the repo's input.
func planSchedules(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, ticked map[string]forge.Forge,
	gathered []gatheredRepo, errOut io.Writer) (failed bool) {
	for i := range gathered {
		g := &gathered[i]
		if len(g.repo.Schedules) == 0 {
			continue
		}
		var hold bool
		g.in.Schedules, hold = planRepo(ctx, env, db, cfg, ticked[g.repo.Slug], g.repo, g.in.History, errOut)
		g.hold = g.hold || hold
		failed = failed || hold
	}
	return failed
}

// planRepo plans the schedules of one repo with history, its runs; see planSchedules. hold
// reports a line about a failure.
func planRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, f forge.Forge, repo repoconfig.Repo,
	history []runs.Row, errOut io.Writer) (due []dispatch.ScheduledDue, hold bool) {
	now := env.now()
	var skipErr error
	skipped := func(schedule, slot string) bool {
		ok, err := runs.SlotSkipped(ctx, db, repo.Slug, schedule, slot)
		if err != nil {
			skipErr = errors.Join(skipErr, err)
			return true // unknown: no run, the next tick reads it again
		}
		return ok
	}
	plan := scheduled.Plan(now, repo.Schedules, history, skipped)
	if skipErr != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: plan %s: %s\n", repo.Slug, oneLine(skipErr))
		hold = true
	}
	for _, d := range plan {
		slot := cron.Slot(d.Slot)
		name := repo.Slug + " " + scheduled.TriggerKey(d.Schedule.Name, slot)
		fail := func(format string, args ...any) {
			_, _ = fmt.Fprintf(errOut, "lighthouse: %s: %s\n", name, fmt.Sprintf(format, args...))
			hold = true
		}
		job, ok := env.job(d.Schedule.Job)
		if !ok {
			// No hold and no failure: a schedule whose job is not registered would else make
			// every tick exit 1 and skip the tick state, a full gather each tick. The slot stays
			// unsettled, so the line returns at each gather of the repo until the job exists or
			// the block goes.
			_, _ = fmt.Fprintf(errOut, "lighthouse: %s: no job %s\n", name, d.Schedule.Job)
			continue
		}
		job, err := scheduled.Bind(job, d.Schedule)
		if err != nil {
			fail("config: %s", oneLine(err))
			continue
		}
		jenv := scheduled.Env{Repo: repo, Schedule: d.Schedule, Now: now, Run: env.runner(), Home: env.Getenv("HOME"), History: history,
			Forge: f, Trusted: trustedOf(f, cfg)}
		v, err := job.Gate(ctx, jenv)
		if err != nil {
			// Review focus 3: a failed gate is no verdict; the slot stays open.
			fail("gate: %s", oneLine(err))
			continue
		}
		if v.Skip != "" {
			if err := runs.SkipSlot(ctx, db, repo.Slug, d.Schedule.Name, slot, v.Skip, now); err != nil {
				fail("record skip: %s", oneLine(err))
				continue
			}
			_, _ = fmt.Fprintf(errOut, "lighthouse: %s: skipped: %s\n", name, strings.ReplaceAll(strings.TrimSpace(v.Skip), "\n", "; "))
			continue
		}
		lp, err := job.Launch(v, jenv)
		if err != nil {
			fail("launch: %s", oneLine(err))
			continue
		}
		due = append(due, dispatch.ScheduledDue{
			Schedule: d.Schedule.Name, Profile: d.Schedule.Profile, Slot: slot, Verdict: v, Plan: lp,
		})
	}
	return due, hold
}

// trustedOf is the Trusted of a scheduled.Env: the answer_permissions check on f; nil without a forge.
func trustedOf(f forge.Forge, cfg hostconfig.Dispatch) func(context.Context, string) (bool, error) {
	if f == nil {
		return nil
	}
	return dispatch.NewTrust(f, cfg.AnswerPermissions).Trusted
}

// scheduledJob resolves schedule of repo for apply: its job and the scheduled.Env it runs in.
func scheduledJob(env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, forges map[string]forge.Forge,
	repos map[string]repoconfig.Repo) func(repo, schedule string) (scheduled.Job, scheduled.Env, error) {
	return func(slug, name string) (scheduled.Job, scheduled.Env, error) {
		repo, ok := repos[slug]
		if !ok {
			return nil, scheduled.Env{}, fmt.Errorf("no repo %s", slug)
		}
		for _, s := range repo.Schedules {
			if s.Name != name {
				continue
			}
			job, ok := env.job(s.Job)
			if !ok {
				return nil, scheduled.Env{}, fmt.Errorf("no job %s", s.Job)
			}
			job, err := scheduled.Bind(job, s)
			if err != nil {
				return nil, scheduled.Env{}, fmt.Errorf("params of %s: %w", s.Name, err)
			}
			// The history is what a job that tells kinds of session apart reads in Stage.
			history, err := runs.History(context.Background(), db, slug)
			if err != nil {
				return nil, scheduled.Env{}, fmt.Errorf("history of %s: %w", slug, err)
			}
			return job, scheduled.Env{Repo: repo, Schedule: s, Now: env.now(), Run: env.runner(),
				Home: env.Getenv("HOME"), History: history, Forge: forges[slug], Trusted: trustedOf(forges[slug], cfg)}, nil
		}
		return nil, scheduled.Env{}, fmt.Errorf("no schedule %s in %s", name, slug)
	}
}

// scheduledRemote returns the Remote the branch of a scheduled run of a repo goes to: the repo on
// its forge host over HTTPS (GitBase in tests), with the forge token as credential. Only the name
// of the variable that holds the token goes to the remote (forge.TokenVar), which hands it to git
// through an inline credential helper; the push runs from a temporary repository in stateDir.
// The token itself is in no argument, URL or file.
func scheduledRemote(env tickEnv, stateDir string, repos map[string]repoconfig.Repo) func(string) (scheduled.Remote, error) {
	return func(slug string) (scheduled.Remote, error) {
		repo, ok := repos[slug]
		if !ok {
			return nil, fmt.Errorf("no repo %s", slug)
		}
		name, err := forge.TokenVar(repo.Kind, repo.TokenEnv, env.Getenv)
		if err != nil {
			return nil, err
		}
		base := env.GitBase
		if base == "" {
			base = "https://" + cmp.Or(repo.Host, "github.com")
		}
		user := "x-access-token" // what GitHub takes with any token
		if repo.Kind == "gitlab" {
			user = "oauth2"
		}
		return scheduled.HTTPSRemote{URL: base + "/" + repo.Slug + ".git", User: user, TokenVar: name,
			TempDir: stateDir, Getenv: env.Getenv}, nil
	}
}

// repoDefers are the defers among actions of repo slug, of issues and scheduled runs alike:
// SaveDeferred keys a scheduled one on its schedule.
func repoDefers(actions []dispatch.Action, slug string) []dispatch.Action {
	var out []dispatch.Action
	for _, a := range actions {
		if a.Kind == dispatch.KindDefer && a.Repo == slug {
			out = append(out, a)
		}
	}
	return out
}
