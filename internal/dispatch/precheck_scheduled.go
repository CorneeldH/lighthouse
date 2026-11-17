package dispatch

import (
	"time"

	"github.com/CorneeldH/lighthouse/internal/cron"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

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
