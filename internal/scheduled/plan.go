package scheduled

import (
	"time"

	"github.com/CorneeldH/lighthouse/internal/cron"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// Due is a schedule whose latest slot is up for a run.
type Due struct {
	Schedule repoconfig.Schedule
	Slot     time.Time
}

// Plan returns, per schedule, at most one Due: the latest slot at or before now that passes the
// weeks filter. Missed earlier slots are not caught up one by one. A schedule yields nothing when
// that slot is settled (runs.SlotSettled on history, or skipped), when history holds an open run
// of the schedule, or when its cron expression never fires. now's location decides the slot
// strings. Plan reads no clock and no database.
func Plan(now time.Time, schedules []repoconfig.Schedule, history []runs.Row, skipped func(schedule, slot string) bool) []Due {
	var out []Due
	for _, s := range schedules {
		if hasOpen(history, s.Name) {
			continue
		}
		expr, err := cron.Parse(s.Cron)
		if err != nil {
			continue // repoconfig validates the expression; a bad one never runs
		}
		slot, ok := expr.Last(now, cron.Filter{Weeks: s.Weeks, Anchor: s.Anchor})
		if !ok {
			continue
		}
		key := cron.Slot(slot)
		if runs.SlotSettled(history, s.Name, key) || skipped(s.Name, key) {
			continue
		}
		out = append(out, Due{Schedule: s, Slot: slot})
	}
	return out
}

func hasOpen(history []runs.Row, schedule string) bool {
	for _, r := range history {
		if r.Open && r.Schedule == schedule {
			return true
		}
	}
	return false
}
