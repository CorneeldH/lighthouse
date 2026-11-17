// Package scheduledtest is test support for packages that exercise scheduled runs. It is not
// production behaviour: nothing outside _test.go files imports it.
package scheduledtest

import (
	"context"
	"strings"
	"sync"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Job is a configurable scheduled.Job. Set the fields before use; every call is recorded in Calls
// as "<method>" (Gate, Launch, Stage, Cleanup, Check, Finish). A nil hook does nothing.
type Job struct {
	JobName string // Name(); "test" when empty

	Verdict    scheduled.Verdict // returned by Gate
	GateErr    error             // returned by Gate
	Plan       scheduled.LaunchPlan
	LaunchErr  error
	CheckErr   error    // returned by Check
	AllowedPre []string // Allowed is true for a path with one of these prefixes

	StageFn  func(ctx context.Context, env scheduled.Env, v scheduled.Verdict) error
	FinishFn func(ctx context.Context, env scheduled.Env, dir string, v scheduled.Verdict) error

	mu    sync.Mutex
	calls []string
}

var _ scheduled.Job = (*Job)(nil)

func (j *Job) record(m string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.calls = append(j.calls, m)
}

// Calls returns the recorded method names in call order.
func (j *Job) Calls() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.calls...)
}

func (j *Job) Name() string {
	if j.JobName == "" {
		return "test"
	}
	return j.JobName
}

func (j *Job) Gate(context.Context, scheduled.Env) (scheduled.Verdict, error) {
	j.record("Gate")
	return j.Verdict, j.GateErr
}

func (j *Job) Launch(scheduled.Verdict, scheduled.Env) (scheduled.LaunchPlan, error) {
	j.record("Launch")
	return j.Plan, j.LaunchErr
}

func (j *Job) Stage(ctx context.Context, env scheduled.Env, v scheduled.Verdict) error {
	j.record("Stage")
	if j.StageFn != nil {
		return j.StageFn(ctx, env, v)
	}
	return nil
}

func (j *Job) Cleanup(context.Context, scheduled.Env, string) error {
	j.record("Cleanup")
	return nil
}

func (j *Job) Allowed(path string) bool {
	for _, p := range j.AllowedPre {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func (j *Job) Check(context.Context, scheduled.Env, string) error {
	j.record("Check")
	return j.CheckErr
}

func (j *Job) Finish(ctx context.Context, env scheduled.Env, dir string, v scheduled.Verdict) error {
	j.record("Finish")
	if j.FinishFn != nil {
		return j.FinishFn(ctx, env, dir, v)
	}
	return nil
}
