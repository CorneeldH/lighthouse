package triage

import (
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/issuestate"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Gate0 is a fixed stop of route-issue gate 0.
type Gate0 struct {
	Blockers []int  // open blocked_by numbers
	Label    string // labels.blocked_issue or labels.blocked_external when present
}

// FixedGate0 reports the gate 0 stop: the issue carries a blocked label, or has an open
// blocked_by without needs_mind or blocked_issue. The rule is
// issuestate.GateZero.
func FixedGate0(f Facts, l repoconfig.Labels) (Gate0, bool) {
	s, ok := issuestate.GateZero(f.Labels, f.Blockers, l)
	if !ok {
		return Gate0{}, false
	}
	return Gate0{Blockers: s.Blockers, Label: s.Label}, true
}

// Deviation is one way an issue breaks the label invariant.
type Deviation struct {
	Kind   string   // "missing", "many" or "blocking"
	Prefix string   // for missing and many
	Found  []string // the labels that broke it
}

// Invariant lists how the labels break the invariant of lighthouse-write-issue step 3a/3b: one
// label per configured prefix, at most one blocking label. A key the repo lacks is no demand.
func Invariant(labels []string, l repoconfig.Labels) []Deviation {
	var out []Deviation
	for _, prefix := range []string{l.LocationPrefix, l.AreaPrefix} {
		if prefix == "" {
			continue
		}
		var found []string
		for _, name := range labels {
			if strings.HasPrefix(name, prefix) {
				found = append(found, name)
			}
		}
		switch {
		case len(found) == 0:
			out = append(out, Deviation{Kind: "missing", Prefix: prefix})
		case len(found) > 1:
			out = append(out, Deviation{Kind: "many", Prefix: prefix, Found: found})
		}
	}
	var blocking []string
	for _, name := range labels {
		if name != "" && slices.Contains(l.Blocking(), name) {
			blocking = append(blocking, name)
		}
	}
	if len(blocking) > 1 {
		out = append(out, Deviation{Kind: "blocking", Found: blocking})
	}
	return out
}
