// Package archscan is the scheduled job architecture-scan: a periodic, unattended scan of the
// architecture of a repository that leaves one decision issue. This file holds the registration;
// the gate, staging and checks follow in later tasks, and their stubs here report not implemented.
package archscan

import (
	"context"
	"errors"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
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

// Gate implements scheduled.Job (stub).
func (*Job) Gate(context.Context, scheduled.Env) (scheduled.Verdict, error) {
	return scheduled.Verdict{}, errNotImplemented
}

// Launch implements scheduled.Job (stub).
func (*Job) Launch(scheduled.Verdict, scheduled.Env) (scheduled.LaunchPlan, error) {
	return scheduled.LaunchPlan{}, errNotImplemented
}

// Stage implements scheduled.Job (stub).
func (*Job) Stage(context.Context, scheduled.Env, scheduled.Verdict) error { return errNotImplemented }

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
