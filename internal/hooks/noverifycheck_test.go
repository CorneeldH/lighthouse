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

const noVerifyRefusal = "no-verify-check: --no-verify is not allowed in this repo.\n" +
	"\n" +
	"The test gate is a Claude Code PreToolUse hook, not a git hook, so the\n" +
	"flag skips nothing today — but it will as soon as git hooks are added.\n" +
	"If something fails: fix it. If it really has to be skipped, ask.\n"

func noVerifyPayload(command string) hook.Payload {
	raw, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"command": command}, "cwd": "/"})
	return hook.Payload{Raw: raw}
}

// Red when NoVerifyCheck judges by the branch (a feature branch passes) or words the refusal
// differently from the text commit-test-gate used to give.
func TestNoVerifyCheckBlocksOnAFeatureBranch(t *testing.T) {
	dir := repo(t, "feature")
	t.Chdir(dir)
	res := NoVerifyCheck(context.Background(), "", "", noVerifyPayload("git commit --no-verify -m x"))
	if res.Code != 2 || res.Stderr != noVerifyRefusal {
		t.Fatalf("got %d %q, want 2 %q", res.Code, res.Stderr, noVerifyRefusal)
	}
}

// Red when NoVerifyCheck stops reading the short -n of `git commit`.
func TestNoVerifyCheckBlocksShortNOnCommit(t *testing.T) {
	for _, command := range []string{"git commit -n -m x", "git commit -anm x", "git -C /tmp commit -n -m x"} {
		res := NoVerifyCheck(context.Background(), "", "", noVerifyPayload(command))
		if res.Code != 2 || res.Stderr != noVerifyRefusal {
			t.Errorf("%q: got %d %q", command, res.Code, res.Stderr)
		}
	}
}

// Red when NoVerifyCheck matches the raw text instead of the parsed commit invocation.
func TestNoVerifyCheckAllowsNoVerifyWordInMessage(t *testing.T) {
	for _, command := range []string{`git commit -m "no --no-verify"`, "git log -n 1", "git status", ""} {
		res := NoVerifyCheck(context.Background(), "", "", noVerifyPayload(command))
		if res.Code != 0 || res.Stderr != "" {
			t.Errorf("%q: got %d %q, want pass", command, res.Code, res.Stderr)
		}
	}
}

// Red when the block depends on a stand: main-commit-gate off, and commit-test-gate off, must
// not unblock it. Goes through hook.Run, which is where a stand would be applied.
func TestNoVerifyCheckStillBlocksWhenMainCommitGateIsOff(t *testing.T) {
	root := levelRoot(t, "level = \"light\"\n[components]\n\"main-commit-gate\" = \"off\"\n\"commit-test-gate\" = \"off\"\n")
	level, _, err := levels.ReadInstalled(root)
	if err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"command": "git commit --no-verify -m x"}})
	var out, errOut bytes.Buffer
	code := hook.Run(context.Background(), "no-verify-check", "", bytes.NewReader(in), &out, &errOut,
		hook.Env{Root: root, Level: level})
	if code != 2 || errOut.String() != noVerifyRefusal {
		t.Fatalf("got %d %q, want 2 %q", code, errOut.String(), noVerifyRefusal)
	}
}

// Red when no-verify-check leaves levels.BaseHooks (a corrupt components.toml then makes
// hook.Run answer COULD NOT CHECK instead of the refusal).
func TestNoVerifyCheckIgnoresCorruptComponentsFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, levels.InstalledPath), "corrupt = = =")
	_, _, lerr := levels.ReadInstalled(root)
	if lerr == nil {
		t.Fatal("fixture must be unreadable")
	}
	in, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"command": "git commit -n -m x"}})
	var out, errOut bytes.Buffer
	code := hook.Run(context.Background(), "no-verify-check", "", bytes.NewReader(in), &out, &errOut,
		hook.Env{Root: root, LevelErr: lerr})
	if code != 2 || errOut.String() != noVerifyRefusal {
		t.Fatalf("got %d %q, want 2 %q", code, errOut.String(), noVerifyRefusal)
	}
}

// Red when a payload that cannot be read passes: the hook is blocking, so it fails closed.
func TestNoVerifyCheckBlocksAnUnreadablePayload(t *testing.T) {
	res := NoVerifyCheck(context.Background(), "", "", hook.Payload{Raw: []byte("not json")})
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, "no-verify-check: COULD NOT CHECK") {
		t.Fatalf("got %d %q", res.Code, res.Stderr)
	}
}

// Red when pack/core/hooks/hooks.json stops registering no-verify-check on PreToolUse Bash,
// or pack/core/hooks/blocking stops listing it (a crash would then let the flag through).
func TestNoVerifyCheckRegisteredAndBlocking(t *testing.T) {
	data, err := os.ReadFile("../../pack/core/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range f.Hooks["PreToolUse"] {
		for _, h := range g.Hooks {
			if strings.HasSuffix(h.Command, "run.sh no-verify-check") && g.Matcher == "Bash" {
				found = true
			}
		}
	}
	if !found {
		t.Error("no-verify-check not registered on PreToolUse Bash")
	}
	blocking, err := os.ReadFile("../../pack/core/hooks/blocking")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains("\n"+string(blocking), "\nno-verify-check\n") {
		t.Error("no-verify-check not in hooks/blocking")
	}
}
