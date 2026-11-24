package transcript

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// asst builds an assistant line; extra is spliced into the top-level object.
func asst(id, req, model, at, usage, extra string) string {
	idPart := ""
	if id != "" {
		idPart = fmt.Sprintf(`"id":%q,`, id)
	}
	reqPart := ""
	if req != "" {
		reqPart = fmt.Sprintf(`"requestId":%q,`, req)
	}
	return fmt.Sprintf(`{"type":"assistant",%s%s"sessionId":"s1","timestamp":%q,"message":{%s"model":%q,"usage":%s}}`,
		extra, reqPart, at, idPart, model, usage)
}

func use(in, out, cr, cc int64) string {
	return fmt.Sprintf(`{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}`, in, out, cr, cc)
}

func f(name string, lines ...string) File {
	return File{Name: name, R: strings.NewReader(strings.Join(lines, "\n") + "\n")}
}

func mustParse(t *testing.T, files ...File) Reading {
	t.Helper()
	r, err := Parse(files)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseRealShapeFixture(t *testing.T) {
	fh, err := os.Open("testdata/real-shape.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fh.Close() })
	r := mustParse(t, File{Name: "sess-real.jsonl", R: fh})
	u := r.UsageBetween(time.Time{}, time.Time{})["model-a"]
	want := telemetry.Tokens{Input: 2, Output: 50, CacheRead: 200, CacheWrite1h: 300}
	if u.Tokens != want {
		t.Fatalf("tokens = %+v, want %+v", u.Tokens, want)
	}
	if r.SessionKey() != "sess-real" {
		t.Fatalf("session = %q", r.SessionKey())
	}
}

func TestParseDedupesMessageAcrossLines(t *testing.T) {
	u := use(10, 5, 100, 0)
	main := f("s1.jsonl",
		asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", u, ""),
		asst("m1", "r1", "model-a", "2026-01-01T10:00:01Z", u, ""),
		asst("m1", "r1", "model-a", "2026-01-01T10:00:02Z", u, ""))
	sub := f("s1/subagents/agent-x.jsonl",
		asst("m2", "r2", "model-b", "2026-01-01T10:01:00Z", use(1, 2, 3, 4), `"isSidechain":true,`))
	got := mustParse(t, main, sub).UsageBetween(time.Time{}, time.Time{})
	if len(got) != 2 {
		t.Fatalf("models = %v", got)
	}
	if a := got["model-a"].Tokens; a.Input != 10 || a.Output != 5 || a.CacheRead != 100 {
		t.Fatalf("model-a = %+v", a)
	}
	if b := got["model-b"].Tokens; b.Input != 1 || b.Output != 2 || b.CacheRead != 3 || b.CacheWrite5m != 4 {
		t.Fatalf("model-b = %+v", b)
	}
}

func TestParseDedupesAcrossMainAndSubagent(t *testing.T) {
	u := use(10, 5, 0, 0)
	got := mustParse(t,
		f("s1.jsonl", asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", u, "")),
		f("s1/subagents/a.jsonl", asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", u, ""))).
		UsageBetween(time.Time{}, time.Time{})
	if got["model-a"].Tokens.Input != 10 {
		t.Fatalf("counted twice: %+v", got["model-a"])
	}
}

func TestParseFallsBackToRequestID(t *testing.T) {
	u := use(10, 5, 0, 0)
	got := mustParse(t, f("s1.jsonl",
		asst("", "r1", "model-a", "2026-01-01T10:00:00Z", u, ""),
		asst("", "r1", "model-a", "2026-01-01T10:00:01Z", u, ""),
		asst("", "r2", "model-a", "2026-01-01T10:00:02Z", u, ""))).UsageBetween(time.Time{}, time.Time{})
	if got["model-a"].Tokens.Input != 20 {
		t.Fatalf("input = %d, want 20", got["model-a"].Tokens.Input)
	}
}

func TestParseLineWithoutIDsCountsAlone(t *testing.T) {
	u := use(10, 5, 0, 0)
	got := mustParse(t, f("s1.jsonl",
		asst("", "", "model-a", "2026-01-01T10:00:00Z", u, ""),
		asst("", "", "model-a", "2026-01-01T10:00:01Z", u, ""))).UsageBetween(time.Time{}, time.Time{})
	if got["model-a"].Tokens.Input != 20 {
		t.Fatalf("input = %d, want 20", got["model-a"].Tokens.Input)
	}
}

func TestParseIgnoresOtherLineTypes(t *testing.T) {
	r := mustParse(t, f("s1.jsonl",
		`{"type":"api-request-blob","message":{"id":"m9","model":"model-a","usage":{"input_tokens":99}}}`,
		`{"type":"cost-state","modelUsage":{"model-a":{"inputTokens":99}}}`,
		`{"type":"attachment","timestamp":"2026-01-01T10:00:00Z","message":{"id":"m8","model":"model-a","usage":{"input_tokens":99}}}`))
	if got := r.UsageBetween(time.Time{}, time.Time{}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if s, e := r.Bounds(); !s.IsZero() || !e.IsZero() {
		t.Fatalf("bounds %v %v", s, e)
	}
}

func TestParseSplitsCacheWrite(t *testing.T) {
	split := `{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}`
	got := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", split, ""),
		asst("m2", "r2", "model-b", "2026-01-01T10:00:00Z", use(1, 1, 0, 30), ""))).
		UsageBetween(time.Time{}, time.Time{})
	if a := got["model-a"].Tokens; a.CacheWrite5m != 10 || a.CacheWrite1h != 20 {
		t.Fatalf("split = %+v", a)
	}
	if b := got["model-b"].Tokens; b.CacheWrite5m != 30 || b.CacheWrite1h != 0 {
		t.Fatalf("unsplit = %+v", b)
	}
}

func TestParseUsageBetweenWindows(t *testing.T) {
	r := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", use(10, 1, 0, 0), ""),
		asst("m2", "r2", "model-a", "2026-01-01T12:00:00Z", use(20, 1, 0, 0), "")))
	mid := ts("2026-01-01T11:00:00Z")
	first := r.UsageBetween(time.Time{}, mid)["model-a"].Tokens.Input
	second := r.UsageBetween(mid, time.Time{})["model-a"].Tokens.Input
	all := r.UsageBetween(time.Time{}, time.Time{})["model-a"].Tokens.Input
	if first != 10 || second != 20 || first+second != all {
		t.Fatalf("first=%d second=%d all=%d", first, second, all)
	}
	// [from,to): a message exactly at `to` belongs to the next window.
	if got := r.UsageBetween(time.Time{}, ts("2026-01-01T12:00:00Z"))["model-a"].Tokens.Input; got != 10 {
		t.Fatalf("end is exclusive, got %d", got)
	}
}

func TestParseBoundsUnordered(t *testing.T) {
	r := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "model-a", "2026-01-01T12:00:00Z", use(1, 1, 0, 0), ""),
		asst("m2", "r2", "model-a", "2026-01-01T10:00:00Z", use(1, 1, 0, 0), ""),
		asst("m3", "r3", "model-a", "2026-01-01T11:00:00Z", use(1, 1, 0, 0), "")))
	s, e := r.Bounds()
	if !s.Equal(ts("2026-01-01T10:00:00Z")) || !e.Equal(ts("2026-01-01T12:00:00Z")) {
		t.Fatalf("bounds %v %v", s, e)
	}
}

func withTable(t *testing.T, fn func(string, time.Time, telemetry.Tokens) (float64, bool)) {
	t.Helper()
	old := estimate
	estimate = fn
	t.Cleanup(func() { estimate = old })
}

func TestUsageCostPerMessageTimestamp(t *testing.T) {
	boundary := ts("2026-01-01T12:00:00Z")
	withTable(t, func(model string, at time.Time, tk telemetry.Tokens) (float64, bool) {
		if model != "model-a" {
			return 0, false
		}
		rate := 1.0
		if !at.Before(boundary) {
			rate = 10.0
		}
		return float64(tk.Input) * rate, true
	})
	r := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "model-a", "2026-01-01T11:00:00Z", use(100, 0, 0, 0), ""),
		asst("m2", "r2", "model-a", "2026-01-01T13:00:00Z", use(100, 0, 0, 0), "")))
	c := r.UsageBetween(time.Time{}, time.Time{})["model-a"].Cost
	if c == nil || *c != 1100 { // 100*1 + 100*10, not 200 at one price
		t.Fatalf("cost = %v, want 1100", c)
	}
}

func TestUsageCostNilForUnknownModel(t *testing.T) {
	withTable(t, func(model string, _ time.Time, tk telemetry.Tokens) (float64, bool) {
		return float64(tk.Input), model == "known"
	})
	got := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "known", "2026-01-01T11:00:00Z", use(5, 0, 0, 0), ""),
		asst("m2", "r2", "mystery", "2026-01-01T11:00:00Z", use(5, 0, 0, 0), ""))).
		UsageBetween(time.Time{}, time.Time{})
	if got["known"].Cost == nil || *got["known"].Cost != 5 {
		t.Fatalf("known cost = %v", got["known"].Cost)
	}
	if got["mystery"].Cost != nil {
		t.Fatalf("mystery cost = %v, want nil", *got["mystery"].Cost)
	}
	if got["mystery"].Tokens.Input != 5 {
		t.Fatalf("tokens must still count: %+v", got["mystery"])
	}
}

func TestParseTruncatedLastLine(t *testing.T) {
	good := asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", use(10, 1, 0, 0), "")
	r, err := Parse([]File{{Name: "s1.jsonl", R: strings.NewReader(good + "\n" + `{"type":"assistant","message":{"id":"m2","mod`)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.UsageBetween(time.Time{}, time.Time{})["model-a"].Tokens.Input; got != 10 {
		t.Fatalf("input = %d", got)
	}
}

func TestParseOnlyUserLines(t *testing.T) {
	r := mustParse(t, f("s1.jsonl",
		`{"type":"user","sessionId":"s1","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"x"}}`))
	got := r.UsageBetween(time.Time{}, time.Time{})
	if got == nil || len(got) != 0 {
		t.Fatalf("got %v, want empty non-nil map", got)
	}
	if s, _ := r.Bounds(); s.IsZero() {
		t.Fatal("user lines still bound the session")
	}
}

func TestParseSkipsSyntheticAndUsageless(t *testing.T) {
	r := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "<synthetic>", "2026-01-01T10:00:00Z", use(7, 7, 7, 7), ""),
		`{"type":"assistant","timestamp":"2026-01-01T10:00:01Z","message":{"id":"m2","model":"model-a"}}`,
		`not json at all`,
		`[1,2]`))
	if got := r.UsageBetween(time.Time{}, time.Time{}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestParseHugeLine(t *testing.T) {
	big := strings.Repeat("x", 5<<20)
	line := fmt.Sprintf(`{"type":"assistant","timestamp":"2026-01-01T10:00:00Z","note":%q,"message":{"id":"m1","model":"model-a","usage":%s}}`, big, use(3, 0, 0, 0))
	r := mustParse(t, f("s1.jsonl", line))
	if got := r.UsageBetween(time.Time{}, time.Time{})["model-a"].Tokens.Input; got != 3 {
		t.Fatalf("input = %d", got)
	}
}

func TestParseDuplicateTakesMaxAndEarliestAt(t *testing.T) {
	r := mustParse(t, f("s1.jsonl",
		asst("m1", "r1", "model-a", "2026-01-01T10:00:02Z", use(1, 1, 0, 0), ""),
		asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", use(1, 8, 0, 0), ""),
		asst("m1", "r1", "model-a", "2026-01-01T10:00:01Z", use(1, 5, 0, 0), "")))
	if len(r.Messages) != 1 {
		t.Fatalf("messages = %d", len(r.Messages))
	}
	if got := r.Messages[0].Tokens.Output; got != 8 {
		t.Fatalf("output = %d, want max 8", got)
	}
	if !r.Messages[0].At.Equal(ts("2026-01-01T10:00:00Z")) {
		t.Fatalf("At = %v, want earliest", r.Messages[0].At)
	}
}

func TestParseDuplicateCacheWriteNotDoubleCounted(t *testing.T) {
	split := `{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":300}}`
	for _, lines := range [][]string{
		{asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", use(1, 1, 0, 300), ""), asst("m1", "r1", "model-a", "2026-01-01T10:00:01Z", split, "")},
		{asst("m1", "r1", "model-a", "2026-01-01T10:00:00Z", split, ""), asst("m1", "r1", "model-a", "2026-01-01T10:00:01Z", use(1, 1, 0, 300), "")},
	} {
		got := mustParse(t, f("s1.jsonl", lines...)).UsageBetween(time.Time{}, time.Time{})["model-a"].Tokens
		if got.CacheWrite() != 300 {
			t.Fatalf("cache write = %+v, want total 300", got)
		}
	}
}
