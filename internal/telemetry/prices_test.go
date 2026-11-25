package telemetry

import (
	"math"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func testTable() priceTable {
	return priceTable{"claude-x": {{
		Model: "claude-x", AsOf: day("2026-01-01"), Source: "test",
		Input: 1, Output: 2, CacheRead: 3, CacheWrite5m: 4, CacheWrite1h: 5,
	}}}
}

func TestEstimateKnownModel(t *testing.T) {
	m := int64(1_000_000)
	tok := Tokens{Input: m, Output: m, CacheRead: m, CacheWrite5m: m, CacheWrite1h: m}
	cost, ok := estimateWith(testTable(), "claude-x", day("2026-06-01"), tok)
	if !ok || !near(cost, 15) {
		t.Fatalf("cost = %v ok = %v, want 15 true", cost, ok)
	}
	if tok.CacheWrite() != 2*m {
		t.Fatalf("CacheWrite = %d", tok.CacheWrite())
	}
}

func TestEstimateUnknownModel(t *testing.T) {
	for _, model := range []string{"claude-y", "claude-x[1m]", "claude-x-20250101", ""} {
		if _, ok := estimateWith(testTable(), model, day("2026-06-01"), Tokens{Input: 1}); ok {
			t.Errorf("model %q: ok = true, want false", model)
		}
	}
}

func TestEstimateUsesPriceAtTimestamp(t *testing.T) {
	table := priceTable{"claude-x": {
		{Model: "claude-x", AsOf: day("2026-01-01"), Input: 1},
		{Model: "claude-x", AsOf: day("2026-03-01"), Input: 10},
	}}
	tok := Tokens{Input: 1_000_000}
	for _, c := range []struct {
		at   string
		want float64
	}{{"2025-06-01", 1}, {"2026-02-01", 1}, {"2026-03-01", 10}, {"2026-09-01", 10}} {
		got, ok := estimateWith(table, "claude-x", day(c.at), tok)
		if !ok || !near(got, c.want) {
			t.Errorf("at %s: got %v ok=%v, want %v", c.at, got, ok, c.want)
		}
	}
}

func TestEmbeddedPricesParse(t *testing.T) {
	table, err := parsePrices(pricesTOML)
	if err != nil {
		t.Fatal(err)
	}
	if len(table) == 0 {
		t.Fatal("empty price table")
	}
	for _, model := range []string{"claude-sonnet-5-5", "claude-opus-5-5"} {
		if len(table[model]) == 0 {
			t.Errorf("model %q missing", model)
		}
	}
	for model, rows := range table {
		for _, r := range rows {
			if r.Source == "" || r.AsOf.IsZero() {
				t.Errorf("%s: source or as_of missing", model)
			}
			if r.Input < 0 || r.Output < 0 || r.CacheRead < 0 || r.CacheWrite5m < 0 || r.CacheWrite1h < 0 {
				t.Errorf("%s: negative price", model)
			}
		}
	}
	if _, ok := Estimate("claude-opus-5-5", day("2026-11-24"), Tokens{Input: 1}); !ok {
		t.Error("Estimate does not know claude-opus-5-5")
	}
}
