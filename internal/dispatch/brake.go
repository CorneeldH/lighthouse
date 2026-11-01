package dispatch

import "fmt"

// profileSoftware is the repo profile the software limit counts.
const profileSoftware = "software"

// triggerUsage is the trigger of a resume of a run parked by the usage brake.
const triggerUsage = "usage"

// Brake is the host-wide state the brake reads, once per tick (spec "De rem"); nil in Input is no brake.
type Brake struct {
	Paused            bool     `json:"paused"`
	Manual            bool     `json:"manual"` // lighthouse run: only pause and usage
	UsageMeasured     bool     `json:"usage_measured"`
	SessionPct        int      `json:"session_pct"`
	WeekPct           int      `json:"week_pct"`
	HaltPctSession    int      `json:"halt_pct_session"`
	HaltPctWeek       int      `json:"halt_pct_week"`
	MemAvailableGiB   *float64 `json:"mem_available_gib"` // nil: not measured
	MinMemAvailableGB int      `json:"min_mem_available_gb"`
	ReservePerRunGB   int      `json:"reserve_per_run_gb"`
	Slots             int      `json:"slots"`
	MaxSoftware       int      `json:"max_software"`
	InUse             int      `json:"in_use"`          // open plus parked runs on the host
	SoftwareInUse     int      `json:"software_in_use"` // of those, profile "software"
}

// apply walks the starts and resumes of ps in order and lets each through, drops it (pause)
// or turns it into a defer with the text of the first closed brake: pause, usage, memory,
// slots, software. Other actions pass untouched. A parked resume skips slots and software and
// does not count in their totals, but counts for memory.
func (b *Brake) apply(ps []planned) []planned {
	out := make([]planned, 0, len(ps))
	passed, admitted, admittedSoftware := 0, 0, 0
	for _, p := range ps {
		if p.group() != 2 {
			out = append(out, p)
			continue
		}
		if b.Paused {
			continue
		}
		if reason := b.closed(p, passed, admitted, admittedSoftware); reason != "" {
			out = append(out, planned{Action: Action{
				Kind: KindDefer, Repo: p.Repo, Issue: p.Issue, Language: p.Language, Reason: reason,
			}})
			continue
		}
		passed++
		if p.Kind == KindStart && !p.parked {
			admitted++
			if p.software {
				admittedSoftware++
			}
		}
		out = append(out, p)
	}
	return out
}

// closed returns the defer text of the first closed brake for p, or "" when all are open.
func (b *Brake) closed(p planned, passed, admitted, admittedSoftware int) string {
	if !b.UsageMeasured {
		return "usage: not measured"
	}
	if b.SessionPct >= b.HaltPctSession || b.WeekPct >= b.HaltPctWeek {
		return fmt.Sprintf("usage: session %d%%, week %d%%", b.SessionPct, b.WeekPct)
	}
	if b.Manual {
		return ""
	}
	if b.MemAvailableGiB == nil {
		return "memory: MemAvailable not measured"
	}
	if *b.MemAvailableGiB-float64(passed*b.ReservePerRunGB) < float64(b.MinMemAvailableGB) {
		return fmt.Sprintf("memory: MemAvailable %.1f GiB - %d x %d GiB < %d GiB",
			*b.MemAvailableGiB, passed, b.ReservePerRunGB, b.MinMemAvailableGB)
	}
	if p.parked {
		return ""
	}
	if b.InUse+admitted >= b.Slots {
		return fmt.Sprintf("slots: %d of %d in use", b.InUse+admitted, b.Slots)
	}
	if p.software && b.SoftwareInUse+admittedSoftware >= b.MaxSoftware {
		return fmt.Sprintf("software: %d of %d in use", b.SoftwareInUse+admittedSoftware, b.MaxSoftware)
	}
	return ""
}
