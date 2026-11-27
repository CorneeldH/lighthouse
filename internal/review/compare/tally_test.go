package compare

import (
	"math"
	"testing"
)

func f(id, source, sevCodex, sevFallback, decision string, round int) Finding {
	return Finding{ID: id, Source: source, SevCodex: sevCodex, SevFallback: sevFallback,
		Decision: decision, Round: round}
}

func TestTally_CounterfactualUsesOwnSeverity(t *testing.T) {
	res := Tally([]Record{{Findings: []Finding{f("a", "both", "MINOR", "MAJOR", "adopted", 1)}}})
	if got := res.CodexOnlyBM; got.Adopted != 1 || got.Caught != 0 {
		t.Errorf("Codex alone: want 1 in the denominator, 0 caught, got %+v", got)
	}
	if got := res.OpusOnlyBM; got.Adopted != 1 || got.Caught != 1 {
		t.Errorf("Opus alone: want 1 in the denominator, 1 caught, got %+v", got)
	}
}

func TestTally_OnlyRound1Counts(t *testing.T) {
	res := Tally([]Record{{Findings: []Finding{
		f("a", "codex", "MAJOR", "", "adopted", 1),
		f("b", "", "BLOCKER", "", "adopted", 2),
		f("c", "codex", "BLOCKER", "", "adopted", 2),
	}}})
	total := 0
	for _, c := range res.Sources {
		total += c.N
	}
	if total != 1 || res.Sources[""].N != 0 {
		t.Errorf("only the round-1 finding counts, got %+v", res.Sources)
	}
	if res.CodexOnlyBM.Adopted != 1 {
		t.Errorf("denominator must ignore round 2, got %+v", res.CodexOnlyBM)
	}
}

func TestTally_OpusOnlyMajorLowersCodexCapture(t *testing.T) {
	res := Tally([]Record{{Findings: []Finding{
		f("a", "codex", "MAJOR", "", "adopted", 1),
		f("b", "fallback", "", "MAJOR", "adopted", 1),
	}}})
	c := res.CodexOnlyBM
	if c.Adopted != 2 || c.Caught != 1 {
		t.Fatalf("want 1 of 2 caught, got %+v", c)
	}
	if !c.Defined || c.Low >= c.High || c.High >= 1 {
		t.Errorf("capture below 100%% expected with an interval, got %+v", c)
	}
}

func TestTally_NoAdoptedBlockerMajor_NoDivision(t *testing.T) {
	res := Tally([]Record{{Findings: []Finding{
		f("a", "codex", "MINOR", "", "adopted", 1),
		f("b", "codex", "MAJOR", "", "rejected", 1),
	}}})
	for _, c := range []Capture{res.CodexOnlyBM, res.OpusOnlyBM} {
		if c != (Capture{}) || c.Defined {
			t.Errorf("want the zero Capture, got %+v", c)
		}
		if math.IsNaN(c.Low) || math.IsNaN(c.High) {
			t.Errorf("NaN in %+v", c)
		}
	}
	if res.Sources["codex"].N != 2 || res.Sources["codex"].Rejected != 1 {
		t.Errorf("counts by source still hold, got %+v", res.Sources["codex"])
	}
	if Tally(nil).CodexOnlyBM.Defined {
		t.Error("no records: undefined")
	}
}

func TestTally_CountsPerSeverityAndDecision(t *testing.T) {
	res := Tally([]Record{{Findings: []Finding{
		f("a", "codex", "MAJOR", "", "adopted", 1),
		f("b", "codex", "MINOR", "", "rejected", 1),
		f("c", "codex", "MAJOR", "", "left_open", 1),
		f("d", "codex", "MAJOR", "", "", 1),
	}}})
	c := res.Sources["codex"]
	if c.N != 4 || c.Adopted != 1 || c.Rejected != 1 || c.LeftOpen != 1 ||
		c.BySource["MAJOR"] != 3 || c.BySource["MINOR"] != 1 {
		t.Errorf("got %+v", c)
	}
}

func TestWilson_KnownValues(t *testing.T) {
	lo, hi := Wilson(9, 10)
	if math.Abs(lo-0.5958) > 0.01 || math.Abs(hi-0.9821) > 0.01 {
		t.Errorf("Wilson(9,10) = %.4f..%.4f, want about 0.60..0.98", lo, hi)
	}
	if lo, hi := Wilson(0, 0); lo != 0 || hi != 0 {
		t.Errorf("Wilson(0,0) = %v..%v, want 0..0", lo, hi)
	}
	if lo, hi := Wilson(10, 10); hi > 1 || lo <= 0.6 {
		t.Errorf("Wilson(10,10) = %v..%v", lo, hi)
	}
}
