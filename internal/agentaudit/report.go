package agentaudit

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// report writes the text of spec §7: sections in a fixed order, separated by a blank
// line, with tables aligned in columns.
type report struct {
	out     io.Writer
	started bool
}

// line writes one line; write errors are ignored, like fmt.Print to a terminal.
func (w *report) line(format string, args ...any) {
	_, _ = fmt.Fprintf(w.out, format+"\n", args...)
}

// section starts a block: a blank line before every block but the first.
func (w *report) section(heading string) {
	if w.started {
		w.line("")
	}
	w.started = true
	if heading != "" {
		w.line("%s", heading)
	}
}

// table writes rows of cells aligned in columns, each row behind indent.
func (w *report) table(indent string, rows [][]string) {
	tw := tabwriter.NewWriter(w.out, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, indent+strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

func stateText(s Status) string {
	if s.Reason == "" {
		return s.State
	}
	return s.State + " — " + s.Reason
}

// sources is the status line per source, then one per hook that could not be read.
func (w *report) sources(statuses []Status) {
	w.section("Bronnen")
	rows := make([][]string, 0, len(statuses))
	for _, s := range statuses {
		label := s.Source
		if strings.Contains(s.Source, "/") {
			label = "hook " + s.Source
		}
		rows = append(rows, []string{label, stateText(s)})
	}
	w.table("  ", rows)
}

// findings is a section of findings, each its key in brackets and its text lines below.
func (w *report) findings(heading string, fs []Finding) {
	w.section(fmt.Sprintf("%s (%d)", heading, len(fs)))
	if len(fs) == 0 {
		w.line("  geen")
	}
	for _, f := range fs {
		w.line("  [%s]", f.Key)
		for _, t := range f.Text {
			w.line("    %s", t)
		}
	}
}

// stale is one line per ignore key that no longer occurs.
func (w *report) stale(keys []string) {
	if len(keys) == 0 {
		return
	}
	w.section("")
	for _, k := range keys {
		w.line("ignore: %s komt niet meer voor", k)
	}
}

func (w *report) surface(s Surface, maxDesc int) {
	w.section("Oppervlak (skills en agents, altijd geladen)")
	if len(s.PerSource) == 0 {
		w.line("  geen skills, commands of agents")
		return
	}
	rows := [][]string{{"bron", "aantal", "tekens", "~tokens"}}
	for _, ss := range s.PerSource {
		rows = append(rows, []string{ss.Source, fmt.Sprint(ss.Count), fmt.Sprint(ss.Chars), fmt.Sprint(ss.Tokens)})
	}
	w.table("  ", rows)
	w.line("  Grootste posten")
	w.table("  ", postRows(s.Top, size))
	if len(s.TooLong) > 0 {
		w.line("  Description langer dan %d tekens", maxDesc)
		w.table("  ", postRows(s.TooLong, func(sk Skill) int { return sk.DescChars }))
	}
}

func postRows(skills []Skill, chars func(Skill) int) [][]string {
	rows := make([][]string, 0, len(skills))
	for _, sk := range skills {
		rows = append(rows, []string{sk.Source, sk.Name, fmt.Sprintf("%d tekens", chars(sk))})
	}
	return rows
}

// inventory lists everything Collect found, each item with its source.
func (w *report) inventory(inv *Inventory) {
	w.section("Inventaris")

	w.line("  hooks (%d)", len(inv.Hooks))
	rows := make([][]string, 0, len(inv.Hooks))
	for _, h := range inv.Hooks {
		matcher := h.Matcher
		if matcher == "" {
			matcher = "*"
		}
		script := "inline"
		if h.Script != "" {
			script = "script " + h.Shown
		}
		rows = append(rows, []string{h.Source, h.Event, matcher, h.Command, script, blockingText(h)})
	}
	w.table("    ", rows)

	w.line("  skills, commands en agents (%d)", len(inv.Skills))
	rows = rows[:0]
	for _, s := range inv.Skills {
		kind := s.Kind
		if s.Harness != "claude" {
			kind += " (" + s.Harness + ")"
		}
		rows = append(rows, []string{s.Source, kind, s.Name, s.Path})
	}
	w.table("    ", rows)

	w.line("  rules (%d)", len(inv.Rules))
	rows = rows[:0]
	for _, r := range inv.Rules {
		scope := "altijd"
		if r.HasPaths {
			scope = "paths"
		}
		rows = append(rows, []string{r.Source, r.Path, scope})
	}
	w.table("    ", rows)

	w.line("  permissies (%d)", len(inv.Permissions))
	rows = rows[:0]
	for _, p := range inv.Permissions {
		rows = append(rows, []string{p.Source, p.Kind, p.Rule})
	}
	w.table("    ", rows)

	w.line("  mcp-servers (%d)", len(inv.MCP))
	rows = rows[:0]
	for _, m := range inv.MCP {
		rows = append(rows, []string{m.Source, m.Name, m.Origin})
	}
	w.table("    ", rows)

	w.line("  hookscripts (%d)", len(inv.Scripts))
	for _, s := range inv.Scripts {
		w.line("    %s", s)
	}
}

func blockingText(h Hook) string {
	switch {
	case h.Unreadable != "":
		return StateCouldNotCh
	case h.Blocking:
		return "blokkerend"
	default:
		return "niet blokkerend"
	}
}
