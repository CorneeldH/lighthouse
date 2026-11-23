package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/levellog"
	"github.com/CorneeldH/lighthouse/internal/levels"
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

// writeSource gives root the layout of lighthouse's own checkout, the only place `dev` builds.
func writeSource(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "cmd", "lighthouse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownHookExits3(t *testing.T) {
	tests := []struct {
		manifest string
		source   bool
		want     string
	}{
		{"v0.12.1", true, "COULD NOT CHECK: unknown hook nope — update lighthouse to v0.12.1\n"},
		{"dev", true, "COULD NOT CHECK: unknown hook nope — go build -o .lighthouse/bin/lighthouse ./cmd/lighthouse\n"},
		{"dev", false, "COULD NOT CHECK: unknown hook nope — install the latest lighthouse release\n"},
		{"", false, "COULD NOT CHECK: unknown hook nope — install the latest lighthouse release\n"},
	}
	for _, tt := range tests {
		env := newEnv(t, tt.manifest, "v0.13.0")
		if tt.source {
			writeSource(t, env.Root)
		}
		code, out, errOut := call(t, "nope", "s1", env)
		if code != ExitUnknown || out != "" || errOut != tt.want {
			t.Errorf("manifest %q source %v: code %d out %q err %q, want %d %q", tt.manifest, tt.source, code, out, errOut, ExitUnknown, tt.want)
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
	writeSource(t, env.Root)
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

// A build.failed outside lighthouse's own checkout is not ours to report: nothing builds there.
func TestNoBuildNoticeWithoutSource(t *testing.T) {
	register(t, "ok", okHook("ok"))
	env := newEnv(t, "dev", "dev")
	writeBuildFailed(t, env.Root)
	if _, out, _ := call(t, "ok", "s1", env); out != "" {
		t.Errorf("no cmd/lighthouse/main.go: stdout %q", out)
	}
}

func TestTwoNoticesJoin(t *testing.T) {
	register(t, "msg", func(context.Context, string, string, Payload) Result {
		return Result{Output: map[string]any{"systemMessage": "from hook", "other": 1.0}}
	})
	env := newEnv(t, "v0.12.1", "v0.13.0")
	writeSource(t, env.Root)
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

// A panicking hook could not check: exit 3, so run.sh applies the blocking list, and no notice.
func TestPanickingHookExits3(t *testing.T) {
	register(t, "boom", func(context.Context, string, string, Payload) Result {
		panic("nil map")
	})
	code, out, errOut := call(t, "boom", "s1", newEnv(t, "v0.12.1", "v0.13.0"))
	if code != ExitUnknown || out != "" || errOut != "COULD NOT CHECK: hook boom: panic: nil map\n" {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

// Parallel hook calls of one session claim the notice once: the claim is one atomic create.
func TestConcurrentCallsShowNoticeOnce(t *testing.T) {
	register(t, "ok", okHook("ok"))
	env := newEnv(t, "v0.12.1", "v0.13.0")
	const n = 64
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		shown = make(chan string, n)
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var out, errOut bytes.Buffer
			Run(context.Background(), "ok", "", strings.NewReader(`{"session_id":"s1"}`), &out, &errOut, env)
			if out.Len() > 0 {
				shown <- out.String()
			}
		}()
	}
	close(start)
	wg.Wait()
	close(shown)
	if got := len(shown); got != 1 {
		t.Errorf("notice shown %d times, want 1", got)
	}
}

// When the marker cannot be written the notice still shows, every time.
func TestNoticeShowsWhenMarkerCannotBeWritten(t *testing.T) {
	register(t, "ok", okHook("ok"))
	env := newEnv(t, "v0.12.1", "v0.13.0")
	env.StateDir = filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(env.StateDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, out, _ := call(t, "ok", "s1", env); message(t, out) != versionNotice {
			t.Errorf("call %d: message %q, want %q", i, message(t, out), versionNotice)
		}
	}
}

// A binary built from source reports "dev", which is no tag and gets no v.
func TestVersionNoticeNamesDevBinaryWithoutV(t *testing.T) {
	register(t, "ok", okHook("ok"))
	_, out, _ := call(t, "ok", "s1", newEnv(t, "v0.12.1", "dev"))
	if got, want := message(t, out), "lighthouse dev ≠ v0.12.1 in this repo — run lighthouse init --upgrade"; got != want {
		t.Errorf("message %q, want %q", got, want)
	}
}

// Without a state directory there is no private place for the marker: no notice at all, rather
// than one per hook call or a marker in a shared directory.
func TestNoNoticeWithoutStateDir(t *testing.T) {
	register(t, "ok", okHook("ok"))
	env := newEnv(t, "v0.12.1", "v0.13.0")
	env.StateDir = ""
	if _, out, _ := call(t, "ok", "s1", env); out != "" {
		t.Errorf("stdout %q, want none", out)
	}
}

// A hook with plain text writes it to stdout as it is, not as JSON.
func TestRunWritesText(t *testing.T) {
	register(t, "text", func(context.Context, string, string, Payload) Result {
		return Result{Text: "code-language-check: NOTICE — x\n"}
	})
	code, out, errOut := call(t, "text", "s1", newEnv(t, "dev", "dev"))
	if code != 0 || out != "code-language-check: NOTICE — x\n" || errOut != "" {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

// Under bash-write-check the outer hook owns the notices: a nested call adds none and spends
// no marker, so the outer call still shows it.
func TestRunNestedSkipsNotices(t *testing.T) {
	register(t, "text", func(context.Context, string, string, Payload) Result {
		return Result{Text: "plain\n"}
	})
	env := newEnv(t, "v0.12.1", "v0.13.0")
	writeSource(t, env.Root)
	writeBuildFailed(t, env.Root)
	env.Nested = true
	code, out, errOut := call(t, "text", "s1", env)
	if code != 0 || out != "plain\n" || errOut != "" {
		t.Errorf("nested: code %d out %q err %q, want 0 %q", code, out, errOut, "plain\n")
	}
	if entries, err := os.ReadDir(env.StateDir); err != nil || len(entries) != 0 {
		t.Errorf("state dir after a nested call: %v, %v; want empty", entries, err)
	}
	env.Nested = false
	if _, out, _ := call(t, "text", "s1", env); message(t, out) != versionNotice+"\n"+buildNotice {
		t.Errorf("outer call after nested: stdout %q, want both notices", out)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

// Text that cannot reach stdout was not delivered: COULD NOT CHECK, not a silent pass.
func TestRunTextWriteFails(t *testing.T) {
	register(t, "text", func(context.Context, string, string, Payload) Result {
		return Result{Text: "plain\n"}
	})
	var errOut bytes.Buffer
	code := Run(context.Background(), "text", "", strings.NewReader(`{}`), failingWriter{}, &errOut, newEnv(t, "dev", "dev"))
	if want := "COULD NOT CHECK: hook text: writing output: pipe closed\n"; code != ExitUnknown || errOut.String() != want {
		t.Errorf("code %d err %q, want %d %q", code, errOut.String(), ExitUnknown, want)
	}
}

// Red when Names drops a registered hook, or lists them in registration order.
func TestNamesListsRegistered(t *testing.T) {
	noop := func(context.Context, string, string, Payload) Result { return Result{} }
	register(t, "zz-names-b", noop)
	register(t, "zz-names-a", noop)
	var got []string
	for _, n := range Names() {
		if strings.HasPrefix(n, "zz-names-") {
			got = append(got, n)
		}
	}
	if len(got) != 2 || got[0] != "zz-names-a" || got[1] != "zz-names-b" {
		t.Errorf("Names() test entries = %v, want [zz-names-a zz-names-b]", got)
	}
	if !sort.StringsAreSorted(Names()) {
		t.Errorf("Names() = %v, not sorted", Names())
	}
}

// logEnv is an env whose root is a git repository, with the logbook in a private state dir.
func logEnv(t *testing.T) Env {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	env := newEnv(t, "", "")
	if out, err := exec.Command("git", "init", "-q", env.Root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return env
}

func logged(t *testing.T, env Env) map[string]int {
	t.Helper()
	counts, err := levellog.Count(env.Root, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func installedWith(c string, s levels.Stand) levels.Installed {
	return levels.Installed{Level: "light", Components: map[string]levels.Stand{c: s}}
}

func blocker(ran *bool) Func {
	return func(context.Context, string, string, Payload) Result {
		*ran = true
		return Result{Code: 2, Stderr: "first line\nsecond line\n"}
	}
}

// red if a hook on stand log still prints, blocks or leaves out the logbook line.
func TestLogStandCountsAndStaysSilent(t *testing.T) {
	env := logEnv(t)
	env.Level = installedWith("blk", levels.StandLog)
	var ran bool
	register(t, "blk", blocker(&ran))
	code, out, errOut := call(t, "blk", "s", env)
	if code != 0 || out != "" || errOut != "" || !ran {
		t.Fatalf("code=%d out=%q err=%q ran=%v, want 0, silent, ran", code, out, errOut, ran)
	}
	if got := logged(t, env); got["blk"] != 1 {
		t.Fatalf("logbook %v, want one line for blk", got)
	}
}

// red if a panic under stand log is reported as COULD NOT CHECK instead of logged.
func TestLogStandPanicStillExitsZero(t *testing.T) {
	env := logEnv(t)
	env.Level = installedWith("boom", levels.StandLog)
	register(t, "boom", func(context.Context, string, string, Payload) Result { panic("kaput") })
	code, out, errOut := call(t, "boom", "s", env)
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q, want 0 and silent", code, out, errOut)
	}
	if got := logged(t, env); got["boom"] != 1 {
		t.Fatalf("logbook %v, want one line for boom", got)
	}
}

// red if a failing logbook turns into an exit code or a message.
func TestLogStandWithUnwritableLogExitsZero(t *testing.T) {
	env := logEnv(t)
	blockedState := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blockedState, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blockedState) // a file: no directory can be made under it
	env.Level = installedWith("blk", levels.StandLog)
	var ran bool
	register(t, "blk", blocker(&ran))
	code, out, errOut := call(t, "blk", "s", env)
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q, want 0 and silent", code, out, errOut)
	}
}

// red if a hook on stand off still runs.
func TestOffStandSkipsTheHook(t *testing.T) {
	env := logEnv(t)
	env.Level = installedWith("blk", levels.StandOff)
	var ran bool
	register(t, "blk", blocker(&ran))
	code, out, errOut := call(t, "blk", "s", env)
	if code != 0 || out != "" || errOut != "" || ran {
		t.Fatalf("code=%d out=%q err=%q ran=%v, want 0, silent, not run", code, out, errOut, ran)
	}
}

// red if stand on changes the result of a hook or logs it.
func TestOnStandIsUnchanged(t *testing.T) {
	env := logEnv(t)
	env.Level = installedWith("blk", levels.StandOn)
	var ran bool
	register(t, "blk", blocker(&ran))
	code, _, errOut := call(t, "blk", "s", env)
	if code != 2 || errOut != "first line\nsecond line\n" {
		t.Fatalf("code=%d err=%q, want the hook's own 2 and stderr", code, errOut)
	}
	if got := logged(t, env); len(got) != 0 {
		t.Fatalf("logbook %v, want empty on stand on", got)
	}
}

// red if an unreadable components.toml lets a hook through or is not exit 3.
func TestCorruptComponentsFileIsExit3(t *testing.T) {
	env := logEnv(t)
	env.LevelErr = errors.New("parse: bad")
	var ran bool
	register(t, "blk", blocker(&ran))
	code, _, errOut := call(t, "blk", "s", env)
	if code != ExitUnknown || ran || !strings.Contains(errOut, "COULD NOT CHECK: blk: components.toml cannot be read: parse: bad") {
		t.Fatalf("code=%d err=%q ran=%v, want exit 3 with COULD NOT CHECK", code, errOut, ran)
	}
}

// red if a base hook looks at LevelErr or at its stand.
func TestBaseHookIgnoresCorruptComponentsFile(t *testing.T) {
	old := levels.BaseHooks
	levels.BaseHooks = append(slices.Clone(old), "basey")
	t.Cleanup(func() { levels.BaseHooks = old })
	env := logEnv(t)
	env.LevelErr = errors.New("parse: bad")
	env.Level = installedWith("basey", levels.StandOff)
	var ran bool
	register(t, "basey", blocker(&ran))
	code, _, errOut := call(t, "basey", "s", env)
	if !ran || code != 2 || strings.Contains(errOut, "COULD NOT CHECK") {
		t.Fatalf("code=%d err=%q ran=%v, want the hook to run untouched", code, errOut, ran)
	}
}

// red if the log stand lets the version notice through.
func TestLogStandSuppressesVersionNotice(t *testing.T) {
	env := logEnv(t)
	env.ManifestVersion, env.BinaryVersion = "0.12.1", "0.13.0"
	env.Level = installedWith("ok", levels.StandLog)
	register(t, "ok", func(context.Context, string, string, Payload) Result { return Result{} })
	code, out, errOut := call(t, "ok", "s", env)
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q, want silent", code, out, errOut)
	}
	if got := logged(t, env); len(got) != 0 {
		t.Fatalf("logbook %v, want empty for a quiet pass", got)
	}
}

// red if a missing components.toml (zero Installed) is read as anything but on.
func TestAbsentComponentsFileMeansOn(t *testing.T) {
	env := logEnv(t)
	var ran bool
	register(t, "blk", blocker(&ran))
	if code, _, _ := call(t, "blk", "s", env); code != 2 || !ran {
		t.Fatalf("code=%d ran=%v, want the hook to run normally", code, ran)
	}
}
