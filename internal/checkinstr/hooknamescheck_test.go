package checkinstr

import (
	"encoding/json"
	"testing"
)

// hookSettings is a settings.json with one lighthouse hook per script, on the given events. A
// script is the part after `.claude/hooks/lighthouse/`, so "run.sh main-commit-gate" is run.sh
// with its argument.
func hookSettings(t *testing.T, hooks map[string][]string) tree {
	t.Helper()
	events := map[string][]any{}
	for script, on := range hooks {
		for _, event := range on {
			events[event] = append(events[event], map[string]any{"hooks": []any{map[string]any{
				"type":    "command",
				"command": `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/` + script + " " + event,
			}}})
		}
	}
	data, err := json.Marshal(map[string]any{"hooks": events})
	if err != nil {
		t.Fatal(err)
	}
	return tree{".claude/settings.json": string(data)}
}

// Check 14: a lighthouse hook is named after what it does, on the events that name allows.
// Class: cli-output. At risk: a hook whose name promises more or less than its registration does.
func TestHookNamesCheck(t *testing.T) {
	const ok = "ok    every lighthouse hook is named after what it does"
	t.Run("named after what they do", func(t *testing.T) {
		files := hookSettings(t, map[string][]string{
			"run.sh main-commit-gate": {"PreToolUse"},
			"md-link-check.sh":        {"PostToolUse"},
			"go-lint-gate.sh":         {"Stop"},
		})
		assertVerdict(t, check(t, files, runOptions{}), verdict{code: 0, lines: []string{ok}})
	})

	cases := []struct {
		name  string
		hooks map[string][]string
		want  verdict
	}{
		{"a run.sh gate only after the fact", map[string][]string{"run.sh foo-gate": {"PostToolUse"}},
			verdict{code: 1, lines: []string{"FAIL  hook-names: 'foo-gate' is a gate but no registration is on PreToolUse, Stop or SubagentStop"}}},
		{"a script name without a suffix", map[string][]string{"worktree-guard.sh": {"PreToolUse"}},
			verdict{code: 1, lines: []string{"FAIL  hook-names: 'worktree-guard' ends on neither -gate nor -check"}}},
		{"the name behind run.sh is its argument", map[string][]string{"run.sh worktree-guard": {"PreToolUse"}},
			verdict{code: 1,
				lines:  []string{"FAIL  hook-names: 'worktree-guard' ends on neither -gate nor -check"},
				absent: []string{"FAIL  hook-names: 'run' ends on neither -gate nor -check"}}},
		{"a gate only after the fact", map[string][]string{"foo-gate.sh": {"PostToolUse"}},
			verdict{code: 1, lines: []string{"FAIL  hook-names: 'foo-gate' is a gate but no registration is on PreToolUse, Stop or SubagentStop"}}},
		{"a check on Stop", map[string][]string{"foo-check.sh": {"Stop"}},
			verdict{code: 1, lines: []string{"FAIL  hook-names: 'foo-check' is a check but is registered on Stop or SubagentStop"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, hookSettings(t, tc.hooks), runOptions{}), tc.want)
		})
	}

	t.Run("project hooks are not judged", func(t *testing.T) {
		files := tree{".claude/settings.json": `{"hooks":{` +
			`"PreToolUse":[{"hooks":[{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh main-commit-gate PreToolUse"}]}],` +
			`"PostToolUse":[{"hooks":[` +
			`{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/own.sh"},` +
			`{"type":"command","command":"npx prettier --write"}]}]}}`}
		assertVerdict(t, check(t, files, runOptions{}), verdict{code: 0, lines: []string{ok}})
	})
	t.Run("a broken settings.json", func(t *testing.T) {
		r := check(t, tree{".claude/settings.json": "{not json"}, runOptions{})
		assertVerdict(t, r, verdict{code: 1, lines: []string{
			"FAIL  hook-names: '{root}/.claude/settings.json' is not valid JSON (or not an object)"}})
	})
	t.Run("no settings.json is SKIPPED", func(t *testing.T) {
		r := check(t, tree{}, runOptions{})
		assertVerdict(t, r, verdict{code: 0,
			lines:  []string{"SKIPPED  hook-names: '{root}/.claude/settings.json' does not exist, so hook names cannot be checked here"},
			absent: []string{ok}})
	})
}
