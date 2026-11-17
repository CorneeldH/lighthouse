package dispatch

import (
	"time"

	"github.com/CorneeldH/lighthouse/internal/cron"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// runLimit is the moment the time limit of open run r runs out: the host's max_runtime after
// the start for an issue run; for a scheduled run the max_runtime of its record (the host's when
// it has none) while pr_at is empty, and pr_at + ScheduledPRTimeout after. Decide applies the same
// limits, so a run past one is settled without a board change.
func runLimit(d hostconfig.Dispatch, r runs.OpenRun) time.Time {
	switch {
	case r.Schedule == "":
		return r.StartedAt.Add(d.MaxRuntime)
	case !r.PRAt.IsZero():
		return r.PRAt.Add(ScheduledPRTimeout)
	}
	return r.StartedAt.Add(scheduledMaxRuntime(r.Verdict, d.MaxRuntime))
}

// ScheduleBoundaries are the latest slot at or before now of each schedule, through its weeks
// filter: a boundary for the precheck (Due), so a repo whose board did not change is gathered,
// planned and gated again once a new slot is up instead of at its next stale gather. A schedule
// whose cron does not parse or never fires gives none.
func ScheduleBoundaries(schedules []repoconfig.Schedule, now time.Time) []time.Time {
	var out []time.Time
	for _, s := range schedules {
		expr, err := cron.Parse(s.Cron)
		if err != nil {
			continue // repoconfig validates the expression; a bad one never runs
		}
		if slot, ok := expr.Last(now, cron.Filter{Weeks: s.Weeks, Anchor: s.Anchor}); ok {
			out = append(out, slot)
		}
	}
	return out
}
