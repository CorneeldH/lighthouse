package direction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const del = "\x00delete"

const validTOML = `[project]
language = "nl"

[forge]
kind = "github"
repo = "O/r"

[labels]
area_prefix = "2-area:"

[paths]
labels = "docs/labels.md"
tenets = "docs/tenets.md"
areas = "docs/areas"
strategy = "docs/strategy.md"
`

const validLabels = "| Label | Kleur | Betekenis |\n|---|---|---|\n" +
	"| `2-area:core` | `4C1D95` | core |\n| `2-area:init` | `5B21B6` | init |\n"

const validStrategy = `# Strategie

## Diagnose

x

## Voor wie

x

## Leidend beleid

- x

## Bewust niet

- **S1** een
- **S2** twee

## Roadmap

x
`

const validTenets = `# Uitgangspunten

## A. Een boven twee

*Bewijs*: [a](a.md), [b](a.md)

## B. Drie boven vier

*Bewijs*: [a](a.md), [b](a.md)
`

func validArea(extra string) string {
	return "# Area\n\n## Crux\n\nx\n\n## Leidend beleid\n\n- x\n\n## Bewust niet\n\n" + extra
}

// fixture writes a valid direction repo into a temp dir, applies over (a value of del removes
// the file) and returns the root.
func fixture(t *testing.T, over map[string]string) string {
	t.Helper()
	files := map[string]string{
		"lighthouse.toml":    validTOML,
		"docs/labels.md":     validLabels,
		"docs/strategy.md":   validStrategy,
		"docs/tenets.md":     validTenets,
		"docs/a.md":          "x\n",
		"docs/areas/core.md": validArea("- **N1** niet dit → init\n- **N2** of dat\n"),
		"docs/areas/init.md": validArea("- **N1** niets → core\n"),
	}
	for k, v := range over {
		files[k] = v
	}
	headings, err := os.ReadFile("../../pack/core/headings.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[".lighthouse/headings.toml"]; !ok {
		files[".lighthouse/headings.toml"] = string(headings)
	}
	root := t.TempDir()
	for p, c := range files {
		if c == del {
			continue
		}
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// wantOne asserts that errs has exactly one entry and that it contains every part of want.
func wantOne(t *testing.T, errs []string, want ...string) {
	t.Helper()
	if len(errs) != 1 {
		t.Fatalf("want exactly one error with %q, got %q", want, errs)
	}
	for _, w := range want {
		if !strings.Contains(errs[0], w) {
			t.Errorf("error %q lacks %q", errs[0], w)
		}
	}
}

// Classification: unit; guards the baseline so a case below cannot pass on a broken fixture.
// A change that makes the valid fixture report an error fails it.
func TestCheckValidFixtureIsClean(t *testing.T) {
	if errs := Check(fixture(t, nil)); len(errs) != 0 {
		t.Fatalf("valid fixture reports %q", errs)
	}
}

// Classification: unit; guards that the area labels and the area files stay in step.
// A change that stops comparing one side to the other fails it.
func TestCheckAreaFilePerLabel(t *testing.T) {
	t.Run("label without file", func(t *testing.T) {
		wantOne(t, Check(fixture(t, map[string]string{"docs/areas/init.md": del})), "docs/areas/init.md", "label")
	})
	t.Run("file without label", func(t *testing.T) {
		wantOne(t, Check(fixture(t, map[string]string{"docs/areas/extra.md": validArea("")})), "docs/areas/extra.md:1", "label")
	})
}

// Classification: unit; guards the three headings of an area file, in order.
// A change that accepts a missing, swapped or extra heading fails it.
func TestCheckAreaHeadings(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"missing": {"# A\n\n## Crux\n\nx\n\n## Bewust niet\n\n- **N1** a\n", "Leidend beleid"},
		"swapped": {"# A\n\n## Leidend beleid\n\n## Crux\n\n## Bewust niet\n\n- **N1** a\n", "docs/areas/init.md:3"},
		"extra":   {validArea("- **N1** a\n") + "\n## Extra\n", "docs/areas/init.md:15"},
		"near":    {validArea("- **N1** a\n") + "\n## Nabij doel\n", "Nabij doel"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			wantOne(t, Check(fixture(t, map[string]string{"docs/areas/init.md": c.body})), c.want)
		})
	}
}

// Classification: unit; guards the N-IDs of the deliberate no's and their pointers.
// A change that stops checking an ID or a target area fails it.
func TestCheckNotNowIDs(t *testing.T) {
	cases := map[string]struct{ bullets, want string }{
		"no id":       {"- zonder id\n", "ID"},
		"two ids":     {"- **N1** een **N2** twee\n", "ID"},
		"duplicate":   {"- **N1** a\n- **N1** b\n", "dubbel"},
		"letter form": {"- **N1a** a\n", "ID"},
		"bad target":  {"- **N1** a → nergens\n", "nergens"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			wantOne(t, Check(fixture(t, map[string]string{"docs/areas/init.md": validArea(c.bullets)})), "docs/areas/init.md", c.want)
		})
	}
	t.Run("valid target", func(t *testing.T) {
		if errs := Check(fixture(t, map[string]string{"docs/areas/init.md": validArea("- **N1** a → core\n")})); len(errs) != 0 {
			t.Fatalf("got %q", errs)
		}
	})
}

// Classification: unit; guards the tenet headings: one "boven", unique letters, no gap, evidence.
// A change that drops one of these rules fails it.
func TestCheckTenetHeadings(t *testing.T) {
	ev := "*Bewijs*: [a](a.md), [b](a.md)\n"
	cases := map[string]struct{ doc, want string }{
		"no boven":    {"## A. Een zonder twee\n\n" + ev, "boven"},
		"two boven":   {"## A. Een boven twee boven drie\n\n" + ev, "boven"},
		"duplicate":   {"## A. Een boven twee\n\n" + ev + "\n## A. Nog boven ander\n\n" + ev, "dubbel"},
		"gap":         {"## A. Een boven twee\n\n" + ev + "\n## C. Drie boven vier\n\n" + ev, "B"},
		"one link":    {"## A. Een boven twee\n\n*Bewijs*: [a](a.md)\n", "Bewijs"},
		"no evidence": {"## A. Een boven twee\n\nalleen tekst\n", "Bewijs"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			wantOne(t, Check(fixture(t, map[string]string{"docs/tenets.md": "# U\n\n" + c.doc})), "docs/tenets.md", c.want)
		})
	}
	t.Run("order is free", func(t *testing.T) {
		doc := "# U\n\n## B. Drie boven vier\n\n" + ev + "\n## A. Een boven twee\n\n" + ev
		if errs := Check(fixture(t, map[string]string{"docs/tenets.md": doc})); len(errs) != 0 {
			t.Fatalf("got %q", errs)
		}
	})
	t.Run("lapsed letter counts and needs no evidence", func(t *testing.T) {
		doc := "# U\n\n## A. Een boven twee\n\n" + ev + "\n## B. Oud boven nieuw (vervallen)\n\nweg\n\n## C. Drie boven vier\n\n" + ev
		if errs := Check(fixture(t, map[string]string{"docs/tenets.md": doc})); len(errs) != 0 {
			t.Fatalf("got %q", errs)
		}
	})
}

// Classification: unit; guards that every second-level heading of the tenets file has the tenet form.
// A change that skips a heading of another form fails it.
func TestCheckTenetHeadingForm(t *testing.T) {
	ev := "*Bewijs*: [a](a.md), [b](a.md)\n"
	for name, head := range map[string]string{"lower case": "## a. Een boven twee", "no letter": "## Foo", "two letters": "## AB. Een boven twee"} {
		t.Run(name, func(t *testing.T) {
			doc := "# U\n\n## A. Een boven twee\n\n" + ev + "\n" + head + "\n\n" + ev
			wantOne(t, Check(fixture(t, map[string]string{"docs/tenets.md": doc})), "docs/tenets.md:7", "vorm")
		})
	}
}

// Classification: unit; guards that a lapsed tenet keeps the tenet form (only its evidence is exempt).
// A change that exempts a lapsed tenet from the form fails it.
func TestCheckLapsedTenetKeepsBoven(t *testing.T) {
	ev := "*Bewijs*: [a](a.md), [b](a.md)\n"
	doc := "# U\n\n## A. Een boven twee\n\n" + ev + "\n## B. Oud (vervallen)\n\nweg\n"
	wantOne(t, Check(fixture(t, map[string]string{"docs/tenets.md": doc})), "docs/tenets.md:7", "boven")
}

// Classification: unit; guards that a trailing space on "## Bewust niet" still gets the ID check.
// A change that makes the section lookup exact-match only fails it.
func TestCheckSectionTolerantOfTrailingSpace(t *testing.T) {
	body := strings.Replace(validArea("- zonder id\n"), "## Bewust niet", "## Bewust niet ", 1)
	wantOne(t, Check(fixture(t, map[string]string{"docs/areas/init.md": body})), "docs/areas/init.md", "ID")
}

// Classification: unit; guards the strategy: headings in order and unique S-IDs, one per bullet.
// A change that drops one of these rules fails it.
func TestCheckStrategy(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"order":     {strings.Replace(validStrategy, "## Diagnose", "## Roadmap2", 1), "Diagnose"},
		"duplicate": {strings.Replace(validStrategy, "**S2**", "**S1**", 1), "dubbel"},
		"two ids":   {strings.Replace(validStrategy, "**S2** twee", "**S2** twee **S3**", 1), "ID"},
		"no id":     {strings.Replace(validStrategy, "**S2** twee", "twee", 1), "ID"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			errs := Check(fixture(t, map[string]string{"docs/strategy.md": c.doc}))
			if len(errs) == 0 {
				t.Fatal("no error")
			}
			if !strings.Contains(strings.Join(errs, "\n"), c.want) {
				t.Errorf("errors %q lack %q", errs, c.want)
			}
		})
	}
}

// Classification: unit; guards that an unset key or an unreadable file is an error naming the key.
// A change that skips instead of failing fails it.
func TestCheckUnsetOrUnreadable(t *testing.T) {
	for _, key := range []string{"strategy", "tenets", "areas"} {
		t.Run("unset "+key, func(t *testing.T) {
			toml := strings.Replace(validTOML, key+" = ", "# "+key+" = ", 1)
			wantOne(t, Check(fixture(t, map[string]string{"lighthouse.toml": toml})), "paths."+key)
		})
	}
	t.Run("unreadable strategy", func(t *testing.T) {
		wantOne(t, Check(fixture(t, map[string]string{"docs/strategy.md": del})), "paths.strategy")
	})
}

// Classification: unit; guards that a relative link must point at something.
// A change that stops resolving links fails it.
func TestCheckLinksResolve(t *testing.T) {
	doc := strings.Replace(validStrategy, "- x\n", "- [weg](planning/weg.md)\n", 1)
	wantOne(t, Check(fixture(t, map[string]string{"docs/strategy.md": doc})), "docs/strategy.md:13", "planning/weg.md")
}

// Classification: unit; guards that all errors come back at once, in order of path and line.
// A change that stops at the first error fails it.
func TestCheckReportsAllErrorsAtOnce(t *testing.T) {
	errs := Check(fixture(t, map[string]string{
		"docs/strategy.md":   strings.Replace(validStrategy, "**S2**", "**S1**", 1),
		"docs/areas/init.md": validArea("- geen id\n"),
	}))
	if len(errs) != 2 {
		t.Fatalf("want 2 errors, got %q", errs)
	}
	if !strings.HasPrefix(errs[0], "docs/areas/init.md") || !strings.HasPrefix(errs[1], "docs/strategy.md") {
		t.Errorf("not sorted by path: %q", errs)
	}
}

// Classification: integration; the real documents of this repo must have their shape.
// A change to a document that breaks its shape fails it.
func TestDirectionDocumentsHaveTheirShape(t *testing.T) {
	for _, e := range Check(filepath.Join("..", "..")) {
		t.Errorf("%s", e)
	}
}

const englishTOML = `[project]
language = "en"

[forge]
kind = "github"
repo = "O/r"

[labels]
area_prefix = "2-area:"

[paths]
labels = "docs/labels.md"
tenets = "docs/tenets.md"
areas = "docs/areas"
strategy = "docs/strategy.md"
`

const englishStrategy = "# Strategy\n\n## Diagnosis\n\nx\n\n## For whom\n\nx\n\n## Guiding policy\n\n- x\n\n## Deliberately not\n\n- **S1** one\n\n## Roadmap\n\nx\n"

func englishArea(extra string) string {
	return "# Area\n\n## Crux\n\nx\n\n## Guiding policy\n\n- x\n\n## Deliberately not\n\n" + extra
}

// Classification: unit; guards that the shape check reads headings through the headings table.
// A change that hardcodes the Dutch headings fails it.
func TestCheckAcceptsEnglishHeadings(t *testing.T) {
	root := fixture(t, map[string]string{
		"lighthouse.toml":    englishTOML,
		"docs/strategy.md":   englishStrategy,
		"docs/tenets.md":     "# Tenets\n\n## A. X over Y\n\n*Evidence*: [a](a.md), [b](a.md)\n",
		"docs/areas/core.md": englishArea("- **N1** not this -> init\n"),
		"docs/areas/init.md": englishArea("- **N1** nothing\n"),
	})
	if errs := Check(root); len(errs) != 0 {
		t.Fatalf("english repo reports %q", errs)
	}
}

// Classification: unit; guards that reading accepts every variant and that the plain word
// " over " in a Dutch title does not count as a second separator.
// A change that insists on one language per repo, or counts every variant, fails it.
func TestCheckAcceptsEveryVariantInOneRepo(t *testing.T) {
	root := fixture(t, map[string]string{
		"docs/strategy.md":   englishStrategy,
		"docs/areas/init.md": englishArea("- **N1** nothing\n"),
		"docs/tenets.md": "# Uitgangspunten\n\n## A. Praten over de klant boven aannames\n\n*Bewijs*: [a](a.md), [b](a.md)\n\n" +
			"## B. X over Y\n\n*Evidence*: [a](a.md), [b](a.md)\n",
	})
	if errs := Check(root); len(errs) != 0 {
		t.Fatalf("mixed repo reports %q", errs)
	}
}

// Classification: unit; guards that a missing headings table is a finding, never silent green.
// A change that skips the check when the table is absent fails it.
func TestCheckMissingHeadingsTableIsFinding(t *testing.T) {
	root := fixture(t, map[string]string{".lighthouse/headings.toml": del})
	errs := Check(root)
	if len(errs) == 0 || !strings.Contains(errs[0], ".lighthouse/headings.toml:1: kan niet laden") {
		t.Fatalf("errs = %q", errs)
	}
}

// Classification: unit; guards the opt-out: a repo that sets none of the three direction keys is
// not checked. A change that checks such a repo anyway fails it.
func TestCheckSkipsRepoWithoutDirectionKeys(t *testing.T) {
	toml := strings.NewReplacer("strategy = ", "# strategy = ", "tenets = ", "# tenets = ", "areas = ", "# areas = ").Replace(validTOML)
	if errs := Check(fixture(t, map[string]string{"lighthouse.toml": toml})); errs != nil {
		t.Errorf("want nil, got %q", errs)
	}
}

// Classification: unit; guards that a repo without [forge] repo and without direction keys is
// silent, so the check does not depend on the forge. A change back to repoconfig.Load fails it.
func TestCheckSkipsRepoWithoutForgeRepo(t *testing.T) {
	if errs := Check(fixture(t, map[string]string{"lighthouse.toml": "[project]\nlanguage = \"nl\"\n"})); errs != nil {
		t.Errorf("want nil, got %q", errs)
	}
}

// Classification: unit; guards that one key opts the repo in: the two others are then errors.
// A change that skips on a partial set fails it.
func TestCheckStillFailsOnPartialKeys(t *testing.T) {
	toml := "[project]\nlanguage = \"nl\"\n\n[labels]\narea_prefix = \"2-area:\"\n\n[paths]\nlabels = \"docs/labels.md\"\nareas = \"docs/areas\"\n"
	errs := Check(fixture(t, map[string]string{"lighthouse.toml": toml}))
	if len(errs) != 2 || !strings.Contains(errs[0], "paths.strategy is niet gezet") || !strings.Contains(errs[1], "paths.tenets is niet gezet") {
		t.Errorf("want the errors for strategy and tenets, got %q", errs)
	}
}

// Classification: unit; guards that no taxonomy is no error: an empty areas directory without
// paths.labels passes. A change that demands paths.labels again fails it.
func TestCheckAcceptsEmptyAreasWithoutTaxonomy(t *testing.T) {
	toml := "[project]\nlanguage = \"nl\"\n\n[paths]\nareas = \"docs/areas\"\nstrategy = \"docs/strategy.md\"\ntenets = \"docs/tenets.md\"\n"
	root := fixture(t, map[string]string{"lighthouse.toml": toml, "docs/areas/core.md": del, "docs/areas/init.md": del})
	if err := os.MkdirAll(filepath.Join(root, "docs", "areas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if errs := Check(root); len(errs) != 0 {
		t.Errorf("want no errors, got %q", errs)
	}
}

// Classification: unit; guards that CheckConfig reports an unparsable lighthouse.toml and accepts
// a missing one. A change that swallows the parse error fails it.
func TestCheckConfig(t *testing.T) {
	if err := CheckConfig(fixture(t, map[string]string{"lighthouse.toml": "[paths\n"})); err == nil {
		t.Error("want an error for a broken lighthouse.toml")
	}
	if err := CheckConfig(fixture(t, nil)); err != nil {
		t.Errorf("valid toml: %v", err)
	}
	if err := CheckConfig(fixture(t, map[string]string{"lighthouse.toml": del})); err != nil {
		t.Errorf("missing toml: %v", err)
	}
}
