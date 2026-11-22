package repopins

import (
	"strings"
	"testing"
)

const (
	blockBegin = "<!-- lighthouse:begin core -->"
	blockEnd   = "<!-- lighthouse:end core -->"
)

// coreBlock returns the core block text that init installs, read from the package.
func coreBlock(t *testing.T) string {
	t.Helper()
	return read(t, "pack", "core", "instructions", "AGENTS.md")
}

// oneLine collapses all whitespace, so a pinned sentence may wrap in the file.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// projectPart returns AGENTS.md without the managed block: the rules lighthouse keeps for itself.
func projectPart(t *testing.T) string {
	t.Helper()
	text := read(t, "AGENTS.md")
	before, rest, ok := strings.Cut(text, blockBegin)
	if !ok {
		t.Fatalf("AGENTS.md has no %s", blockBegin)
	}
	_, after, ok := strings.Cut(rest, blockEnd)
	if !ok {
		t.Fatalf("AGENTS.md has no %s", blockEnd)
	}
	return before + after
}

func requireTokens(t *testing.T, text string, tokens ...string) {
	t.Helper()
	for _, tok := range tokens {
		if !strings.Contains(text, tok) {
			t.Errorf("missing %q", tok)
		}
	}
}

// A production change that fails this test: CLAUDE.md no longer starts with the `@AGENTS.md`
// import, so Claude Code stops loading the shared rules.
func TestClaudeMDImportsAgentsMD(t *testing.T) {
	first, _, _ := strings.Cut(read(t, "CLAUDE.md"), "\n")
	if first != "@AGENTS.md" {
		t.Errorf("CLAUDE.md line 1 = %q, want @AGENTS.md", first)
	}
}

// A production change that fails this test: AGENTS.md loses the name of a guard hook or of
// the escape hatch, so an agent no longer learns how the gates work.
func TestAgentsMDNamesTheGuardsAndTheEscapeHatch(t *testing.T) {
	requireTokens(t, read(t, "AGENTS.md"),
		"main-commit-gate", "worktree-check", ".claude/allow-main", ".claude/worktrees/")
}

// A production change that fails this test: the core block grows past 186 words or drops a
// rule every repo needs. It carries only the chain rules a hook, a fact the model cannot derive
// or a proven mistake supports, the `heading:<key>` notation and the rule for the language of code.
func TestCoreBlockWordBudget(t *testing.T) {
	text := coreBlock(t)
	if n := len(strings.Fields(text)); n > 186 {
		t.Errorf("core block has %d words, budget is 186", n)
	}
	requireTokens(t, text,
		"main-commit-gate", "worktree", "squash", "[project] language",
		"[project] code_language", "code_language_paths", "heading:<key>",
		".lighthouse/headings.toml")
}

// A production change that fails this test: the core block stops saying how to read a
// `heading:<key>`, so agents write the literal key into artefacts.
func TestCoreBlockSaysHowToReadAHeading(t *testing.T) {
	requireTokens(t, oneLine(coreBlock(t)),
		"lighthouse headings <key>",
		"not the literal `heading:<key>`",
		"reading accepts every language variant")
}

// A production change that fails this test: the core block no longer tells an agent to stop
// when `lighthouse config` fails with `unknown command`, so an old binary reads as an unset key.
func TestCoreBlockSaysAnOldBinaryIsNoUnsetKey(t *testing.T) {
	requireTokens(t, oneLine(coreBlock(t)),
		"`lighthouse config` exits 1 with `unknown command` on stderr: the binary is too old. "+
			`Stop and say so; that is not "not set".`)
}

// A production change that fails this test: the core block drops the rule that an agent
// finishes what it is authorised to do instead of offering it.
func TestCoreBlockSaysFinishWhatIsAuthorised(t *testing.T) {
	requireTokens(t, oneLine(coreBlock(t)),
		"Finish what you are already authorised to do; an offer to do it is not done.")
}

// A production change that fails this test: a rule of the core block is copied into the
// project part of AGENTS.md, so it is said twice and drifts.
func TestProjectPartDoesNotRepeatTheCoreBlock(t *testing.T) {
	part := projectPart(t)
	for _, tok := range []string{
		"squash", "git worktree add", "blocks `git commit`", "touch .claude/allow-main",
		"commit-test-gate", "--no-verify",
	} {
		if strings.Contains(part, tok) {
			t.Errorf("project part of AGENTS.md repeats core-block token %q", tok)
		}
	}
}

// A production change that fails this test: AGENTS.md loses a rule that only lighthouse itself
// has (Dutch artefacts, the allow-main rule, links, the planning folders) or regrows a Bootstrap section.
func TestProjectPartKeepsTheLighthouseRules(t *testing.T) {
	part := projectPart(t)
	requireTokens(t, part,
		"Dutch", ".claude/allow-main", "relative link",
		"docs/developer/planning/specs/", "docs/developer/planning/plans/")
	if strings.Contains(part, "## Bootstrap") {
		t.Error("project part of AGENTS.md has a Bootstrap section")
	}
}

// A production change that fails this test: AGENTS.md gets the same heading twice, which
// splits one section in two.
func TestAgentsMDHeadingsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, line := range strings.Split(read(t, "AGENTS.md"), "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			t.Errorf("duplicate heading %q", line)
		}
		seen[line] = true
	}
}
