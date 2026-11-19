// Package scheduled plans and describes scheduled runs: jobs the controller starts on a cron
// schedule instead of on an issue. This package holds the pure planner and the Job interface;
// the tick wiring lives elsewhere. It must not be imported by runs or repoconfig.
package scheduled

import (
	"context"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Verdict is the outcome of a job's Go gate. Skip is non-empty when the slot is not worth a run;
// it is the reason. Mode and Args select the job's slash-command variant, Score is the gate's
// measure (for logs and the run row).
type Verdict struct {
	Skip       string
	Mode, Args string
	Score      int
}

// LaunchPlan says how the agent of a scheduled run starts.
type LaunchPlan struct {
	Command      string        // slash command, with its arguments
	Instructions string        // unattended rules, written to the instructions file
	Tools        []string      // the available toolset; empty keeps the carrier default
	MaxRuntime   time.Duration // limit of the agent phase, up to and including the check
}

// Env is what a job sees. Dir is the workplace, set from Stage on.
type Env struct {
	Repo     repoconfig.Repo
	Schedule repoconfig.Schedule
	Dir      string
	Now      time.Time
	Run      workplace.Runner
	// Home is the home directory of the host user, where Claude Code keeps its session files.
	// History is every run of the repo, open and closed; a job that tells kinds of session apart
	// reads it. Both are set wherever a job's Gate or Stage runs.
	Home    string
	History []runs.Row
}

// Job is one kind of scheduled run. The controller calls Gate, then Launch; in the workplace
// Stage runs after Prepare and before Spawn; after the session Cleanup, the containment on
// Allowed, Check, Finish and the secret scan follow, in that order.
type Job interface {
	// Name is the job name a [[schedule]] block names in `job`.
	Name() string
	// Gate decides, without an agent, whether this slot gets a run.
	Gate(ctx context.Context, env Env) (Verdict, error)
	// Launch describes the agent start for a Verdict that is not a skip.
	Launch(v Verdict, env Env) (LaunchPlan, error)
	// Stage prepares the workplace (for instance writes the corpus). env.Dir is the workplace.
	Stage(ctx context.Context, env Env, v Verdict) error
	// Cleanup removes staging files before the containment check.
	Cleanup(ctx context.Context, env Env, dir string) error
	// Allowed reports whether the run may write path (relative to the workplace).
	Allowed(path string) bool
	// Check verifies completeness of the result; an error makes the run failed.
	Check(ctx context.Context, env Env, dir string) error
	// Finish completes the result: mirroring, index.md.
	Finish(ctx context.Context, env Env, dir string, v Verdict) error
}

// Configurable is implemented by a job whose behaviour depends on the params of its schedule.
// The registry holds one instance per job name; Configure returns the instance for one schedule,
// or an error when the params are invalid (a configuration error, reported at each tick).
type Configurable interface {
	Configure(s repoconfig.Schedule) (Job, error)
}

// Bind returns the job to use for schedule s: j configured for s when j is Configurable, else j.
// Every caller that gets a job from Lookup for a schedule goes through Bind.
func Bind(j Job, s repoconfig.Schedule) (Job, error) {
	if c, ok := j.(Configurable); ok {
		return c.Configure(s)
	}
	return j, nil
}

// TriggerKey is the trigger key of the run row of schedule name for slot (YYYYMMDDTHHMM):
// scheduled:<name>:<slot>. It is the only key runs.SlotSettled and runs.Take look at.
func TriggerKey(name, slot string) string {
	return runs.TriggerScheduled + ":" + name + ":" + slot
}

var (
	mu       sync.RWMutex
	registry = map[string]Job{}
)

// Register makes j available to Lookup under j.Name(); a later Register of the same name
// replaces the earlier one. The names must stay in sync with the job allow-list
// (scheduleJobs) in internal/repoconfig/schedule.go: a name there without a Register here
// fails at the first tick, a name here without one there never runs.
func Register(j Job) {
	mu.Lock()
	defer mu.Unlock()
	registry[j.Name()] = j
}

// Lookup returns the registered job called name.
func Lookup(name string) (Job, bool) {
	mu.RLock()
	defer mu.RUnlock()
	j, ok := registry[name]
	return j, ok
}
