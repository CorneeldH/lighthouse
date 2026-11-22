//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot is the lighthouse checkout: the e2e tests run in its e2e/ directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// entireHook matches entire's own hooks (#194), each a no-op when entire is not installed.
// The project owns them; init does not manage them.
var entireHook = regexp.MustCompile(
	`^sh -c 'if ! command -v entire >/dev/null 2>&1; then (?:.*; )?exit 0; fi; ` +
		`exec entire hooks claude-code [a-z-]+'$`)

type settingsFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func readSettings(t *testing.T) settingsFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s settingsFile
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// registered returns the commands under event (and matcher, unless empty) that name script.
func registered(t *testing.T, event, matcher, script string) []string {
	t.Helper()
	var found []string
	for _, block := range readSettings(t).Hooks[event] {
		if matcher != "" && block.Matcher != matcher {
			continue
		}
		for _, h := range block.Hooks {
			if strings.Contains(h.Command, script) {
				found = append(found, h.Command)
			}
		}
	}
	return found
}

// registeredOne is the one command under event and matcher that names script, and it must
// live under .claude/hooks/lighthouse/.
func registeredOne(t *testing.T, event, matcher, script string) string {
	t.Helper()
	cmds := registered(t, event, matcher, script)
	if len(cmds) != 1 {
		t.Fatalf("%s under %s %q: %q", script, event, matcher, cmds)
	}
	if !strings.Contains(cmds[0], "/.claude/hooks/lighthouse/") {
		t.Fatalf("registration outside .claude/hooks/lighthouse/: %s", cmds[0])
	}
	return cmds[0]
}

// hookPath is a directory holding only the tools run.sh needs, plus git, sh and the built
// lighthouse (the tools of RUN_SH_TOOLS of the old pytest kit), so the hook runs on a PATH
// without go: run.sh in this dev checkout then builds nothing and takes that binary.
func hookPath(t *testing.T, bin string) string {
	t.Helper()
	dir := t.TempDir()
	tools := []string{"sh", "bash", "dirname", "grep", "sed", "awk", "cat", "mktemp", "rm", "head",
		"mkdir", "find", "touch", "date", "git", "env"}
	for _, tool := range tools {
		src, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("tool %s not on PATH: %v", tool, err)
		}
		if err := os.Symlink(src, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(bin, filepath.Join(dir, "lighthouse")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runRegistered runs the registered command through sh, as Claude Code does, in repo, with
// CLAUDE_PROJECT_DIR the lighthouse checkout and the payload on stdin.
func runRegistered(t *testing.T, command string, payload map[string]any, repo string) (int, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the registered hooks are POSIX shell commands")
	}
	stdin, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, kv := range cleanEnv() {
		if key, _, _ := strings.Cut(kv, "="); key != "PATH" {
			env = append(env, kv)
		}
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = repo
	cmd.Env = append(env, "PATH="+hookPath(t, buildLighthouse(t)), "CLAUDE_PROJECT_DIR="+repoRoot(t))
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	return 0, stdout.String(), stderr.String()
}

func bashPayload(cmd, repo string) map[string]any {
	return map[string]any{"tool_name": "Bash", "cwd": repo, "tool_input": map[string]string{"command": cmd}}
}

// A production change that fails this test: a wrong path or a missing registration of
// main-commit-gate in lighthouse's own .claude/settings.json, or a gate that lets a commit on
// main through, so the guard of this repo is silently off.
func TestRegisteredBashHookBlocksACommitOnMain(t *testing.T) {
	repo := freshRepo(t)
	command := registeredOne(t, "PreToolUse", "Bash", "run.sh main-commit-gate")

	code, _, stderr := runRegistered(t, command, bashPayload(`git commit -m "x"`, repo), repo)

	if code != 2 {
		t.Errorf("exit = %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "Main-commit gate: blocked") {
		t.Errorf("stderr lacks the block message: %s", stderr)
	}
	if strings.Contains(stderr, "COULD NOT CHECK") {
		t.Errorf("the gate could not check: %s", stderr)
	}
}

// A production change that fails this test: a main-commit-gate that blocks a commit on a
// feature branch, so no work can be committed in this repo.
func TestRegisteredBashHookAllowsACommitOnABranch(t *testing.T) {
	repo := freshRepo(t)
	git(t, repo, "checkout", "-q", "-b", "issue-1-x")
	command := registeredOne(t, "PreToolUse", "Bash", "run.sh main-commit-gate")

	code, _, stderr := runRegistered(t, command, bashPayload(`git commit -m "x"`, repo), repo)

	if code != 0 {
		t.Errorf("exit = %d, want 0; stderr: %s", code, stderr)
	}
}

// A production change that fails this test: a wrong path or a missing registration of
// worktree-check under the Edit|Write matcher, or a check that stops warning at a write into
// the main checkout.
func TestRegisteredEditHookWarnsInMainCheckout(t *testing.T) {
	repo := freshRepo(t)
	command := registeredOne(t, "PreToolUse", "Edit|Write", "run.sh worktree-check")
	payload := map[string]any{
		"tool_name":  "Edit",
		"cwd":        repo,
		"tool_input": map[string]string{"file_path": filepath.Join(repo, "README.md")},
	}

	code, stdout, stderr := runRegistered(t, command, payload, repo)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	var out struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !strings.Contains(out.SystemMessage, "Worktree check:") {
		t.Errorf("systemMessage = %q, want it to hold %q", out.SystemMessage, "Worktree check:")
	}
}

// A production change that fails this test: a hook registered in .claude/settings.json
// outside .claude/hooks/lighthouse/ (a hand-written script init does not manage), other than
// entire's own guarded hooks.
func TestNoRegistrationOutsideLighthouseHooks(t *testing.T) {
	n := 0
	for event, blocks := range readSettings(t).Hooks {
		for _, block := range blocks {
			for _, h := range block.Hooks {
				n++
				if entireHook.MatchString(h.Command) {
					continue
				}
				if !strings.HasPrefix(h.Command, `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/`) {
					t.Errorf("%s: registration outside .claude/hooks/lighthouse/: %s", event, h.Command)
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no hooks registered")
	}
}

// A production change that fails this test: the installed copy of this repo stops registering the
// pack/go hooks, so gofmt-check no longer runs after an edit or go-lint-gate no longer runs at
// Stop. TestGoHooksRegistered covers the same hooks in a fresh init.
func TestDogfoodGoHooksRegistered(t *testing.T) {
	if got := registered(t, "PostToolUse", "Edit|Write", "gofmt-check.sh"); len(got) != 1 {
		t.Errorf("gofmt-check.sh under PostToolUse Edit|Write: %q, want one", got)
	}
	if got := registered(t, "Stop", "", "go-lint-gate.sh"); len(got) != 1 {
		t.Errorf("go-lint-gate.sh under Stop: %q, want one", got)
	}
}

// A production change that fails this test: a commit of .claude/allow-main, which switches
// the main-commit guard off for everyone for good.
func TestAllowMainSentinelIsNotTracked(t *testing.T) {
	out, err := exec.Command("git", "-C", repoRoot(t), "ls-files", "--", ".claude/allow-main").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "" {
		t.Errorf(".claude/allow-main is in git: %q", got)
	}
}
