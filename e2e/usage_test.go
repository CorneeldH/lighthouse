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

// usageEnv is a clean environment with a temporary cache, host.toml and PATH prefix.
func usageEnv(t *testing.T, xdg, binDir string) []string {
	t.Helper()
	return append(cleanEnv(),
		"XDG_CACHE_HOME="+xdg,
		"LIGHTHOUSE_HOST_TOML="+filepath.Join(t.TempDir(), "host.toml"),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TZ=Europe/Amsterdam",
	)
}

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runUsage(t *testing.T, bin string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("lighthouse %v: %v\n%s\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func TestUsageRefreshE2E(t *testing.T) {
	bin := buildLighthouse(t)
	fixture, err := filepath.Abs("../internal/usage/testdata/usage_full.json")
	if err != nil {
		t.Fatal(err)
	}
	xdg, binDir, seen := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "seen")
	writeScript(t, binDir, "claude", "pwd >> '"+seen+"'\necho \"$LIGHTHOUSE_USAGE_REFRESH\" >> '"+seen+"'\ncat '"+fixture+"'\n")
	env := usageEnv(t, xdg, binDir)

	if stdout, _ := runUsage(t, bin, env, "usage", "refresh"); stdout != "usage: refreshed\n" {
		t.Errorf("first refresh stdout = %q", stdout)
	}
	cachePath := filepath.Join(xdg, "lighthouse", "usage-window.json")
	cache, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Session struct {
			Pct int `json:"pct"`
		} `json:"session"`
	}
	if err := json.Unmarshal(cache, &state); err != nil {
		t.Fatal(err)
	}
	if state.Session.Pct != 99 {
		t.Errorf("session.pct = %d, want 99", state.Session.Pct)
	}
	alarm, err := os.ReadFile(filepath.Join(xdg, "lighthouse", "usage-window.alarm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(alarm), "venster 99 ") {
		t.Errorf("alarm = %q, want prefix %q", alarm, "venster 99 ")
	}
	wantSeen := filepath.Join(xdg, "lighthouse", "usage-refresh") + "\n1\n"
	if got, _ := os.ReadFile(seen); string(got) != wantSeen {
		t.Errorf("script saw %q, want %q", got, wantSeen)
	}

	if stdout, _ := runUsage(t, bin, env, "usage", "refresh"); stdout != "usage: fresh\n" {
		t.Errorf("second refresh stdout = %q", stdout)
	}
	if got, _ := os.ReadFile(seen); string(got) != wantSeen {
		t.Errorf("script ran again within the TTL: %q", got)
	}

	writeScript(t, binDir, "claude", "exit 1\n")
	_, stderr := runUsage(t, bin, env, "usage", "refresh", "--force")
	if !strings.HasPrefix(stderr, "usage: failed: ") {
		t.Errorf("forced refresh stderr = %q", stderr)
	}
	if after, _ := os.ReadFile(cachePath); !bytes.Equal(after, cache) {
		t.Errorf("cache changed after a failed refresh:\n%s", after)
	}
}

func TestUsageTickWithoutClaude(t *testing.T) {
	bin := buildLighthouse(t)
	xdg, binDir, marker := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "ran")
	writeScript(t, binDir, "pgrep", "exit 1\n")
	writeScript(t, binDir, "claude", "touch '"+marker+"'\n")

	if stdout, _ := runUsage(t, bin, usageEnv(t, xdg, binDir), "usage", "tick"); stdout != "usage: idle\n" {
		t.Errorf("tick stdout = %q", stdout)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("claude ran although pgrep found no claude process")
	}
}
