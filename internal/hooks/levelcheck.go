package hooks

import (
	"context"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/levels"
)

func init() { hook.Register("level-check", LevelCheck) }

// LevelCheck is the SessionStart hook that names every core gate whose stand is not on, so a
// session knows from its first moment that this repo works outside the chain. It never blocks
// (code 0), so it has no guards.toml entry. It is a base hook: it has no stand of its own and
// reads components.toml itself, because hook.Run ignores a corrupt file for it. Stand log
// counts as not on: a gate on log does not stop anything. An unreadable file is reported as
// COULD NOT CHECK in the systemMessage, still with code 0.
func LevelCheck(_ context.Context, root string, _ string, _ hook.Payload) hook.Result {
	installed, _, err := levels.ReadInstalled(root)
	if err != nil {
		return hook.Result{Output: map[string]any{
			"systemMessage": "level-check: COULD NOT CHECK — " + strings.ReplaceAll(err.Error(), "\n", " "),
		}}
	}
	var lines []string
	for _, gate := range levels.CoreGates {
		if st := installed.Stand(gate); st != levels.StandOn {
			lines = append(lines, levels.CoreGateWarning(gate, st))
		}
	}
	if len(lines) == 0 {
		return hook.Result{}
	}
	text := strings.Join(lines, "\n")
	return hook.Result{Output: map[string]any{
		"systemMessage": text,
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": text,
		},
	}}
}
