// Package measurerecovery is the scheduled job measure-recovery: per served repo it runs the
// recovery measurement of `lighthouse measure-recovery` (recovery.Measure) on the repo's clone,
// in the controller's process and without an agent (scheduled.Direct).
package measurerecovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery"
)

// Name is the job name a [[schedule]] block names in `job`.
const Name = "measure-recovery"

const (
	defaultDays = 30
	maxDays     = 3650
)

func init() { scheduled.Register(New()) }

// Job is the measure-recovery job. The registered instance is unbound (days 0): Configure
// returns the instance for one schedule, and the unbound one refuses to run.
type Job struct {
	days int
}

var (
	_ scheduled.Direct       = (*Job)(nil)
	_ scheduled.Configurable = (*Job)(nil)
)

// New returns the unconfigured job, as the registry holds it.
func New() scheduled.Job { return &Job{} }

var (
	errUnbound = errors.New("measure-recovery: job is not configured for a schedule (scheduled.Bind)")
	errInAgent = errors.New("measure-recovery: runs in process (scheduled.Direct); it has no workplace")
	errNoForge = errors.New("measure-recovery: no forge for the repo in this tick")
)

// Configure implements scheduled.Configurable: params.days (a whole number from 1 to 3650,
// default 30) is the span of merged PRs to measure; any other key is an error.
func (*Job) Configure(s repoconfig.Schedule) (scheduled.Job, error) {
	days, err := parseDays(s.Params)
	if err != nil {
		return nil, err
	}
	return &Job{days: days}, nil
}

// parseDays validates params over the default. It reports every problem; an unknown key is a
// problem (a typo would else run with the default silently).
func parseDays(params map[string]any) (int, error) {
	var errs []error
	for _, k := range slices.Sorted(maps.Keys(params)) {
		if k != "days" {
			errs = append(errs, fmt.Errorf("params: unknown key %s", scheduled.ShowPath(k)))
		}
	}
	days := defaultDays
	if v, ok := params["days"]; ok {
		var n int64
		switch x := v.(type) {
		case int64:
			n = x
		case int:
			n = int64(x)
		default:
			n = -1
			errs = append(errs, errors.New("params days: must be a whole number"))
		}
		if n != -1 && (n < 1 || n > maxDays) {
			errs = append(errs, fmt.Errorf("params days: must be between 1 and %d", maxDays))
		}
		days = int(n)
	}
	if len(errs) > 0 {
		return 0, errors.Join(errs...)
	}
	return days, nil
}

// Name implements scheduled.Job.
func (*Job) Name() string { return Name }

// Gate implements scheduled.Job: every slot gets a run; the measurement itself skips what is
// settled.
func (*Job) Gate(context.Context, scheduled.Env) (scheduled.Verdict, error) {
	return scheduled.Verdict{}, nil
}

// Launch implements scheduled.Job: there is no agent, so the plan is empty.
func (*Job) Launch(scheduled.Verdict, scheduled.Env) (scheduled.LaunchPlan, error) {
	return scheduled.LaunchPlan{}, nil
}

// Stage implements scheduled.Job; a Direct job has no workplace, so it is never called.
func (*Job) Stage(context.Context, scheduled.Env, scheduled.Verdict) error { return errInAgent }

// Cleanup implements scheduled.Job; never called for a Direct job.
func (*Job) Cleanup(context.Context, scheduled.Env, string) error { return errInAgent }

// Allowed implements scheduled.Job: the job writes nothing in a workplace.
func (*Job) Allowed(string) bool { return false }

// Check implements scheduled.Job; never called for a Direct job.
func (*Job) Check(context.Context, scheduled.Env, string) error { return errInAgent }

// Finish implements scheduled.Job; never called for a Direct job.
func (*Job) Finish(context.Context, scheduled.Env, string, scheduled.Verdict) error {
	return errInAgent
}

// Run implements scheduled.Direct: it measures env.Repo in its clone (env.Repo.Dir) over the
// last days with the tick's forge, into db. The per-PR lines of the measurement have no reader
// in a tick and are dropped; an error fails the run.
func (j *Job) Run(ctx context.Context, db *sql.DB, env scheduled.Env) error {
	if j.days == 0 {
		return errUnbound
	}
	if env.Forge == nil {
		return errNoForge
	}
	return recovery.Measure(ctx, db, env.Forge, env.Repo, j.days, env.Now, io.Discard)
}
