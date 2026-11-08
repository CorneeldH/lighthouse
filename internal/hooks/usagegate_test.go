package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

// Classification: this is a hook/gate change; the likely regression surface is the one JSON
// object on stdout, the usage-gate: lines on stderr and the keys and markers on disk.

const (
	gateRun   = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"
	parkSteps = "commit the work in progress, then stop: do not start a new step, do not dispatch a " +
		"subagent, do not schedule a wake-up and do not write a handoff"
)

type gateDirs struct{ tmp, cache, state, resume, ctl string }

// gateEnv is a clean environment under a tmp dir: no alarm, no controller run, no refresh,
// both [usage_guard] options off, and a PATH without lighthouse.
func gateEnv(t *testing.T) gateDirs {
	t.Helper()
	tmp := t.TempDir()
	g := gateDirs{
		tmp:    tmp,
		cache:  filepath.Join(tmp, "cache"),
		state:  filepath.Join(tmp, "state"),
		resume: filepath.Join(tmp, "data", "lighthouse", "resume"),
		ctl:    filepath.Join(tmp, "ctl"),
	}
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("XDG_CACHE_HOME", g.cache)
	t.Setenv("XDG_STATE_HOME", g.state)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	t.Setenv("LIGHTHOUSE_CONTROLLER_STATE", g.ctl)
	t.Setenv("LIGHTHOUSE_USAGE_REFRESH", "")
	setOptions(t, g, "false", "false")
	emptyPath(t)
	return g
}

func setOptions(t *testing.T, g gateDirs, resume, controller string) {
	t.Helper()
	path := writeFile(t, filepath.Join(g.tmp, "lighthouse.toml"),
		"[usage_guard]\nresume = "+resume+"\ncontroller = "+controller+"\n")
	t.Setenv("LIGHTHOUSE_TOML", path)
}

// writeAlarm writes a venster alarm at 92% that resets at 14:30, an hour from now; it returns
// the reset_at.
func writeAlarm(t *testing.T, g gateDirs) string {
	t.Helper()
	at := time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	writeFile(t, filepath.Join(g.cache, "lighthouse", "usage-window.alarm"), "venster 92 14:30 "+at+"\n")
	return at
}

func writePause(t *testing.T, g gateDirs, line string) {
	t.Helper()
	writeFile(t, filepath.Join(g.ctl, "PAUSE"), line+"\n")
}

func reason(t *testing.T, res hook.Result) string {
	t.Helper()
	if res.Output["decision"] != "block" {
		t.Fatalf("output %v, want a block", res.Output)
	}
	r, _ := res.Output["reason"].(string)
	return r
}

func session(extra map[string]any) map[string]any {
	p := map[string]any{"session_id": "s1", "cwd": "/w"}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func TestUsageGateSilent(t *testing.T) {
	tests := []struct {
		name, stand, refresh string
		alarm                bool
	}{
		{"no alarm prompt", "prompt", "", false},
		{"no alarm tool", "tool", "", false},
		{"no alarm subagent", "subagent", "", false},
		{"no alarm stop", "stop", "", false},
		{"refresh child", "tool", "1", true},
		{"refresh child prompt", "prompt", "1", true},
		{"unknown stand", "nope", "", true},
		{"no stand", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gateEnv(t)
			t.Setenv("LIGHTHOUSE_USAGE_REFRESH", tt.refresh)
			if tt.alarm {
				writeAlarm(t, g)
			}
			res := call(t, usageGate, tt.stand, session(nil))
			if res.Code != 0 || res.Output != nil || res.Stderr != "" {
				t.Errorf("got %+v, want nothing", res)
			}
		})
	}
}

func TestUsageGateSpeaksOncePerWindow(t *testing.T) {
	g := gateEnv(t)
	writeAlarm(t, g)
	handoff := "`/lighthouse-write-handoff " + filepath.Join(g.resume, "s1.handoff.md") + "`"
	head := "Usage is at 92% of the session window, which resets at 14:30."

	res := call(t, usageGate, "tool", session(nil))
	want := head + " Before anything else: commit the work in progress, run " + handoff +
		", and report where the work stands (worktree, branch, what is still open). Do not start a new step and do not dispatch a subagent."
	if got := reason(t, res); got != want || res.Stderr != "" || res.Code != 0 {
		t.Fatalf("tool: reason %q stderr %q code %d\nwant %q", got, res.Stderr, res.Code, want)
	}
	if res := call(t, usageGate, "tool", session(nil)); res.Output != nil {
		t.Errorf("second tool call spoke: %v", res.Output)
	}

	res = call(t, usageGate, "subagent", session(map[string]any{"agent_id": "a1"}))
	if got := reason(t, res); !strings.HasPrefix(got, head+" Stop now and report to the main thread") {
		t.Errorf("subagent reason %q", got)
	}

	res = call(t, usageGate, "prompt", session(nil))
	hso, _ := res.Output["hookSpecificOutput"].(map[string]any)
	ctx, _ := hso["additionalContext"].(string)
	if hso["hookEventName"] != "UserPromptSubmit" || !strings.HasPrefix(ctx, head+" Wrap up: finish only the current step") {
		t.Errorf("prompt output %v", res.Output)
	}
}

// Stop prints its block and writes its key: the next stop in the window is silent.
func TestUsageGateStopWritesItsKey(t *testing.T) {
	g := gateEnv(t)
	at := writeAlarm(t, g)
	key := filepath.Join(g.state, "lighthouse", "usage-guard", "venster-"+at+".stop.s1.done")

	res := call(t, usageGate, "stop", session(nil))
	if got := reason(t, res); !strings.Contains(got, " Commit nothing more. ") {
		t.Fatalf("stop reason %q", got)
	}
	if _, err := os.Stat(key); err != nil {
		t.Fatalf("stop key after the block: %v", err)
	}
	if res := call(t, usageGate, "stop", session(nil)); res.Output != nil || res.Stderr != "" {
		t.Errorf("second stop: %+v, want nothing", res)
	}
}

func TestUsageGateWarnsOnOneLine(t *testing.T) {
	g := gateEnv(t)
	writeAlarm(t, g)
	blocker := writeFile(t, filepath.Join(g.tmp, "file"), "")
	t.Setenv("XDG_STATE_HOME", blocker)

	res := call(t, usageGate, "tool", session(nil))
	prefix := "usage-gate: cannot write keys in " + filepath.Join(blocker, "lighthouse", "usage-guard") + ": "
	if res.Output != nil || res.Code != 0 || !strings.HasPrefix(res.Stderr, prefix) ||
		strings.Count(res.Stderr, "\n") != 1 || !strings.HasSuffix(res.Stderr, "\n") {
		t.Errorf("got %+v, want no output and one line starting %q", res, prefix)
	}
}

func TestWarnLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "usage-gate: plain\n"},
		{"fatal: x\nhint: y\n", "usage-gate: fatal: x hint: y\n"},
		{"a\r\nb\rc", "usage-gate: a b c\n"},
		{"a\n\nb", "usage-gate: a  b\n"},
	}
	for _, tt := range tests {
		if got := warnLine(tt.in); got != tt.want {
			t.Errorf("warnLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestUsageGatePause(t *testing.T) {
	paused := "The controller has paused this run until 21:00. Now " + parkSteps +
		". The controller resumes this session after 21:00."
	alarmParks := "Usage is at 92% of the session window, which resets at 14:30. Now " + parkSteps +
		". The controller resumes this session after 14:30."
	tests := []struct {
		name, stand, run, flag string
		alarm                  bool
		want                   string // "" is no output
	}{
		{"valid flag", "tool", gateRun, "2099-01-01T21:00:00+02:00", false, paused},
		{"valid flag Z", "tool", gateRun, "2099-01-01T19:00:00Z", false,
			"The controller has paused this run until 19:00. Now " + parkSteps + ". The controller resumes this session after 19:00."},
		// RFC 3339 needs seconds; date -d took this, park.ReadPause does not.
		{"no seconds", "tool", gateRun, "2099-01-01T21:00+02:00", false, ""},
		{"past", "tool", gateRun, "2000-01-01T00:00:00Z", false, ""},
		{"invalid flag", "tool", gateRun, "morgen", false, ""},
		{"invalid flag with alarm", "tool", gateRun, "morgen", true, alarmParks},
		{"only in tool", "stop", gateRun, "2099-01-01T21:00:00+02:00", false, ""},
		{"run not uuid", "tool", "../x", "2099-01-01T21:00:00+02:00", false, ""},
		{"no run", "tool", "", "2099-01-01T21:00:00+02:00", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gateEnv(t)
			setOptions(t, g, "false", "true")
			t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", tt.run)
			writePause(t, g, tt.flag)
			if tt.alarm {
				writeAlarm(t, g)
			}
			res := call(t, usageGate, tt.stand, session(nil))
			if res.Stderr != "" || res.Code != 0 {
				t.Fatalf("stderr %q code %d", res.Stderr, res.Code)
			}
			if tt.want == "" {
				if res.Output != nil {
					t.Errorf("output %v, want none", res.Output)
				}
				return
			}
			if got := reason(t, res); got != tt.want {
				t.Errorf("reason %q\nwant   %q", got, tt.want)
			}
		})
	}
}

// The pause marker carries the first line of the flag as written.
func TestUsageGatePauseOncePerFlag(t *testing.T) {
	g := gateEnv(t)
	setOptions(t, g, "false", "true")
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", gateRun)
	writePause(t, g, "2099-01-01T21:00:00+02:00")

	if res := call(t, usageGate, "tool", session(nil)); res.Output == nil {
		t.Fatal("first tool call did not park")
	}
	b, err := os.ReadFile(filepath.Join(g.resume, "s1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["reset_at"] != "2099-01-01T21:00:00+02:00" || m["window"] != "pause" || m["pause"] != true {
		t.Errorf("marker %v", m)
	}
	if res := call(t, usageGate, "tool", session(nil)); res.Output != nil {
		t.Errorf("second tool call: %v, want none", res.Output)
	}
}

// fakeLighthouse puts a lighthouse on PATH that writes "<args>|<LIGHTHOUSE_USAGE_REFRESH>" to
// the returned file and then sleeps 2 s.
func fakeLighthouse(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake lighthouse is a sh script")
	}
	bin := t.TempDir()
	out := filepath.Join(t.TempDir(), "called")
	script := "#!/bin/sh\nprintf '%s|%s\\n' \"$*\" \"$LIGHTHOUSE_USAGE_REFRESH\" > " + out + "\nexec sleep 2\n"
	if err := os.WriteFile(filepath.Join(bin, "lighthouse"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
	return out
}

func waitFor(path string, d time.Duration) string {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
	}
	return ""
}

func TestUsageGateRefreshIsDetached(t *testing.T) {
	gateEnv(t)
	out := fakeLighthouse(t)
	start := time.Now()
	res := call(t, usageGate, "prompt", session(nil))
	if took := time.Since(start); took > time.Second {
		t.Errorf("prompt took %v, want under 1 s", took)
	}
	if res.Output != nil || res.Stderr != "" {
		t.Errorf("prompt without alarm: %+v", res)
	}
	if got := waitFor(out, 5*time.Second); got != "usage refresh|1\n" {
		t.Errorf("refresh call %q, want %q", got, "usage refresh|1\n")
	}
}

func TestUsageGateRefreshOnlyAtPrompt(t *testing.T) {
	g := gateEnv(t)
	writeAlarm(t, g)
	out := fakeLighthouse(t)
	for _, stand := range []string{"tool", "subagent", "stop"} {
		call(t, usageGate, stand, session(map[string]any{"agent_id": "a1"}))
	}
	if got := waitFor(out, 300*time.Millisecond); got != "" {
		t.Errorf("refresh ran outside prompt: %q", got)
	}
}

func TestUsageGateRefreshWithoutLighthouse(t *testing.T) {
	g := gateEnv(t)
	writeAlarm(t, g)
	res := call(t, usageGate, "prompt", session(nil))
	if res.Stderr != "" || res.Output == nil {
		t.Errorf("got %+v, want the prompt note and no stderr", res)
	}
}

// Through the runtime the prompt note and a runtime notice are one JSON object.
func TestUsageGatePromptWithNoticeIsOneObject(t *testing.T) {
	g := gateEnv(t)
	writeAlarm(t, g)
	env := hook.Env{ManifestVersion: "0.1.0", BinaryVersion: "0.2.0", StateDir: t.TempDir()}
	var out, errOut bytes.Buffer
	code := hook.Run(context.Background(), "usage-gate", "prompt",
		strings.NewReader(`{"session_id":"s1","cwd":"/w"}`), &out, &errOut, env)
	if code != 0 || errOut.String() != "" {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("stdout %q, want one line", out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout %q: %v", out.String(), err)
	}
	if got["systemMessage"] != "lighthouse v0.2.0 ≠ v0.1.0 in this repo — run lighthouse init --upgrade" {
		t.Errorf("systemMessage %v", got["systemMessage"])
	}
	if _, ok := got["hookSpecificOutput"].(map[string]any); !ok {
		t.Errorf("no hookSpecificOutput in %v", got)
	}
}
