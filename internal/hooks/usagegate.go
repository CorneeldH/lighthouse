// Package hooks holds the hooks that run as `lighthouse hook <name> [stand]`. Each registers
// itself with the runtime in internal/hook from init; internal/cli imports this package for that.
package hooks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/usageguard"
)

func init() { hook.Register("usage-gate", usageGate) }

// uuidShape is the shape LIGHTHOUSE_CONTROLLER_RUN must have to count as a controller run.
var uuidShape = regexp.MustCompile(`^[0-9a-f-]+$`)

var gateStands = map[string]bool{"prompt": true, "tool": true, "subagent": true, "stop": true}

// usageGate wraps the session up once per window when the usage alarm of `lighthouse usage` is
// up (see the usage-gate contract document). Stands: prompt (UserPromptSubmit), tool (PostToolUse), subagent
// (SubagentStop), stop (Stop). Fail-open: always code 0, at most one JSON object, and an error
// is one `usage-gate: ` line on stderr.
func usageGate(_ context.Context, _ string, stand string, p hook.Payload) hook.Result {
	// Inside the child session of a meter refresh the gate is silent.
	if os.Getenv("LIGHTHOUSE_USAGE_REFRESH") == "1" || !gateStands[stand] {
		return hook.Result{}
	}
	if stand == "prompt" {
		startRefresh()
	}
	now := time.Now()
	until, flag := pauseFlag(stand, now)
	var alarm *usage.Alarm
	if paths, err := usage.DefaultPaths(); err == nil {
		if a, ok := paths.CurrentAlarm(now, park.ControllerRun(os.Getenv)); ok {
			alarm = &a
		}
	}
	if alarm == nil && until == "" {
		return hook.Result{}
	}

	var stderr strings.Builder
	warn := func(msg string) { stderr.WriteString(warnLine(msg)) }
	dirs, err := usageguard.DirsFromEnv(os.Getenv)
	if err != nil {
		warn(err.Error())
		return hook.Result{Stderr: stderr.String()}
	}
	c := usageguard.BuildContext(stand, alarm, until, flag, p.Raw, now.UTC(), os.Getenv)
	dec := usageguard.Decide(c, dirs, func() usageguard.Options { return usageguard.ReadOptions(warn) }, warn)
	if dec.KeyAfter != "" {
		writeKey(dec.KeyAfter, warn)
	}
	return hook.Result{Output: dec.Output, Stderr: stderr.String()}
}

// startRefresh starts `lighthouse usage refresh` from PATH, detached and without waiting: the
// meter keeps its own TTL and lock. Without lighthouse on PATH nothing happens.
func startRefresh() {
	bin, err := exec.LookPath("lighthouse")
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "usage", "refresh")
	cmd.Env = append(os.Environ(), "LIGHTHOUSE_USAGE_REFRESH=1")
	cmd.SysProcAttr = detachAttr()
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// pauseFlag is the first line of the controller's PAUSE flag as written, and its path, when the
// flag holds: only in tool, inside a controller run. Otherwise both are empty.
func pauseFlag(stand string, now time.Time) (until, flag string) {
	if stand != "tool" || !uuidShape.MatchString(os.Getenv("LIGHTHOUSE_CONTROLLER_RUN")) {
		return "", ""
	}
	path, err := park.PauseFile(os.Getenv)
	if err != nil {
		return "", ""
	}
	if _, ok := park.ReadPause(path, now); !ok {
		return "", ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return line, path
}

// writeKey writes the stop key once the block is decided, so the next stop in the window is
// silent.
func writeKey(path string, warn func(string)) {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		warn("cannot write " + path + ": " + err.Error())
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		warn("cannot write " + path + ": " + err.Error())
		return
	}
	_ = f.Close()
}

// warnLine is msg as one stderr line, also a multi-line message (git's stderr, say).
func warnLine(msg string) string {
	msg = strings.ReplaceAll(strings.ReplaceAll(msg, "\r\n", "\n"), "\r", "\n")
	return "usage-gate: " + strings.ReplaceAll(strings.TrimSuffix(msg, "\n"), "\n", " ") + "\n"
}
