package archscan

import (
	"path/filepath"
	"strings"
	"testing"

	lhconfig "github.com/CorneeldH/lighthouse/internal/config"
)

// testHeadings loads the headings table the way the job does for a target repo, from a file.
func testHeadings(t *testing.T) headings {
	t.Helper()
	h, err := lhconfig.LoadHeadingsFile(filepath.Join("..", "..", "..", "pack", "core", "headings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

const tail = `
## Nog open uit eerdere scans

- #12 — Oude kandidaat

## Methode

Venster 3 maanden, basiscommit abc123.

## Topaanbeveling

Begin met de eerste kandidaat.
` // lighthouse: allow-language

const validResult = `# Scan van 2026-11-25

## Kandidaten

### Splits de store (` + "`split-store`" + `)

- [ ] Afgewezen
Bestanden: internal/store
- Telt 12 bestanden [niet nagemeten]
- Importeert ` + "`rg`" + ` [nagemeten: ` + "`rg -c store internal`" + ` → 12 treffers]

### Verwijder de cache (` + "`drop-cache`" + `)

- [x] Afgewezen
- Cache wordt nergens gelezen [niet meetbaar vanaf hier]
` + tail // lighthouse: allow-language

func resultWith(claims string) string {
	return "## Kandidaten\n\n### X (`x`)\n\n- [ ] Afgewezen\n" + claims + "\n" + tail // lighthouse: allow-language
}

func TestParseResultValid(t *testing.T) {
	r, err := parseResult([]byte(validResult), testHeadings(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(r.Candidates))
	}
	a, b := r.Candidates[0], r.Candidates[1]
	if a.Key != "split-store" || a.Name != "Splits de store" || a.Rejected { // lighthouse: allow-language
		t.Errorf("first candidate = %+v", a)
	}
	if b.Key != "drop-cache" || !b.Rejected {
		t.Errorf("second candidate = %+v", b)
	}
	if len(a.Claims) != 2 || a.Claims[0].Mark != "niet-nagemeten" { // lighthouse: allow-language
		t.Fatalf("claims = %+v", a.Claims)
	}
	c := a.Claims[1]
	if c.Mark != "nagemeten" || c.Command != "rg -c store internal" || c.Outcome != "12 treffers" { // lighthouse: allow-language
		t.Errorf("measured claim = %+v", c)
	}
	if b.Claims[0].Mark != "niet-meetbaar" { // lighthouse: allow-language
		t.Errorf("mark = %q", b.Claims[0].Mark)
	}
	if !strings.HasPrefix(a.Raw, "### Splits") || strings.Contains(a.Raw, "Verwijder") { // lighthouse: allow-language
		t.Errorf("raw = %q", a.Raw)
	}
	if len(r.StillOpen) != 1 || r.StillOpen[0] != 12 {
		t.Errorf("still open = %v", r.StillOpen)
	}
	if r.Body != validResult {
		t.Error("body differs from input")
	}
}

func TestParseResultZeroCandidates(t *testing.T) {
	in := "## Kandidaten\n\nGeen kandidaten gevonden in dit venster.\n" + tail // lighthouse: allow-language
	r, err := parseResult([]byte(in), testHeadings(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 0 {
		t.Errorf("candidates = %v", r.Candidates)
	}
}

func TestParseResultEmptyFile(t *testing.T) {
	for _, in := range []string{"", "  \n\n"} {
		if _, err := parseResult([]byte(in), testHeadings(t)); err == nil {
			t.Errorf("%q: want error", in)
		}
	}
}

func TestParseResultMissingHeading(t *testing.T) {
	for _, h := range []string{"## Kandidaten", "## Nog open uit eerdere scans", "## Methode", "## Topaanbeveling"} { // lighthouse: allow-language
		in := strings.Replace(validResult, h, "## Iets anders", 1) // lighthouse: allow-language
		_, err := parseResult([]byte(in), testHeadings(t))
		if err == nil {
			t.Errorf("without %q: want error", h)
		}
	}
}

func TestParseResultClaimTwoMarks(t *testing.T) {
	in := resultWith("- Iets [niet nagemeten] [niet meetbaar vanaf hier]") // lighthouse: allow-language
	if _, err := parseResult([]byte(in), testHeadings(t)); err == nil {
		t.Error("want error")
	}
}

func TestParseResultClaimNoMark(t *testing.T) {
	for _, c := range []string{"- Iets zonder markering", "- Iets [onbekend]"} { // lighthouse: allow-language
		if _, err := parseResult([]byte(resultWith(c)), testHeadings(t)); err == nil {
			t.Errorf("%q: want error", c)
		}
	}
}

func TestParseResultNagemetenWithoutOutcome(t *testing.T) {
	for _, c := range []string{
		"- Iets [nagemeten: `rg x`]",
		"- Iets [nagemeten: `rg x` → ]",
		"- Iets [nagemeten: `` → 3]",
		"- Iets [nagemeten]",
	} {
		if _, err := parseResult([]byte(resultWith(c)), testHeadings(t)); err == nil {
			t.Errorf("%q: want error", c)
		}
	}
}

func TestParseResultClaimWithBracketsInCommand(t *testing.T) {
	in := resultWith("- Veel vinkjes [nagemeten: `rg \"\\[x\\]\" -c` → 4 [x] treffers]") // lighthouse: allow-language
	r, err := parseResult([]byte(in), testHeadings(t))
	if err != nil {
		t.Fatal(err)
	}
	c := r.Candidates[0].Claims[0]
	if c.Command != `rg "\[x\]" -c` || c.Outcome != "4 [x] treffers" { // lighthouse: allow-language
		t.Errorf("claim = %+v", c)
	}
}

func TestParseResultAcceptsEnglishHeadings(t *testing.T) {
	in := `# Scan of 2026-11-25

## Candidates

### Split the store (` + "`split-store`" + `)

- [ ] Afgewezen
- Twelve files [niet nagemeten]

## Still open from earlier scans

- #3 — Old

## Method

Window.

## Top recommendation

Start here.
` // lighthouse: allow-language
	r, err := parseResult([]byte(in), testHeadings(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 1 || len(r.StillOpen) != 1 {
		t.Errorf("result = %+v", r)
	}
}

func TestParseResultDuplicateKey(t *testing.T) {
	in := strings.Replace(validResult, "`drop-cache`", "`split-store`", 1)
	if _, err := parseResult([]byte(in), testHeadings(t)); err == nil {
		t.Error("want error")
	}
}

func TestRejectedKeys(t *testing.T) {
	got := RejectedKeys(validResult)
	if len(got) != 1 || !got["drop-cache"] {
		t.Errorf("got %v", got)
	}
}

func TestWithCheckboxesKeepsLiveTick(t *testing.T) {
	r, err := parseResult([]byte(validResult), testHeadings(t))
	if err != nil {
		t.Fatal(err)
	}
	out := r.WithCheckboxes(map[string]bool{"split-store": true}) // lighthouse: allow-language
	got := RejectedKeys(out)
	if len(got) != 2 || !got["split-store"] || !got["drop-cache"] {
		t.Errorf("rejected = %v", got)
	}
	if strings.Count(out, "- [ ]") != 0 {
		t.Errorf("unticked box left:\n%s", out)
	}
	if out2 := r.WithCheckboxes(nil); out2 != validResult {
		t.Error("nil live must keep the body as it is")
	}
}
