package usageguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

var (
	testNow   = time.Date(2026, 9, 6, 11, 58, 0, 0, time.UTC)
	testAlarm = &usage.Alarm{Kind: "venster", Pct: 92, ResetLocal: "14:30", ResetAt: "2026-09-06T12:30:00Z"}
)

const (
	testRun   = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"
	testUntil = "2026-10-03T21:00:00+02:00"
)

var (
	optsOff        = Options{}
	optsResume     = Options{Resume: true}
	optsController = Options{Controller: true}
	optsBoth       = Options{Resume: true, Controller: true}
)

// envFunc is a getenv over a fixed map.
func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// testDirs are the dirs under XDG_STATE_HOME=<tmp>/state and XDG_DATA_HOME=<tmp>/data.
func testDirs(t *testing.T) (Dirs, string) {
	t.Helper()
	tmp := t.TempDir()
	d, err := DirsFromEnv(envFunc(map[string]string{
		"XDG_STATE_HOME": filepath.Join(tmp, "state"),
		"XDG_DATA_HOME":  filepath.Join(tmp, "data"),
		"HOME":           filepath.Join(tmp, "home"),
	}))
	if err != nil {
		t.Fatalf("DirsFromEnv: %v", err)
	}
	return d, tmp
}

// mkCtx builds a Context from a payload (a string, or a map encoded as JSON) with the alarm
// of the tests, outside a controller run, and local time UTC.
func mkCtx(t *testing.T, stand string, payload any) Context {
	t.Helper()
	text, ok := payload.(string)
	if !ok {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		text = string(b)
	}
	c := BuildContext(stand, testAlarm, "", "", []byte(text), testNow, envFunc(nil))
	c.Loc = time.UTC
	return c
}

type recorder struct{ msgs []string }

func (r *recorder) warn(m string) { r.msgs = append(r.msgs, m) }

func decideWith(c Context, d Dirs, o Options) (Decision, []string) {
	r := &recorder{}
	dec := Decide(c, d, func() Options { return o }, r.warn)
	return dec, r.msgs
}

func doneKeys(t *testing.T, d Dirs) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(d.State, "*.done"))
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, p := range m {
		out = append(out, filepath.Base(p))
	}
	sort.Strings(out)
	return out
}

func readMarkerFile(t *testing.T, d Dirs, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(d.Resume, name+".json"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("marker not JSON: %v", err)
	}
	return m
}

func reasonOf(t *testing.T, out map[string]any) string {
	t.Helper()
	if out == nil {
		t.Fatal("no output")
	}
	if r, ok := out["reason"].(string); ok {
		if out["decision"] != "block" {
			t.Fatalf("decision = %v, want block", out["decision"])
		}
		return r
	}
	h, ok := out["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("output has neither reason nor hookSpecificOutput: %v", out)
	}
	s, _ := h["additionalContext"].(string)
	return s
}
