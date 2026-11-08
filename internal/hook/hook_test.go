package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Classification: this is a hook/gate change; the likely regression surface is the exit code,
// stderr and the systemMessage a hook call prints, and how often a notice repeats in a session.

// call runs hook name with a payload for session and returns the exit code, stdout and stderr.
func call(t *testing.T, name, session string, env Env) (int, string, string) {
	t.Helper()
	payload := `{"session_id":"` + session + `"}`
	var out, errOut bytes.Buffer
	code := Run(context.Background(), name, "", strings.NewReader(payload), &out, &errOut, env)
	return code, out.String(), errOut.String()
}

func register(t *testing.T, name string, f Func) {
	t.Helper()
	mu.Lock()
	registry[name] = f
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		delete(registry, name)
		mu.Unlock()
	})
}

func newEnv(t *testing.T, manifest, binary string) Env {
	t.Helper()
	return Env{Root: t.TempDir(), ManifestVersion: manifest, BinaryVersion: binary, StateDir: t.TempDir()}
}

func message(t *testing.T, stdout string) string {
	t.Helper()
	if stdout == "" {
		return ""
	}
	var got struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	return got.SystemMessage
}

const versionNotice = "lighthouse v0.13.0 ≠ v0.12.1 in this repo — run lighthouse init --upgrade"

func okHook(string) Func {
	return func(context.Context, string, string, Payload) Result { return Result{} }
}

func TestUnknownHookExits3(t *testing.T) {
	tests := []struct {
		manifest string
		want     string
	}{
		{"v0.12.1", "COULD NOT CHECK: unknown hook nope — update lighthouse to v0.12.1\n"},
		{"dev", "COULD NOT CHECK: unknown hook nope — go build -o .lighthouse/bin/lighthouse ./cmd/lighthouse\n"},
	}
	for _, tt := range tests {
		code, out, errOut := call(t, "nope", "s1", newEnv(t, tt.manifest, "v0.13.0"))
		if code != ExitUnknown || out != "" || errOut != tt.want {
			t.Errorf("manifest %s: code %d out %q err %q, want %d %q", tt.manifest, code, out, errOut, ExitUnknown, tt.want)
		}
	}
}

func TestVersionNoticeOncePerSession(t *testing.T) {
	register(t, "ok", okHook("ok"))
	env := newEnv(t, "v0.12.1", "v0.13.0")
	steps := []struct{ session, want string }{
		{"s1", versionNotice},
		{"s1", ""},
		{"s2", versionNotice},
	}
	for i, s := range steps {
		code, out, _ := call(t, "ok", s.session, env)
		if got := message(t, out); code != 0 || got != s.want {
			t.Errorf("step %d (%s): code %d message %q, want %q", i, s.session, code, got, s.want)
		}
	}
}

func TestVersionNoticeNotOnBlock(t *testing.T) {
	blocking := true
	register(t, "gate", func(context.Context, string, string, Payload) Result {
		if blocking {
			return Result{Code: 2, Stderr: "blocked\n"}
		}
		return Result{}
	})
	env := newEnv(t, "v0.12.1", "v0.13.0")
	code, out, errOut := call(t, "gate", "s1", env)
	if code != 2 || out != "" || errOut != "blocked\n" {
		t.Fatalf("block: code %d out %q err %q", code, out, errOut)
	}
	blocking = false
	if _, out, _ := call(t, "gate", "s1", env); message(t, out) != versionNotice {
		t.Errorf("after block: message %q, want %q", message(t, out), versionNotice)
	}
}

func TestNoVersionNoticeForDev(t *testing.T) {
	register(t, "ok", okHook("ok"))
	for _, binary := range []string{"v0.13.0", "dev"} {
		if _, out, _ := call(t, "ok", "s1", newEnv(t, "dev", binary)); out != "" {
			t.Errorf("binary %s: stdout %q, want none", binary, out)
		}
	}
}

func TestNoVersionNoticeWhenEqual(t *testing.T) {
	register(t, "ok", okHook("ok"))
	if _, out, _ := call(t, "ok", "s1", newEnv(t, "v0.13.0", "v0.13.0")); out != "" {
		t.Errorf("stdout %q, want none", out)
	}
}

func writeBuildFailed(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".lighthouse", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.failed"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

const buildNotice = "lighthouse build failed — see .lighthouse/bin/build.log; build it by hand with go build -o .lighthouse/bin/lighthouse ./cmd/lighthouse"

func TestBuildFailedNotice(t *testing.T) {
	register(t, "ok", okHook("ok"))
	env := newEnv(t, "dev", "dev")
	writeBuildFailed(t, env.Root)
	steps := []struct{ session, want string }{{"s1", buildNotice}, {"s1", ""}, {"s2", buildNotice}}
	for i, s := range steps {
		_, out, _ := call(t, "ok", s.session, env)
		if got := message(t, out); got != s.want {
			t.Errorf("step %d: message %q, want %q", i, got, s.want)
		}
	}
}

func TestNoBuildNoticeWithoutMarker(t *testing.T) {
	register(t, "ok", okHook("ok"))
	if _, out, _ := call(t, "ok", "s1", newEnv(t, "dev", "dev")); out != "" {
		t.Errorf("no build.failed: stdout %q", out)
	}
}

func TestTwoNoticesJoin(t *testing.T) {
	register(t, "msg", func(context.Context, string, string, Payload) Result {
		return Result{Output: map[string]any{"systemMessage": "from hook", "other": 1.0}}
	})
	env := newEnv(t, "v0.12.1", "v0.13.0")
	writeBuildFailed(t, env.Root)
	code, out, _ := call(t, "msg", "s1", env)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if got, want := message(t, out), "from hook\n"+versionNotice+"\n"+buildNotice; got != want {
		t.Errorf("message %q, want %q", got, want)
	}
	var all map[string]any
	if err := json.Unmarshal([]byte(out), &all); err != nil || all["other"] != 1.0 {
		t.Errorf("other output keys lost: %q", out)
	}
}

func TestHookReceivesArguments(t *testing.T) {
	var gotRoot, gotStand string
	var gotPayload Payload
	register(t, "args", func(_ context.Context, root, stand string, p Payload) Result {
		gotRoot, gotStand, gotPayload = root, stand, p
		return Result{Code: 0, Stderr: "warn\n"}
	})
	env := newEnv(t, "dev", "dev")
	var out, errOut bytes.Buffer
	in := `{"session_id":"abc","x":1}`
	code := Run(context.Background(), "args", "pre", strings.NewReader(in), &out, &errOut, env)
	if code != 0 || gotRoot != env.Root || gotStand != "pre" || gotPayload.SessionID != "abc" || string(gotPayload.Raw) != in || errOut.String() != "warn\n" {
		t.Errorf("code %d root %q stand %q payload %+v err %q", code, gotRoot, gotStand, gotPayload, errOut.String())
	}
}

func TestRegisterTwicePanics(t *testing.T) {
	register(t, "dup", okHook("dup"))
	defer func() {
		if recover() == nil {
			t.Error("second Register did not panic")
		}
	}()
	Register("dup", okHook("dup"))
}
