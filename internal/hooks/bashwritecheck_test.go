package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBashWriteCheckPreWritesTheMarkerAndStaysSilent(t *testing.T) {
	dir := repo(t, "main")
	xdg := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	t.Setenv("LIGHTHOUSE_BASH_GATES_START_NS", "")

	res := call(t, BashWriteCheck, "pre", map[string]any{
		"session_id": "sess-go", "tool_use_id": "toolu_go", "hook_event_name": "PreToolUse",
		"cwd": dir, "tool_input": map[string]any{"command": "true"},
	})

	if res.Code != 0 || res.Stderr != "" || res.Output != nil {
		t.Fatalf("got code %d, stderr %q, output %v; want 0 and nothing else", res.Code, res.Stderr, res.Output)
	}
	if _, err := os.Stat(filepath.Join(xdg, "lighthouse-bash-gates", "sess-go", "toolu_go.json")); err != nil {
		t.Fatalf("pre wrote no marker: %v", err)
	}
}

func TestBashWriteCheckPreNeverBlocks(t *testing.T) {
	res := call(t, BashWriteCheck, "pre", "[]")

	if res.Code != 0 || res.Stderr != "" || res.Output != nil {
		t.Fatalf("got code %d, stderr %q, output %v; want 0 and nothing else", res.Code, res.Stderr, res.Output)
	}
}

func TestBashWriteCheckPostPassesTheVerdictOfPost(t *testing.T) {
	const want = "bash-write-check: COULD NOT CHECK: the hook payload on stdin is not a JSON object"

	res := call(t, BashWriteCheck, "post", "[]")

	if res.Code != 2 || res.Stderr != want+"\n" {
		t.Fatalf("got code %d, stderr %q; want 2 and %q", res.Code, res.Stderr, want+"\n")
	}
	out, _ := res.Output["hookSpecificOutput"].(map[string]any)
	if out["hookEventName"] != "PostToolUse" || out["additionalContext"] != want {
		t.Fatalf("got output %v; want PostToolUse with %q", res.Output, want)
	}
}

func TestBashWriteCheckUnknownModeIsLoud(t *testing.T) {
	const want = "bash-write-check: COULD NOT CHECK — unknown mode, expected pre or post"
	cases := []struct {
		name, payload, event string
	}{
		{"event of the payload", `{"hook_event_name": "PostToolUseFailure", "cwd": "/x"}`, "PostToolUseFailure"},
		{"no event", `{"cwd": "/x"}`, "PostToolUse"},
		{"not JSON", `kapot "hook_event_name":"PreToolUse"`, "PreToolUse"},
		{"first line that names one", "{\n\"cwd\": \"/x\",\n\"hook_event_name\": \"Stop\",\n\"x\": {\"hook_event_name\": \"Other\"}\n}", "Stop"},
		{"last on its line", `{"hook_event_name": "A", "x": {"hook_event_name": "B"}}`, "B"},
		{"an empty first one", "{\"hook_event_name\": \"\",\n\"hook_event_name\": \"Stop\"}", "PostToolUse"},
		{"not only letters", `{"hook_event_name": "Post-Tool"}`, "PostToolUse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t, BashWriteCheck, "foo", tc.payload)

			if res.Code != 2 || res.Stderr != want+"\n" {
				t.Fatalf("got code %d, stderr %q; want 2 and %q", res.Code, res.Stderr, want+"\n")
			}
			out, _ := res.Output["hookSpecificOutput"].(map[string]any)
			if out["hookEventName"] != tc.event || out["additionalContext"] != want {
				t.Fatalf("got output %v; want event %q with %q", res.Output, tc.event, want)
			}
		})
	}
}
