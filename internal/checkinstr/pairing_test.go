package checkinstr

import "testing"

// Check 8: a skill with `binding: hard` names a hook that exists and is registered in the `hooks`
// block of settings.json.
// Class: cli-output. At risk: which hard skills check-instructions calls enforced.
func TestPairingCheck(t *testing.T) {
	strict := []string{"binding: hard", "origin: own"}
	hardSkill := func(body string) tree {
		return mirrored("paia-streng", skillText("paia-streng", "Doet iets.", strict, body))
	}
	const naming = "De afdwinging zit in `.claude/hooks/streng.sh`.\n"
	script := tree{".claude/hooks/streng.sh": "#!/usr/bin/env bash\nexit 0\n"}
	settingsFor := func(command string) tree {
		return tree{".claude/settings.json": `{"hooks":{"PostToolUse":[{"matcher":"Edit",` +
			`"hooks":[{"type":"command","command":"` + command + `"}]}]}}`}
	}
	const (
		counterpart = "FAIL  pairing: 'paia-streng' carries 'binding: hard' but names no counterpart that exists and is registered in settings.json"
		green       = "ok    every binding: hard has a registered hook"
		jsonCause   = "FAIL  pairing: '{root}/.claude/settings.json' is not valid JSON (or not an object), so 'paia-streng' cannot be checked for a registered hook"
	)
	registered := "$CLAUDE_PROJECT_DIR/.claude/hooks/streng.sh"
	cases := []struct {
		name  string
		files tree
		want  verdict
	}{
		{"no hook named",
			emptySkillRoots().with(hardSkill("Deze skill noemt geen enkele hook.\n")),
			verdict{code: 1, lines: []string{counterpart}}},
		{"a registered hook",
			emptySkillRoots().with(hardSkill(naming), script, settingsFor(registered)),
			verdict{code: 0, lines: []string{green}}},
		{"a registered hook in a subdirectory",
			emptySkillRoots().with(
				mirrored("paia-streng", skillText("paia-streng", "Doet iets.", strict, "De afdwinging zit in `.claude/hooks/lighthouse/streng-check.sh`.\n")),
				tree{".claude/hooks/lighthouse/streng-check.sh": "#!/usr/bin/env bash\nexit 0\n"},
				settingsFor("$CLAUDE_PROJECT_DIR/.claude/hooks/lighthouse/streng-check.sh")),
			verdict{code: 0, lines: []string{green}}},
		{"a hook named but not registered",
			emptySkillRoots().with(hardSkill(naming), script, tree{".claude/settings.json": `{"hooks":{}}`}),
			verdict{code: 1, lines: []string{counterpart}}},
		{"a hook registered but its script missing",
			emptySkillRoots().with(hardSkill(naming), settingsFor(registered)),
			verdict{code: 1, lines: []string{counterpart}}},
		// As a bare string in the deny list the path can never match a `command` field; as an
		// object with a command it can, so only the `hooks` block guard keeps it out.
		{"a command outside the hooks block is no registration",
			emptySkillRoots().with(hardSkill(naming), script,
				tree{".claude/settings.json": `{"hooks":{},"permissions":{"deny":[{"command":".claude/hooks/streng.sh"}]}}`}),
			verdict{code: 1, lines: []string{counterpart}}},
		{"a .disabled variant is no registration",
			emptySkillRoots().with(hardSkill(naming), script, settingsFor(registered+".disabled")),
			verdict{code: 1, lines: []string{counterpart}}},
		{"settings.json that is no object",
			emptySkillRoots().with(hardSkill(naming), script, tree{".claude/settings.json": "[1, 2, 3]"}),
			verdict{code: 1, lines: []string{jsonCause}, absent: []string{counterpart}}},
		{"settings.json that is not JSON",
			emptySkillRoots().with(hardSkill(naming), script, tree{".claude/settings.json": "dit is geen json {"}),
			verdict{code: 1, lines: []string{jsonCause}, absent: []string{counterpart}}},
		{"settings.json that is empty",
			emptySkillRoots().with(hardSkill(naming), script, tree{".claude/settings.json": ""}),
			verdict{code: 1, lines: []string{jsonCause}, absent: []string{counterpart}}},
		{"missing settings.json still fails a skill without a hook path, and says SKIPPED for the other",
			emptySkillRoots().with(
				mirrored("paia-aaa", skillText("paia-aaa", "Doet iets.", strict, "Zie `.claude/hooks/streng.sh`.\n")),
				mirrored("paia-zzz", skillText("paia-zzz", "Doet iets anders.", strict, "Deze skill noemt geen enkele hook.\n"))),
			verdict{code: 1, lines: []string{
				"FAIL  pairing: 'paia-zzz' carries 'binding: hard' but names no counterpart that exists and is registered in settings.json",
				"SKIPPED  pairing: '{root}/.claude/settings.json' does not exist, so registration cannot be checked here",
			}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, tc.files, runOptions{}), tc.want)
		})
	}
}
