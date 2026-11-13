package hooks

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

const unknownMode = "bash-write-check: COULD NOT CHECK — unknown mode, expected pre or post"

// loudEvent matches the event the way the shell wrapper read it without a JSON parser: per line,
// the last `"hook_event_name": "<letters>"` on that line.
var loudEvent = regexp.MustCompile(`.*"hook_event_name" *: *"([A-Za-z]*)"`)

// BashWriteCheck lets the Edit|Write checks also see what a Bash call wrote. pre (PreToolUse)
// writes the marker and seeds the ledger, and never blocks: an exit 2 there would block the Bash
// call itself. post (PostToolUse and PostToolUseFailure) sends the files the call wrote through
// their gates and gives exit 0 or 2. root is the lighthouse root whose hook directory holds the
// gates.
func BashWriteCheck(_ context.Context, root, stand string, p hook.Payload) hook.Result {
	switch stand {
	case "pre":
		bashwrites.Pre(p.Raw, time.Now)
		return hook.Result{}
	case "post":
		code, output, stderr := bashwrites.Post(p.Raw, root)
		return hook.Result{Code: code, Output: output, Stderr: stderr}
	}
	return hook.Result{
		Code:   2,
		Stderr: unknownMode + "\n",
		Output: map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":     eventOf(p.Raw),
			"additionalContext": unknownMode,
		}},
	}
}

// eventOf is the event of the first line that names one, PostToolUse when none does or the first
// that does names an empty one. It reads the raw text, so a payload that is not JSON still has one.
func eventOf(raw []byte) string {
	for _, line := range strings.Split(string(raw), "\n") {
		if m := loudEvent.FindStringSubmatch(line); m != nil {
			if m[1] == "" {
				break
			}
			return m[1]
		}
	}
	return "PostToolUse"
}
