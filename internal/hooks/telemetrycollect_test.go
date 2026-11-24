package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/version"
)

type starts struct{ roots []string }

// fakeStarter replaces the collector start for one test and records the repo roots it got.
func fakeStarter(t *testing.T, block chan struct{}) *starts {
	t.Helper()
	s := &starts{}
	old := startCollector
	startCollector = func(root string) error {
		s.roots = append(s.roots, root)
		if block != nil {
			<-block
		}
		return nil
	}
	t.Cleanup(func() { startCollector = old })
	return s
}

func collectRoot(t *testing.T, manifest string) (root, state string) {
	t.Helper()
	root = t.TempDir()
	if manifest != "" {
		writeFile(t, filepath.Join(root, ".lighthouse", "manifest.toml"), manifest)
	}
	state = t.TempDir()
	t.Setenv("LIGHTHOUSE_STATE_DIR", state)
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	t.Setenv("LIGHTHOUSE_TELEMETRY_COLLECT", "")
	return root, state
}

func runCollect(root, session string) hook.Result {
	return TelemetryCollect(context.Background(), root, "", hook.Payload{SessionID: session})
}

// Red when the hook writes no observation, or one without the binary or pack version.
func TestTelemetryCollectWritesSidecar(t *testing.T) {
	root, state := collectRoot(t, "version = \"1.2.3\"\n")
	s := fakeStarter(t, nil)
	res := runCollect(root, "sess-1")
	if res.Code != 0 {
		t.Fatalf("code %d", res.Code)
	}
	obs, err := collect.ReadSidecar(state, "sess-1")
	if err != nil || len(obs) != 1 {
		t.Fatalf("observations %v, err %v", obs, err)
	}
	if obs[0].PackVersion != "1.2.3" || obs[0].LighthouseVersion != version.String() || obs[0].At.IsZero() {
		t.Errorf("observation %+v", obs[0])
	}
	if len(s.roots) != 1 || s.roots[0] != root {
		t.Errorf("starts %v, want one for %s", s.roots, root)
	}
}

// Red when a controller run starts the collector, or when it skips the sidecar; also for off.
func TestTelemetryCollectSidecarInControllerRun(t *testing.T) {
	for _, env := range []struct{ k, v string }{
		{"LIGHTHOUSE_CONTROLLER_RUN", "0f8e2c1a-1b2c-4d5e-8f90-123456789abc"},
		{"LIGHTHOUSE_TELEMETRY_COLLECT", "off"},
	} {
		t.Run(env.k, func(t *testing.T) {
			root, state := collectRoot(t, "version = \"1\"\n")
			t.Setenv(env.k, env.v)
			s := fakeStarter(t, nil)
			if res := runCollect(root, "s2"); res.Code != 0 {
				t.Fatalf("code %d", res.Code)
			}
			if obs, _ := collect.ReadSidecar(state, "s2"); len(obs) != 1 {
				t.Errorf("observations %v, want one", obs)
			}
			if len(s.roots) != 0 {
				t.Errorf("collector started: %v", s.roots)
			}
		})
	}
}

// Red when a broken manifest blocks, is shown in the session, or leaves no line in the log.
func TestTelemetryCollectNeverBlocks(t *testing.T) {
	root, state := collectRoot(t, "version = [broken\n")
	fakeStarter(t, nil)
	res := runCollect(root, "s3")
	if res.Code != 0 || res.Stderr != "" || res.Output != nil {
		t.Fatalf("result %+v, want silent code 0", res)
	}
	b, err := os.ReadFile(filepath.Join(state, "telemetry", "hook.log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "manifest") {
		t.Errorf("log %q, want one line about the manifest", b)
	}
}

// Red when the hook waits for the collector to finish.
func TestTelemetryCollectReturnsWithoutWaiting(t *testing.T) {
	root, _ := collectRoot(t, "version = \"1\"\n")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	fakeStarter(t, block)
	old := startWait
	startWait = 20 * time.Millisecond
	t.Cleanup(func() { startWait = old })
	done := make(chan hook.Result)
	go func() { done <- runCollect(root, "s4") }()
	select {
	case res := <-done:
		if res.Code != 0 {
			t.Errorf("code %d", res.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook did not return while the starter blocks")
	}
}

// Red when a payload without a session id writes a file or starts the collector.
func TestTelemetryCollectNoSessionID(t *testing.T) {
	root, state := collectRoot(t, "version = \"1\"\n")
	s := fakeStarter(t, nil)
	if res := runCollect(root, ""); res.Code != 0 {
		t.Fatalf("code %d", res.Code)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("state dir has %d entries, want none", len(entries))
	}
	if len(s.roots) != 0 {
		t.Errorf("collector started: %v", s.roots)
	}
}
