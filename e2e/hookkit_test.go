//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// skipOnWindows skips t on native Windows, where the behaviour under test does not exist.
func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why + "; see docs/host-assumptions.md")
	}
}

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
	if out := run(t, repo, bin, "init", "--yes", "--level", "strict"); !strings.HasPrefix(out, wantPackages) {
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
// executable stubs. A tool that is not on the PATH fails the test through needTool (a skip
// outside CI); lighthouse and the dropped tool are exempt. It lets a test fail the preflight
// on exactly one missing tool.
func toolDir(t *testing.T, bin string, tools []string, drop string, stubs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		if tool == drop || tool == "lighthouse" {
			continue
		}
		needTool(t, tool)
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
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

// lighthouseStub is a fake `lighthouse` that runs body for every call but `turn-roots` and
// `config`, which go to the binary bin: a Stop gate needs them to find its roots and to read
// its configuration.
func lighthouseStub(bin, body string) string {
	quoted := "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	return "#!/bin/bash\ncase \"$1\" in turn-roots|config) exec " + quoted + ` "$@";; esac` + "\n" + body
}

// pathWithoutLighthouse is the PATH of the test process with every directory that holds a
// `lighthouse` replaced by a directory of symlinks without it, so the other tools there stay
// reachable.
func pathWithoutLighthouse(t *testing.T) string {
	t.Helper()
	shadow := t.TempDir()
	var dirs []string
	for i, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if info, err := os.Stat(filepath.Join(entry, "lighthouse")); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			dirs = append(dirs, entry)
			continue
		}
		items, err := os.ReadDir(entry)
		if err != nil {
			t.Fatal(err)
		}
		copyDir := filepath.Join(shadow, fmt.Sprint(i))
		if err := os.MkdirAll(copyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Name() == "lighthouse" {
				continue
			}
			if err := os.Symlink(filepath.Join(entry, item.Name()), filepath.Join(copyDir, item.Name())); err != nil {
				t.Fatal(err)
			}
		}
		dirs = append(dirs, copyDir)
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// The transcript helpers write what `lighthouse turn-roots` reads: one JSON object per line, a
// prompt as a `user` entry with text, a tool call as an `assistant` entry with a `tool_use`
// block, its result as a `user` entry with a `tool_result` block.
const (
	promptTS = "2026-10-25T10:00:00.000Z"
	toolTS   = "2026-10-25T10:00:01.000Z"
	resultTS = "2026-10-25T10:00:02.000Z"
)

// stamp is at as a transcript timestamp in UTC, the format of promptTS.
func stamp(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

func promptEntry(cwd, ts string) map[string]any {
	return map[string]any{
		"type": "user", "timestamp": ts, "cwd": cwd,
		"message": map[string]any{"role": "user", "content": "go"},
	}
}

func toolEntry(name string, input map[string]any, cwd, ts string) map[string]any {
	return map[string]any{
		"type": "assistant", "timestamp": ts, "cwd": cwd,
		"message": map[string]any{"role": "assistant", "content": []map[string]any{
			{"type": "tool_use", "id": "toolu_1", "name": name, "input": input},
		}},
	}
}

func resultEntry(cwd, ts string) map[string]any {
	return map[string]any{
		"type": "user", "timestamp": ts, "cwd": cwd,
		"message": map[string]any{"role": "user", "content": []map[string]any{
			{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"},
		}},
	}
}

// writeTranscript writes entries as JSON lines to path and returns path.
func writeTranscript(t *testing.T, path string, entries ...map[string]any) string {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// stopPayload is the payload of a Stop hook.
func stopPayload(cwd, transcript string, active bool) map[string]any {
	return map[string]any{
		"session_id": "s1", "transcript_path": transcript, "cwd": cwd,
		"hook_event_name": "Stop", "stop_hook_active": active,
	}
}
