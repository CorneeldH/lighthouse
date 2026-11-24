package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/version"
)

func init() { hook.Register("telemetry-collect", TelemetryCollect) }

// startWait is how long the hook waits for the collector to start before it returns anyway.
var startWait = time.Second

// startCollector starts `lighthouse telemetry collect` for root, detached and without waiting
// for it to finish. Without lighthouse on PATH nothing happens. Tests replace it.
var startCollector = func(root string) error {
	bin, err := exec.LookPath("lighthouse")
	if err != nil {
		return nil
	}
	cmd := exec.Command(bin, "telemetry", "collect", "--repo-dir", root, "--since-last")
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// TelemetryCollect is the SessionStart hook that notes the installed versions for the session
// in its sidecar file and starts the collector in the background. It never blocks (code 0),
// never opens the database and never shows an error in the session: a failure is one line in
// <state>/telemetry/hook.log. In a controller run (LIGHTHOUSE_CONTROLLER_RUN) and with
// LIGHTHOUSE_TELEMETRY_COLLECT=off it writes the sidecar but starts no collector.
func TelemetryCollect(_ context.Context, root string, _ string, p hook.Payload) hook.Result {
	if p.SessionID == "" {
		return hook.Result{}
	}
	state, err := collect.StateDir(os.Getenv)
	if err != nil {
		return hook.Result{}
	}
	logLine := func(msg string) {
		dir := filepath.Join(state, "telemetry")
		if os.MkdirAll(dir, 0o700) != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(dir, "hook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		msg = strings.ReplaceAll(strings.ReplaceAll(msg, "\r", " "), "\n", " ")
		_, _ = fmt.Fprintf(f, "%s telemetry-collect: COULD NOT CHECK — %s\n", time.Now().UTC().Format(time.RFC3339), msg)
	}

	obs := collect.Observation{At: time.Now().UTC(), LighthouseVersion: version.String()}
	if m, err := manifest.Read(root); err != nil {
		logLine("manifest: " + err.Error())
	} else {
		obs.PackVersion = m.Version
	}
	if err := collect.AppendSidecar(state, p.SessionID, obs); err != nil {
		logLine("sidecar: " + err.Error())
	}

	if os.Getenv("LIGHTHOUSE_CONTROLLER_RUN") != "" || os.Getenv("LIGHTHOUSE_TELEMETRY_COLLECT") == "off" {
		return hook.Result{}
	}
	start := startCollector
	done := make(chan error, 1)
	go func() { done <- start(root) }()
	select {
	case err := <-done:
		if err != nil {
			logLine("start: " + err.Error())
		}
	case <-time.After(startWait):
	}
	return hook.Result{}
}
