package initcmd

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/internal/direction"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
	"github.com/CorneeldH/lighthouse/pack"
)

// The default paths: a run with --direction and no keys of its own writes there.
var (
	strategyRel = projectconfig.DefaultStrategyPath
	tenetsRel   = projectconfig.DefaultTenetsPath
	areasRel    = projectconfig.DefaultAreasPath
	taxRel      = projectconfig.DefaultLabelsPath
)

// taxonomy has two area labels with the prefix of the config below, and one other label.
const taxonomy = "# Labels\n\n| Label | Color |\n|---|---|\n| `needs-hands` | `FBCA04` |\n| `2-area:core` | `AAAAAA` |\n| `2-area:init` | `BBBBBB` |\n"

func configWithTaxonomy(language string) string {
	return "[project]\nlanguage = \"" + language + "\"\n\n[paths]\nlabels = \"" + taxRel + "\"\n\n[labels]\narea_prefix = \"2-area:\"\n"
}

// directionPacks is testPacks plus the real headings table and the real direction templates of
// core, so a seeded file is what a user gets.
func directionPacks(t *testing.T) fstest.MapFS {
	t.Helper()
	m := testPacks()
	for _, rel := range []string{"headings.toml", "templates/strategy.md", "templates/tenets.md", "templates/area.md"} {
		b, err := fs.ReadFile(pack.Files, "core/"+rel)
		if err != nil {
			t.Fatal(err)
		}
		m["core/"+rel] = &fstest.MapFile{Data: b}
	}
	return m
}

// mentionsDirection matches the word, not a temp path that holds the test name.
var mentionsDirection = regexp.MustCompile(`(?i)\bdirection\b`)

func withPacks(m fstest.MapFS) func(*Options) { return func(o *Options) { o.Packs = m } }

func withDirection(o *Options) { o.Direction = true }

func mustRun(t *testing.T, root string, mod func(*Options)) result {
	t.Helper()
	r := run(t, root, "", mod)
	if r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	return r
}

// AC 1: without keys and without the flag, init and --upgrade seed nothing and say nothing.
func TestInitWithoutDirectionWantsNothing(t *testing.T) {
	root := newRepo(t)
	dp := withPacks(directionPacks(t))
	for _, mod := range []func(*Options){all(dp, yes), all(dp, yes, upgrade)} {
		r := mustRun(t, root, mod)
		if mentionsDirection.MatchString(r.out + r.err) {
			t.Errorf("output mentions direction:\n%s\n%s", r.out, r.err)
		}
		if exists(root, path.Dir(strategyRel)) {
			t.Fatal("direction documents written without --direction or keys")
		}
		cfg := read(t, root, "lighthouse.toml")
		for _, key := range []string{"strategy", "tenets", "areas"} {
			if strings.Contains(cfg, key) {
				t.Errorf("lighthouse.toml has %s:\n%s", key, cfg)
			}
		}
	}
}

func checkSeeded(t *testing.T, root, language string) {
	t.Helper()
	want := map[string]string{"nl": "## Leidend beleid", "en": "## Guiding policy"}[language]
	for _, rel := range []string{strategyRel, areasRel + "/core.md", areasRel + "/init.md"} {
		got := read(t, root, rel)
		if !strings.Contains(got, want) {
			t.Errorf("%s (%s) lacks %q:\n%s", rel, language, want, got)
		}
		if strings.Contains(got, "{{") {
			t.Errorf("%s keeps a marker:\n%s", rel, got)
		}
	}
	if got := read(t, root, tenetsRel); strings.Contains(got, "{{") {
		t.Errorf("%s keeps a marker:\n%s", tenetsRel, got)
	}
	cfg := read(t, root, "lighthouse.toml")
	for _, kv := range []string{`strategy = "` + strategyRel + `"`, `tenets = "` + tenetsRel + `"`, `areas = "` + areasRel + `"`} {
		if !strings.Contains(cfg, kv) {
			t.Errorf("lighthouse.toml lacks %s:\n%s", kv, cfg)
		}
	}
	if fs := direction.Check(root); len(fs) != 0 {
		t.Errorf("Check after seeding (%s): %v", language, fs)
	}
}

func TestInitDirectionSeedsMissingTemplates(t *testing.T) {
	for _, language := range []string{"nl", "en"} {
		root := newRepo(t)
		write(t, root, "lighthouse.toml", configWithTaxonomy(language))
		write(t, root, taxRel, taxonomy)
		mustRun(t, root, all(withPacks(directionPacks(t)), yes, withDirection))
		checkSeeded(t, root, language)

		// A broken seeded heading is a finding: the check reads what was seeded.
		s := read(t, root, strategyRel)
		policy := map[string]string{"nl": "## Leidend beleid", "en": "## Guiding policy"}[language]
		write(t, root, strategyRel, strings.Replace(s, policy, "## Beleid", 1))
		if fs := direction.Check(root); len(fs) == 0 {
			t.Errorf("%s: Check found nothing after breaking a heading", language)
		}
	}
}

// The headings come from the pack of this run, not from an older .lighthouse/headings.toml.
func TestUpgradeDirectionFromOlderHeadings(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", configWithTaxonomy("nl"))
	write(t, root, taxRel, taxonomy)
	old := testPacks()
	old["core/headings.toml"] = file("[acceptance_criteria]\nnl = \"Acceptatiecriteria\"\nen = \"Acceptance criteria\"\n")
	mustRun(t, root, all(withPacks(old), yes))
	mustRun(t, root, all(withPacks(directionPacks(t)), yes, upgrade, withDirection))
	checkSeeded(t, root, "nl")
}

func TestUpgradeSeedsFilesForKeysAlreadySet(t *testing.T) {
	root := newRepo(t)
	// The keys are set by hand; no run passes the flag.
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"en\"\n\n[paths]\nlabels = \""+taxRel+"\"\nstrategy = \""+strategyRel+"\"\ntenets = \""+tenetsRel+"\"\nareas = \""+areasRel+"\"\n\n[labels]\narea_prefix = \"2-area:\"\n")
	write(t, root, taxRel, taxonomy)
	dp := withPacks(directionPacks(t))
	mustRun(t, root, all(dp, yes))
	checkSeeded(t, root, "en")

	if err := os.Remove(filepath.Join(root, strategyRel)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, areasRel)); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, all(dp, yes, upgrade))
	checkSeeded(t, root, "en")

	before := snapshot(t, root)
	r := mustRun(t, root, all(dp, yes, upgrade))
	if r.out != "No changes.\n" {
		t.Errorf("second upgrade: %q", r.out)
	}
	if !sameSnapshot(before, snapshot(t, root)) {
		t.Error("second upgrade changed files")
	}
}

func TestUpgradeNeverOverwritesLocalEdits(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", configWithTaxonomy("nl"))
	write(t, root, taxRel, taxonomy)
	dp := withPacks(directionPacks(t))
	mustRun(t, root, all(dp, yes, withDirection))
	const mine = "# mijn strategie\n"
	write(t, root, strategyRel, mine)
	for _, mod := range []func(*Options){all(dp, yes, upgrade), all(dp, yes, upgrade, withDirection)} {
		r := mustRun(t, root, mod)
		if got := read(t, root, strategyRel); got != mine {
			t.Fatalf("strategie.md = %q", got)
		}
		if !strings.Contains(r.err, "direction: "+strategyRel+" exists; left as it is") {
			t.Errorf("stderr lacks the exists line:\n%s", r.err)
		}
	}
	if strings.Contains(read(t, root, manifest.Path), "strategie.md") {
		t.Fatal("seeded file in the manifest")
	}
}

func TestDirectionWithoutTaxonomySeedsNoAreas(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"en\"\n")
	r := mustRun(t, root, all(withPacks(directionPacks(t)), yes, withDirection))
	entries, err := os.ReadDir(filepath.Join(root, areasRel))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".gitkeep" {
		t.Fatalf("areas holds %v, want only .gitkeep", entries)
	}
	n := 0
	for _, l := range strings.Split(r.err, "\n") {
		if strings.HasPrefix(l, "direction: ") && strings.Contains(l, "area") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want one explanation line, got %d:\n%s", n, r.err)
	}
	if fs := direction.Check(root); len(fs) != 0 {
		t.Errorf("Check: %v", fs)
	}
}

// A taxonomy without labels of the prefix is the same as none.
func TestDirectionWithoutAreaLabelsSeedsNoAreas(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", configWithTaxonomy("en"))
	write(t, root, taxRel, "| Label | Color |\n|---|---|\n| `needs-hands` | `FBCA04` |\n")
	mustRun(t, root, all(withPacks(directionPacks(t)), yes, withDirection))
	entries, err := os.ReadDir(filepath.Join(root, areasRel))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".gitkeep" {
		t.Fatalf("areas holds %v, want only .gitkeep", entries)
	}
	if fs := direction.Check(root); len(fs) != 0 {
		t.Errorf("Check: %v", fs)
	}
}

func TestDirectionRejectsPathsOutsideRepo(t *testing.T) {
	cases := map[string]struct{ cfg, tax string }{
		"strategy":     {"[paths]\nstrategy = \"../x.md\"\n", ""},
		"tenets":       {"[paths]\ntenets = \"/tmp/x.md\"\n", ""},
		"areas":        {"[paths]\nareas = \"a/../../b\"\n", ""},
		"area label":   {configWithTaxonomy("en"), "| Label | Color |\n|---|---|\n| `2-area:../y` | `AAAAAA` |\n"},
		"nested label": {configWithTaxonomy("en"), "| Label | Color |\n|---|---|\n| `2-area:a/b` | `AAAAAA` |\n"},
	}
	for name, c := range cases {
		root := newRepo(t)
		write(t, root, "lighthouse.toml", c.cfg)
		if c.tax != "" {
			write(t, root, taxRel, c.tax)
		}
		before := snapshot(t, root)
		r := run(t, root, "", all(withPacks(directionPacks(t)), yes, withDirection))
		if r.code != 1 {
			t.Errorf("%s: rc %d\n%s", name, r.code, r.err)
		}
		if !sameSnapshot(before, snapshot(t, root)) {
			t.Errorf("%s: files written", name)
		}
		if exists(filepath.Dir(root), "x.md") || exists(filepath.Dir(root), "y.md") {
			t.Errorf("%s: wrote outside the repo", name)
		}
	}
}

// An existing empty areas directory is not in git: the keep file is still planned.
func TestDirectionKeepsExistingEmptyAreasDirectory(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"en\"\n")
	if err := os.MkdirAll(filepath.Join(root, areasRel), 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, all(withPacks(directionPacks(t)), yes, withDirection))
	if _, err := os.Stat(filepath.Join(root, areasRel, ".gitkeep")); err != nil {
		t.Errorf(".gitkeep: %v", err)
	}
}

// Backslashes and volume names are not clean relative paths on any platform.
func TestSafeRelRejectsBackslashAndVolume(t *testing.T) {
	for _, p := range []string{`..\\outside.md`, `docs\\x.md`, `C:x.md`} {
		if filepath.VolumeName(p) == "" && !strings.Contains(p, `\`) {
			continue // C:x.md has no volume on this platform
		}
		if safeRel("paths.strategy", p) == nil {
			t.Errorf("safeRel accepted %q", p)
		}
	}
}
