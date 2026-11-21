//go:build e2e

// Black-box tests of the hook registrations `lighthouse init --yes` writes to
// .claude/settings.json: which event, matcher, command and timeout each hook gets, that every
// registration runs under a lighthouse.toml with only [project] and [forge], and that a blocking
// Bash hook blocks while the lighthouse binary is missing.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// registration is one hook of .claude/settings.json.
type registration struct {
	event, matcher, command string
	timeout                 int
}

// registrations reads every hook of the settings.json of repo, not only those under matcher Bash.
func registrations(t *testing.T, repo string) []registration {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	var all []registration
	for event, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				all = append(all, registration{event, g.Matcher, h.Command, h.Timeout})
			}
		}
	}
	slices.SortFunc(all, func(a, b registration) int {
		return strings.Compare(a.event+"|"+a.command, b.event+"|"+b.command)
	})
	return all
}

// script is what follows the hooks directory in the command: `run.sh <hook> [stage]`.
func (r registration) script() string {
	_, script, _ := strings.Cut(r.command, "/.claude/hooks/lighthouse/")
	return script
}

// hook is the lighthouse hook `run.sh <hook> [stage]` runs.
func (r registration) hook() string {
	words := strings.Fields(r.script())
	if len(words) < 2 || words[0] != "run.sh" {
		return ""
	}
	return words[1]
}

func initedRepo(t *testing.T) (bin, repo string) {
	t.Helper()
	bin = buildLighthouse(t)
	repo = freshRepo(t)
	run(t, repo, bin, "init", "--yes")
	return bin, repo
}

func TestHookRegistrations(t *testing.T) {
	_, repo := initedRepo(t)
	all := registrations(t, repo)
	cases := []struct {
		event, matcher, script string
		timeout                int
	}{
		{"PreToolUse", "Bash", "run.sh main-commit-gate", 15},
		{"PreToolUse", "Bash", "run.sh commit-test-gate pre", 300},
		{"PostToolUse", "Bash", "run.sh commit-test-gate post", 300},
		{"PostToolUseFailure", "Bash", "run.sh commit-test-gate post", 300},
		{"PreToolUse", "Bash", "run.sh bash-write-check pre", 5},
		{"PostToolUse", "Bash", "run.sh bash-write-check post", 120},
		{"PostToolUseFailure", "Bash", "run.sh bash-write-check post", 120},
		{"PreToolUse", "Bash", "run.sh pr-create-gate", 15},
		{"PreToolUse", "Bash", "run.sh workflow-push-gate", 30},
	}
	for _, tc := range cases {
		t.Run(tc.event+" "+tc.script, func(t *testing.T) {
			var timeouts []int
			for _, r := range all {
				if r.event == tc.event && r.matcher == tc.matcher && r.script() == tc.script {
					timeouts = append(timeouts, r.timeout)
				}
			}
			if !slices.Equal(timeouts, []int{tc.timeout}) {
				t.Errorf("timeouts of %s under %s/%s = %v, want [%d]", tc.script, tc.event, tc.matcher, timeouts, tc.timeout)
			}
		})
	}

	t.Run("run.sh is executable", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no exec bit")
		}
		info, err := os.Stat(filepath.Join(repo, ".claude", "hooks", "lighthouse", "run.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 != 0o111 {
			t.Errorf("run.sh mode = %v, want executable", info.Mode().Perm())
		}
	})
}

// The hooks a run.sh without lighthouse blocks, besides: a PreToolUse Bash hook that is not in
// the list of blocking hooks would pass every call while the binary is missing.
func TestBlockingBashRegistrationsBlockWithoutLighthouse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run.sh is a POSIX shell script")
	}
	_, repo := initedRepo(t)
	toolDir := t.TempDir()
	for _, tool := range runShTools {
		if found, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(found, filepath.Join(toolDir, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var env []string
	for _, kv := range cleanEnv() {
		if key, _, _ := strings.Cut(kv, "="); key != "PATH" {
			env = append(env, kv)
		}
	}
	env = append(env, "PATH="+toolDir, "CLAUDE_PROJECT_DIR="+repo)
	payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","cwd":"` + repo + `","tool_input":{"command":"true"}}`

	var blocked, passed []string
	for _, r := range registrations(t, repo) {
		if r.event != "PreToolUse" || r.matcher != "Bash" {
			continue
		}
		cmd := exec.Command("bash", "-c", r.command)
		cmd.Dir = repo
		cmd.Env = env
		cmd.Stdin = strings.NewReader(payload)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		code := 0
		if err := cmd.Run(); err != nil {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("%s did not run: %v", r.script(), err)
			}
			code = exit.ExitCode()
		}
		if code == 2 && strings.Contains(stderr.String(), "COULD NOT CHECK: lighthouse not on PATH") {
			blocked = append(blocked, r.script())
		} else {
			passed = append(passed, fmt.Sprintf("%s: rc %d, stdout %q, stderr %q", r.script(), code, stdout.String(), stderr.String()))
		}
	}
	want := []string{
		"run.sh bash-write-check pre", "run.sh commit-test-gate pre", "run.sh main-commit-gate",
		"run.sh pr-create-gate", "run.sh workflow-push-gate",
	}
	slices.Sort(blocked)
	if !slices.Equal(blocked, want) {
		t.Errorf("hooks that block without lighthouse (rc 2, COULD NOT CHECK: lighthouse not on PATH) = %q, want %q; the others ran as: %q", blocked, want, passed)
	}
}

// minimalToml is all a repository needs: every other key is optional.
const minimalToml = "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"

// editPayload is the payload of a hook on an Edit of path.
func editPayload(event, path, cwd string) map[string]any {
	return map[string]any{
		"hook_event_name": event, "tool_name": "Edit", "cwd": cwd,
		"tool_input": map[string]string{"file_path": path},
	}
}

// minimalPayload is the payload of hook. main-commit-gate, commit-test-gate pre, worktree-check and
// workflow-push-gate reach a configuration read with it (a commit on a feature branch, a push in a
// controller run, an edit in the main checkout); commit-test-gate post reaches it only after a
// commit that pre recorded (see runRegistration's caller). The other hooks only prove: exit 0 and
// no COULD NOT CHECK. The `git status` payloads of the hook fixtures return before any read.
func minimalPayload(t *testing.T, r registration, project, worktree string) map[string]any {
	t.Helper()
	bash := func(command string) map[string]any {
		return map[string]any{
			"hook_event_name": r.event, "session_id": "s1", "tool_name": "Bash", "cwd": worktree,
			"tool_input": map[string]string{"command": command},
		}
	}
	switch hook := r.hook(); hook {
	case "worktree-check":
		// A write in the main checkout on main: only there does the guard read forge.default_branch.
		writeFile(t, project, "doc.md", "# doc\n")
		return editPayload(r.event, filepath.Join(project, "doc.md"), worktree)
	case "md-link-check":
		writeFile(t, worktree, "doc.md", "# doc\n\nSee [the readme](README.md).\n")
		return editPayload(r.event, filepath.Join(worktree, "doc.md"), worktree)
	case "code-language-check":
		// Without code_language the hook is off.
		writeFile(t, worktree, "mod.go", "package mod\n")
		return editPayload(r.event, filepath.Join(worktree, "mod.go"), worktree)
	case "usage-gate":
		return bash("ls")
	case "permission-check":
		return map[string]any{
			"hook_event_name": r.event, "session_id": "s1", "cwd": worktree,
			"notification_type": "permission_prompt", "message": "Claude needs your permission to use Write",
		}
	case "main-commit-gate", "commit-test-gate":
		return bash("git commit -m x")
	case "workflow-push-gate":
		// origin/main gives the range a start, so the push of a branch without workflows passes.
		git(t, worktree, "update-ref", "refs/remotes/origin/main", "HEAD")
		return bash("git push -u origin HEAD")
	case "pr-create-gate":
		return bash("gh pr create --repo Acme/widgets --body 'Refs #5 — waiting for the deploy'")
	case "bash-write-check":
		writeFile(t, worktree, "a.md", "x\n")
		return bash("echo x > a.md")
	default:
		t.Fatalf("no payload for %s: add one to minimalPayload", r.script())
		return nil
	}
}

func TestEveryRegistrationRunsUnderMinimalConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run.sh is a POSIX shell script")
	}
	bin, project := initedRepo(t)
	// init leaves lighthouse.toml uncommitted, so the worktree below does not carry one: the hooks
	// there read the project's (CLAUDE_PROJECT_DIR), which is the minimal one written here.
	writeFile(t, project, "lighthouse.toml", minimalToml)
	worktree := filepath.Join(filepath.Dir(project), "feature")
	git(t, project, "worktree", "add", "-q", "-b", "feature", worktree)

	all := registrations(t, project)
	var hooks []string
	for _, r := range all {
		if h := r.hook(); !slices.Contains(hooks, h) {
			hooks = append(hooks, h)
		}
	}
	slices.Sort(hooks)
	wantHooks := []string{
		"bash-write-check", "code-language-check", "commit-test-gate", "main-commit-gate", "md-link-check",
		"permission-check", "pr-create-gate", "usage-gate", "workflow-push-gate", "worktree-check",
	}
	if !slices.Equal(hooks, wantHooks) {
		t.Fatalf("registered hooks = %q, want %q: give a new one a payload in minimalPayload", hooks, wantHooks)
	}

	for _, r := range all {
		t.Run(r.event+" "+r.matcher+" "+r.script(), func(t *testing.T) {
			state := t.TempDir()
			if r.hook() == "commit-test-gate" && r.script() == "run.sh commit-test-gate post" {
				// Post only reads test_gate.command for a commit that pre recorded: run pre on the
				// feature branch, commit, then post.
				pre := registration{event: "PreToolUse", matcher: "Bash", command: strings.Replace(r.command, " post", " pre", 1)}
				if code, out := runRegistration(t, bin, pre, minimalPayload(t, pre, project, worktree), project, worktree, state); code != 0 {
					t.Fatalf("pre: rc %d, output %q", code, out)
				}
				writeFile(t, worktree, "c-"+r.event+".txt", "x\n")
				git(t, worktree, "add", "-A")
				git(t, worktree, "commit", "-qm", "x")
			}
			code, out := runRegistration(t, bin, r, minimalPayload(t, r, project, worktree), project, worktree, state)
			// run.sh lets a hook that is not blocking pass with a systemMessage: look for the text too.
			if code != 0 || strings.Contains(out, "COULD NOT CHECK") {
				t.Errorf("rc %d, stdout+stderr %q; want rc 0 without COULD NOT CHECK", code, out)
			}
		})
	}
}

// runRegistration runs the command of r with payload in worktree, under the environment of a
// repository without host.toml, and returns the exit code and stdout+stderr. A state directory
// shared between calls lets post find what pre recorded.
func runRegistration(t *testing.T, bin string, r registration, payload map[string]any, project, worktree, state string) (int, string) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", r.command)
	cmd.Dir = worktree
	cmd.Env = append(hookEnv(bin), "CLAUDE_PROJECT_DIR="+project,
		"LIGHTHOUSE_HOST_TOML="+filepath.Join(state, "no-host.toml"),
		"LIGHTHOUSE_GATE_SLOT_DIR="+filepath.Join(state, "slots"),
		"XDG_RUNTIME_DIR="+state, "XDG_STATE_HOME="+state, "XDG_CONFIG_HOME="+state, "HOME="+state)
	if r.hook() == "workflow-push-gate" {
		cmd.Env = append(cmd.Env, "LIGHTHOUSE_CONTROLLER_RUN=0f8e2c1a-1b2c-4d5e-8f90-123456789abc")
	}
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("hook did not run: %v", err)
		}
		return exit.ExitCode(), out.String()
	}
	return 0, out.String()
}
