package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// selfFile holds the needles and the sample sources of the scans below as literals; a scan of
// itself would always fail, so it is the one whole-file exemption.
const selfFile = "ci/domains/sources_test.go"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// goFiles lists the .go files of the repository the way git sees them (tracked or untracked,
// not ignored); a filesystem walk would also read .claude/worktrees/.
func goFiles(t *testing.T) (string, []string) {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		t.Fatal("git ls-files found no .go file")
	}
	return root, files
}

var defaultPathLine = regexp.MustCompile(`(?m)^\s*"paths\.(?:specs|plans)":.*$`)

// withoutExemptions blanks the text that a Go file may name although classify keeps the path out
// of go: replace-only, never a whole-file exemption (except selfFile, handled by the caller).
func withoutExemptions(rel, text string) string {
	switch {
	case rel == "internal/config/config.go":
		// paths.specs and paths.plans default to a path under docs/: the value of an absent key,
		// handed to the caller as a string; Go reads nothing there.
		return defaultPathLine.ReplaceAllString(text, "")
	case rel == "internal/roborev/size.go":
		// The size rule of roborev-gate classifies a changed path as code or not; it reads no file.
		return strings.ReplaceAll(text, "docs/", "")
	case rel == "internal/pins/lane/branch.go", rel == "internal/pins/lane/branch_test.go":
		// The lane writes a changelog fragment onto its own pins branch; Go reads none there.
		return strings.ReplaceAll(text, "changelog.d", "")
	case rel == "internal/pins/lane/body.go", rel == "internal/pins/lane/body_test.go":
		// docs/pins.md is text in an issue or PR body; Go reads no file there.
		return strings.ReplaceAll(text, "docs/pins.md", "")
	case rel == "internal/config/config_test.go":
		// TestInstallDocGitLabExampleParses reads docs/install.md; classify keeps that file in go.
		return strings.ReplaceAll(text, "docs/install.md", "")
	case strings.HasPrefix(rel, "internal/repopins/") || rel == "e2e/literalgrep_test.go" || rel == "e2e/gettingstarted_test.go":
		// They read docs/ and ci/literal-* files that classify keeps in go (goDocs, and the literal
		// lists left out of notGo), as internal/config/config_test.go does.
		return strings.ReplaceAll(strings.ReplaceAll(text, "docs/", ""), "ci/literal-", "")
	case rel == "internal/dispatch/apply_scheduled.go":
		// A doc comment points at docs/workplace.md; Go reads no file there.
		return strings.ReplaceAll(text, "docs/workplace.md", "")
	case rel == "internal/scheduled/maprefresh/e2e_test.go", rel == "internal/scheduled/maprefresh/maprefresh_test.go":
		// docs/map is the publish_dir of the map-refresh job: a path in a temp repo, never this repo.
		return strings.ReplaceAll(text, "docs/", "")
	case strings.HasPrefix(rel, "ci/changelog/") && strings.HasSuffix(rel, ".go"):
		// ci/changelog reads changelog.d/ of the checkout only during check and release, in jobs
		// that run on every PR or release; its tests read only testdata/ and temporary repos.
		return strings.ReplaceAll(strings.ReplaceAll(text, "changelog.d", ""), "docs/releasing.md", "")
	case rel == "ci/domains/domains.go":
		// The tables that classify the paths hold these needles as literals; they read no file.
		return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(text,
			"docs/", ""), "changelog.d", ""), "ci/literal-", "")
	case rel == "ci/domains/domains_test.go":
		// The classify cases name the paths they classify; no file is read.
		return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(text,
			"docs/", ""), "changelog.d", ""), "ci/literal-", "")
	case strings.HasPrefix(rel, "ci/loop_roadmap/") && strings.HasSuffix(rel, ".go"):
		// The code and its tests name docs/site/data/loop.json and docs/developer/labels.md as
		// output text; the tests read a temporary map, never the repository's. loop-roadmap.yml
		// runs on every PR, so the exemption hides no missed CI run.
		return strings.ReplaceAll(text, "docs/", "")
	case rel == "internal/review/ledger_test.go" || rel == "internal/cli/reviewindex_test.go":
		// docs/... are plan paths in a ledger header: text stored in a row, never read from this repo.
		return strings.ReplaceAll(text, "docs/", "")
	case rel == "internal/cli/reviewrun_test.go" || rel == "internal/review/runner_ports_test.go":
		// docs/doc.md and docs/spec.md are document arguments of a review run in test data: paths in
		// a temporary repo, never this repo's docs.
		return strings.ReplaceAll(text, "docs/", "")
	case rel == "internal/telemetry/record_test.go":
		// docs/... are RecordPath values of a finding in test data: strings stored in a row.
		return strings.ReplaceAll(text, "docs/", "")
	case strings.HasPrefix(rel, "internal/scheduled/reflect/"):
		// docs/developer/reflect is the output_dir of the reflect job, a path in the target repo
		// of the job (a temp repo in tests), never this repo's docs.
		return strings.ReplaceAll(text, "docs/", "")
	case rel == "internal/repoconfig/repoconfig_test.go":
		// docs/... are the Document paths of a labels document inside a test string, never read from this repo.
		return strings.ReplaceAll(text, "docs/", "")
	case strings.HasPrefix(rel, "internal/scheduled/mapplanned/"):
		// docs/... are map documents and changelog.d is the fragment directory of the target repo of
		// the job (a temp repo in tests), never this repo's.
		return strings.ReplaceAll(strings.ReplaceAll(text, "docs/", ""), "changelog.d", "")
	}
	return text
}

// TestGoSourcesDoNotReadNotGoPaths fails when Go code starts to read a path that classify keeps
// out of go.
func TestGoSourcesDoNotReadNotGoPaths(t *testing.T) {
	root, files := goFiles(t)
	var named []string
	for _, rel := range files {
		if rel == selfFile {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := withoutExemptions(rel, string(b))
		for _, needle := range []string{"docs/", "changelog.d", "ci/literal-"} {
			if strings.Contains(text, needle) {
				named = append(named, rel+": "+needle)
			}
		}
	}
	if len(named) > 0 {
		t.Errorf("Go names a path outside the go domain in ci/domains: %v", named)
	}
}

const rootMD = `README\.md|CHANGELOG\.md|CONTRIBUTING\.md|SECURITY\.md|CLA\.md`

// A string literal that climbs out of its package (`../`, once or more) into tests/ or a loose
// root .md file. RE2 has no lookahead: the fixtures are filtered in repoReads.
var repoRead = regexp.MustCompile("[\"`]((?:\\.\\./)+(?:tests/[^\"`]*|" + rootMD + "))[\"`]")

// The same read built from parts, filepath.Join-style: `".."` parts, then a part that reaches
// tests/ or a loose root .md file.
var repoReadParts = regexp.MustCompile(`(?:"\.\."\s*,\s*)+"(?:tests"(?:\s*,\s*"pack"\s*,\s*"fixtures")?|tests/[^"]*"|(?:` + rootMD + `)")`)

// readsFixtures reports whether a read names one of the test directories Go may read.
func readsFixtures(s string) bool {
	return strings.Contains(s, "tests/pack/fixtures/") || strings.Contains(s, "tests/pack/python/testdata/ruff-format-check/")
}

var fixturePartsPrefix = regexp.MustCompile(`"tests"\s*,\s*"pack"\s*,\s*"fixtures"$`)

// repoReads returns the paths of this repo that Go source text reads from above its package
// directory, tests/pack/fixtures/ excepted.
func repoReads(text string) []string {
	var out []string
	for _, m := range repoRead.FindAllStringSubmatch(text, -1) {
		if !readsFixtures(m[1]) {
			out = append(out, m[1])
		}
	}
	for _, m := range repoReadParts.FindAllString(text, -1) {
		if fixturePartsPrefix.MatchString(m) || readsFixtures(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func TestRepoReadsFindsAReadAboveThePackage(t *testing.T) {
	text := "os.ReadFile(\"../../tests/x.py\")\nos.ReadFile(`../README.md`)\n\"../../CHANGELOG.md\"\n" +
		"\"../../CONTRIBUTING.md\"\n`../SECURITY.md`\n\"../CLA.md\""
	want := []string{"../../tests/x.py", "../README.md", "../../CHANGELOG.md", "../../CONTRIBUTING.md", "../SECURITY.md", "../CLA.md"}
	if got := repoReads(text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRepoReadsSkipsFixturesTempReposAndInnerDotdot(t *testing.T) {
	text := "os.ReadFile(\"../../tests/pack/fixtures/protocol/headers.json\")\n" +
		"writeFile(t, repo, \"tests/test_x.py\", \"\")\n" +
		"filepath.Join(repo, \"README.md\")\n" +
		"unitText(\"file\", \".claude/hooks/lighthouse/../../../README.md\", \"\")\n"
	if got := repoReads(text); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

func TestRepoReadsFindsAReadBuiltFromParts(t *testing.T) {
	text := "filepath.Join(\"..\", \"..\", \"README.md\")\n" +
		"filepath.Join(\"..\", \"..\", \"tests\", \"test_x.py\")\n" +
		"filepath.Join(\"..\", \"CHANGELOG.md\")\n" +
		"filepath.Join(\"..\", \"..\", \"tests/test_x.py\")\n"
	if got := repoReads(text); len(got) != 4 {
		t.Errorf("got %d reads %v, want 4", len(got), got)
	}
}

func TestRepoReadsPartsSkipFixturesAndOtherDirs(t *testing.T) {
	text := "filepath.Join(\"..\", \"..\", \"tests\", \"pack\", \"fixtures\", \"protocol\", \"headers.json\")\n" +
		"filepath.Join(\"..\", \"..\", \"tests/pack/fixtures/protocol/headers.json\")\n" +
		"filepath.Join(\"..\", \"dispatch\", \"testdata\", \"snapshot.json\")\n" +
		"filepath.Join(repo, \"tests\", \"test_x.py\")\n"
	if got := repoReads(text); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// TestGoSourcesDoNotReadTestsOrRootMD fails when Go code starts to read tests/ (outside the
// fixtures) or a loose root .md file. A path from a variable plus parts is not seen.
func TestGoSourcesDoNotReadTestsOrRootMD(t *testing.T) {
	root, files := goFiles(t)
	var reads []string
	for _, rel := range files {
		if rel == selfFile {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, p := range repoReads(string(b)) {
			reads = append(reads, rel+": "+p)
		}
	}
	if len(reads) > 0 {
		t.Errorf("Go reads a path outside the go domain in ci/domains: %v", reads)
	}
}
