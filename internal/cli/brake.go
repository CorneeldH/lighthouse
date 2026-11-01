package cli

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/meminfo"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// hostFacts is what the brake and the usage resume read from the host, once per tick.
type hostFacts struct {
	Brake      dispatch.Brake
	PauseUntil time.Time                // zero: no active pause
	Markers    map[string][]park.Marker // per repo slug: controller markers whose last run is there
	MarkerDir  string
}

// meter is env.Meter, or a meter on the default cache directory and the [usage] table of host.toml.
func (e tickEnv) meter() (usage.Meter, error) {
	if e.Meter != nil {
		return *e.Meter, nil
	}
	paths, err := usage.DefaultPaths()
	if err != nil {
		return usage.Meter{}, err
	}
	cfg, err := usage.LoadConfig(e.HostToml)
	if err != nil {
		return usage.Meter{}, err
	}
	return usage.NewMeter(paths, cfg), nil
}

// memInfo is MemInfo, or /proc/meminfo.
func (e tickEnv) memInfo() string {
	if e.MemInfo != "" {
		return e.MemInfo
	}
	return "/proc/meminfo"
}

// readHostFacts reads the pause flag, the usage, the free memory, the runs and the resume markers.
// A run counts once: open, or parked (a controller marker whose last run is resumable). A marker
// whose session has no run counts for nothing. An unreadable meter, marker directory or database
// is an error, because the brake cannot check; a missing meminfo is not, it only closes the
// memory brake.
func readHostFacts(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch) (hostFacts, error) {
	var f hostFacts
	pauseFile, err := park.PauseFile(env.Getenv)
	if err != nil {
		return hostFacts{}, err
	}
	if f.MarkerDir, err = park.MarkerDir(env.Getenv); err != nil {
		return hostFacts{}, err
	}
	meter, err := env.meter()
	if err != nil {
		return hostFacts{}, fmt.Errorf("usage meter: %w", err)
	}
	var paused bool
	f.PauseUntil, paused = park.ReadPause(pauseFile, env.now())
	b := dispatch.Brake{
		Paused: paused, HaltPctSession: meter.Config.HaltPctSession, HaltPctWeek: meter.Config.HaltPctWeek,
		MinMemAvailableGB: cfg.MinMemAvailableGB, ReservePerRunGB: cfg.ReservePerRunGB,
		Slots: cfg.Slots, MaxSoftware: cfg.MaxSoftware,
	}
	if state, ok := meter.Measure(); ok {
		b.UsageMeasured, b.SessionPct, b.WeekPct = true, state.Session.Pct, state.Week.Pct
	}
	if gib, err := meminfo.AvailableGiB(env.memInfo()); err == nil {
		b.MemAvailableGiB = &gib
	}

	open, err := runs.OpenAll(ctx, db)
	if err != nil {
		return hostFacts{}, err
	}
	for _, r := range open {
		b.InUse++
		if r.Profile == dispatch.ProfileSoftware {
			b.SoftwareInUse++
		}
	}
	markers, err := park.ReadMarkers(f.MarkerDir)
	if err != nil {
		return hostFacts{}, fmt.Errorf("resume markers: %w", err)
	}
	f.Markers = map[string][]park.Marker{}
	for _, m := range markers {
		if !m.Controller {
			continue
		}
		repo, last, ok, err := runs.LastOfSession(ctx, db, m.SessionID)
		if err != nil {
			return hostFacts{}, err
		}
		if !ok {
			continue
		}
		f.Markers[repo] = append(f.Markers[repo], m)
		if runs.Resumable(last, m.Key()) {
			b.InUse++
			if last.Profile == dispatch.ProfileSoftware {
				b.SoftwareInUse++
			}
		}
	}
	f.Brake = b
	return f, nil
}
