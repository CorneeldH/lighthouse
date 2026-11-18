package report

import (
	"fmt"
	"strings"
	"time"
)

// text is the fixed wording of a report in one language.
type text struct {
	title      string // Sprintf: kind word, label, repo
	kinds      map[Kind]string
	done       string
	inProgress string
	merged     string
	closed     string
	openPRs    string
	roadmap    string
	none       string
	stamp      string // Sprintf: range, snapshot time
	status     map[Status]string
	months     [12]string
}

// reportText is the wording per [project] language; an unknown language uses "en".
var reportText = map[string]text{
	"nl": {
		title:      "# Verslag %s %s · %s",
		kinds:      map[Kind]string{KindDay: "dag", KindWeek: "week", KindMonth: "maand"},
		done:       "## Gedaan",
		inProgress: "## Gaande",
		merged:     "### Gemergde PR's (%d)",
		closed:     "### Gesloten issues (%d)",
		openPRs:    "### Open PR's (%d)",
		roadmap:    "### Nieuw op de roadmap (%d)",
		none:       "Geen.",
		stamp:      "%s. Open PR's: stand op %s.",
		status: map[Status]string{
			StatusDraft: "concept", StatusConflict: "conflict", StatusRed: "rood",
			StatusBusy: "bezig", StatusGreen: "groen", StatusNoChecks: "geen checks",
		},
		months: [12]string{"januari", "februari", "maart", "april", "mei", "juni", "juli", "augustus", "september", "oktober", "november", "december"},
	},
	"en": {
		title:      "# Report %s %s · %s",
		kinds:      map[Kind]string{KindDay: "day", KindWeek: "week", KindMonth: "month"},
		done:       "## Done",
		inProgress: "## In progress",
		merged:     "### Merged PRs (%d)",
		closed:     "### Closed issues (%d)",
		openPRs:    "### Open PRs (%d)",
		roadmap:    "### New on the roadmap (%d)",
		none:       "None.",
		stamp:      "%s. Open PRs: state at %s.",
		status: map[Status]string{
			StatusDraft: "draft", StatusConflict: "conflict", StatusRed: "red",
			StatusBusy: "busy", StatusGreen: "green", StatusNoChecks: "no checks",
		},
		months: [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	},
}

// Render writes the report as markdown in language, "en" for an unknown one.
// Every section stays, an empty one says "none".
func Render(r Report, language string) string {
	t, ok := reportText[language]
	if !ok {
		t = reportText["en"]
	}
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	section := func(heading string, n int, items func()) {
		line(fmt.Sprintf(heading, n))
		line("")
		if n == 0 {
			line(t.none)
		} else {
			items()
		}
		line("")
	}

	line(fmt.Sprintf(t.title, t.kinds[r.Period.Kind], r.Period.Label, r.Repo))
	line("")
	line(fmt.Sprintf(t.stamp, t.dateRange(r.Period), r.At.Format("2006-01-02 15:04")))
	line("")
	line(t.done)
	line("")
	section(t.merged, len(r.Merged), func() {
		for _, p := range r.Merged {
			line(item(p.Number, p.URL, p.Title, ""))
		}
	})
	section(t.closed, len(r.Closed), func() {
		for _, i := range r.Closed {
			line(item(i.Number, i.URL, i.Title, ""))
		}
	})
	line(t.inProgress)
	line("")
	section(t.openPRs, len(r.OpenPRs), func() {
		for _, p := range r.OpenPRs {
			line(item(p.Number, p.URL, p.Title, t.status[PRStatus(p)]))
		}
	})
	line(fmt.Sprintf(t.roadmap, len(r.Roadmap)))
	line("")
	if len(r.Roadmap) == 0 {
		line(t.none)
	}
	for i, g := range r.Roadmap {
		if i > 0 {
			line("")
		}
		line("#### " + oneLine(g.Milestone.Title))
		line("")
		for _, is := range g.Issues {
			line(item(is.Number, is.URL, is.Title, ""))
		}
	}
	return b.String()
}

// item is one list line; only "#N" is inside the link, the title is one line.
func item(n int, url, title, status string) string {
	s := fmt.Sprintf("- [#%d](%s) %s", n, url, oneLine(title))
	if status != "" {
		s += " — " + status
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// dateRange names the days of the period: "16–22 november 2026".
func (t text) dateRange(p Period) string {
	first := p.Since
	last := p.Until.AddDate(0, 0, -1)
	switch p.Kind {
	case KindMonth:
		return fmt.Sprintf("%s %d", t.months[first.Month()-1], first.Year())
	case KindWeek:
		switch {
		case first.Year() != last.Year():
			return fmt.Sprintf("%s – %s", t.day(first, true), t.day(last, true))
		case first.Month() != last.Month():
			return fmt.Sprintf("%s – %s", t.day(first, false), t.day(last, true))
		}
		return fmt.Sprintf("%d–%s", first.Day(), t.day(last, true))
	}
	return t.day(first, true)
}

// day is "3 november 2026", or "3 november" without the year.
func (t text) day(d time.Time, year bool) string {
	s := fmt.Sprintf("%d %s", d.Day(), t.months[d.Month()-1])
	if year {
		s += fmt.Sprintf(" %d", d.Year())
	}
	return s
}
