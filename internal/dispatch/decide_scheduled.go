package dispatch

import (
	"strconv"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// ScheduledDue is a schedule whose slot is up and whose gate let it through. The tick runs
// scheduled.Plan, Job.Gate and Job.Launch, all reading the repo or the clock; Decide turns each
// into a KindScheduledStart under the brake.
type ScheduledDue struct {
	Schedule string               `json:"schedule"`
	Profile  string               `json:"profile"`
	Slot     string               `json:"slot"` // YYYYMMDDTHHMM
	Verdict  scheduled.Verdict    `json:"verdict"`
	Plan     scheduled.LaunchPlan `json:"plan"`
}

// ScheduledBranch is the branch of attempt of schedule's run for slot: scheduled/<name>-<slot>-<attempt>.
func ScheduledBranch(schedule, slot string, attempt int) string {
	return "scheduled/" + schedule + "-" + slot + "-" + strconv.Itoa(attempt)
}

// maxScheduledAttempts is how often a slot is tried (spec §2): attempt 1 and 2.
const maxScheduledAttempts = 2

// decideScheduled gives the end of every open scheduled run of repo (settleScheduled) and plans
// a scheduled start for each due schedule: always on this host (node ""), in the schedule's
// profile, as the attempt after the runs its trigger key already has. A schedule with an open
// run, also one that ends in this tick, or a slot with all its attempts used, gets none; the
// planner leaves those out already, and Decide does not rely on it.
func decideScheduled(in Input, repo RepoInput) []planned {
	out := settleScheduled(in, repo)
	for _, d := range repo.Schedules {
		key := scheduled.TriggerKey(d.Schedule, d.Slot)
		attempts, open := 0, false
		for _, r := range repo.History {
			open = open || r.Open && r.Schedule == d.Schedule
			if r.TriggerKey == key {
				attempts++
			}
		}
		if open || attempts >= maxScheduledAttempts {
			continue
		}
		attempt := attempts + 1
		a := Action{
			Kind: KindScheduledStart, Repo: repo.Snapshot.Repo, Schedule: d.Schedule, Slot: d.Slot,
			Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: key, Attempt: attempt,
			Profile: d.Profile, Command: d.Plan.Command, Prompt: d.Plan.Command,
			Branch: ScheduledBranch(d.Schedule, d.Slot, attempt), Language: repo.Language,
			Tools: d.Plan.Tools, Instructions: d.Plan.Instructions,
			Verdict: scheduled.Record{Verdict: d.Verdict, MaxRuntime: d.Plan.MaxRuntime}.Encode(),
		}
		// No issue, so no priority: comparePlanned puts a scheduled start first by its kind.
		out = append(out, planned{Action: a, band: PriorityBand(""), software: d.Profile == ProfileSoftware})
	}
	return out
}
