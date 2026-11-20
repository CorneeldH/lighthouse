//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// needTool skips a test when name is not on the PATH; in CI it fails instead, so a missing
// tool never turns a gate test into a silent skip.
func needTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s not on PATH; CI must install it before the e2e tests", name)
		}
		t.Skipf("%s not on PATH (local run): the hook under test needs it", name)
	}
}

// packRepo is a fresh repository with the trigger files written, after `init --yes`; it fails
// when the packages that init reports do not start with wantPackages.
func packRepo(t *testing.T, bin string, trigger map[string]string, wantPackages string) string {
	t.Helper()
	repo := freshRepo(t)
	for rel, content := range trigger {
		writeFile(t, repo, rel, content)
	}
	if out := run(t, repo, bin, "init", "--yes"); !strings.HasPrefix(out, wantPackages) {
		t.Fatalf("init: want output starting with %q, got:\n%s", wantPackages, out)
	}
	return repo
}

// registeredHook reads .claude/settings.json and returns the command and timeout of the one
// hook under event/matcher whose command ends in script.
func registeredHook(t *testing.T, repo, event, matcher, script string) (string, int) {
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
	var commands []string
	var timeouts []int
	for _, g := range s.Hooks[event] {
		if g.Matcher != matcher {
			continue
		}
		for _, h := range g.Hooks {
			if strings.HasSuffix(h.Command, script) {
				commands = append(commands, h.Command)
				timeouts = append(timeouts, h.Timeout)
			}
		}
	}
	if len(commands) != 1 {
		t.Fatalf("%s registrations under %s/%q: %v", script, event, matcher, commands)
	}
	return commands[0], timeouts[0]
}

// toolDir is a directory with symlinks to the real tools minus drop, a `lighthouse` symlink to
// bin (hookEnv puts bin on the child PATH only, the test process PATH has no lighthouse), and
// executable stubs. A tool that is not on the PATH is left out. It lets a test fail the
// preflight on exactly one missing tool.
func toolDir(t *testing.T, bin string, tools []string, drop string, stubs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		if tool == drop {
			continue
		}
		if real, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if drop != "lighthouse" {
		if err := os.Symlink(bin, filepath.Join(dir, "lighthouse")); err != nil {
			t.Fatal(err)
		}
	}
	for name, script := range stubs {
		p := filepath.Join(dir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runPackHook runs command the way Claude Code does: `bash -c`, in repo, with CLAUDE_PROJECT_DIR
// set and the payload as JSON on stdin (a string goes in unchanged). env (KEY=value) overrides
// hookEnv(bin) key by key.
func runPackHook(t *testing.T, bin, repo, command string, payload any, env []string) (int, string, string) {
	t.Helper()
	var in []byte
	if s, ok := payload.(string); ok {
		in = []byte(s)
	} else {
		var err error
		if in, err = json.Marshal(payload); err != nil {
			t.Fatal(err)
		}
	}
	overrides := map[string]bool{"CLAUDE_PROJECT_DIR": true}
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		overrides[key] = true
	}
	var full []string
	for _, kv := range hookEnv(bin) {
		if key, _, _ := strings.Cut(kv, "="); !overrides[key] {
			full = append(full, kv)
		}
	}
	full = append(full, "CLAUDE_PROJECT_DIR="+repo)
	full = append(full, env...)

	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = repo
	cmd.Env = full
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	return 0, stdout.String(), stderr.String()
}
