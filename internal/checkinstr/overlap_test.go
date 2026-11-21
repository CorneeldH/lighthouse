package checkinstr

import "testing"

// Check 4: two descriptions that share trigger words without one of them bounding itself.
// Class: cli-output. At risk: which pairs of descriptions check-instructions calls overlapping.
func TestOverlapChecks(t *testing.T) {
	const broad = "Gebruik bij het schrijven aanmaken bijwerken en opschonen van github " +
		"issues labels milestones binnen deze repository."
	pair := func(secondDescription string) tree {
		return emptySkillRoots().with(
			mirrored("paia-issue-een", skillText("paia-issue-een", broad, nil, "# een\n")),
			mirrored("paia-issue-twee", skillText("paia-issue-twee", secondDescription, nil, "# twee\n")),
		)
	}
	const fail = "FAIL  overlap without a bounding clause: 'paia-issue-een' and 'paia-issue-twee' share 9 trigger words"
	const ok = "ok    no unbounded overlap between descriptions"
	folded := "---\nname: paia-issue-twee\ndescription: >\n" +
		"  Gebruik bij het schrijven aanmaken bijwerken en opschonen van github\n" +
		"  issues labels milestones binnen deze repository.\n---\n\n# twee\n"
	stopped := func(stopwords string) tree {
		return emptySkillRoots().with(
			mirrored("paia-een", skillText("paia-een", stopwords+" appels.", nil, "# een\n")),
			mirrored("paia-twee", skillText("paia-twee", stopwords+" peren.", nil, "# twee\n")),
		)
	}
	cases := []struct {
		name  string
		files tree
		want  verdict
	}{
		{"overlapping descriptions without a clause", pair(broad), verdict{code: 1, lines: []string{fail}}},
		{"a bounding clause makes the overlap acceptable",
			pair(broad + " LET OP - gaat het om sluiten, gebruik paia-issue-een."),
			verdict{code: 0, lines: []string{ok}}},
		{"a scope word without another skill is no clause",
			pair(broad + " LET OP - twijfel je, gebruik dan deze skill."),
			verdict{code: 1, lines: []string{fail}}},
		{"an English not-for clause makes the overlap acceptable",
			pair(broad + " Not for closing - use the paia-issue-een skill for those."),
			verdict{code: 0, lines: []string{ok}}},
		{"not-for inside a longer word is no clause",
			pair(broad + " You cannot format paia-issue-een."),
			verdict{code: 1, lines: []string{fail}}},
		{"not-for ending inside a longer word is no clause",
			pair(broad + " Do not format paia-issue-een."),
			verdict{code: 1, lines: []string{fail}}},
		{"not-for starting inside a longer word is no clause",
			pair(broad + " You cannot for paia-issue-een."),
			verdict{code: 1, lines: []string{fail}}},
		{"a scope word inside a longer word is no clause",
			pair(broad + " Dit raakt ook de buitenwereld van paia-issue-een."),
			verdict{code: 1, lines: []string{fail}}},
		{"a folded description overlaps like a flat one",
			emptySkillRoots().with(
				mirrored("paia-issue-een", skillText("paia-issue-een", broad, nil, "# een\n")),
				tree{
					".claude/skills/paia-issue-twee/SKILL.md": folded,
					".agents/skills/paia-issue-twee/SKILL.md": folded,
				}),
			verdict{code: 1, lines: []string{fail}}},
		{"Dutch stopwords are no trigger words",
			stopped("gebruik wanneer welke worden binnen altijd"),
			verdict{code: 0, lines: []string{ok}}},
		{"English stopwords are no trigger words",
			stopped("about after before which where their"),
			verdict{code: 0, lines: []string{ok}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, tc.files, runOptions{}), tc.want)
		})
	}
}
