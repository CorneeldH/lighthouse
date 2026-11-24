package history

import (
	"encoding/json"
	"strings"
	"testing"
)

type recDetector struct {
	got  []Record
	hits Hits
}

func (d *recDetector) Add(r Record) {
	d.got = append(d.got, r)
	d.hits.Add(r.File, r.Line, r.Timestamp)
}

func (d *recDetector) Finish(Env) Finding {
	return Finding{Key: "test", Status: "found", Confidence: "high", Sources: d.hits.Top()}
}

func TestAnalyze_MergesSessionByTimestamp(t *testing.T) {
	parent := writeLines(t,
		`{"type":"user","timestamp":"2026-10-01T10:51:00Z"}`,
		`{"type":"pr-link","prNumber":3}`)
	sub := writeLines(t, `{"type":"user","timestamp":"2026-10-01T10:45:00Z"}`)
	d := &recDetector{}
	old := newDetectors
	newDetectors = func(Env) []Detector { return []Detector{d} }
	defer func() { newDetectors = old }()

	rep := Analyze([]SessionFiles{{ID: "s", Paths: []string{parent, sub}}}, Env{}, AnalyzeOptions{Sources: true})
	if len(d.got) != 3 {
		t.Fatalf("records %d", len(d.got))
	}
	order := []string{d.got[0].Timestamp, d.got[1].Timestamp, d.got[2].Type}
	if order[0] != "2026-10-01T10:45:00Z" || order[1] != "2026-10-01T10:51:00Z" || order[2] != "pr-link" {
		t.Fatalf("order %v", order)
	}
	if rep.Sessions != 1 || rep.Schema != 1 {
		t.Fatalf("%+v", rep)
	}
	if rep.Span.First != "2026-10-01" || rep.Span.Last != "2026-10-01" {
		t.Fatalf("span %+v", rep.Span)
	}
	if len(rep.Findings) != 1 || len(rep.Findings[0].Sources) == 0 || rep.Findings[0].Sources[0].File != parent {
		t.Fatalf("sources %+v", rep.Findings)
	}
	rep = Analyze([]SessionFiles{{ID: "s", Paths: []string{parent}}}, Env{}, AnalyzeOptions{})
	if len(rep.Findings[0].Sources) != 0 {
		t.Fatal("sources must be omitted without Sources")
	}
}

func TestAnalyze_NoSessions(t *testing.T) {
	d := &recDetector{}
	old := newDetectors
	newDetectors = func(Env) []Detector { return []Detector{d} }
	defer func() { newDetectors = old }()
	rep := Analyze(nil, Env{}, AnalyzeOptions{})
	if rep.Findings[0].Status != "no_data" || rep.Level.Recommended != "light" || rep.Level.Reason != "no history" {
		t.Fatalf("%+v", rep)
	}
	newDetectors = old
	b, _ := json.Marshal(Analyze(nil, Env{}, AnalyzeOptions{}))
	for _, k := range []string{`"schema":1`, `"sessions":0`, `"skipped_lines":0`, `"findings":[{"key":"trunk","status":"no_data"`, `"key":"work_tracking","status":"no_data"`, `"level":{"recommended":"light","reason":"no history"}`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("missing %s in %s", k, b)
		}
	}
}

func TestAnalyze_SkippedAndSince(t *testing.T) {
	p := writeLines(t, `bad`, `{"type":"user","timestamp":"2026-10-01T10:00:00Z"}`)
	rep := Analyze([]SessionFiles{{ID: "s", Paths: []string{p}}}, Env{}, AnalyzeOptions{})
	if rep.SkippedLines != 1 {
		t.Fatalf("%+v", rep)
	}
}
