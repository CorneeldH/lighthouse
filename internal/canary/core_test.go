//go:build canary

package canary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

// controllerRun is a uuid-shaped LIGHTHOUSE_CONTROLLER_RUN: workflow-push-gate does nothing outside one.
const controllerRun = "LIGHTHOUSE_CONTROLLER_RUN=123e4567-e89b-42d3-a456-426614174000"

// commandCase is a canary of a Bash-command hook: testdata/<hook>/<id>.cmd is the command.
type commandCase struct {
	hook, script string
	setup        func(t *testing.T, root string) (env []string)
}

// addOrigin gives root a bare remote next to it as origin, with main pushed to it.
func addOrigin(t *testing.T, root string) {
	t.Helper()
	parent := filepath.Dir(root)
	fixturesetup.Build(t, parent, []map[string]any{
		{"bare": "origin.git"},
		{"origin": "repo", "url": filepath.Join(parent, "origin.git")},
	})
	fixturesetup.Git(t, root, "push", "-q", "origin", "main")
}

// writeConfig replaces lighthouse.toml of root with text: the canary needs only the keys it names.
func writeConfig(t *testing.T, root, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(text+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

var commandCases = map[string]commandCase{
	"commit-to-main": {"main-commit-gate", "run.sh main-commit-gate", func(*testing.T, string) []string { return nil }},
	"push-to-main": {"main-commit-gate", "run.sh main-commit-gate", func(t *testing.T, root string) []string {
		addOrigin(t, root)
		return nil
	}},
	"no-verify": {"commit-test-gate", "run.sh commit-test-gate pre", func(*testing.T, string) []string { return nil }},
	"tests-red": {"commit-test-gate", "run.sh commit-test-gate pre", func(t *testing.T, root string) []string {
		writeConfig(t, root, "[test_gate]\ncommand = \"false\"")
		return nil
	}},
	"pr-body": {"pr-create-gate", "run.sh pr-create-gate", func(t *testing.T, root string) []string {
		writeConfig(t, root, "[forge]\nkind = \"github\"\nrepo = \"example/canary\"")
		return nil
	}},
	"workflow-path": {"workflow-push-gate", "run.sh workflow-push-gate", func(t *testing.T, root string) []string {
		addOrigin(t, root)
		fixturesetup.Git(t, root, "switch", "-q", "-c", "topic")
		fixturesetup.Build(t, root, []map[string]any{
			{"file": ".github/workflows/ci.yml", "text": "on: push\n"},
			{"commit": ".", "message": "add workflow"},
		})
		return []string{controllerRun}
	}},
}

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

// fileCase is a canary of a file hook: testdata/<hook>/<id>.<ext> is the file, written as
// canary.<ext> into a repository whose path does not hold the ID.
type fileCase struct {
	hook, script string
	setup        func(t *testing.T, root string)
}

var fileCases = map[string]fileCase{
	"dead":     {"md-link-check", "run.sh md-link-check", nil},
	"absolute": {"md-link-check", "run.sh md-link-check", nil},
	"non-english": {"code-language-check", "run.sh code-language-check", func(t *testing.T, root string) {
		writeConfig(t, root, "[project]\ncode_language = \"en\"\ncode_language_paths = [\"**\"]")
	}},
}

// Every declared reason of a core hook has a canary case, and every case a declared reason.
func TestEveryCoreReasonHasACanaryCase(t *testing.T) {
	var cases, declared []string
	for id, c := range commandCases {
		cases = append(cases, c.hook+"/"+id)
	}
	for id, c := range fileCases {
		cases = append(cases, c.hook+"/"+id)
	}
	for _, name := range hook.DeclaredHooks() {
		if name == "bash-write-check" {
			continue
		}
		ids, ok := hook.Reasons(name)
		if !ok {
			t.Fatalf("%s is declared but has no reasons entry", name)
		}
		for _, id := range ids {
			declared = append(declared, name+"/"+id)
		}
	}
	slices.Sort(cases)
	slices.Sort(declared)
	if !slices.Equal(cases, declared) {
		t.Errorf("canary cases %v, want one per declared reason %v", cases, declared)
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
