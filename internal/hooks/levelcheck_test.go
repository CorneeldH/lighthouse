package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/levels"
)

func levelRoot(t *testing.T, components string) string {
	t.Helper()
	root := t.TempDir()
	if components != "" {
		writeFile(t, filepath.Join(root, levels.InstalledPath), components)
	}
	return root
}

func runLevelCheck(root string) hook.Result {
	return LevelCheck(context.Background(), root, "", hook.Payload{})
}

func contextOf(t *testing.T, res hook.Result) string {
	t.Helper()
	spec, _ := res.Output["hookSpecificOutput"].(map[string]any)
	if spec["hookEventName"] != "SessionStart" {
		t.Fatalf("hookEventName = %v, want SessionStart", spec["hookEventName"])
	}
	text, _ := spec["additionalContext"].(string)
	return text
}

// Red when LevelCheck warns about a gate that is on, or about a repo without components.toml.
func TestLevelCheckSilentWhenGatesOn(t *testing.T) {
	for name, file := range map[string]string{
		"no file":   "",
		"all on":    "level = \"strict\"\nsince = \"x\"\n",
		"other off": "level = \"light\"\n[components]\n\"commit-test-gate\" = \"off\"\n",
	} {
		res := runLevelCheck(levelRoot(t, file))
		if res.Code != 0 || len(res.Output) != 0 || res.Stderr != "" {
			t.Errorf("%s: want empty result, got %+v", name, res)
		}
	}
}

// Red when LevelCheck drops a core gate that is not on, or words the line differently from init
// (levels.CoreGateWarning), or leaves out one of the two output fields.
func TestLevelCheckWarnsForEachUnderstoodGate(t *testing.T) {
	root := levelRoot(t, "level = \"light\"\n[components]\n\"main-commit-gate\" = \"off\"\n\"pr-create-gate\" = \"off\"\n\"commit-test-gate\" = \"off\"\n")
	res := runLevelCheck(root)
	want := strings.Join([]string{
		levels.CoreGateWarning("main-commit-gate", levels.StandOff),
		levels.CoreGateWarning("pr-create-gate", levels.StandOff),
	}, "\n")
	if res.Code != 0 {
		t.Fatalf("code = %d, want 0", res.Code)
	}
	if got := res.Output["systemMessage"]; got != want {
		t.Errorf("systemMessage = %q, want %q", got, want)
	}
	if got := contextOf(t, res); got != want {
		t.Errorf("additionalContext = %q, want %q", got, want)
	}
	if !strings.Contains(want, "lighthouse: main-commit-gate is off — this repo works outside the chain") {
		t.Errorf("wording drifted: %q", want)
	}
}

// Red when LevelCheck treats stand log as on: a gate on log does not stop anything.
func TestLevelCheckLogCountsAsNotOn(t *testing.T) {
	res := runLevelCheck(levelRoot(t, "[components]\n\"worktree-check\" = \"log\"\n"))
	want := levels.CoreGateWarning("worktree-check", levels.StandLog)
	if got := res.Output["systemMessage"]; got != want {
		t.Errorf("systemMessage = %q, want %q", got, want)
	}
}

// Red when LevelCheck blocks, or stays silent, on a components.toml it cannot read.
func TestLevelCheckUnreadableFileSaysCouldNotCheckAndExitsZero(t *testing.T) {
	res := runLevelCheck(levelRoot(t, "this is = not [toml"))
	msg, _ := res.Output["systemMessage"].(string)
	if res.Code != 0 || !strings.HasPrefix(msg, "level-check: COULD NOT CHECK") {
		t.Errorf("want code 0 and a COULD NOT CHECK systemMessage, got %+v", res)
	}
}

// Red when level-check is no base hook (hook.Run then exits 3 on a corrupt file) or is not
// registered under that name.
func TestLevelCheckThroughHookRunIgnoresCorruptFile(t *testing.T) {
	root := levelRoot(t, "corrupt = = =")
	_, _, lerr := levels.ReadInstalled(root)
	if lerr == nil {
		t.Fatal("fixture must be unreadable")
	}
	var out, errOut bytes.Buffer
	code := hook.Run(context.Background(), "level-check", "", strings.NewReader("{}"), &out, &errOut,
		hook.Env{Root: root, LevelErr: lerr})
	if code != 0 {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout %q: %v", out.String(), err)
	}
	if msg, _ := got["systemMessage"].(string); !strings.Contains(msg, "COULD NOT CHECK") {
		t.Errorf("systemMessage = %q", msg)
	}
}

// Red when pack/core/hooks/hooks.json stops registering level-check on SessionStart, or
// registers it as a blocking guard.
func TestLevelCheckRegisteredOnSessionStart(t *testing.T) {
	data, err := os.ReadFile("../../pack/core/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	for _, g := range f.Hooks["SessionStart"] {
		for _, h := range g.Hooks {
			if strings.HasSuffix(h.Command, "/hooks/run.sh level-check") {
				return
			}
		}
	}
	t.Fatal("level-check is not registered under SessionStart")
}
