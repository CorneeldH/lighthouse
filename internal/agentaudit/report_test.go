package agentaudit

import (
	"bytes"
	"strings"
	"testing"
)

// TestInventoryShortensLongCommand: a long inline command is cut to 80 characters in the
// inventory, a newline in it becomes a space, and a short one stays whole.
func TestInventoryShortensLongCommand(t *testing.T) {
	inv := &Inventory{Hooks: []Hook{
		{Source: "project", Event: "PreToolUse", Matcher: "Bash",
			Command: "jq -r '.tool_input.command' | grep -qE '(rm -rf|git push --force|git reset --hard)' && { echo blocked >&2; exit 2; }"},
		{Source: "project", Event: "Stop", Command: "echo één\n  exit 0"},
	}}
	var out bytes.Buffer
	(&report{out: &out}).inventory(inv)
	lines := strings.Split(out.String(), "\n")
	want := []string{
		"    project  PreToolUse  Bash  jq -r '.tool_input.command' | grep -qE '(rm -rf|git push --force|git reset --ha…  inline  niet blokkerend",
		"    project  Stop        *     echo één exit 0                                                                   inline  niet blokkerend",
	}
	if len(lines) < 4 || lines[2] != want[0] || lines[3] != want[1] {
		t.Errorf("hook rows:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}
