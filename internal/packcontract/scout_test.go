package packcontract

import (
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// The persona issue-impact-scout: what no runtime enforces. The tool allowlist, the model and
// the turn limit, the eight sections of the IMPACT PACK in order, the caller search before the
// analogy, and the output rules. That the file exists and is the only agent is TestCorePack;
// its name is TestScoutNameMatchesPersona (internal/kpi); its search budget is
// TestScoutBudgetMatchesPersona. The call from start-issue is in issueflow_test.go.

const scoutAgent = "core/agents/issue-impact-scout.md"

// scoutSections are the sections of the IMPACT PACK, in contract order.
var scoutSections = []string{
	"IMPACT PACK",
	"AFFECTED COMPONENTS",
	"CALLERS AND COUNTERS",
	"BEST ANALOGY",
	"APPLICABLE RULES",
	"TESTS AND EVIDENCE",
	"RISKS AND UNCERTAINTIES",
	"OUT OF SCOPE",
}

// scoutFront is the frontmatter of the persona.
type scoutFront struct {
	Description string `yaml:"description"`
	Tools       string `yaml:"tools"`
	Model       string `yaml:"model"`
	MaxTurns    int    `yaml:"maxTurns"`
}

func parseScoutFront(t *testing.T) scoutFront {
	t.Helper()
	var f scoutFront
	if err := yaml.Unmarshal([]byte(frontmatter(t, readPack(t, scoutAgent))), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// scoutBody is the persona below its frontmatter, whitespace collapsed.
func scoutBody(t *testing.T) string {
	t.Helper()
	body := readPack(t, scoutAgent)
	_, rest, ok := strings.Cut(strings.TrimPrefix(body, "---\n"), "\n---\n")
	if !ok {
		t.Fatal("frontmatter is not closed")
	}
	return flat(rest)
}

// Classification: fails when the scout runs on another model, or its turn limit changes without
// the body that explains the limit changing with it.
func TestScoutModelAndTurnLimit(t *testing.T) {
	f := parseScoutFront(t)
	if f.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet", f.Model)
	}
	if f.MaxTurns != 12 {
		t.Errorf("maxTurns = %d, want 12", f.MaxTurns)
	}
	wantAll(t, scoutBody(t), "The `maxTurns: 12` in the frontmatter bounds *turns*, not searches:")
}

// Classification: fails when the tool allowlist gains a tool (Bash cannot be bounded to
// read-only) or loses one of Read, Grep and Glob.
func TestScoutToolAllowlistIsReadOnly(t *testing.T) {
	var tools []string
	for tool := range strings.SplitSeq(parseScoutFront(t).Tools, ",") {
		tools = append(tools, strings.TrimSpace(tool))
	}
	sort.Strings(tools)
	if !slices.Equal(tools, []string{"Glob", "Grep", "Read"}) {
		t.Errorf("tools = %v, want Glob, Grep, Read", tools)
	}
}

// Classification: fails when the description stops bounding the scout to start-issue at gate 4b
// and 4c.
func TestScoutDescriptionBoundsTheUseToStartIssue(t *testing.T) {
	wantAll(t, parseScoutFront(t).Description,
		"Invoked only by lighthouse-start-issue at gate 4b and 4c, after the worktree exists.")
}

// Classification: fails when a section leaves the IMPACT PACK block, a section is added to it, or
// the order of the sections changes.
func TestScoutPackSectionsFollowTheContractOrder(t *testing.T) {
	block := section(t, readPack(t, scoutAgent), "```text\nIMPACT PACK", "\n```")
	var got []string
	for line := range strings.SplitSeq(strings.TrimPrefix(block, "```text\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "- ") {
			got = append(got, line)
		}
	}
	if !slices.Equal(got, scoutSections) {
		t.Errorf("sections in the block:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(scoutSections, "\n"))
	}
}

// Classification: fails when headings.toml gives impact_pack a translation, which the persona,
// naming it literally, would not follow.
func TestScoutImpactPackHeadingIsTheHeadingsValue(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(packRoot, "core", "headings.toml"))
	got, err := config.Variants("impact_pack")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{scoutSections[0]}) {
		t.Errorf("impact_pack variants = %v, want only %q", got, scoutSections[0])
	}
}

// Classification: fails when the caller search loses a place to look (relative imports, entry
// points and deploy units, counters on a directory), its per-directory summary, or the rule that
// an empty outcome names where it searched.
func TestScoutDemandsTheCallerSearch(t *testing.T) {
	wantAll(t, scoutBody(t),
		"**Callers and counters — mandatory, before the analogy.** For every module or file from step 2:",
		"search importers and callers repo-wide: one `Grep` over the whole worktree on import form and path together, without a source-root anchor (`x[./]y`).",
		"Count the relative import too (`from ..x import y`, `from .y import`): search `import y` next to the package `x`.",
		"In the hits, look at least at the entry points: command-line modules, web bridges and services, scripts, and deploy units that run the module (`ExecStart=`, `-m <package>.…`); that is where callers get missed;",
		"If the issue adds a file to a directory, `Grep` in the tests on the path of that directory; a test that lists that directory or pins a number is a counter, even when it does not name the module.",
		"More than five in one directory: one line per directory, with the count and two examples.",
		"Exception: `CALLERS AND COUNTERS` is never a bare `- none`, but `- none found — searched <where> for <terms>`: without a search location, absence is no outcome.")
}

// Classification: fails when the caller search moves after the analogy, or the budget rule stops
// making the analogy and the rules pay for the limit instead of the callers.
func TestScoutCallerSearchComesBeforeTheAnalogy(t *testing.T) {
	body := scoutBody(t)
	wantOrder(t, body, "**Callers and counters — mandatory, before the analogy.**", "That is your analogy.")
	wantAll(t, body, "The caller search (step 3) comes before the analogy and the rules, so that the limit costs those two sections and not the callers.")
}

// Classification: fails when the scout may write a preamble or a closing next to the pack.
func TestScoutPackIsTheWholeAnswer(t *testing.T) {
	wantAll(t, scoutBody(t),
		"Your first character is the `I` of `IMPACT PACK`.",
		"No preamble, no reasoning, no announcement that you are going to write the pack, no closing after it.",
		"You answer with exactly these sections, in this order, and with nothing around them:")
}

// Classification: fails when a line reference no longer needs the range to have been read, or a
// range the scout is unsure of stops falling back to the file name alone.
func TestScoutBindsALineNumberToWhatWasActuallyRead(t *testing.T) {
	wantAll(t, scoutBody(t),
		"A line reference is allowed only if you actually read that line range.",
		"If you are sure of the file and not of the range, give the file name without lines — a wrong line number is worse than no line number, because precision is what this pack sells.")
}

// Classification: fails when the scout may design, plan, or guess what it cannot establish.
func TestScoutForbidsDesigningAndPlanning(t *testing.T) {
	wantAll(t, scoutBody(t),
		"**No design.** You choose no solution, you weigh no alternatives, you recommend nothing.",
		"**No plan.** No task order, no implementation instruction, no code proposal.",
		"What you cannot establish from the repo, you record as `UNKNOWN: <what you could not establish>`. You do not guess and you fill nothing in from general knowledge.")
}

// scoutSourceIssueRe is an issue number of the source repo in parentheses.
var scoutSourceIssueRe = regexp.MustCompile(`\(#\d+\)`)

// Classification: fails when the persona names a directory layout or an issue number of the
// source repo, or drifts into Dutch.
func TestScoutIsDetachedAndEnglish(t *testing.T) {
	body := readPack(t, scoutAgent)
	wantNone(t, body, "src/cli/", "src/web/", "services.py", "-m src.")
	if m := scoutSourceIssueRe.FindString(body); m != "" {
		t.Errorf("source issue number %s", m)
	}
	wantNone(t, strings.ToLower(body), dutchWords...)
}
