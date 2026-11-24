package history

import (
	"sort"
	"time"
)

// SessionFiles is one session: the main transcript and its subagent transcripts.
type SessionFiles struct {
	ID    string
	Paths []string
}

type AnalyzeOptions struct {
	Since   time.Time
	Sources bool
}

// detectors lists the detectors of a run; tasks add theirs here.
func detectors(env Env) []Detector { return []Detector{newTrunk(env), newWorkTracking()} }

// newDetectors is a variable so tests can plug in their own.
var newDetectors = detectors

type keyed struct {
	rec Record
	key time.Time
}

// Analyze feeds every record of every session, in timestamp order per session, to the
// detectors and builds the report.
func Analyze(files []SessionFiles, env Env, o AnalyzeOptions) Report {
	rep := Report{Schema: 1, Sessions: len(files), Findings: []Finding{}}
	dets := newDetectors(env)
	var first, last time.Time
	for _, s := range files {
		var recs []keyed
		for _, p := range s.Paths {
			var run time.Time
			n, err := ReadFile(p, o.Since, func(r Record) {
				if ts, ok := parseTime(r.Timestamp); ok {
					if ts.After(run) {
						run = ts
					}
					if first.IsZero() || ts.Before(first) {
						first = ts
					}
					if ts.After(last) {
						last = ts
					}
				}
				recs = append(recs, keyed{r, run})
			})
			rep.SkippedLines += n
			if err != nil {
				rep.SkippedLines++
			}
		}
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].key.Before(recs[j].key) })
		for _, k := range recs {
			for _, d := range dets {
				d.Add(k.rec)
			}
		}
	}
	if !first.IsZero() {
		rep.Span = Span{First: first.UTC().Format("2006-01-02"), Last: last.UTC().Format("2006-01-02")}
	}
	for _, d := range dets {
		f := d.Finish(env)
		if rep.Sessions == 0 {
			f.Status = "no_data"
		}
		if !o.Sources {
			f.Sources = nil
		}
		rep.Findings = append(rep.Findings, f)
	}
	rep.Level = decideLevel(rep)
	return rep
}

// decideLevel is a stub until the level rules land.
func decideLevel(r Report) Level {
	if r.Sessions == 0 {
		return Level{Recommended: "light", Reason: "no history"}
	}
	return Level{Recommended: "light", Reason: "not evaluated"}
}
