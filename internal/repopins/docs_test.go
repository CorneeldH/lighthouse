package repopins

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// section returns the text between `## <title>` and the next `## ` heading of doc, and fails
// the test when the heading is missing.
func section(t *testing.T, doc, title string) string {
	t.Helper()
	re := regexp.MustCompile(`(?ms)^## ` + regexp.QuoteMeta(title) + `\n(.*?)(?:^## |\z)`)
	m := re.FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("document lacks the heading '## %s'", title)
	}
	return m[1]
}

// initFlags returns the flags `lighthouse init` registers, read from the `Var(&o.x, "<flag>"`
// calls of internal/cli/init.go.
func initFlags(t *testing.T) map[string]bool {
	t.Helper()
	flags := map[string]bool{}
	for _, m := range regexp.MustCompile(`Var\(&o\.\w+, "([\w-]+)"`).FindAllStringSubmatch(read(t, "internal", "cli", "init.go"), -1) {
		flags[m[1]] = true
	}
	if len(flags) == 0 {
		t.Fatal("no init flags found in internal/cli/init.go")
	}
	return flags
}

// requiredTools returns the tools named by pack/*/requires.toml: the top-level tables, plus the
// tools in `forge.<kind>` tables. `forge` itself is not a tool.
func requiredTools(t *testing.T) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "pack", "*", "requires.toml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no pack/*/requires.toml found: %v", err)
	}
	tools := map[string]bool{}
	for _, p := range paths {
		var doc map[string]any
		if _, err := toml.DecodeFile(p, &doc); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for key, val := range doc {
			if key != "forge" {
				tools[key] = true
				continue
			}
			kinds, _ := val.(map[string]any)
			for _, kind := range kinds {
				for tool := range kind.(map[string]any) {
					tools[tool] = true
				}
			}
		}
	}
	return tools
}

func codeSpans(text string) []string {
	var out []string
	for _, m := range regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

const fence = "(?ms)^[ \t]*```[^\n]*\n(.*?)^[ \t]*```"

// fencedBlocks returns the fenced blocks of text, also those indented under a list item.
func fencedBlocks(text string) []string {
	var out []string
	for _, m := range regexp.MustCompile(fence).FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// codeSpansOutsideFences returns the inline spans of text, outside its fenced blocks.
func codeSpansOutsideFences(text string) []string {
	return codeSpans(regexp.MustCompile(fence).ReplaceAllString(text, ""))
}

// firstCells returns the first cell of every body row of the first markdown table in text.
func firstCells(text string) []string {
	var out []string
	inTable := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") {
			inTable = 0
			continue
		}
		inTable++
		if inTable > 2 {
			cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			out = append(out, strings.TrimSpace(cells[0]))
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func gettingStarted(t *testing.T) string { return read(t, "docs", "getting-started.md") }
func agentGuide(t *testing.T) string     { return read(t, "docs", "site", "agents", "install.md") }

// tablesNamedInRequirements returns the tools in backticks in the first column of the
// Requirements table.
func tablesNamedInRequirements(t *testing.T) map[string]bool {
	t.Helper()
	named := map[string]bool{}
	for _, cell := range firstCells(section(t, gettingStarted(t), "Requirements")) {
		for _, span := range codeSpans(cell) {
			named[span] = true
		}
	}
	return named
}

// A production change that fails this test: adding a tool to a pack/*/requires.toml without
// adding it to the Requirements table of docs/getting-started.md.
func TestRequirementsTableNamesEveryTool(t *testing.T) {
	named := tablesNamedInRequirements(t)
	for _, tool := range sortedKeys(requiredTools(t)) {
		if !named[tool] {
			t.Errorf("docs/getting-started.md Requirements lacks %q", tool)
		}
	}
}

// A production change that fails this test: removing or renaming a tool in a
// pack/*/requires.toml while the Requirements table still names it.
func TestRequirementsMatchCodeSpansExactly(t *testing.T) {
	required := requiredTools(t)
	for _, tool := range sortedKeys(tablesNamedInRequirements(t)) {
		if !required[tool] {
			t.Errorf("Requirements names %q, which no requires.toml has", tool)
		}
	}
}

// A production change that fails this test: changing the question `lighthouse init` asks in
// initcmd.go (`Apply these changes? [y/N]`) without updating the guide that quotes it.
func TestInitPromptMatchesTheBinary(t *testing.T) {
	m := regexp.MustCompile(`ask\(o\.Out, in, "([^"]+)"\)`).FindStringSubmatch(read(t, "internal", "initcmd", "initcmd.go"))
	if m == nil {
		t.Fatal("initcmd.go has no ask(o.Out, in, \"…\") call")
	}
	prompt := strings.TrimSpace(m[1])
	if !strings.Contains(section(t, gettingStarted(t), "Run lighthouse init"), prompt) {
		t.Errorf("Run lighthouse init does not quote the prompt %q", prompt)
	}
}

var flagInSpan = regexp.MustCompile(`--(\w[\w-]*)`)

// A production change that fails this test: removing or renaming an `init` flag that
// docs/getting-started.md tells the reader to use.
func TestGettingStartedInitFlagsExist(t *testing.T) {
	used := map[string]bool{}
	for _, span := range codeSpans(section(t, gettingStarted(t), "Run lighthouse init")) {
		for _, m := range flagInSpan.FindAllStringSubmatch(span, -1) {
			used[m[1]] = true
		}
	}
	if !used["yes"] || !used["upgrade"] {
		t.Errorf("Run lighthouse init no longer names --yes and --upgrade: %v", sortedKeys(used))
	}
	known := initFlags(t)
	for _, flag := range sortedKeys(used) {
		if !known[flag] {
			t.Errorf("guide names unknown init flag --%s", flag)
		}
	}
}

var skillRef = regexp.MustCompile(`/lighthouse-([\w-]+)`)

// A production change that fails this test: renaming or removing a skill under pack/*/skills/
// while "Your first issue" still tells the reader to type it.
func TestFirstIssueNamesOnlyExistingSkills(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(repoRoot(t), "pack", "*", "skills", "*"))
	if err != nil {
		t.Fatal(err)
	}
	skills := map[string]bool{}
	for _, d := range dirs {
		skills[filepath.Base(d)] = true
	}
	named := skillRef.FindAllStringSubmatch(section(t, gettingStarted(t), "Your first issue"), -1)
	if len(named) == 0 {
		t.Fatal("Your first issue names no skills")
	}
	for _, m := range named {
		if !skills[m[1]] {
			t.Errorf("Your first issue names /lighthouse-%s, which is no skill", m[1])
		}
	}
}

// A production change that fails this test: a change to the chain (issue, start, PR) that
// reorders or drops a step in the guide's walk-through.
func TestFirstIssueWalksTheChainInOrder(t *testing.T) {
	text := section(t, gettingStarted(t), "Your first issue")
	last := -1
	for _, cmd := range []string{"/lighthouse-write-issue", "/lighthouse-start-issue", "/lighthouse-write-pr"} {
		loc := regexp.MustCompile(regexp.QuoteMeta(cmd) + `(?:[^\w-]|$)`).FindStringIndex(text)
		if loc == nil {
			t.Fatalf("Your first issue lacks %s", cmd)
		}
		if loc[0] <= last {
			t.Errorf("%s comes before the previous step of the chain", cmd)
		}
		last = loc[0]
	}
}

func agentSection(t *testing.T, title string) string {
	t.Helper()
	return section(t, agentGuide(t), title)
}

var agentInstallBinary = regexp.MustCompile(`(?ms)^## Install the binary\n.*?^## `)

// The History section runs `lighthouse history`, whose flags are not init flags.
var agentHistory = regexp.MustCompile(`(?ms)^## History\n.*?^## `)

// A production change that fails this test: removing or renaming an `init` flag that the
// agent guide tells the agent to run (outside the download blocks); --help is cobra's own.
func TestAgentGuideInitFlagsExist(t *testing.T) {
	text := agentInstallBinary.ReplaceAllString(agentGuide(t), "## ")
	text = agentHistory.ReplaceAllString(text, "## ")
	chunks := append(codeSpansOutsideFences(text), fencedBlocks(text)...)
	flagRe := regexp.MustCompile(`(?:^|[^\w-])--(\w[\w-]*)`)
	used := map[string]bool{}
	for _, chunk := range chunks {
		for _, m := range flagRe.FindAllStringSubmatch(chunk, -1) {
			used[m[1]] = true
		}
	}
	for _, want := range []string{"pack", "no-pack", "upgrade"} {
		if !used[want] {
			t.Errorf("agent guide no longer names --%s", want)
		}
	}
	known := initFlags(t)
	known["help"] = true
	for _, flag := range sortedKeys(used) {
		if !known[flag] {
			t.Errorf("agent guide names unknown init flag --%s", flag)
		}
	}
}

// A production change that fails this test: adding or removing a superpowers entry in
// pack/core/requires.toml without updating the count in the agent guide's Choices.
func TestAgentGuideSuperpowersCount(t *testing.T) {
	var needs struct {
		Superpowers []string `toml:"superpowers"`
	}
	if _, err := toml.Decode(read(t, "pack", "core", "requires.toml"), &needs); err != nil {
		t.Fatal(err)
	}
	var named []string
	for _, m := range regexp.MustCompile(`Without Superpowers, (\d+) lighthouse skills`).FindAllStringSubmatch(agentSection(t, "Choices"), -1) {
		named = append(named, m[1])
	}
	want := strconv.Itoa(len(needs.Superpowers))
	if len(named) != 1 || named[0] != want {
		t.Errorf("Choices names %v skills without Superpowers; pack/core/requires.toml has %s", named, want)
	}
}

// A production change that fails this test: adding a tool to a pack/*/requires.toml without
// naming it in the agent guide's "Look first" step.
func TestAgentGuideNamesEveryRequiredTool(t *testing.T) {
	named := map[string]bool{}
	for _, span := range codeSpansOutsideFences(agentSection(t, "Look first")) {
		named[span] = true
	}
	for _, tool := range sortedKeys(requiredTools(t)) {
		if !named[tool] {
			t.Errorf("Look first lacks %q", tool)
		}
	}
}

var languagesVar = regexp.MustCompile(`var languages = \[\]string\{([^}]*)\}`)

// A production change that fails this test: adding or removing a language in `languages` of
// internal/config/config.go without updating the languages the agent guide offers.
func TestAgentGuideLanguagesAreTheConfigLanguages(t *testing.T) {
	m := languagesVar.FindStringSubmatch(read(t, "internal", "config", "config.go"))
	if m == nil {
		t.Fatal("internal/config/config.go has no `var languages = []string{…}`")
	}
	want := map[string]bool{}
	for _, l := range regexp.MustCompile(`"([a-z]{2})"`).FindAllStringSubmatch(m[1], -1) {
		want[l[1]] = true
	}
	var lines []string
	for _, line := range strings.Split(agentSection(t, "Choices"), "\n") {
		if strings.Contains(line, "`[project] language`") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("want one Choices line on `[project] language`, got %d", len(lines))
	}
	offered := map[string]bool{}
	for _, l := range regexp.MustCompile("`([a-z]{2})`").FindAllStringSubmatch(lines[0], -1) {
		offered[l[1]] = true
	}
	if strings.Join(sortedKeys(offered), ",") != strings.Join(sortedKeys(want), ",") {
		t.Errorf("guide offers languages %v, config accepts %v", sortedKeys(offered), sortedKeys(want))
	}
}

// failureMessages returns the message that opens each item of "When something fails": the
// code span after the `- ` bullet, which may be delimited by a longer run of backticks.
func failureMessages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(agentSection(t, "When something fails"), "\n") {
		rest, ok := strings.CutPrefix(line, "- `")
		if !ok {
			continue
		}
		open := "`" + rest[:len(rest)-len(strings.TrimLeft(rest, "`"))]
		rest = strings.TrimPrefix(rest, strings.TrimPrefix(open, "`"))
		end := strings.Index(rest, open)
		if end < 0 {
			t.Fatalf("unterminated code span in %q", line)
		}
		out = append(out, strings.TrimSpace(rest[:end]))
	}
	if len(out) == 0 {
		t.Fatal("When something fails quotes no message")
	}
	return out
}

// nonTestGoSource returns the concatenated text of every non-test .go file under internal/.
func nonTestGoSource(t *testing.T) string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")
	var sb strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		sb.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// A production change that fails this test: rewording a message lighthouse prints, or its
// prefix, while the agent guide still tells the agent to expect the old text.
func TestAgentGuideFailureMessagesArePrinted(t *testing.T) {
	// Printed by cobra through `lighthouse: %v` in internal/cli/root.go, not by lighthouse itself.
	cobra := map[string]bool{"unknown flag: --<name>": true}
	prefixes := []struct{ prefix, file, printf string }{
		{"lighthouse init: ", "initcmd/initcmd.go", `"lighthouse init: %v\n"`},
		{"lighthouse: ", "cli/root.go", `"lighthouse: %v\n"`},
	}
	src := nonTestGoSource(t)
	placeholder := regexp.MustCompile(`<[^>]*>|…`)
	for _, message := range failureMessages(t) {
		t.Run(message, func(t *testing.T) {
			for _, p := range prefixes {
				if strings.HasPrefix(message, p.prefix) {
					if !strings.Contains(read(t, "internal", p.file), p.printf) {
						t.Errorf("internal/%s lacks %s", p.file, p.printf)
					}
					message = strings.TrimPrefix(message, p.prefix)
					break
				}
			}
			if cobra[message] {
				return
			}
			for _, piece := range placeholder.Split(message, -1) {
				if strings.Trim(piece, " ()") == "" {
					continue
				}
				if !strings.Contains(src, piece) {
					t.Errorf("lighthouse prints no %q", piece)
				}
			}
		})
	}
}

// A production change that fails this test: removing or rewording the upgrade notes
// (`note: host.toml is missing`, `fits this repository (found `) in internal/initcmd while the
// guide still lists them as expected on a healthy install.
func TestAgentGuideUpgradeNotesAreNamed(t *testing.T) {
	var src strings.Builder
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "internal", "initcmd", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	text := agentSection(t, "When something fails")
	for _, note := range []string{"note: host.toml is missing", "fits this repository (found "} {
		if !strings.Contains(src.String(), note) {
			t.Errorf("init no longer prints %q", note)
		}
		if !strings.Contains(text, note) {
			t.Errorf("When something fails lacks %q", note)
		}
	}
}

// A production change that fails this test: changing what an up-to-date `init --upgrade`
// prints to stdout, so the guide's "Stdout must be exactly `No changes.`" check breaks.
func TestAgentGuideNoChangesIsWhatInitPrints(t *testing.T) {
	if !strings.Contains(agentSection(t, "Check"), "No changes.") {
		t.Error("Check does not name `No changes.`")
	}
	if !strings.Contains(read(t, "internal", "initcmd", "initcmd.go"), `Fprintln(o.Out, "No changes.")`) {
		t.Error("init no longer prints `No changes.` on stdout")
	}
}

// A production change that fails this test: rewording the CLAUDE.md import warning of `init`,
// or a guide that no longer asks for the `@AGENTS.md` line before it goes on to Check.
func TestAgentGuideClaudeMDImportIsAsked(t *testing.T) {
	const warning = "warning: CLAUDE.md does not import AGENTS.md"
	if !strings.Contains(read(t, "internal", "initcmd", "initcmd.go"), warning) {
		t.Fatalf("init no longer prints %q", warning)
	}
	run := agentSection(t, "Run lighthouse init")
	i := strings.Index(run, warning)
	if i < 0 {
		t.Fatalf("Run lighthouse init lacks %q", warning)
	}
	step := run[i:]
	if !strings.Contains(step, "`@AGENTS.md`") || !strings.Contains(step, "recommended") {
		t.Error("the step after the warning does not ask for `@AGENTS.md` with a recommendation")
	}
	if strings.Index(step, "`@AGENTS.md`") > strings.Index(step, "[Check](#check)") {
		t.Error("the guide goes on to Check before it asks for `@AGENTS.md`")
	}
	if !strings.Contains(agentSection(t, "Ground rules"), "`@AGENTS.md`") {
		t.Error("Ground rules does not name the `@AGENTS.md` exception")
	}
}

// A production change that fails this test: adding a goos/goarch matrix entry, or renaming the
// `lighthouse_${GOOS}_${GOARCH}` asset name, in .github/workflows/build.yml without the download
// blocks of the agent guide following (removing an entry stays green). The Windows block is not required (removing it stays green); WSL stays named as the alternative.
func TestAgentGuideBinaryBlocks(t *testing.T) {
	const download = "https://github.com/CorneeldH/lighthouse/releases/latest/download/"
	build := read(t, ".github", "workflows", "build.yml")
	if !strings.Contains(build, `NAME="lighthouse_${GOOS}_${GOARCH}"`) {
		t.Fatal("build.yml no longer names its assets lighthouse_${GOOS}_${GOARCH}")
	}
	pairs := regexp.MustCompile(`- goos: (\w+)\n\s+goarch: (\w+)`).FindAllStringSubmatch(build, -1)
	if len(pairs) < 4 {
		t.Fatalf("build.yml matrix has %d goos/goarch pairs, want at least 4", len(pairs))
	}
	assets := []string{"SHA256SUMS"}
	for _, p := range pairs {
		if p[1] != "windows" {
			assets = append(assets, "lighthouse_"+p[1]+"_"+p[2])
		}
	}
	text := agentSection(t, "Install the binary")
	for _, asset := range assets {
		if !strings.Contains(text, download+asset) {
			t.Errorf("Install the binary lacks %s%s", download, asset)
		}
	}
	for _, needle := range []string{"sha256sum --check", "shasum -a 256 -c", "WSL"} {
		if !strings.Contains(text, needle) {
			t.Errorf("Install the binary lacks %q", needle)
		}
	}
}

// A production change that fails this test: removing the agent guide link from the install
// page or the home page, the paste line from the install page's agent block, or the
// `.agent-prompt` selector from the addCopyButtons call in download.js.
func TestAgentGuideIsLinkedFromInstallPage(t *testing.T) {
	const paste = "Read https://corneeldh.github.io/lighthouse/agents/install.md and install lighthouse in this repo."
	install := read(t, "docs", "site", "install.html")
	for name, page := range map[string]string{"install.html": install, "index.html": read(t, "docs", "site", "index.html")} {
		if !strings.Contains(page, `href="agents/install.md"`) {
			t.Errorf("%s does not link agents/install.md", name)
		}
	}
	start := strings.Index(install, `id="agent"`)
	if start < 0 {
		t.Fatal(`install.html has no element with id="agent"`)
	}
	end := strings.Index(install[start:], "</section>")
	if end < 0 {
		t.Fatal(`the id="agent" block of install.html is not closed`)
	}
	block := install[start : start+end]
	if !strings.Contains(block, paste) {
		t.Errorf("the id=\"agent\" block of install.html lacks the paste line %q", paste)
	}
	if !strings.Contains(block, `href="agents/install.md"`) {
		t.Error("the id=\"agent\" block of install.html does not link the agent guide")
	}
	if !regexp.MustCompile(`addCopyButtons\(\[[^\]]*\.agent-prompt`).MatchString(read(t, "docs", "site", "download.js")) {
		t.Error("download.js does not add a copy button to .agent-prompt")
	}
}

func TestGettingStartedNamesUninstall(t *testing.T) {
	doc := gettingStarted(t)
	body := section(t, doc, "Remove lighthouse")
	for _, want := range []string{"lighthouse uninstall", "--yes", "--force"} {
		if !strings.Contains(body, want) {
			t.Errorf("section 'Remove lighthouse' does not name %s", want)
		}
	}
}
