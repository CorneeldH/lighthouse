package checkinstr

import (
	"path/filepath"
	"testing"
)

// Check 5: a personal skill with the name of a project skill wins silently.
// Class: cli-output. At risk: a shadowed skill, or an absent personal directory, read as fine.
func TestShadowCheck(t *testing.T) {
	files := emptySkillRoots().with(mirrored("paia-dubbel", skillText("paia-dubbel", "Doet iets.", nil, "# x\n")))
	t.Run("a shadowing personal skill", func(t *testing.T) {
		r := check(t, files, runOptions{personalSkills: []string{"paia-dubbel"}})
		assertVerdict(t, r, verdict{code: 1, lines: []string{
			"FAIL  shadow: 'paia-dubbel' is in {personal} and in .claude/skills/; the personal version wins",
		}})
	})
	t.Run("no personal directory cannot be measured", func(t *testing.T) {
		r := check(t, emptySkillRoots(), runOptions{personal: filepath.Join(t.TempDir(), "bestaat-niet")})
		assertVerdict(t, r, verdict{code: 0,
			lines:  []string{"SKIPPED  shadow: '{personal}' does not exist on this machine, so there is nothing to measure here"},
			absent: []string{"ok    no personal skill shadows a project skill"}})
	})
}

// Check 7: a rule without a non-empty `paths:` in its frontmatter loads every session.
// Class: cli-output. At risk: which rule files check-instructions calls path-scoped.
func TestScopingCheck(t *testing.T) {
	rule := func(name, text string) tree { return tree{".claude/rules/" + name: text} }
	framed := func(front string) string { return "---\n" + front + "\n---\n\ntekst\n" }
	const scoped = "ok    every rule is path-scoped"
	failFor := func(name string) string {
		return "FAIL  scoping: '" + name + "' has no paths: block with a non-empty value in its frontmatter"
	}
	cases := []struct {
		name  string
		files tree
		want  verdict
	}{
		{"a rule without paths", rule("los.md", framed("description: iets")), verdict{code: 1, lines: []string{failFor("los.md")}}},
		{"a rule with paths", rule("goed.md", framed("description: iets\npaths:\n  - src/**")), verdict{code: 0, lines: []string{scoped}}},
		{"no rules directory is SKIPPED", tree{},
			verdict{code: 0, lines: []string{"SKIPPED  scoping: '{root}/.claude/rules' does not exist, so there is no rule to check here"}, absent: []string{scoped}}},
		{"a bare paths without a value", rule("kaal.md", framed("description: iets\npaths:")), verdict{code: 1, lines: []string{failFor("kaal.md")}}},
		{"an empty paths list", rule("leeg.md", framed("description: iets\npaths: []")), verdict{code: 1, lines: []string{failFor("leeg.md")}}},
		{"the word paths mid-line", rule("vals.md", framed("description: zie ook paths: elders in de tekst")), verdict{code: 1, lines: []string{failFor("vals.md")}}},
		{"paths only in the body", rule("vals2.md", "---\ndescription: iets\n---\n\npaths:\n  - src/**\n"), verdict{code: 1, lines: []string{failFor("vals2.md")}}},
		{"an empty line before the list", rule("leegregel.md", framed("description: iets\npaths:\n\n  - src/**")), verdict{code: 0, lines: []string{scoped}}},
		{"a YAML comment before the list", rule("comment.md", framed("description: iets\npaths:\n  # de twee mappen\n  - src/**")), verdict{code: 0, lines: []string{scoped}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, tc.files, runOptions{}), tc.want)
		})
	}
}
