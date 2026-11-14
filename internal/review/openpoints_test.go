package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir holds the cases shared with tests/pack/fixtures/plan_review_record/.
const fixtureDir = "../../tests/pack/fixtures/plan_review_record"

// useHeadings points the config at the nl headings table and a lighthouse.toml of language.
func useHeadings(t *testing.T, language string) {
	t.Helper()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	writeFile(t, toml, "[project]\nlanguage = \""+language+"\"\n")
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("LIGHTHOUSE_HEADINGS", "../../pack/core/headings.toml")
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSectionOnlyMatchesTheLastSectionOutsideFences(t *testing.T) {
	useHeadings(t, "nl")
	var fx struct {
		Cases []struct {
			Text string `json:"text"`
			Span []int  `json:"span"`
		} `json:"cases"`
	}
	readFixture(t, "49-open-section-only-matches-the-last-section-outside-fences", &fx)
	for _, c := range fx.Cases {
		start, end, ok, err := OpenSection(c.Text)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case c.Span == nil && ok:
			t.Errorf("OpenSection(%q) = (%d, %d), want no block", c.Text, start, end)
		case c.Span != nil && (!ok || start != c.Span[0] || end != c.Span[1]):
			t.Errorf("OpenSection(%q) = (%d, %d, %v), want (%d, %d)", c.Text, start, end, ok, c.Span[0], c.Span[1])
		}
	}
}

func TestOpenAfterReviewHeadingFollowsLanguage(t *testing.T) {
	for _, language := range []string{"nl", "en"} {
		t.Run(language, func(t *testing.T) {
			useHeadings(t, language)
			var fx struct {
				Round int `json:"round"`
				Point struct {
					FindingID string `json:"finding_id"`
					Severity  string `json:"severity"`
					Section   string `json:"section"`
					Problem   string `json:"problem"`
				} `json:"point"`
				StartsWith string   `json:"starts_with"`
				Contains   []string `json:"contains"`
			}
			readFixture(t, "72-open-after-review-heading-follows-language-"+language, &fx)
			p := Point{fx.Point.FindingID, fx.Point.Severity, fx.Point.Section, fx.Point.Problem}
			block, err := RenderOpen(fx.Round, []Point{p}, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(block, fx.StartsWith) {
				t.Errorf("block = %q, want prefix %q", block, fx.StartsWith)
			}
			for _, want := range fx.Contains {
				if !strings.Contains(block, want) {
					t.Errorf("block = %q, want it to contain %q", block, want)
				}
			}
			// OpenSection finds the block again in every language.
			if _, _, ok, err := OpenSection("# Plan\n\n" + block); err != nil || !ok {
				t.Errorf("OpenSection does not find the block again: ok=%v err=%v", ok, err)
			}
		})
	}
}

// attemptWithRound3 is an attempt dir with one round-3 of MAJOR, BLOCKER and MINOR, the
// MAJOR decided accept, the BLOCKER reject and the MINOR minor_no.
func attemptWithRound3(t *testing.T) (attempt, sidecar string) {
	t.Helper()
	attempt = t.TempDir()
	writeFile(t, filepath.Join(attempt, "attempt.json"), `{"kind":"plan","chain_key":"c","attempt":"a","plan":"p.md","reviewed_sha":"x"}`)
	writeFile(t, filepath.Join(attempt, "round-3.json"), `{"findings":[
		{"severity":"MAJOR","plan_section":"Taak 1","problem":"p"},
		{"severity":"BLOCKER","plan_section":"Taak 2","problem":"q"},
		{"severity":"MINOR","problem":"r"}]}`)
	sidecar = filepath.Join(attempt, "round-3.decisions.json")
	writeFile(t, sidecar, `[{"finding_id":"r3-f1","decision":"accept","reason":"fix"},
		{"finding_id":"r3-f2","decision":"reject","reason":"no"},
		{"finding_id":"r3-f3","decision":"minor_no","reason":"x"}]`)
	return attempt, sidecar
}

func TestAcceptOpenRewritesSidecarAndDocument(t *testing.T) {
	useHeadings(t, "nl")
	attempt, sidecar := attemptWithRound3(t)
	doc := filepath.Join(t.TempDir(), "plan.md")
	writeFile(t, doc, "# Plan\n")
	block, err := AcceptOpen(attempt, doc)
	if err != nil {
		t.Fatal(err)
	}
	wantBlock := "## Open na review\n\n" +
		"Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;\n" +
		"de volgende fase of de PR-review weegt ze opnieuw.\n\n" +
		"- **r3-f1** (MAJOR, Taak 1): p\n"
	if block != wantBlock {
		t.Errorf("block = %q, want %q", block, wantBlock)
	}
	gotDoc, _ := os.ReadFile(doc)
	if string(gotDoc) != "# Plan\n\n"+wantBlock {
		t.Errorf("document = %q", gotDoc)
	}
	var rows []map[string]any
	data, _ := os.ReadFile(sidecar)
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["decision"] != "accepted_open" || rows[0]["reason"] != "round_limit; was accept: fix" ||
		rows[1]["decision"] != "reject" || rows[2]["decision"] != "minor_no" {
		t.Errorf("sidecar = %v", rows)
	}
}

func TestAcceptOpenTwiceChangesNothing(t *testing.T) {
	useHeadings(t, "nl")
	attempt, sidecar := attemptWithRound3(t)
	doc := filepath.Join(t.TempDir(), "plan.md")
	writeFile(t, doc, "# Plan\n")
	first, err := AcceptOpen(attempt, doc)
	if err != nil {
		t.Fatal(err)
	}
	sidecarBytes, _ := os.ReadFile(sidecar)
	docBytes, _ := os.ReadFile(doc)
	second, err := AcceptOpen(attempt, doc)
	if err != nil {
		t.Fatalf("second AcceptOpen: %v: an accepted_open point must stay an open point", err)
	}
	sidecarAgain, _ := os.ReadFile(sidecar)
	docAgain, _ := os.ReadFile(doc)
	if second != first || string(sidecarAgain) != string(sidecarBytes) || string(docAgain) != string(docBytes) {
		t.Errorf("second run changed something:\nblock %q vs %q\ndoc %q vs %q", second, first, docAgain, docBytes)
	}
}

func TestAcceptOpenWithNothingOpenWritesNothing(t *testing.T) {
	useHeadings(t, "nl")
	attempt := t.TempDir()
	writeFile(t, filepath.Join(attempt, "attempt.json"), `{"kind":"spec","chain_key":"c","attempt":"a","spec":"s.md","reviewed_sha":"x"}`)
	writeFile(t, filepath.Join(attempt, "round-1.json"), `{"findings":[{"severity":"MAJOR","spec_section":"§1","problem":"p"}]}`)
	sidecar := filepath.Join(attempt, "round-1.decisions.json")
	writeFile(t, sidecar, `[{"finding_id":"r1-f1","decision":"defer","reason":"later"}]`)
	doc := filepath.Join(t.TempDir(), "spec.md")
	writeFile(t, doc, "# Spec\n")
	_, err := AcceptOpen(attempt, doc)
	if !errors.Is(err, ErrNoOpenPoints) {
		t.Fatalf("AcceptOpen error = %v, want ErrNoOpenPoints (defer closes a spec finding)", err)
	}
	if got, _ := os.ReadFile(doc); string(got) != "# Spec\n" {
		t.Errorf("document = %q, want it untouched", got)
	}
	if got, _ := os.ReadFile(sidecar); !strings.Contains(string(got), `"defer"`) {
		t.Errorf("sidecar = %q, want it untouched", got)
	}
}
