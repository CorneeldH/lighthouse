//go:build canary

package canary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

func TestCoreCommandCanaries(t *testing.T) {
	for id, c := range commandCases {
		t.Run(id, func(t *testing.T) {
			root := installRepo(t, "core")
			if strings.Contains(root, id) {
				t.Fatalf("the reason ID %q is in the repository path %s", id, root)
			}
			env := c.setup(t, root)
			data, err := os.ReadFile(filepath.Join("testdata", c.hook, id+".cmd"))
			if err != nil {
				t.Fatal(err)
			}
			command := fixturesetup.Fill(t, strings.TrimSpace(string(data)), root)
			payload, err := json.Marshal(map[string]any{
				"cwd": root, "hook_event_name": "PreToolUse", "tool_name": "Bash",
				"tool_input": map[string]any{"command": command},
			})
			if err != nil {
				t.Fatal(err)
			}
			r := runHook(t, root, c.script, string(payload), env...)
			requireCanary(t, r, id, "["+c.hook+"/"+id+"]")
		})
	}
}

func TestCoreFileCanaries(t *testing.T) {
	for id, c := range fileCases {
		t.Run(id, func(t *testing.T) {
			root := installRepo(t, "core")
			if c.setup != nil {
				c.setup(t, root)
			}
			sources, err := filepath.Glob(filepath.Join("testdata", c.hook, id+".*"))
			if err != nil || len(sources) != 1 {
				t.Fatalf("testdata for %s/%s: %v, %v", c.hook, id, sources, err)
			}
			data, err := os.ReadFile(sources[0])
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "canary"+filepath.Ext(sources[0]))
			if strings.Contains(file, id) {
				t.Fatalf("the reason ID %q is in the path %s", id, file)
			}
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]any{
				"cwd": root, "hook_event_name": "PostToolUse", "tool_name": "Write",
				"tool_input": map[string]any{"file_path": file},
			})
			if err != nil {
				t.Fatal(err)
			}
			r := runHook(t, root, c.script, string(payload))
			requireCanary(t, r, id, "["+c.hook+"/"+id+"]")
		})
	}
}
