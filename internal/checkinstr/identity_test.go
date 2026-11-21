package checkinstr

import "testing"

// Checks 1, 2 and 3: duplicate name, byte-identical pair within a root, the mirror of directory
// names, and the ok/SKIPPED/summary lines around them.
// Class: cli-output. At risk: which skill tree check-instructions calls red or green.
func TestIdentityChecks(t *testing.T) {
	retro := "---\nname: paia-retro-gedeeld\ndescription: Retro.\n---\n"
	cases := []struct {
		name  string
		files tree
		want  verdict
	}{
		{
			"empty tree is green",
			emptySkillRoots(),
			verdict{code: 0, lines: []string{
				"ok    no names claimed twice",
				"ok    no byte-identical pairs within a skill root",
				"ok    mirror .claude/skills <-> .agents/skills matches",
			}},
		},
		{
			"duplicate name",
			emptySkillRoots().with(
				mirrored("paia-een", skillText("paia-een", "Eerste ding.", nil, "# paia-een\n")),
				mirrored("paia-twee", skillText("paia-een", "Tweede heel ander ding.", nil, "# paia-twee\n")),
			),
			verdict{code: 1, lines: []string{"FAIL  duplicate name 'paia-een' claimed by: paia-een paia-twee "}},
		},
		{
			"byte-identical pair",
			emptySkillRoots().with(mirrored("paia-retro", retro), mirrored("paia_retro", retro)),
			verdict{code: 1, lines: []string{
				"FAIL  byte-identical pair in .claude/skills: paia-retro paia_retro ",
				"FAIL  byte-identical pair in .agents/skills: paia-retro paia_retro ",
			}},
		},
		{
			"mirror break, only in .claude",
			emptySkillRoots().with(tree{".claude/skills/paia-alleen-links/SKILL.md": skillText("paia-alleen-links", "Doet iets.", nil, "")}),
			verdict{code: 1, lines: []string{
				"FAIL  mirror missing: 'paia-alleen-links' is not in both .claude/skills/ and .agents/skills/",
			}},
		},
		{
			"mirror break, only in .agents",
			emptySkillRoots().with(tree{".agents/skills/paia-alleen-rechts/SKILL.md": skillText("paia-alleen-rechts", "Doet iets.", nil, "")}),
			verdict{code: 1, lines: []string{
				"FAIL  mirror missing: 'paia-alleen-rechts' is not in both .claude/skills/ and .agents/skills/",
			}},
		},
		{
			"a verbatim mirror is not a duplicate",
			emptySkillRoots().with(mirrored("paia-gespiegeld", skillText("paia-gespiegeld", "Doet iets.", nil, "# x\n"))),
			verdict{code: 0, lines: []string{
				"ok    no names claimed twice",
				"ok    no byte-identical pairs within a skill root",
			}},
		},
		{
			"without a skills directory checks 1 and 2 say SKIPPED",
			tree{},
			verdict{code: 0,
				lines: []string{
					"SKIPPED  duplicate names: no .claude/skills, so there is nothing to measure here",
					"SKIPPED  byte-identical: no .claude/skills, so there is nothing to measure here",
				},
				absent: []string{"ok    no names claimed twice", "ok    no byte-identical pairs within a skill root"}},
		},
		{
			"the summary counts the findings",
			emptySkillRoots().with(tree{".claude/skills/paia-alleen-links/SKILL.md": skillText("paia-alleen-links", "Doet iets.", nil, "")}),
			verdict{code: 1, lines: []string{"1 finding(s)."}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, tc.files, runOptions{}), tc.want)
		})
	}
}
