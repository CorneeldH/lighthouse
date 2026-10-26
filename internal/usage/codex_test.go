package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fakeAppServerEnv = "LIGHTHOUSE_FAKE_APP_SERVER"

var codexFixedNow = time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC)

// TestFakeAppServer is the fake codex app-server: the test binary runs itself
// as the child process. Without the env variable it is skipped.
func TestFakeAppServer(t *testing.T) {
	if os.Getenv(fakeAppServerEnv) == "" {
		t.Skip("only runs as the child of a fake reader")
	}
	// Every mode ignores SIGTERM and drains stdin to EOF, so a method written
	// before the client closed the pipe is always logged.
	signal.Ignore(syscall.SIGTERM)
	mode := os.Getenv("FAKE_MODE")
	logf, err := os.OpenFile(os.Getenv("FAKE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(4)
	}
	logLine := func(s string) { _, _ = logf.WriteString(s + "\n") }
	logLine("home=" + os.Getenv("CODEX_HOME"))
	if mode == "stubborn" {
		logLine("pid=" + strconv.Itoa(os.Getpid()))
	}
	out := func(s string) { _, _ = os.Stdout.WriteString(s + "\n") }

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			os.Exit(0)
		}
		var msg struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		logLine(msg.Method)
		if msg.Method == "account/rateLimitResetCredit/consume" {
			os.Exit(3)
		}
		if msg.ID == nil {
			continue
		}
		switch *msg.ID {
		case 1:
			out(`{"id":1,"result":{}}`)
			switch mode {
			case "eof":
				os.Exit(0)
			case "stubborn":
				time.Sleep(time.Hour)
			}
		case 2:
			fakeAnswer(t, mode, out)
		}
	}
}

func fakeAnswer(t *testing.T, mode string, out func(string)) {
	t.Helper()
	answer := func(mutate func(map[string]any)) {
		out(`{"id":2,"result":` + fakeFixture(t, mutate) + `}`)
	}
	switch mode {
	case "ok":
		answer(nil)
	case "noise":
		out("niet-json")
		out(`{"id":7,"result":{}}`)
		answer(nil)
	case "flat":
		answer(func(r map[string]any) { delete(r, "rateLimitsByLimitId") })
	case "float":
		answer(func(r map[string]any) {
			everyLimit(r, func(l map[string]any) { l["primary"].(map[string]any)["usedPercent"] = 54.6 })
		})
	case "nowindow":
		answer(func(r map[string]any) { everyLimit(r, func(l map[string]any) { delete(l, "secondary") }) })
	case "nullreset":
		answer(func(r map[string]any) {
			everyLimit(r, func(l map[string]any) { l["primary"].(map[string]any)["resetsAt"] = nil })
		})
	case "error":
		out(`{"id":2,"error":{"message":"x"}}`)
	case "noresult":
		out(`{"id":2}`)
	}
}

// everyLimit applies f to both places the fixture carries the codex limits.
func everyLimit(r map[string]any, f func(map[string]any)) {
	if l, ok := r["rateLimits"].(map[string]any); ok {
		f(l)
	}
	if per, ok := r["rateLimitsByLimitId"].(map[string]any); ok {
		f(per["codex"].(map[string]any))
	}
}

// fakeFixture returns the fixture as one compact line, after mutate.
func fakeFixture(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "codex", "app_server_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(m)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// fakeReaderModes builds a reader whose command is this test binary; modes
// maps a home to its fake mode.
func fakeReaderModes(t *testing.T, modes map[string]string, homes ...string) (CodexReader, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "fake.log")
	return CodexReader{
		Homes:   homes,
		Timeout: 5 * time.Second,
		Now:     func() time.Time { return codexFixedNow },
		Command: func(ctx context.Context, home string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakeAppServer$")
			cmd.Env = append(os.Environ(),
				fakeAppServerEnv+"=1",
				"FAKE_MODE="+modes[home],
				"FAKE_LOG="+logPath,
				"CODEX_HOME="+home,
			)
			return cmd
		},
	}, logPath
}

func fakeReader(t *testing.T, mode string, homes ...string) (CodexReader, string) {
	t.Helper()
	modes := map[string]string{}
	for _, h := range homes {
		modes[h] = mode
	}
	return fakeReaderModes(t, modes, homes...)
}

func wantFixtureState() CodexState {
	return CodexState{
		Source:      "app-server",
		GeneratedAt: "2026-09-28T22:00:00Z",
		Session:     CodexWindow{Pct: 55, ResetAt: "2026-09-28T23:23:29Z"},
		Week:        CodexWindow{Pct: 66, ResetAt: "2026-10-03T17:17:59Z"},
	}
}

func TestCodexAppServerReadsFixture(t *testing.T) {
	// ok: plain answer; noise: non-JSON and a foreign id first; flat: only
	// the top-level rateLimits.
	for _, mode := range []string{"ok", "noise", "flat"} {
		t.Run(mode, func(t *testing.T) {
			r, _ := fakeReader(t, mode, "h")
			got, err := r.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != wantFixtureState() {
				t.Errorf("got %+v, want %+v", got, wantFixtureState())
			}
		})
	}
}

func TestCodexAppServerMethods(t *testing.T) {
	r, logPath := fakeReader(t, "ok", "h")
	if _, err := r.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "home=h\ninitialize\ninitialized\naccount/rateLimits/read\n"
	if string(raw) != want {
		t.Errorf("methods = %q, want %q", raw, want)
	}
}

func TestCodexAppServerFloatPct(t *testing.T) {
	r, _ := fakeReader(t, "float", "h")
	got, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.Pct != 54 {
		t.Errorf("Session.Pct = %d, want 54", got.Session.Pct)
	}
}

func TestCodexAppServerRejects(t *testing.T) {
	for _, mode := range []string{"error", "noresult", "eof", "nowindow", "nullreset", "hang"} {
		t.Run(mode, func(t *testing.T) {
			r, _ := fakeReader(t, mode, "h")
			if mode == "hang" {
				r.Timeout = 300 * time.Millisecond
			}
			start := time.Now()
			got, err := r.Read(context.Background())
			if err == nil {
				t.Fatalf("want an error, got %+v", got)
			}
			if got != (CodexState{}) {
				t.Errorf("state = %+v, want zero", got)
			}
			if time.Since(start) > 3*time.Second {
				t.Errorf("took %v, want under 3s", time.Since(start))
			}
		})
	}
}

func TestCodexAppServerStubborn(t *testing.T) {
	r, logPath := fakeReader(t, "stubborn", "h")
	r.Timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if took := time.Since(start); took > 300*time.Millisecond+2*time.Second+time.Second {
		t.Errorf("took %v, want within timeout + WaitDelay + 1s", took)
	}
	if !strings.Contains(err.Error(), "no answer within the timeout") {
		t.Errorf("error = %q, want the timeout reason", err)
	}
	raw, rerr := os.ReadFile(logPath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var pid int
	for _, l := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(l, "pid="); ok {
			pid, _ = strconv.Atoi(v)
		}
	}
	if pid == 0 {
		t.Fatalf("no pid in log %q", raw)
	}
	requireProcessGone(t, pid)
}

func TestCodexReaderFirstHomeWins(t *testing.T) {
	r, logPath := fakeReader(t, "ok", "a", "b")
	if _, err := r.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(logPath)
	if strings.Contains(string(raw), "home=b") || !strings.Contains(string(raw), "home=a") {
		t.Errorf("log = %q, want only home=a", raw)
	}
}

func TestCodexReaderSecondHome(t *testing.T) {
	r, _ := fakeReaderModes(t, map[string]string{"a": "eof", "b": "ok"}, "a", "b")
	got, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != wantFixtureState() {
		t.Errorf("got %+v, want %+v", got, wantFixtureState())
	}
}

func TestCodexReaderAllHomesFail(t *testing.T) {
	r, _ := fakeReader(t, "noresult", "a", "b")
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	want := "app-server: a: app-server returned an answer without result; b: app-server returned an answer without result; rollout-log: no usable rate_limits line in the session logs"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestNewCodexReaderNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := NewCodexReader(); err == nil {
		t.Error("want an error without HOME")
	}
}

func TestNewCodexReaderHomes(t *testing.T) {
	t.Setenv("HOME", "/h")
	r, err := NewCodexReader()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Homes) != 2 || r.Homes[0] != "/h/.codex" || r.Homes[1] != "/h/.codex-specreview" {
		t.Errorf("Homes = %v", r.Homes)
	}
	if r.Timeout != 20*time.Second {
		t.Errorf("Timeout = %v, want 20s", r.Timeout)
	}
}
