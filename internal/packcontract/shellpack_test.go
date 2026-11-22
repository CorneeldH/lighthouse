package packcontract

import (
	"strings"
	"testing"
)

// shell pack: what lives only in the text of the skill shell-standard (pack/shell has no
// instruction block since #390). Name, paths, links and English are covered generically (the
// triage of #368).

const (
	shellSkill  = "shell/skills/shell-standard"
	strictLook  = "lighthouse config shell.strict_dirs"
	shellDescOC = " In OpenCode the enforcing hook does not run; run the KPI commands by hand."
)

// shellParagraph returns the first paragraph for which keep is true.
func shellParagraph(t *testing.T, body string, keep func(string) bool) string {
	t.Helper()
	for _, p := range textParagraphs(body) {
		if keep(p) {
			return p
		}
	}
	t.Fatal("no such paragraph")
	return ""
}

// Classification: fails when the agents variant of shell-standard differs from the claude
// variant by more than the OpenCode note and description sentence, or the note loses its
// by-hand command and the strict_dirs lookup. Check 11 of check-instructions skips skills with
// lighthouse metadata, so nothing else compares the variants.
func TestShellStandardVariantsKeepTheirDifference(t *testing.T) {
	v := variants(t, shellSkill)
	claude, agents := v["claude"], v["agents"]
	wantNone(t, claude, "OpenCode")
	var notes []string
	for _, p := range textParagraphs(agents) {
		if strings.Contains(p, "OpenCode does not run") {
			notes = append(notes, p)
		}
	}
	if len(notes) != 1 {
		t.Fatalf("want one OpenCode note, got %d", len(notes))
	}
	wantAll(t, notes[0], ".lighthouse/scripts/shellcheck-repo.sh --severity warning -- <file>", strictLook)
	wantAll(t, frontmatter(t, agents), "In OpenCode the enforcing hook does not run")
	got := stripOpenCode(agents, shellDescOC)
	want := textParagraphs(claude)
	if len(got) != len(want) {
		t.Fatalf("agents without OpenCode has %d paragraphs, claude %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("paragraph %d differs:\nagents: %q\nclaude: %q", i, got[i], want[i])
		}
	}
}

// Classification: fails when the skill names a fixed directory instead of reading the strict
// directories with `lighthouse config shell.strict_dirs`.
func TestShellStandardStrictDirsFromConfig(t *testing.T) {
	for variant, body := range variants(t, shellSkill) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, strictLook)
			wantNone(t, body, "scripts/hooks", "tests/integration")
		})
	}
}

// Classification: fails when the skill's three-outcomes paragraph claims the codes for every
// gate script instead of only shellcheck-repo.sh, or loses the hook's exit 2 and
// COULD NOT CHECK line.
func TestShellStandardOutcomesMatchTheHook(t *testing.T) {
	for variant, body := range variants(t, shellSkill) {
		t.Run(variant, func(t *testing.T) {
			para := shellParagraph(t, body, func(p string) bool {
				return strings.HasPrefix(p, "**Three outcomes, not two.**")
			})
			wantAll(t, para, "`shellcheck-repo.sh`", "exit 2", "COULD NOT CHECK")
			wantNone(t, para, "Every gate script here returns")
		})
	}
}

// Classification: fails when the advice on the root of a worktree edit goes back to
// `$CLAUDE_PROJECT_DIR` (the hook says it is the wrong tree) or loses LIGHTHOUSE_REPO_ROOT and
// the path the hook received.
func TestShellStandardRootAdviceMatchesTheHook(t *testing.T) {
	for variant, body := range variants(t, shellSkill) {
		t.Run(variant, func(t *testing.T) {
			para := shellParagraph(t, body, func(p string) bool {
				return strings.Contains(p, "points at the wrong tree")
			})
			wantAll(t, para, "`LIGHTHOUSE_REPO_ROOT`", "path you received")
			wantNone(t, para, "Use `$CLAUDE_PROJECT_DIR`")
		})
	}
}

// Classification: fails when the skill grows a class table of its own or loses the pointer to
// lighthouse-test-standard and its classification sentence in the domain section.
func TestShellStandardPointsToTheTestingSkill(t *testing.T) {
	for variant, body := range variants(t, shellSkill) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, sectionUntilNext(t, body, "## Domain"), "lighthouse-test-standard",
				"*Classify first*", "Before the first edit",
				"Class: <class>. At risk: <the observable behaviour that could change>.")
			wantNone(t, body, "`pure-logic`")
		})
	}
}
