package scheduled

import (
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// Classification: pure logic. These tests fail if Plan stops choosing the latest slot through the
// weeks filter, plans a slot that SlotSettled or skipped call handled, ignores an open run of the
// same schedule (or of another schedule), or treats a single retry as settled.

func settledRow(schedule, slot string, o runs.Outcome) runs.Row {
	return runs.Row{Schedule: schedule, TriggerKey: TriggerKey(schedule, slot), Outcome: o}
}

func TestPlan(t *testing.T) {
	loc := time.UTC
	// Monday 2026-11-16 03:30; the daily 03:00 slot of that day is the latest.
	now := time.Date(2026, 11, 16, 3, 30, 0, 0, loc)
	daily := repoconfig.Schedule{Name: "reflect", Job: "reflect", Cron: "0 3 * * *"}
	other := repoconfig.Schedule{Name: "map", Job: "map-refresh", Cron: "0 3 * * *"}
	// Anchor is the week of 2026-11-09; weeks=2 allows 11-09 and 11-23, not 11-16.
	biweekly := repoconfig.Schedule{Name: "reflect", Job: "reflect", Cron: "0 3 * * *", Weeks: 2,
		Anchor: time.Date(2026, 11, 9, 0, 0, 0, 0, loc)}
	none := func(string, string) bool { return false }

	tests := []struct {
		name      string
		now       time.Time
		schedules []repoconfig.Schedule
		history   []runs.Row
		skipped   func(string, string) bool
		want      []string // "schedule@slot"
	}{
		{"due slot", now, []repoconfig.Schedule{daily}, nil, none, []string{"reflect@20261116T0300"}},
		{"settled done", now, []repoconfig.Schedule{daily},
			[]runs.Row{settledRow("reflect", "20261116T0300", runs.Done)}, none, nil},
		{"settled failed", now, []repoconfig.Schedule{daily},
			[]runs.Row{settledRow("reflect", "20261116T0300", runs.Failed)}, none, nil},
		{"skipped", now, []repoconfig.Schedule{daily}, nil,
			func(s, slot string) bool { return s == "reflect" && slot == "20261116T0300" }, nil},
		{"skipped other slot", now, []repoconfig.Schedule{daily}, nil,
			func(s, slot string) bool { return slot == "20261115T0300" }, []string{"reflect@20261116T0300"}},
		{"earlier slot settled, later one due", now.Add(72 * time.Hour), []repoconfig.Schedule{daily},
			[]runs.Row{settledRow("reflect", "20261116T0300", runs.Done)}, none, []string{"reflect@20261119T0300"}},
		{"never settled gives the latest", now.Add(48 * time.Hour), []repoconfig.Schedule{daily}, nil, none,
			[]string{"reflect@20261118T0300"}},
		{"weeks filter skips the odd week", now, []repoconfig.Schedule{biweekly}, nil, none, []string{"reflect@20261115T0300"}},
		{"open run", now, []repoconfig.Schedule{daily},
			[]runs.Row{{Schedule: "reflect", TriggerKey: TriggerKey("reflect", "20261115T0300"), Open: true}}, none, nil},
		{"open run of another schedule", now, []repoconfig.Schedule{daily, other},
			[]runs.Row{{Schedule: "reflect", TriggerKey: TriggerKey("reflect", "20261115T0300"), Open: true}}, none,
			[]string{"map@20261116T0300"}},
		{"one retry still due", now, []repoconfig.Schedule{daily},
			[]runs.Row{settledRow("reflect", "20261116T0300", runs.Retry)}, none, []string{"reflect@20261116T0300"}},
		{"two retries settled", now, []repoconfig.Schedule{daily},
			[]runs.Row{settledRow("reflect", "20261116T0300", runs.Retry), settledRow("reflect", "20261116T0300", runs.Retry)},
			none, nil},
		{"settled slot of another schedule does not count", now, []repoconfig.Schedule{daily},
			[]runs.Row{settledRow("map", "20261116T0300", runs.Done)}, none, []string{"reflect@20261116T0300"}},
		{"cron never fires", now, []repoconfig.Schedule{{Name: "x", Job: "reflect", Cron: "0 0 30 2 *"}}, nil, none, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Plan(tt.now, tt.schedules, tt.history, tt.skipped)
			var gs []string
			for _, d := range got {
				gs = append(gs, d.Schedule.Name+"@"+slotOf(d))
			}
			if len(gs) != len(tt.want) {
				t.Fatalf("Plan = %v, want %v", gs, tt.want)
			}
			for i := range gs {
				if gs[i] != tt.want[i] {
					t.Errorf("Plan[%d] = %s, want %s", i, gs[i], tt.want[i])
				}
			}
		})
	}
}

func TestPlanWeeksFilterReachesBack(t *testing.T) {
	// Tuesday 2026-11-17: the odd week (11-16) is filtered, so the latest allowed daily slot is
	// Sunday 2026-11-15 03:00 (the week of 11-09), not today's.
	loc := time.UTC
	s := repoconfig.Schedule{Name: "r", Job: "reflect", Cron: "0 3 * * *", Weeks: 2,
		Anchor: time.Date(2026, 11, 9, 0, 0, 0, 0, loc)}
	got := Plan(time.Date(2026, 11, 17, 5, 0, 0, 0, loc), []repoconfig.Schedule{s}, nil,
		func(string, string) bool { return false })
	if len(got) != 1 || slotOf(got[0]) != "20261115T0300" {
		t.Fatalf("Plan = %v, want slot 20261115T0300", got)
	}
}

func TestTriggerKey(t *testing.T) {
	if got := TriggerKey("reflect", "20261116T0300"); got != "scheduled:reflect:20261116T0300" {
		t.Errorf("TriggerKey = %q", got)
	}
}

func TestRegisterLookup(t *testing.T) {
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup of an unregistered job succeeded")
	}
	j := &fakeJob{}
	Register(j)
	got, ok := Lookup("fake")
	if !ok || got != Job(j) {
		t.Fatalf("Lookup = %v, %v; want the registered job", got, ok)
	}
}
