package hooks

import (
	"context"
	"os"
	"path"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/automemory"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

// memoryDenied is the reason of a deny.
func memoryDenied(target string) string {
	return "memory-gate: auto-memory write blocked (" + target + ").\n" +
		"AGENTS.md forbids harness memory: it lives on one machine and nobody reviews it.\n" +
		"Put a lasting rule in the repo with /lighthouse-place-instruction; move existing memory with /lighthouse-migrate-memory.\n"
}

// MemoryGate denies a write into a Claude Code auto-memory directory: an Edit or Write of such a
// file, or a Bash command whose redirect or file argument is one. It denies through
// permissionDecision with exit 0, not exit 2, so it is no blocking hook. Reading stays free. The stands
// `log` and `off` are hook.Run's.
func MemoryGate(_ context.Context, _, _ string, p hook.Payload) hook.Result {
	top, ok := hook.JQFields(p.Raw)
	if !ok {
		return hook.CouldNotCheck("memory-gate: COULD NOT CHECK — the payload is not valid JSON.\n")
	}
	var targets []string
	var cwd string
	if hook.JQText(top["tool_name"]) == "Bash" {
		command, c, ok := readGatePayload(p.Raw)
		if !ok {
			return hook.CouldNotCheck("memory-gate: COULD NOT CHECK — the payload is not valid JSON.\n")
		}
		cwd = c
		targets = automemory.WriteTargets(command)
	} else {
		filePath, c, ok := readWritePayload(p.Raw)
		if !ok {
			return hook.CouldNotCheck("memory-gate: COULD NOT CHECK — the payload is not valid JSON.\n")
		}
		cwd = c
		if filePath != "" {
			targets = []string{filePath}
		}
	}

	// Without a home only the `.claude/projects` pattern applies.
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	configDir := automemory.ConfigDir(os.Getenv, home)
	for _, target := range targets {
		if automemory.IsMemoryPath(absoluteTarget(target, cwd), home, configDir) {
			return hook.Result{Output: map[string]any{"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": memoryDenied(target),
			}}}
		}
	}
	return hook.Result{}
}

// absoluteTarget puts cwd before a relative target. A target that starts with `~` or `$HOME` is
// absolute: automemory.Normalize expands it.
func absoluteTarget(target, cwd string) string {
	if cwd == "" || isAbsPath(target) || strings.HasPrefix(target, "~") ||
		strings.HasPrefix(target, "$HOME") || strings.HasPrefix(target, "${HOME}") {
		return target
	}
	return path.Clean(strings.ReplaceAll(cwd, `\`, "/") + "/" + strings.ReplaceAll(target, `\`, "/"))
}
