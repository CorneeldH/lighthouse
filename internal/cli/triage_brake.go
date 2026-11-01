package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// usageBrake is the brake of #116 for the round: the pause flag, then the usage window. It
// closes on the same texts as the brake of tick, which it shares the usage check with.
type usageBrake struct {
	Meter     usage.Meter
	PauseFile string
	Now       func() time.Time
}

// Check reports why the round may not go on, or open. Measure refreshes a stale cache, so a
// check can take as long as one usage call.
func (b usageBrake) Check() (reason string, open bool) {
	if until, paused := park.ReadPause(b.PauseFile, b.Now()); paused {
		return "paused until " + until.Format(time.RFC3339), false
	}
	br := dispatch.Brake{HaltPctSession: b.Meter.Config.HaltPctSession, HaltPctWeek: b.Meter.Config.HaltPctWeek}
	if state, ok := b.Meter.Measure(); ok {
		br.UsageMeasured, br.SessionPct, br.WeekPct = true, state.Session.Pct, state.Week.Pct
	}
	if reason := br.UsageClosed(); reason != "" {
		return reason, false
	}
	return "", true
}

// defaultTriageEnv is the environment of the real command: the process environment, host.toml
// at its usual place, and the brake on the default usage cache and pause flag.
func defaultTriageEnv() (triageEnv, error) {
	env := tickEnv{Getenv: os.Getenv, HostToml: hostconfig.Path()}
	paths, err := usage.DefaultPaths()
	if err != nil {
		return triageEnv{}, fmt.Errorf("usage meter: %w", err)
	}
	cfg, err := usage.LoadConfig(env.HostToml)
	if err != nil {
		return triageEnv{}, fmt.Errorf("usage meter: %w", err)
	}
	pauseFile, err := park.PauseFile(env.Getenv)
	if err != nil {
		return triageEnv{}, err
	}
	return triageEnv{tickEnv: env, Brake: usageBrake{Meter: usage.NewMeter(paths, cfg), PauseFile: pauseFile, Now: time.Now}}, nil
}
