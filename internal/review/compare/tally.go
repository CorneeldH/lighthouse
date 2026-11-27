package compare

import "math"

// Counts tallies the round-1 findings of one source. BySource holds the findings per severity,
// read from the severity of that source itself; the others count per decision.
type Counts struct {
	N                           int
	BySource                    map[string]int
	Adopted, Rejected, LeftOpen int
}

// Capture is the share of adopted BLOCKER and MAJOR findings that one reviewer alone would have
// caught. Adopted is the denominator, Caught the numerator; Low and High are the Wilson 95%
// interval. Defined is false when the denominator is 0: then no share exists.
type Capture struct {
	Adopted, Caught int
	Low, High       float64
	Defined         bool
}

// Result is the tally of the selected records.
type Result struct {
	Sources                 map[string]Counts
	CodexOnlyBM, OpusOnlyBM Capture
}

func blockerOrMajor(sev string) bool { return sev == "BLOCKER" || sev == "MAJOR" }

// ownSeverity is the severity a source gave itself; a finding found by both counts as Codex.
func ownSeverity(f Finding) string {
	if f.Source == "fallback" {
		return f.SevFallback
	}
	return f.SevCodex
}

// Tally counts the round-1 findings per source, severity and decision. The counterfactual
// captures use the severity of each reviewer itself: a finding is in the denominator when it is
// adopted and BLOCKER or MAJOR for at least one source, and caught when it is so for the
// reviewer in question.
func Tally(rs []Record) Result {
	res := Result{Sources: map[string]Counts{}}
	var codexCaught, opusCaught, denom int
	for _, r := range rs {
		for _, f := range r.Findings {
			if f.Round != 1 || f.Source == "" {
				continue
			}
			c := res.Sources[f.Source]
			if c.BySource == nil {
				c.BySource = map[string]int{}
			}
			c.N++
			c.BySource[ownSeverity(f)]++
			switch f.Decision {
			case "adopted":
				c.Adopted++
			case "rejected":
				c.Rejected++
			case "left_open":
				c.LeftOpen++
			}
			res.Sources[f.Source] = c
			if f.Decision != "adopted" {
				continue
			}
			byCodex, byOpus := blockerOrMajor(f.SevCodex), blockerOrMajor(f.SevFallback)
			if !byCodex && !byOpus {
				continue
			}
			denom++
			if byCodex {
				codexCaught++
			}
			if byOpus {
				opusCaught++
			}
		}
	}
	res.CodexOnlyBM = capture(codexCaught, denom)
	res.OpusOnlyBM = capture(opusCaught, denom)
	return res
}

func capture(caught, denom int) Capture {
	if denom == 0 {
		return Capture{}
	}
	lo, hi := Wilson(caught, denom)
	return Capture{Adopted: denom, Caught: caught, Low: lo, High: hi, Defined: true}
}

// Wilson returns the Wilson score interval, 95%, for k successes in n trials; n < 1 gives 0, 0.
func Wilson(k, n int) (low, high float64) {
	if n < 1 {
		return 0, 0
	}
	const z = 1.959964
	p, nn := float64(k)/float64(n), float64(n)
	den := 1 + z*z/nn
	centre := p + z*z/(2*nn)
	half := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn))
	return math.Max(0, (centre-half)/den), math.Min(1, (centre+half)/den)
}
