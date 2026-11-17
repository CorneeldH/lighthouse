package initcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/pins"
)

const pinSection = `
[pins.mytool]
kind = "tool"
repo = "o/mytool"
version = "0.5.1"

[pins.superpowers]
kind = "plugin"
repo = "obra/superpowers"
version = "7.0.2"
`

func withPins(t *testing.T, root string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(root, "lighthouse.toml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(pinSection); err != nil {
		t.Fatal(err)
	}
}

func pinEnv(run pins.Runner, installed string) func(string) pins.Env {
	return func(root string) pins.Env {
		return pins.Env{Root: root, Run: run, InstalledPlugins: installed}
	}
}

func upgradeWith(t *testing.T, root string, env func(string) pins.Env) result {
	return run(t, root, "", func(o *Options) { o.Upgrade = true; o.PinEnv = env })
}

// Fails when drift reaches stdout, when it changes the exit code, or when init does not report it.
func TestInit_reportsPinDriftOnStderrOnly(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	baseline := upgradeWith(t, root, pinEnv(nil, filepath.Join(root, "none.json")))

	withPins(t, root)
	behind := func(context.Context, string, ...string) (string, error) { return "mytool 0.4.0\n", nil }
	r := upgradeWith(t, root, pinEnv(behind, filepath.Join(root, "none.json")))

	if r.code != baseline.code || r.out != baseline.out || r.out != "No changes.\n" {
		t.Fatalf("rc %d out %q, want rc %d out %q", r.code, r.out, baseline.code, baseline.out)
	}
	if strings.Contains(r.out, "mytool") || strings.Contains(r.out, "superpowers") {
		t.Errorf("pin text on stdout: %q", r.out)
	}
	if !strings.Contains(r.err, "mytool lokaal behind") {
		t.Errorf("stderr lacks the drift: %q", r.err)
	}
	if !strings.Contains(r.err, "superpowers lokaal error") {
		t.Errorf("stderr lacks the error of the missing plugin file: %q", r.err)
	}
}

// Fails when a tool that is absent (the CI situation) makes init fail or print on stdout.
func TestInit_noToolsStillNoChanges(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	withPins(t, root)
	absent := func(context.Context, string, ...string) (string, error) { return "", exec.ErrNotFound }
	r := upgradeWith(t, root, pinEnv(absent, filepath.Join(root, "none.json")))
	if r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("rc %d out %q err %q", r.code, r.out, r.err)
	}
}

// Fails when init measures upstream: Git and Releases are nil, so an upstream call would be an error line.
func TestInit_pinCheckIsLocalOnly(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	withPins(t, root)
	current := func(context.Context, string, ...string) (string, error) { return "mytool 0.5.1\n", nil }
	r := upgradeWith(t, root, pinEnv(current, filepath.Join(root, "none.json")))
	if strings.Contains(r.err, "upstream") {
		t.Errorf("stderr mentions an upstream measurement: %q", r.err)
	}
	if strings.Contains(r.err, "mytool") {
		t.Errorf("a current pin is reported: %q", r.err)
	}
}
