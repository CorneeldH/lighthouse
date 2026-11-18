package report

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

const base = "https://github.com/CorneeldH/lighthouse/"

func pr(n int, title string) forge.PRSummary {
	return forge.PRSummary{Number: n, Title: title, URL: base + "pull/" + strconv.Itoa(n)}
}

func issue(n int, title string) forge.Issue {
	return forge.Issue{Number: n, Title: title, URL: base + "issues/" + strconv.Itoa(n)}
}

func sampleReport() Report {
	at := time.Date(2026, 11, 20, 17, 4, 0, 0, time.UTC)
	open := []forge.PRSummary{
		pr(353, "feat(report): Render"),
		pr(354, "docs: uitleg"),
		pr(355, "wip: een"),
		pr(356, "fix: twee"),
		pr(357, "fix: drie"),
		pr(358, "chore: vier"),
	}
	open[0].Mergeable, open[0].Checks = "MERGEABLE", "SUCCESS"
	open[1].Mergeable = "CONFLICTING"
	open[2].Draft = true
	open[3].Checks = "FAILURE"
	open[4].Checks = "PENDING"
	return Report{
		Period: NewPeriod(KindWeek, at),
		Repo:   "CorneeldH/lighthouse",
		At:     at,
		Merged: []forge.PRSummary{
			pr(352, "fix(gitleaks): proza-allowlist"),
			pr(351, "fix(gate): resolve volgt symlinks"),
		},
		Closed:  []forge.Issue{issue(228, "Gitleaks vangt checkpoints")},
		OpenPRs: open,
		Roadmap: []RoadmapGroup{{
			Milestone: forge.Milestone{Number: 1, Title: "Lighthouse zonder Python"},
			Issues:    []forge.Issue{issue(285, "Python eruit")},
		}},
	}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenderGoldenNL(t *testing.T) {
	if got, want := Render(sampleReport(), "nl"), golden(t, "nl.golden"); got != want {
		t.Errorf("nl differs\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderGoldenEN(t *testing.T) {
	if got, want := Render(sampleReport(), "en"), golden(t, "en.golden"); got != want {
		t.Errorf("en differs\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderUnknownLanguageFallsBackToEN(t *testing.T) {
	if Render(sampleReport(), "fr") != golden(t, "en.golden") {
		t.Error("fr should render as en")
	}
}

func TestRenderEmptySectionSaysGeen(t *testing.T) {
	r := Report{Period: NewPeriod(KindDay, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)), Repo: "o/r", At: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}
	got := Render(r, "nl")
	if n := strings.Count(got, "\nGeen.\n"); n != 4 {
		t.Errorf("want 4 x Geen., got %d in:\n%s", n, got)
	}
	for _, h := range []string{"### Gemergde PR's (0)", "### Gesloten issues (0)", "### Open PR's (0)", "### Nieuw op de roadmap (0)"} {
		if !strings.Contains(got, h) {
			t.Errorf("missing heading %q", h)
		}
	}
}

func TestRenderTitleOnOneLine(t *testing.T) {
	r := sampleReport()
	r.Merged = []forge.PRSummary{pr(12, "fix: a\nb] [c |d #12")}
	want := "- [#12](" + base + "pull/12) fix: a b] [c |d #12\n"
	if got := Render(r, "nl"); !strings.Contains(got, want) {
		t.Errorf("want line %q in:\n%s", want, got)
	}
}

func TestRenderOpenPRsStampHeader(t *testing.T) {
	r := sampleReport()
	r.Period = NewPeriod(KindDay, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	want := "3 november 2026. Open PR's: stand op 2026-11-20 17:04.\n"
	if got := Render(r, "nl"); !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
}

func TestRenderWeekRangeInHeader(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		at   time.Time
		want string
	}{
		{"week in one month", KindWeek, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), "16–22 november 2026. "},
		{"week over month", KindWeek, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), "30 november – 6 december 2026. "},
		{"week over year", KindWeek, time.Date(2024, 12, 30, 0, 0, 0, 0, time.UTC), "30 december 2024 – 5 januari 2025. "},
		{"month", KindMonth, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), "november 2026. "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := sampleReport()
			r.Period = NewPeriod(c.kind, c.at)
			if got := Render(r, "nl"); !strings.Contains(got, "\n\n"+c.want) {
				t.Errorf("want %q in:\n%s", c.want, got)
			}
		})
	}
}
