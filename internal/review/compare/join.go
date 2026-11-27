package compare

import (
	"sort"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
	"github.com/CorneeldH/lighthouse/internal/telemetry/report"
)

// Unknown is the value of a class for a record or trajectory without that feature.
const Unknown = "onbekend"

// minGroup is the number of trajectories each compared group needs for a conclusion.
const minGroup = 10

// Class is one value of one feature (route, plangrootte, gebied, repo) with the findings of the
// records in it and the recovery after merge of its trajectories. Trajectories counts the
// distinct linked trajectories; Recovered and Open count those among them, and Measured those
// with at least one recovery row (0 means recovery is not measured, not that none happened). Conclusion is true
// only (never for the class Unknown) when at least 10 trajectories saw a Codex finding and at least 10 an Opus finding.
type Class struct {
	Key, Value      string
	Trajectories    int
	Result          Result
	Recovered, Open int
	Measured        int
	Conclusion      bool
}

type tkey struct {
	repo  string
	issue int
}

type acc struct {
	records       []Record
	trajs         map[tkey]bool
	codex, opus   map[tkey]bool
	recov, opened int
	measured      int
}

// ByClass groups the records by the features of their trajectory. A record without a linked
// trajectory counts for findings, in the class Unknown (for repo: under its own repo when it
// has one), but never for recovery. A trajectory counts once per class, however many records it
// has. Per group means per source-determined trajectory: the Codex group holds the trajectories
// with a finding from codex or both, the Opus group those with a finding from fallback or both.
func ByClass(rs []Record, ts []*report.Trajectory) []Class {
	byT := map[tkey]*report.Trajectory{}
	for _, t := range ts {
		byT[tkey{t.Repo, t.Issue}] = t
	}
	accs := map[[2]string]*acc{}
	get := func(k, v string) *acc {
		a := accs[[2]string{k, v}]
		if a == nil {
			a = &acc{trajs: map[tkey]bool{}, codex: map[tkey]bool{}, opus: map[tkey]bool{}}
			accs[[2]string{k, v}] = a
		}
		return a
	}
	for _, r := range rs {
		var t *report.Trajectory
		if r.Linked {
			t = byT[tkey{r.Repo, r.Issue}]
		}
		feats := map[string]string{"route": Unknown, "plangrootte": Unknown, "gebied": Unknown, "repo": Unknown}
		if r.Repo != "" {
			feats["repo"] = r.Repo
		}
		if t != nil {
			feats["repo"] = t.Repo
			if t.Route != nil && *t.Route != "" {
				feats["route"] = *t.Route
			}
			if s, ok := report.SizeClass(t.PlanTasks); ok {
				feats["plangrootte"] = s
			}
			if t.Area != nil && *t.Area != "" {
				feats["gebied"] = *t.Area
			}
		}
		for k, v := range feats {
			a := get(k, v)
			a.records = append(a.records, r)
			if t == nil {
				continue
			}
			key := tkey{t.Repo, t.Issue}
			if !a.trajs[key] {
				a.trajs[key] = true
				if len(t.Recoveries) > 0 {
					a.measured++
				}
				switch recoveryOf(t) {
				case "recovered":
					a.recov++
				case "open":
					a.opened++
				}
			}
			for _, f := range r.Findings {
				if f.Round != 1 {
					continue
				}
				switch f.Source {
				case "codex":
					a.codex[key] = true
				case "fallback":
					a.opus[key] = true
				case "both":
					a.codex[key], a.opus[key] = true, true
				}
			}
		}
	}
	var out []Class
	for k, a := range accs {
		out = append(out, Class{Key: k[0], Value: k[1], Trajectories: len(a.trajs), Result: Tally(a.records),
			Recovered: a.recov, Open: a.opened, Measured: a.measured,
			Conclusion: k[1] != Unknown && len(a.codex) >= minGroup && len(a.opus) >= minGroup})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// recoveryOf is "open" with any open measurement, "recovered" when the trajectory is closed and
// a PR recovered, otherwise "" (none, or nothing measured).
func recoveryOf(t *report.Trajectory) string {
	var recovered bool
	for _, r := range t.Recoveries {
		switch r.Status {
		case string(telemetry.RecoveryOpen):
			return "open"
		case string(telemetry.RecoveryRecovered):
			recovered = true
		}
	}
	if recovered && t.Closed {
		return "recovered"
	}
	return ""
}
