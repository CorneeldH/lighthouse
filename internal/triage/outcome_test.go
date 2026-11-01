package triage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

var outcomeLabels = repoconfig.Labels{
	BlockedIssue: "3-blocked-issue", BlockedExternal: "3-blocked-external",
	NeedsShaping: "3-needs-shaping", NeedsDecision: "3-needs-decision",
	NeedsHands: "3-needs-hands", NeedsTesting: "3-needs-testing",
	AreaPrefix: "area:",
}

var outcomeTarget = Target{Language: "nl", Worktree: "/r", DefaultBranch: "main", WaitForNight: true}

func outcomeFacts(labels ...string) Facts {
	return Facts{Number: 7, AuthorTrusted: true, Labels: append([]string{"area:x"}, labels...)}
}

func judged(gate int, sub, line string) *Judgment {
	return &Judgment{RouteLine: line, Gate: gate, Sub: sub, Redundancy: "none", Headless: true}
}

func TestChooseRows(t *testing.T) {
	g0 := &Gate0{Blockers: []int{5}, Label: "3-blocked-external"}
	g0Line := "#7 Poort 0: stop — #5, 3-blocked-external"
	dup := judged(4, "b", "#7 Poort 4b: writing-plans — x")
	dup.Redundancy, dup.RedundancyIssue, dup.RedundancyEvidence = "duplicate", 3, "zelfde titel"
	dup0 := judged(0, "", g0Line)
	dup0.Redundancy, dup0.RedundancyIssue, dup0.RedundancyEvidence = "duplicate", 3, "zelfde titel"
	design := judged(4, "c", "#7 Poort 4c: brainstorming — x")
	design.Questions = []Question{{Text: "Hoe?", Options: []string{"een", "twee"}, Recommended: 1}}
	interactive := judged(4, "b", "#7 Poort 4b: x — y")
	interactive.Headless, interactive.HeadlessReason = false, "kiest een smaak"
	untrusted := outcomeFacts()
	untrusted.AuthorTrusted = false

	tests := []struct {
		name   string
		f      Facts
		devs   []Deviation
		g0     *Gate0
		j      *Judgment
		row    Row
		marker protocol.TriageMarker
	}{
		{"invariant wins over 4b", outcomeFacts(), []Deviation{{Kind: "missing", Prefix: "area:"}}, nil, judged(4, "b", "#7 Poort 4b: x — y"),
			RowInvariant, protocol.TriageMarker{Gate: "invariant", Stop: true}},
		{"invariant without judgment", outcomeFacts(), []Deviation{{Kind: "missing", Prefix: "area:"}}, nil, nil,
			RowInvariant, protocol.TriageMarker{Gate: "invariant", Stop: true}},
		{"duplicate at 4b", outcomeFacts(), nil, nil, dup, RowRedundant, protocol.TriageMarker{Gate: "4b"}},
		{"duplicate at gate 0 has no stop", outcomeFacts(), nil, g0, dup0, RowRedundant, protocol.TriageMarker{Gate: "0"}},
		{"gate 0", outcomeFacts(), nil, g0, judged(0, "", g0Line), RowStop,
			protocol.TriageMarker{Gate: "0", Blockers: []int{5}, Label: "3-blocked-external", Stop: true}},
		{"gate 1", outcomeFacts(), nil, nil, judged(1, "", "#7 Poort 1: stop — x"), RowStop, protocol.TriageMarker{Gate: "1"}},
		{"4c with needs-decision", outcomeFacts("3-needs-decision"), nil, nil, design, RowDesign,
			protocol.TriageMarker{Gate: "4c", Label: "3-needs-decision", Stop: true}},
		{"4c without label", outcomeFacts(), nil, nil, design, RowDesign, protocol.TriageMarker{Gate: "4c"}},
		{"headless no", outcomeFacts(), nil, nil, interactive, RowInteractive, protocol.TriageMarker{Gate: "4b"}},
		{"untrusted author", untrusted, nil, nil, judged(4, "b", "#7 Poort 4b: x — y"), RowUntrusted, protocol.TriageMarker{Gate: "4b"}},
		{"needs-hands forwards without label", outcomeFacts("3-needs-hands"), nil, nil, judged(4, "b", "#7 Poort 4b: x — y"),
			RowForward, protocol.TriageMarker{Gate: "4b"}},
		{"gate 2", outcomeFacts(), nil, nil, judged(2, "", "#7 Poort 2: x — y"), RowForward, protocol.TriageMarker{Gate: "2"}},
		{"4a", outcomeFacts(), nil, nil, judged(4, "a", "#7 Poort 4a: x — y"), RowForward, protocol.TriageMarker{Gate: "4a"}},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			got := Choose(c.f, c.devs, c.g0, c.j, outcomeLabels, outcomeTarget)
			if got.Row != c.row {
				t.Errorf("Row = %d, want %d", got.Row, c.row)
			}
			if !reflect.DeepEqual(got.Marker, c.marker) {
				t.Errorf("Marker = %+v, want %+v", got.Marker, c.marker)
			}
			first, _, _ := strings.Cut(got.Comment, "\n")
			if first != protocol.RenderTriageMarker(c.marker) {
				t.Errorf("first line = %q", first)
			}
			if parsed, ok := protocol.ParseTriageMarker(got.Comment); !ok || !reflect.DeepEqual(parsed, c.marker) {
				t.Errorf("parsed marker = %+v, %v; want %+v", parsed, ok, c.marker)
			}
		})
	}
}

// allRows returns one outcome per row.
func allRows() map[string]Outcome {
	g0 := &Gate0{Blockers: []int{114}, Label: "3-blocked-issue"}
	design := judged(4, "c", "#7 Poort 4c: brainstorming — x")
	design.Questions = []Question{
		{Text: "Hoe?\n```evil", Options: []string{"een", "twee"}, Recommended: 1},
		{Text: "Waar?", Options: []string{"hier"}, Recommended: 0},
	}
	dup := judged(4, "b", "#7 Poort 4b: x — y")
	dup.Redundancy, dup.RedundancyIssue, dup.RedundancyEvidence = "duplicate", 3, "zelfde <!-- titel </sub>"
	done := judged(4, "b", "#7 Poort 4b: x — y")
	done.Redundancy, done.RedundancyEvidence = "done", "PR #9"
	inter := judged(4, "b", "#7 Poort 4b: x — y")
	inter.Headless, inter.HeadlessReason = false, "kiest een smaak ~~~"
	untrusted := outcomeFacts()
	untrusted.AuthorTrusted = false
	inv := []Deviation{
		{Kind: "missing", Prefix: "area:"},
		{Kind: "many", Prefix: "loc:", Found: []string{"loc:a", "loc:b"}},
		{Kind: "blocking", Found: []string{"3-needs-hands", "3-blocked-issue"}},
	}
	night := outcomeTarget
	now := outcomeTarget
	now.WaitForNight = false
	return map[string]Outcome{
		"invariant":   Choose(outcomeFacts(), inv, nil, nil, outcomeLabels, outcomeTarget),
		"duplicate":   Choose(outcomeFacts(), nil, nil, dup, outcomeLabels, outcomeTarget),
		"done":        Choose(outcomeFacts(), nil, nil, done, outcomeLabels, outcomeTarget),
		"stop0":       Choose(outcomeFacts(), nil, g0, judged(0, "", "#7 Poort 0: stop — #114, 3-blocked-issue"), outcomeLabels, outcomeTarget),
		"stop1":       Choose(outcomeFacts(), nil, nil, judged(1, "", "#7 Poort 1: stop — x"), outcomeLabels, outcomeTarget),
		"design":      Choose(outcomeFacts("3-needs-decision"), nil, nil, design, outcomeLabels, outcomeTarget),
		"interactive": Choose(outcomeFacts(), nil, nil, inter, outcomeLabels, outcomeTarget),
		"untrusted":   Choose(untrusted, nil, nil, judged(4, "b", "#7 Poort 4b: x — y"), outcomeLabels, outcomeTarget),
		"forward":     Choose(outcomeFacts(), nil, nil, judged(4, "b", "#7 Poort 4b: x — y"), outcomeLabels, night),
		"forwardnow":  Choose(outcomeFacts(), nil, nil, judged(4, "b", "#7 Poort 4b: x — y"), outcomeLabels, now),
	}
}

// The fix line of a forwarded issue: re-triage sets the status back first, or the next tick
// starts the issue on Controller; without wait_for_night there is no handoff to delete.
func TestForwardFixLine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		language string
		night    bool
		want     string
	}{
		{"night nl", "nl", true, "<sub>Corrigeren: niet naar de dispatcher: zet de status terug naar To do en verwijder de 🤖 Handoff. " +
			"Opnieuw triëren: zet eerst de status terug naar To do, verwijder dan deze comment en de 🤖 Handoff. " +
			"Zelf oppakken: zet eerst de status terug naar To do, start dan /lighthouse-start-issue.</sub>"},
		{"now nl", "nl", false, "<sub>Corrigeren: niet naar de dispatcher: zet de status terug naar To do. " +
			"Opnieuw triëren: zet eerst de status terug naar To do, verwijder dan deze comment. " +
			"Zelf oppakken: zet eerst de status terug naar To do, start dan /lighthouse-start-issue.</sub>"},
		{"night en", "en", true, "<sub>To correct: not to the dispatcher: set the status back to To do and delete the 🤖 Handoff. " +
			"Triage again: first set the status back to To do, then delete this comment and the 🤖 Handoff. " +
			"Pick it up yourself: first set the status back to To do, then start /lighthouse-start-issue.</sub>"},
		{"now en", "en", false, "<sub>To correct: not to the dispatcher: set the status back to To do. " +
			"Triage again: first set the status back to To do, then delete this comment. " +
			"Pick it up yourself: first set the status back to To do, then start /lighthouse-start-issue.</sub>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := Target{Language: tc.language, Worktree: "/r", DefaultBranch: "main", WaitForNight: tc.night}
			o := Choose(outcomeFacts(), nil, nil, judged(4, "b", "#7 Poort 4b: x — y"), outcomeLabels, target)
			lines := strings.Split(o.Comment, "\n")
			if last := lines[len(lines)-1]; last != tc.want {
				t.Errorf("fix line = %q\nwant       %q", last, tc.want)
			}
		})
	}
}

func TestCommentShape(t *testing.T) {
	fixForwarded := protocol.Heading("triage_fix_forwarded", "nl")
	fixForwardedNow := protocol.Heading("triage_fix_forwarded_now", "nl")
	fixOther := protocol.Heading("triage_fix_other", "nl")
	for name, o := range allRows() {
		t.Run(name, func(t *testing.T) {
			lines := strings.Split(o.Comment, "\n")
			if !strings.HasPrefix(lines[1], "**Triage** · #7 ") {
				t.Errorf("line 2 = %q", lines[1])
			}
			for _, l := range lines {
				if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "🤖") {
					t.Errorf("forbidden line %q", l)
				}
			}
			fix := fixOther
			switch {
			case name == "forwardnow":
				fix = fixForwardedNow
			case o.Row == RowForward:
				fix = fixForwarded
			}
			if last := lines[len(lines)-1]; last != "<sub>"+fix+"</sub>" {
				t.Errorf("last line = %q", last)
			}
			if strings.Count(o.Comment, "<sub>") != 1 || strings.Count(o.Comment, "<!--") != 1 {
				t.Errorf("model text leaked <sub> or <!--:\n%s", o.Comment)
			}
		})
	}

	design := allRows()["design"].Comment
	for _, want := range []string{
		"**Ontwerpvragen**",
		"1. Hoe? '''evil",
		"   a) een",
		"   b) twee *(aanbevolen)*",
		"2. Waar?",
		"   a) hier *(aanbevolen)*",
	} {
		if !strings.Contains(design, want) {
			t.Errorf("design comment lacks %q:\n%s", want, design)
		}
	}
	if n := strings.Count(design, "*(aanbevolen)*"); n != 2 {
		t.Errorf("recommended count = %d, want one per question", n)
	}

	stop0 := allRows()["stop0"]
	for _, want := range []string{"#114", "3-blocked-issue", protocol.Heading("triage_stop_expires", "nl"), protocol.Heading("triage_stop_note", "nl")} {
		if !strings.Contains(stop0.Comment, want) {
			t.Errorf("stop0 lacks %q:\n%s", want, stop0.Comment)
		}
	}
	if c := allRows()["stop1"].Comment; strings.Contains(c, protocol.Heading("triage_stop_expires", "nl")) {
		t.Errorf("gate 1 has no stop to expire:\n%s", c)
	}
	inv := allRows()["invariant"].Comment
	for _, want := range []string{"geen label met prefix `area:`", "meer dan één label met prefix `loc:`: loc:a, loc:b", "meer dan één blokkadelabel: 3-needs-hands, 3-blocked-issue", "Er is niets aangepast"} {
		if !strings.Contains(inv, want) {
			t.Errorf("invariant lacks %q:\n%s", want, inv)
		}
	}
	if c := allRows()["duplicate"].Comment; !strings.Contains(c, "Mogelijk dubbel van #3: zelfde &lt;!-- titel &lt;/sub>") {
		t.Errorf("duplicate:\n%s", c)
	}
	if c := allRows()["done"].Comment; !strings.Contains(c, "Mogelijk al af: PR #9") {
		t.Errorf("done:\n%s", c)
	}
	if c := allRows()["interactive"].Comment; !strings.Contains(c, "Dit moet interactief: kiest een smaak '''") {
		t.Errorf("interactive:\n%s", c)
	}
}

func TestCommentNotProtocol(t *testing.T) {
	for name, o := range allRows() {
		t.Run(name, func(t *testing.T) {
			if _, ok := protocol.ParseHeader(o.Comment); ok {
				t.Error("ParseHeader accepts the comment")
			}
			if protocol.IsAnswer(o.Comment) {
				t.Error("IsAnswer accepts the comment")
			}
		})
	}
}

func TestHandoffForward(t *testing.T) {
	want := "🤖 Handoff\nissue: #7\nworktree: /r\nbranch: main\nnext: /lighthouse-start-issue 7\nwhen: night"
	if got := RenderHandoff(7, "/r", "main"); got != want {
		t.Errorf("RenderHandoff = %q, want %q", got, want)
	}
	rows := allRows()
	if rows["forward"].Handoff != want {
		t.Errorf("night handoff = %q", rows["forward"].Handoff)
	}
	if !strings.Contains(rows["forward"].Comment, protocol.Heading("triage_forwarded_night", "nl")) {
		t.Error("night comment lacks triage_forwarded_night")
	}
	if rows["forwardnow"].Handoff != "" {
		t.Errorf("handoff without wait_for_night = %q", rows["forwardnow"].Handoff)
	}
	if !strings.Contains(rows["forwardnow"].Comment, protocol.Heading("triage_forwarded_now", "nl")) {
		t.Error("now comment lacks triage_forwarded_now")
	}
	for name, o := range rows {
		if name != "forward" && o.Handoff != "" {
			t.Errorf("%s carries a handoff", name)
		}
	}
}

// TestHandoffStartsInNight: the handoff, as the only comment of a trusted author on a
// Controller item, defers before the night window and starts inside it.
func TestHandoffStartsInNight(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "dispatch", "testdata", "decide", "handoff-start-issue-from-main.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Input dispatch.Input `json:"input"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	body := allRows()["forward"].Handoff
	h, ok := protocol.ParseHeader(body)
	if !ok {
		t.Fatal("ParseHeader rejects the handoff")
	}
	ho, err := protocol.ParseHandoff(h)
	if err != nil || ho.When != "night" || ho.Next != "/lighthouse-start-issue 7" {
		t.Fatalf("ParseHandoff = %+v, %v", ho, err)
	}
	in := fx.Input
	in.Night = "21:00-08:00"
	in.Repos[0].Snapshot.Items[0].Comments[0].Body = body

	in.Now = time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
	got := dispatch.Decide(in)
	if len(got) != 1 || got[0].Kind != dispatch.KindDefer || got[0].Reason != "night" {
		t.Fatalf("at 20:00 got %+v, want one defer for night", got)
	}
	in.Now = time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)
	got = dispatch.Decide(in)
	if len(got) != 1 || got[0].Kind != dispatch.KindStart || got[0].Command != "/lighthouse-start-issue 7" ||
		!strings.HasPrefix(got[0].Branch, "issue-7-") {
		t.Fatalf("at 22:00 got %+v, want one start of issue 7", got)
	}
}
