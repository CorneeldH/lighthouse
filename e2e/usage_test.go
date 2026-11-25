//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

// usageEnv is a clean environment with a temporary cache, host.toml and PATH prefix.
func usageEnv(t *testing.T, xdg, binDir string) []string {
	t.Helper()
	return append(cleanEnv(),
		"XDG_CACHE_HOME="+xdg,
		"LIGHTHOUSE_HOST_TOML="+filepath.Join(t.TempDir(), "host.toml"),
		"PATH="+stubbin.PathValue(t, binDir),
		"TZ=Europe/Amsterdam",
	)
}

// writeScript writes a POSIX stub script: its body uses shell builtins and POSIX tools.
func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	stubbin.RequirePOSIX(t, "usage stub "+name+" runs shell commands (pwd, cat, sed, touch, read loop)")
	stubbin.MakeScript(t, dir, name, body)
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
	// The script's pwd is the physical path; on macOS t.TempDir() sits behind the
	// /var -> /private/var symlink.
	realXDG, err := filepath.EvalSymlinks(xdg)
	if err != nil {
		t.Fatal(err)
	}
	wantSeen := filepath.Join(realXDG, "lighthouse", "usage-refresh") + "\n1\n"
	if got, _ := os.ReadFile(seen); string(got) != wantSeen {
		t.Errorf("script saw %q, want %q", got, wantSeen)
	}

	if stdout, _ := runUsage(t, bin, env, "usage", "refresh"); stdout != "usage: fresh\n" {
		t.Errorf("second refresh stdout = %q", stdout)
	}
	if got, _ := os.ReadFile(seen); string(got) != wantSeen {
		t.Errorf("script ran again within the TTL: %q", got)
	}

	stubbin.Make(t, binDir, "claude", stubbin.Spec{Exit: 1})
	_, stderr := runUsage(t, bin, env, "usage", "refresh", "--force")
	if !strings.HasPrefix(stderr, "usage: failed: ") {
		t.Errorf("forced refresh stderr = %q", stderr)
	}
	if after, _ := os.ReadFile(cachePath); !bytes.Equal(after, cache) {
		t.Errorf("cache changed after a failed refresh:\n%s", after)
	}
}

// TestUsageKeepsControllerStateOut proves that a refresh in an e2e test never writes the alarm
// mirror into the controller state of the developer (#482). A false alarm there, with a reset a
// year ahead, parked real controller runs until that date.
func TestUsageKeepsControllerStateOut(t *testing.T) {
	bin := buildLighthouse(t)
	fixture, err := filepath.Abs("../internal/usage/testdata/usage_full.json")
	if err != nil {
		t.Fatal(err)
	}
	// Stands in for the developer's real state directory: cleanEnv reads os.Environ.
	real := t.TempDir()
	t.Setenv("XDG_STATE_HOME", real)
	xdg, binDir := t.TempDir(), t.TempDir()
	writeScript(t, binDir, "claude", "cat '"+fixture+"'\n")

	runUsage(t, bin, usageEnv(t, xdg, binDir), "usage", "refresh")

	if _, err := os.Stat(filepath.Join(xdg, "lighthouse", "usage-window.alarm")); err != nil {
		t.Fatalf("the refresh wrote no alarm in its own cache, so this test proves nothing: %v", err)
	}
	leaked := filepath.Join(real, "lighthouse", "controller", "usage-window.alarm")
	if got, err := os.ReadFile(leaked); err == nil {
		t.Errorf("the e2e refresh wrote the alarm mirror into the outer XDG_STATE_HOME: %s = %q", leaked, got)
	}
	mirror := filepath.Join(e2eState, "lighthouse", "controller", "usage-window.alarm")
	if got, err := os.ReadFile(mirror); err != nil || !strings.HasPrefix(string(got), "venster 99 ") {
		t.Errorf("alarm mirror in the e2e state = %q (%v), want prefix %q", got, err, "venster 99 ")
	}
}

func TestUsageTickWithoutClaude(t *testing.T) {
	bin := buildLighthouse(t)
	xdg, binDir, marker := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "ran")
	stubbin.Make(t, binDir, "pgrep", stubbin.Spec{Exit: 1})
	writeScript(t, binDir, "claude", "touch '"+marker+"'\n")

	if stdout, _ := runUsage(t, bin, usageEnv(t, xdg, binDir), "usage", "tick"); stdout != "usage: idle\n" {
		t.Errorf("tick stdout = %q", stdout)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("claude ran although pgrep found no claude process")
	}
}

func TestUsageCodexE2E(t *testing.T) {
	bin := buildLighthouse(t)
	raw, err := os.ReadFile("../internal/usage/testdata/codex/app_server_response.json")
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	fixture, methods := filepath.Join(work, "response.json"), filepath.Join(work, "methods")
	if err := os.WriteFile(fixture, compact.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir, xdg := t.TempDir(), t.TempDir()
	writeScript(t, binDir, "codex", "while read -r line; do\n"+
		"  m=$(printf '%s' \"$line\" | sed -n 's/.*\"method\":\"\\([^\"]*\\)\".*/\\1/p')\n"+
		"  echo \"$m\" >> '"+methods+"'\n"+
		"  case \"$m\" in\n"+
		"    initialize) echo '{\"id\":1,\"result\":{}}' ;;\n"+
		"    account/rateLimits/read) printf '{\"id\":2,\"result\":%s}\\n' \"$(cat '"+fixture+"')\" ;;\n"+
		"  esac\n"+
		"done\n")
	alarmPath := filepath.Join(xdg, "lighthouse", "usage-window.alarm")
	alarm := []byte("week 96 do_16:59 2026-09-10T14:59:00Z\n")
	if err := os.MkdirAll(filepath.Dir(alarmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alarmPath, alarm, 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(usageEnv(t, xdg, binDir), "HOME="+t.TempDir())

	stdout, _ := runUsage(t, bin, env, "usage", "codex")
	if !strings.Contains(stdout, `"pct": 55`) {
		t.Errorf("stdout lacks %q:\n%s", `"pct": 55`, stdout)
	}
	log, err := os.ReadFile(methods)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "consume") {
		t.Errorf("app-server was asked to consume:\n%s", log)
	}
	if got, _ := os.ReadFile(alarmPath); !bytes.Equal(got, alarm) {
		t.Errorf("alarm changed: %q", got)
	}
	entries, err := os.ReadDir(filepath.Dir(alarmPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "usage-window.alarm" {
		t.Errorf("cache dir holds %d entries, want only the alarm", len(entries))
	}
}

func TestUsageCodexNotMeasurableE2E(t *testing.T) {
	bin := buildLighthouse(t)
	binDir := t.TempDir()
	env := append(usageEnv(t, t.TempDir(), binDir),
		"HOME="+t.TempDir(),
		// Only the stub dir and the dir of sh: a real codex elsewhere on the PATH must not run.
		"PATH="+stubbin.PathValue(t, binDir, "sh"),
	)
	cmd := exec.Command(bin, "usage", "codex")
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	if stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "not measurable: ") {
		t.Errorf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}
