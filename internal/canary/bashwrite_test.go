//go:build canary

package canary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
)

// gateCase is the canary of one gate of bash-write-check: source is copied to canary<ext> by a Bash
// command, and the block of that gate carries marker, the one of the gate's own canary.
type gateCase struct {
	ext, source, marker string
}

var gateCases = map[string]gateCase{
	"shell-lint-check.sh":      {".sh", "testdata/shell-lint-check/SC2164.sh", "SC2164 ("},
	"ruff-format-check.sh":     {".py", "../../tests/pack/python/testdata/ruff-format-check/E722.py", ": E722 "},
	"gofmt-check.sh":           {".go", "testdata/gofmt-check/parse-error.go", "[gofmt-check/parse-error]"},
	"powershell-lint-check.sh": {".ps1", "testdata/powershell-lint-check/PSAvoidUsingInvokeExpression.ps1", "[powershell-lint-check/PSAvoidUsingInvokeExpression]"},
	"md-link-check":            {".md", "testdata/md-link-check/dead.md", "[md-link-check/dead]"},
	"code-language-check.sh":   {".go", "testdata/code-language-check/non-english.go", "[code-language-check/non-english]"},
}

// bashWritePayload is a Bash payload of one tool call, the same for pre and post.
func bashWritePayload(t *testing.T, root, event, command string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"session_id": "canary", "tool_use_id": "toolu_canary", "cwd": root,
		"hook_event_name": event, "tool_name": "Bash",
		"tool_input": map[string]any{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestBashWriteCheckCanaryPerGate(t *testing.T) {
	for gate, c := range gateCases {
		t.Run(gate, func(t *testing.T) {
			root := installRepo(t, "core", "go", "python", "shell", "powershell")
			writeConfig(t, root, "[project]\ncode_language = \"en\"\ncode_language_paths = [\"**\"]")
			data, err := os.ReadFile(c.source)
			if err != nil {
				t.Fatal(err)
			}
			name := "canary" + c.ext
			if strings.Contains(root, gate) {
				t.Fatalf("the gate %q is in the repository path %s", gate, root)
			}
			env := []string{
				"CLAUDE_PROJECT_DIR=" + root,
				"SHELLCHECK_BIN=" + shellcheckBin(t),
				"XDG_RUNTIME_DIR=" + filepath.Join(filepath.Dir(root), "xdg-runtime"),
			}
			writeFile(t, filepath.Join(filepath.Dir(root), "xdg-runtime", ".keep"), "")
			command := "cp source " + name
			pre := runHook(t, root, "run.sh bash-write-check pre", bashWritePayload(t, root, "PreToolUse", command), env...)
			if pre.Code != 0 {
				t.Fatalf("pre: exit %d\nstderr: %s", pre.Code, pre.Stderr)
			}
			writeFile(t, filepath.Join(root, name), string(data))
			post := runHook(t, root, "run.sh bash-write-check post", bashWritePayload(t, root, "PostToolUse", command), env...)
			requireCanary(t, post, gate, c.marker)
			if !strings.Contains(post.Stderr, bashwrites.GateLabel(gate)) {
				t.Errorf("the block does not name the gate %s:\n%s", gate, post.Stderr)
			}
		})
	}
}

// A gate in bashwrites.Gates without a canary case fails here, and so does a case without a gate.
func TestBashWriteCheckGateSetHasNoRemainder(t *testing.T) {
	var gates, cases []string
	for _, list := range bashwrites.Gates {
		for _, g := range list {
			if !slices.Contains(gates, g) {
				gates = append(gates, g)
			}
		}
	}
	for g, c := range gateCases {
		cases = append(cases, g)
		if _, err := os.Stat(c.source); err != nil {
			t.Errorf("the fixture of %s: %v", g, err)
		}
	}
	slices.Sort(gates)
	slices.Sort(cases)
	if !slices.Equal(gates, cases) {
		t.Errorf("canary cases %v, want one per gate %v", cases, gates)
	}
}
