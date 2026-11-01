package triage

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Row is the row of the spec ("Toepassen") that decides what a round writes for an issue.
type Row int

// The rows 1 to 7 of the spec, in order; the first that fits wins.
const (
	RowInvariant   Row = iota + 1 // the labels break the invariant
	RowRedundant                  // duplicate or done
	RowStop                       // gate 0 or 1
	RowDesign                     // gate 4c
	RowInteractive                // headless no
	RowUntrusted                  // the author is not in answer_permissions
	RowForward                    // 2, 4a or 4b
)

// Outcome is what a round writes for one issue.
type Outcome struct {
	Row     Row
	Marker  protocol.TriageMarker
	Comment string // starts with the marker line
	Handoff string // "" unless Row is RowForward and wait_for_night
}

// Target is where a forwarded issue is started from.
type Target struct {
	Language, Worktree, DefaultBranch string // the main checkout and its default branch, for the handoff
	WaitForNight                      bool
}

// Choose picks the first row that fits; j is nil only when devs is not empty.
func Choose(f Facts, devs []Deviation, g0 *Gate0, j *Judgment, l repoconfig.Labels, t Target) Outcome {
	language := t.Language
	h := func(key string) string { return protocol.Heading(key, language) }
	var o Outcome
	var body []string // paragraphs between the head line and the fix line
	var route string

	row := RowInvariant
	if len(devs) == 0 {
		row = judgedRow(j, g0, f.AuthorTrusted)
	}
	switch row {
	case RowInvariant:
		o.Row = RowInvariant
		o.Marker = protocol.TriageMarker{Gate: "invariant", Stop: true}
		route = fmt.Sprintf("#%d %s", f.Number, h("triage_invariant"))
		body = append(body, invariantLines(devs, h), h("triage_invariant_note"))
	case RowRedundant:
		o.Row = RowRedundant
		o.Marker = protocol.TriageMarker{Gate: gateOf(j, g0)}
		route = routeLine(j, g0)
		text := h("triage_done")
		if j.Redundancy == "duplicate" {
			text = h("triage_duplicate") + " #" + strconv.Itoa(j.RedundancyIssue)
		}
		body = append(body, text+": "+modelText(j.RedundancyEvidence), h("triage_redundant_note"))
	case RowStop:
		o.Row = RowStop
		o.Marker = protocol.TriageMarker{Gate: gateOf(j, g0)}
		if g0 != nil {
			o.Marker = protocol.TriageMarker{Gate: "0", Blockers: g0.Blockers, Label: g0.Label, Stop: true}
		}
		route = routeLine(j, g0)
		body = append(body, h("triage_stop_note"))
	case RowDesign:
		o.Row = RowDesign
		o.Marker = protocol.TriageMarker{Gate: "4c"}
		for _, name := range []string{l.NeedsShaping, l.NeedsDecision} {
			if name != "" && slices.Contains(f.Labels, name) {
				o.Marker.Label, o.Marker.Stop = name, true
				break
			}
		}
		route = routeLine(j, g0)
		body = append(body, h("triage_stop_note"), questionLines(j.Questions, h("triage_design_questions"), h("question_recommended")))
	case RowInteractive:
		o.Row = RowInteractive
		o.Marker = protocol.TriageMarker{Gate: gateOf(j, g0)}
		route = routeLine(j, g0)
		body = append(body, h("triage_interactive")+" "+modelText(j.HeadlessReason))
	case RowUntrusted:
		o.Row = RowUntrusted
		o.Marker = protocol.TriageMarker{Gate: gateOf(j, g0)}
		route = routeLine(j, g0)
		body = append(body, h("triage_untrusted"))
	default: // RowForward
		o.Row = RowForward
		o.Marker = protocol.TriageMarker{Gate: gateOf(j, g0)}
		route = routeLine(j, g0)
		if t.WaitForNight {
			o.Handoff = RenderHandoff(f.Number, t.Worktree, t.DefaultBranch)
			body = append(body, h("triage_forwarded_night"))
		} else {
			body = append(body, h("triage_forwarded_now"))
		}
	}
	if o.Marker.Stop && o.Row != RowInvariant {
		body = append(body, h("triage_stop_expires"))
	}

	fix := h("triage_fix_other")
	switch {
	case o.Row == RowForward && t.WaitForNight:
		fix = h("triage_fix_forwarded")
	case o.Row == RowForward:
		fix = h("triage_fix_forwarded_now") // no handoff to delete
	}
	parts := []string{protocol.RenderTriageMarker(o.Marker), "**" + h("triage_title") + "** · " + route}
	o.Comment = strings.Join(parts, "\n") + "\n\n" + strings.Join(body, "\n\n") + "\n\n<sub>" + fix + "</sub>"
	return o
}

// judgedRow is the row of a valid judgment, never RowInvariant: the first that fits wins.
func judgedRow(j *Judgment, g0 *Gate0, authorTrusted bool) Row {
	switch {
	case j.Redundancy == "duplicate" || j.Redundancy == "done":
		return RowRedundant
	case g0 != nil || j.Gate == 0 || j.Gate == 1:
		return RowStop
	case j.Gate == 4 && j.Sub == "c":
		return RowDesign
	case !j.Headless:
		return RowInteractive
	case !authorTrusted:
		return RowUntrusted
	}
	return RowForward
}

// RenderHandoff writes the handoff comment that /lighthouse-start-issue starts from in the night.
func RenderHandoff(issue int, worktree, branch string) string {
	return strings.Join([]string{
		protocol.Robot + " " + protocol.Heading("kind_handoff", "en"),
		"issue: #" + strconv.Itoa(issue),
		"worktree: " + worktree,
		"branch: " + branch,
		"next: /lighthouse-start-issue " + strconv.Itoa(issue),
		"when: night",
	}, "\n")
}

// gateOf is the marker gate: "0" for a fixed stop, else the gate of the judgment.
func gateOf(j *Judgment, g0 *Gate0) string {
	switch {
	case g0 != nil:
		return "0"
	case j.Gate == 4:
		return "4" + j.Sub
	}
	return strconv.Itoa(j.Gate)
}

func invariantLines(devs []Deviation, h func(string) string) string {
	var lines []string
	for _, d := range devs {
		switch d.Kind {
		case "missing":
			lines = append(lines, fmt.Sprintf("- %s `%s`", h("triage_invariant_missing"), oneLine(d.Prefix)))
		case "many":
			lines = append(lines, fmt.Sprintf("- %s `%s`: %s", h("triage_invariant_many"), oneLine(d.Prefix), oneLine(strings.Join(d.Found, ", "))))
		default:
			lines = append(lines, fmt.Sprintf("- %s: %s", h("triage_invariant_blocking"), oneLine(strings.Join(d.Found, ", "))))
		}
	}
	return strings.Join(lines, "\n")
}

func questionLines(qs []Question, title, recommended string) string {
	lines := []string{"**" + title + "**", ""}
	for i, q := range qs {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, modelText(q.Text)))
		for k, opt := range q.Options {
			line := fmt.Sprintf("   %c) %s", 'a'+k, modelText(opt))
			if k == q.Recommended {
				line += " " + recommended
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

var escapeModelText = strings.NewReplacer(
	"```", "'''", "~~~", "'''",
	"<!--", "&lt;!--", "<sub", "&lt;sub", "</sub", "&lt;/sub",
	"\r\n", " ", "\n", " ", "\r", " ",
)

// oneLine puts free model text on one line, without a code fence, a <sub> or a comment opener,
// as _regel does in paia.
func oneLine(s string) string {
	return strings.TrimSpace(escapeModelText.Replace(s))
}

// modelRefs matches the start of what GitHub turns into a mention or an issue reference:
// @name, #N and owner/repo#N.
var modelRefs = regexp.MustCompile(`@[0-9A-Za-z]|#[0-9]`)

// modelText is oneLine with a zero-width joiner after the @ or # of every mention or issue
// reference, so free model text notifies no one and makes no cross-reference.
func modelText(s string) string {
	return modelRefs.ReplaceAllStringFunc(oneLine(s), func(m string) string { return m[:1] + "\u200d" + m[1:] })
}

// routeLine keeps the parts Go wrote: the whole gate-0 line, or the checked "#<nr> <gate> <n>:"
// prefix of the model's line, whose #<nr> is the issue itself.
func routeLine(j *Judgment, g0 *Gate0) string {
	if g0 != nil {
		return oneLine(j.RouteLine)
	}
	head, tail, ok := strings.Cut(j.RouteLine, ": ")
	if !ok {
		return modelText(j.RouteLine)
	}
	return oneLine(head) + ": " + modelText(tail)
}
