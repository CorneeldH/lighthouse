package commitgate

import (
	"context"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

// InvalidPayload is the block for a payload jq could not read.
const InvalidPayload = HookName + ": COULD NOT CHECK — the payload is not valid JSON.\n" +
	"\n" +
	"Without the command nothing was tested; that is not a green light.\n"

// ReadPayload reads `.tool_input.command` and `.cwd` the way `jq -r '.x // empty'` does. ok is
// false when jq could not index the payload.
func ReadPayload(raw []byte) (command, cwd string, ok bool) {
	top, ok := hook.JQFields(raw)
	if !ok {
		return "", "", false
	}
	toolInput, ok := hook.JQFields(top["tool_input"])
	if !ok {
		return "", "", false
	}
	return hook.JQText(toolInput["command"]), hook.JQText(top["cwd"]), true
}

// mayCommit is the two-step rule on the raw text: `git` and `commit` both in it.
func mayCommit(command string) bool {
	return strings.Contains(command, "git") && strings.Contains(command, "commit")
}

// payloadField is `jq -r '.<key> // empty'` on the top level of raw.
func payloadField(raw []byte, key string) string {
	top, _ := hook.JQFields(raw)
	return hook.JQText(top[key])
}

// Post tests what the commits of this Bash call touched: per root that pre recorded and whose
// HEAD moved, it runs the command with LIGHTHOUSE_GATE_BASE, _HEAD and _FILES. Without such a root
// (nothing recorded, or nothing committed) it is silent, before configuration, slot or brake.
// MovedRoots reads and removes the record, so a repeated post tests nothing.
func Post(_ context.Context, g *Gate, raw []byte) Result {
	command, _, ok := ReadPayload(raw)
	if !ok {
		return Result{Code: 2, Stderr: InvalidPayload}
	}
	if command == "" {
		return Result{}
	}
	moved, err := MovedRoots(raw)
	if err != nil {
		// The two-step rule: without `git` and `commit` in the raw text pre recorded nothing, so
		// a state dir that cannot be read does not block every Bash call. Only on this failure
		// path: `git "com"mit` lacks the word but is a commit.
		if !mayCommit(command) {
			return Result{}
		}
		return g.Block("could not read the recorded HEAD: " + err.Error())
	}
	if len(moved) == 0 {
		return Result{}
	}
	rows := make([]Row, 0, len(moved))
	for _, m := range moved {
		rows = append(rows, Row(m))
	}
	// PostToolUseFailure too: `git commit -m x && false` fails the call, but the commit is there.
	event := "PostToolUse"
	if payloadField(raw, "hook_event_name") == "PostToolUseFailure" {
		event = "PostToolUseFailure"
	}
	return g.RunRoots(rows, event)
}
