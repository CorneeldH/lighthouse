package usageguard

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUnreadablePayload(t *testing.T) {
	for _, text := range []string{"not json", "[]"} {
		t.Run(text, func(t *testing.T) {
			d, _ := testDirs(t)
			c := mkCtx(t, "stop", text)
			if !c.StopHookActive || c.PayloadOK || c.SessionID != "" {
				t.Errorf("ctx = %+v", c)
			}
			if dec, _ := decideWith(c, d, optsOff); dec.Output != nil {
				t.Errorf("stop Output = %v, want nil", dec.Output)
			}
			dec, _ := decideWith(mkCtx(t, "tool", text), d, optsOff)
			if dec.Output["decision"] != "block" {
				t.Errorf("tool Output = %v", dec.Output)
			}
			if got, want := doneKeys(t, d), []string{"venster-2026-09-06T12:30:00Z.tool.done"}; !reflect.DeepEqual(got, want) {
				t.Errorf("keys = %v, want %v", got, want)
			}
		})
	}
}

func TestPromptWarnsWithoutKey(t *testing.T) {
	d, _ := testDirs(t)
	c := mkCtx(t, "prompt", map[string]any{"session_id": "s1"})
	dec, _ := decideWith(c, d, optsOff)
	h, ok := dec.Output["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("Output = %v", dec.Output)
	}
	if h["hookEventName"] != "UserPromptSubmit" {
		t.Errorf("hookEventName = %v", h["hookEventName"])
	}
	text, _ := h["additionalContext"].(string)
	for _, want := range []string{"92%", "14:30", "/lighthouse-write-handoff " + HandoffPath(c, d)} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q: %s", want, text)
		}
	}
	if _, ok := dec.Output["decision"]; ok {
		t.Error("prompt output has a decision")
	}
	if dec.KeyAfter != "" {
		t.Errorf("KeyAfter = %q", dec.KeyAfter)
	}
	if got := doneKeys(t, d); len(got) != 0 {
		t.Errorf("keys = %v", got)
	}
}

func TestToolBlocksOncePerWindow(t *testing.T) {
	d, _ := testDirs(t)
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1"})
	first, _ := decideWith(c, d, optsOff)
	reason := reasonOf(t, first.Output)
	if !strings.Contains(reason, "commit") || !strings.Contains(reason, "/lighthouse-write-handoff "+HandoffPath(c, d)) {
		t.Errorf("reason = %s", reason)
	}
	if second, _ := decideWith(c, d, optsOff); !reflect.DeepEqual(second, Decision{}) {
		t.Errorf("second = %+v, want silent", second)
	}
	other, _ := decideWith(mkCtx(t, "tool", map[string]any{"session_id": "s2"}), d, optsOff)
	if other.Output["decision"] != "block" {
		t.Errorf("s2 Output = %v", other.Output)
	}
}

func TestToolWithAgentIDIsSubagent(t *testing.T) {
	d, _ := testDirs(t)
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1", "agent_id": "a1"})
	dec, _ := decideWith(c, d, optsOff)
	reason := reasonOf(t, dec.Output)
	if reason != subagentSpeak(c)["reason"] {
		t.Errorf("reason = %s", reason)
	}
	if !strings.Contains(reason, "main thread") || strings.Contains(reason, "/lighthouse-write-handoff") || strings.Contains(reason, "CronCreate") {
		t.Errorf("reason = %s", reason)
	}
	if !exists(AlarmKey(c, "subagent", d)) {
		t.Error("subagent key missing")
	}
	if exists(AlarmKey(c, "tool", d)) {
		t.Error("tool key written")
	}
}

func TestSubagentOncePerAgent(t *testing.T) {
	d, _ := testDirs(t)
	a1 := mkCtx(t, "subagent", map[string]any{"session_id": "s1", "agent_id": "a1"})
	if dec, _ := decideWith(a1, d, optsOff); dec.Output["decision"] != "block" {
		t.Errorf("a1 first = %v", dec.Output)
	}
	if dec, _ := decideWith(a1, d, optsOff); dec.Output != nil {
		t.Errorf("a1 second = %v", dec.Output)
	}
	a2 := mkCtx(t, "subagent", map[string]any{"session_id": "s1", "agent_id": "a2"})
	if dec, _ := decideWith(a2, d, optsOff); dec.Output["decision"] != "block" {
		t.Errorf("a2 = %v", dec.Output)
	}
}

func TestStop(t *testing.T) {
	d, _ := testDirs(t)
	active := mkCtx(t, "stop", map[string]any{"session_id": "s1", "stop_hook_active": true})
	if dec, _ := decideWith(active, d, optsOff); !reflect.DeepEqual(dec, Decision{}) {
		t.Errorf("stop_hook_active: %+v, want silent", dec)
	}
	c := mkCtx(t, "stop", map[string]any{"session_id": "s1"})
	dec, _ := decideWith(c, d, optsOff)
	if !strings.Contains(reasonOf(t, dec.Output), "/lighthouse-write-handoff "+HandoffPath(c, d)) {
		t.Errorf("reason = %v", dec.Output)
	}
	if dec.KeyAfter != AlarmKey(c, "stop", d) {
		t.Errorf("KeyAfter = %q", dec.KeyAfter)
	}
	if exists(dec.KeyAfter) {
		t.Error("stop key written before the block is out")
	}
}

func TestStopOncePerWindow(t *testing.T) {
	d, _ := testDirs(t)
	decideWith(mkCtx(t, "tool", map[string]any{"session_id": "s1"}), d, optsOff)
	c := mkCtx(t, "stop", map[string]any{"session_id": "s1"})
	dec, _ := decideWith(c, d, optsOff)
	if dec.Output["decision"] != "block" {
		t.Fatalf("Output = %v", dec.Output)
	}
	if err := os.WriteFile(dec.KeyAfter, nil, 0o644); err != nil { // what the hook does after printing
		t.Fatal(err)
	}
	if again, _ := decideWith(c, d, optsOff); !reflect.DeepEqual(again, Decision{}) {
		t.Errorf("again = %+v, want silent", again)
	}
}

func TestStopPrunesOldKeys(t *testing.T) {
	d, _ := testDirs(t)
	if err := os.MkdirAll(d.State, 0o755); err != nil {
		t.Fatal(err)
	}
	old, recent := filepath.Join(d.State, "old.done"), filepath.Join(d.State, "recent.done")
	for path, days := range map[string]int{old: 15, recent: 13} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	decideWith(mkCtx(t, "stop", map[string]any{"session_id": "s1"}), d, optsOff)
	if exists(old) {
		t.Error("key of 15 days survived")
	}
	if !exists(recent) {
		t.Error("key of 13 days pruned")
	}
}

func TestControllerSpeaksInARun(t *testing.T) {
	d, _ := testDirs(t)
	run := mkCtx(t, "stop", map[string]any{"session_id": "s1"})
	run.RunID = testRun
	dec, _ := decideWith(run, d, optsBoth)
	if r := reasonOf(t, dec.Output); !isControllerReason(r) {
		t.Errorf("run with both options: %s", r)
	}
	plain := mkCtx(t, "stop", map[string]any{"session_id": "s2"})
	dec, _ = decideWith(plain, d, optsBoth)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "CronCreate") {
		t.Errorf("plain session with both options: %s", r)
	}
	other := mkCtx(t, "stop", map[string]any{"session_id": "s3"})
	other.RunID = testRun
	dec, _ = decideWith(other, d, optsResume)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "CronCreate") {
		t.Errorf("run with only resume: %s", r)
	}
}

func TestSubagentNeverParks(t *testing.T) {
	d, _ := testDirs(t)
	c := mkCtx(t, "subagent", map[string]any{"session_id": "s1", "agent_id": "a1"})
	c.RunID = testRun
	dec, _ := decideWith(c, d, optsBoth)
	if !reflect.DeepEqual(dec.Output, subagentSpeak(c)) {
		t.Errorf("Output = %v", dec.Output)
	}
	if exists(d.Resume) {
		t.Error("subagent wrote a marker")
	}
}

func TestOptionsReadOnce(t *testing.T) {
	d, _ := testDirs(t)
	calls := 0
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1"})
	c.RunID = testRun
	Decide(c, d, func() Options { calls++; return optsOff }, func(string) {})
	if calls != 1 {
		t.Errorf("options read %d times, want 1", calls)
	}
}

// blockedDirs points XDG_STATE_HOME or XDG_DATA_HOME at a file.
func blockedDirs(t *testing.T, name string) Dirs {
	t.Helper()
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"XDG_STATE_HOME": filepath.Join(tmp, "state"),
		"XDG_DATA_HOME":  filepath.Join(tmp, "data"),
		"HOME":           filepath.Join(tmp, "home"),
	}
	env[name] = blocker
	d, err := DirsFromEnv(envFunc(env))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestUnwritableStateDirIsSilent(t *testing.T) {
	d := blockedDirs(t, "XDG_STATE_HOME")
	for _, tc := range []struct {
		stand   string
		payload map[string]any
	}{{"tool", map[string]any{"session_id": "s1"}}, {"subagent", map[string]any{"agent_id": "a1"}}} {
		for range 2 {
			dec, warns := decideWith(mkCtx(t, tc.stand, tc.payload), d, optsOff)
			if !reflect.DeepEqual(dec, Decision{}) {
				t.Errorf("%s: %+v, want silent", tc.stand, dec)
			}
			if len(warns) != 1 {
				t.Errorf("%s: warnings = %q, want one", tc.stand, warns)
			}
		}
	}
}

func TestUnwritableStateDirSilencesStop(t *testing.T) {
	d := blockedDirs(t, "XDG_STATE_HOME")
	dec, warns := decideWith(mkCtx(t, "stop", map[string]any{"session_id": "s1"}), d, optsOff)
	if !reflect.DeepEqual(dec, Decision{}) {
		t.Errorf("%+v, want silent", dec)
	}
	if len(warns) != 1 {
		t.Errorf("warnings = %q, want one", warns)
	}
}

// A speaker that fails releases the claim, says nothing and warns once.
func TestFailingSpeakerReleasesKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  string
		opts Options
	}{
		{"resume", "", optsResume},
		{"controller", testRun, optsController},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := blockedDirs(t, "XDG_DATA_HOME")
			c := mkCtx(t, "tool", map[string]any{"session_id": "s1"})
			c.RunID = tc.run
			dec, warns := decideWith(c, d, tc.opts)
			if !reflect.DeepEqual(dec, Decision{}) {
				t.Errorf("%+v, want silent", dec)
			}
			if exists(AlarmKey(c, "tool", d)) {
				t.Error("claim not released")
			}
			if len(warns) != 1 {
				t.Errorf("warnings = %q, want one", warns)
			}
		})
	}
}

// pauseCtx is a tool call of a controller run with a PAUSE flag until testUntil, no alarm.
func pauseCtx(t *testing.T, tmp string, payload map[string]any) Context {
	t.Helper()
	flag := filepath.Join(tmp, "ctl", "PAUSE")
	if !exists(flag) {
		if err := os.MkdirAll(filepath.Dir(flag), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(flag, []byte(testUntil+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if payload == nil {
		payload = map[string]any{"session_id": "s1"}
	}
	c := mkCtx(t, "tool", payload)
	c.Alarm = nil
	c.PauseUntil = testUntil
	c.PauseFlag = flag
	c.RunID = testRun
	return c
}

func TestFailingPauseWithoutAlarmIsSilent(t *testing.T) {
	d := blockedDirs(t, "XDG_DATA_HOME")
	c := pauseCtx(t, t.TempDir(), nil)
	dec, warns := decideWith(c, d, optsController)
	if !reflect.DeepEqual(dec, Decision{}) {
		t.Errorf("%+v, want silent", dec)
	}
	if exists(PauseKey(c, d)) {
		t.Error("pause key written")
	}
	if len(warns) != 1 {
		t.Errorf("warnings = %q, want one", warns)
	}
}

func TestFailingPauseFallsThroughToAlarm(t *testing.T) {
	d, tmp := testDirs(t)
	c := pauseCtx(t, tmp, nil)
	c.Alarm = testAlarm
	// A directory where the pause key goes: the pause speaks, but its key cannot land.
	if err := os.MkdirAll(PauseKey(c, d), 0o755); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(c.PauseFlag, later, later); err != nil {
		t.Fatal(err)
	}
	dec, warns := decideWith(c, d, optsController)
	reason := reasonOf(t, dec.Output)
	if !strings.HasPrefix(reason, "Usage is at 92%") {
		t.Errorf("reason = %s, want the alarm's", reason)
	}
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "pause: ") {
		t.Errorf("warnings = %q, want one pause warning", warns)
	}
}

func TestNoAlarmNoPause(t *testing.T) {
	for _, stand := range []string{"prompt", "tool", "subagent", "stop"} {
		d, _ := testDirs(t)
		c := mkCtx(t, stand, map[string]any{"session_id": "s1", "agent_id": "a1"})
		c.Alarm = nil
		c.RunID = testRun
		if dec, _ := decideWith(c, d, optsBoth); !reflect.DeepEqual(dec, Decision{}) {
			t.Errorf("%s: %+v, want silent", stand, dec)
		}
		if got := doneKeys(t, d); len(got) != 0 {
			t.Errorf("%s: keys = %v", stand, got)
		}
	}
}

func TestPauseParksOncePerFlag(t *testing.T) {
	d, tmp := testDirs(t)
	c := pauseCtx(t, tmp, nil)
	dec, _ := decideWith(c, d, optsController)
	if r := reasonOf(t, dec.Output); !strings.HasPrefix(r, "The controller has paused this run until 21:00.") {
		t.Errorf("reason = %s", r)
	}
	if !exists(PauseKey(c, d)) {
		t.Fatal("pause key missing")
	}
	if again, _ := decideWith(c, d, optsController); !reflect.DeepEqual(again, Decision{}) {
		t.Errorf("again = %+v, want silent", again)
	}
	fi, err := os.Stat(PauseKey(c, d))
	if err != nil {
		t.Fatal(err)
	}
	later := fi.ModTime().Add(10 * time.Second)
	if err := os.Chtimes(c.PauseFlag, later, later); err != nil {
		t.Fatal(err)
	}
	if dec, _ := decideWith(c, d, optsController); !strings.HasPrefix(reasonOf(t, dec.Output), "The controller has paused") {
		t.Errorf("newer flag: %v", dec.Output)
	}
}

func TestPauseNeedsOptionToolAndMainThread(t *testing.T) {
	d, tmp := testDirs(t)
	if dec, _ := decideWith(pauseCtx(t, tmp, nil), d, optsOff); !reflect.DeepEqual(dec, Decision{}) {
		t.Errorf("option off: %+v", dec)
	}
	sub := pauseCtx(t, tmp, map[string]any{"session_id": "s1", "agent_id": "a1"})
	if dec, _ := decideWith(sub, d, optsController); !reflect.DeepEqual(dec, Decision{}) {
		t.Errorf("subagent: %+v", dec)
	}
	for _, stand := range []string{"prompt", "stop"} {
		c := pauseCtx(t, tmp, nil)
		c.Stand = stand
		if dec, _ := decideWith(c, d, optsController); !reflect.DeepEqual(dec, Decision{}) {
			t.Errorf("%s: %+v", stand, dec)
		}
	}
	if got := doneKeys(t, d); len(got) != 0 {
		t.Errorf("keys = %v", got)
	}
}

func TestMarkerAndWriteMarker(t *testing.T) {
	d, _ := testDirs(t)
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1", "cwd": "/repo"})
	m := newMarker(c, "s1", "venster", testAlarm.ResetAt, "tool")
	m.HandoffPath = "/h"
	if err := writeMarker(d, m); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d.Resume, "s1.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "session_id": "s1",
  "cwd": "/repo",
  "window": "venster",
  "reset_at": "2026-09-06T12:30:00Z",
  "created_at": "2026-09-06T11:58:00Z",
  "source": "tool",
  "handoff_path": "/h"
}
`
	if string(b) != want {
		t.Errorf("marker =\n%s\nwant\n%s", b, want)
	}
	entries, err := os.ReadDir(d.Resume)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "s1.json" {
		t.Errorf("resume dir holds %v, want only s1.json", entries)
	}
}

func TestParallelToolSpeaksOnce(t *testing.T) {
	d, _ := testDirs(t)
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1"})
	var wg sync.WaitGroup
	outs := make([]Decision, 2)
	for i := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i] = Decide(c, d, func() Options { return optsOff }, func(string) {})
		}()
	}
	wg.Wait()
	spoke := 0
	for _, o := range outs {
		if o.Output != nil {
			spoke++
		}
	}
	if spoke != 1 {
		t.Errorf("%d calls spoke, want 1", spoke)
	}
}

// --- from test_usage_gate_controller.py: the state a controller run leaves behind ---

var baseFields = []string{"created_at", "cwd", "reset_at", "session_id", "source", "window"}

func keysOf(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

func sortedStrings(s []string) []string { return slices.Sorted(slices.Values(s)) }

func isControllerReason(r string) bool {
	return strings.Contains(r, "controller") && !strings.Contains(r, "/lighthouse-write-handoff") && !strings.Contains(r, "CronCreate")
}

func runCtx(t *testing.T, stand string, cwd string, extra map[string]any) Context {
	t.Helper()
	payload := map[string]any{"session_id": "s1", "cwd": cwd}
	for k, v := range extra {
		payload[k] = v
	}
	c := mkCtx(t, stand, payload)
	c.RunID = testRun
	return c
}

func TestAlarmParksWithControllerReason(t *testing.T) {
	for _, stand := range []string{"tool", "stop"} {
		for _, o := range []Options{optsController, optsBoth} {
			d, tmp := testDirs(t)
			dec, warns := decideWith(runCtx(t, stand, tmp, nil), d, o)
			if r := reasonOf(t, dec.Output); !isControllerReason(r) {
				t.Errorf("%s %+v: reason = %s", stand, o, r)
			}
			if len(warns) != 0 {
				t.Errorf("warnings: %q", warns)
			}
			m := readMarkerFile(t, d, "s1")
			if got, want := keysOf(m), sortedStrings(append([]string{"controller"}, baseFields...)); !reflect.DeepEqual(got, want) {
				t.Errorf("fields = %v, want %v", got, want)
			}
			if m["controller"] != true || m["window"] != "venster" || m["source"] != stand ||
				m["reset_at"] != testAlarm.ResetAt || m["cwd"] != tmp {
				t.Errorf("marker = %v", m)
			}
			if stand == "tool" && !exists(filepath.Join(d.State, "venster-2026-09-06T12:30:00Z.tool.s1."+testRun+".done")) {
				t.Errorf("keys = %v", doneKeys(t, d))
			}
			if stand == "stop" && !strings.HasSuffix(dec.KeyAfter, "stop.s1."+testRun+".done") {
				t.Errorf("KeyAfter = %q", dec.KeyAfter)
			}
			if m, _ := filepath.Glob(filepath.Join(d.Resume, "*.handoff.md")); len(m) != 0 {
				t.Errorf("handoff files: %v", m)
			}
		}
	}
}

func TestWithoutSessionIDRunIDIsSession(t *testing.T) {
	d, tmp := testDirs(t)
	c := mkCtx(t, "tool", map[string]any{"cwd": tmp})
	c.RunID = testRun
	dec, _ := decideWith(c, d, optsController)
	if r := reasonOf(t, dec.Output); !isControllerReason(r) {
		t.Errorf("reason = %s", r)
	}
	if m := readMarkerFile(t, d, testRun); m["session_id"] != testRun {
		t.Errorf("session_id = %v", m["session_id"])
	}
	if m, _ := filepath.Glob(filepath.Join(d.Resume, "*.json")); len(m) != 1 || filepath.Base(m[0]) != testRun+".json" {
		t.Errorf("markers = %v", m)
	}
}

func TestThreeRunsEachOnce(t *testing.T) {
	d, tmp := testDirs(t)
	for _, run := range []string{"aaaa-1", "bbbb-2", "cccc-3"} {
		c := runCtx(t, "tool", tmp, nil)
		c.RunID = run
		first, _ := decideWith(c, d, optsController)
		second, _ := decideWith(c, d, optsController)
		if r := reasonOf(t, first.Output); !isControllerReason(r) {
			t.Errorf("%s: reason = %s", run, r)
		}
		if second.Output != nil {
			t.Errorf("%s: second spoke", run)
		}
	}
	if got := doneKeys(t, d); len(got) != 3 {
		t.Errorf("keys = %v, want 3", got)
	}
}

func TestOptionOffIsPlainSession(t *testing.T) {
	d, tmp := testDirs(t)
	dec, _ := decideWith(runCtx(t, "tool", tmp, nil), d, optsOff)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "/lighthouse-write-handoff") {
		t.Errorf("reason = %s", r)
	}
	if exists(d.Resume) {
		t.Error("resume dir created")
	}
}

func TestInvalidRunIDIsPlainSession(t *testing.T) {
	d, tmp := testDirs(t)
	c := BuildContext("tool", testAlarm, "", "", []byte(`{"session_id":"s1","cwd":"`+tmp+`"}`), testNow,
		envFunc(map[string]string{"LIGHTHOUSE_CONTROLLER_RUN": "XYZ"}))
	c.Loc = time.UTC
	dec, _ := decideWith(c, d, optsController)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "/lighthouse-write-handoff") {
		t.Errorf("reason = %s", r)
	}
	if exists(d.Resume) {
		t.Error("resume dir created")
	}
}

func TestPromptWarning(t *testing.T) {
	d, tmp := testDirs(t)
	dec, _ := decideWith(runCtx(t, "prompt", tmp, nil), d, optsController)
	r := reasonOf(t, dec.Output)
	if !strings.Contains(r, "92%") || strings.Contains(r, "CronCreate") || strings.Contains(r, "/lighthouse-write-handoff") {
		t.Errorf("reason = %s", r)
	}
	if exists(d.Resume) {
		t.Error("resume dir created")
	}
}

func TestSubagentIgnoresController(t *testing.T) {
	d, tmp := testDirs(t)
	dec, _ := decideWith(runCtx(t, "tool", tmp, map[string]any{"agent_id": "a1"}), d, optsController)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "Stop now and report") {
		t.Errorf("reason = %s", r)
	}
	if exists(d.Resume) {
		t.Error("resume dir created")
	}
}

func TestPauseParksOnce(t *testing.T) {
	d, tmp := testDirs(t)
	c := pauseCtx(t, tmp, map[string]any{"session_id": "s1", "cwd": tmp})
	first, warns := decideWith(c, d, optsController)
	r := reasonOf(t, first.Output)
	if !isControllerReason(r) || !strings.Contains(r, "21:00") || strings.Contains(r, testUntil) {
		t.Errorf("reason = %s", r)
	}
	if len(warns) != 0 {
		t.Errorf("warnings: %q", warns)
	}
	m := readMarkerFile(t, d, "s1")
	if got, want := keysOf(m), sortedStrings(append([]string{"controller", "pause"}, baseFields...)); !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
	if m["window"] != "pause" || m["pause"] != true || m["controller"] != true || m["reset_at"] != testUntil || m["source"] != "tool" {
		t.Errorf("marker = %v", m)
	}
	if got, want := doneKeys(t, d), []string{"pause-" + testUntil + "." + testRun + ".done"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if again, _ := decideWith(c, d, optsController); again.Output != nil {
		t.Errorf("second spoke: %v", again.Output)
	}

	m["created_at"] = "2000-01-01T00:00:00Z"
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(d.Resume, "s1.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(c.PauseFlag, later, later); err != nil {
		t.Fatal(err)
	}
	third, _ := decideWith(c, d, optsController)
	if r := reasonOf(t, third.Output); !isControllerReason(r) {
		t.Errorf("reason = %s", r)
	}
	if got := readMarkerFile(t, d, "s1")["created_at"]; got == "2000-01-01T00:00:00Z" {
		t.Error("created_at not refreshed")
	}
}

func TestPauseWithAgentIDDoesNotPark(t *testing.T) {
	d, tmp := testDirs(t)
	c := pauseCtx(t, tmp, map[string]any{"session_id": "s1", "agent_id": "a1", "cwd": tmp})
	if dec, _ := decideWith(c, d, optsController); dec.Output != nil {
		t.Errorf("spoke: %v", dec.Output)
	}
	c.Alarm = testAlarm
	dec, _ := decideWith(c, d, optsController)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "Stop now and report") {
		t.Errorf("reason = %s", r)
	}
	if exists(d.Resume) {
		t.Error("resume dir created")
	}
}

func TestPauseOptionOffFallsThrough(t *testing.T) {
	d, tmp := testDirs(t)
	c := pauseCtx(t, tmp, nil)
	c.Alarm = testAlarm
	dec, _ := decideWith(c, d, optsOff)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "/lighthouse-write-handoff") {
		t.Errorf("reason = %s", r)
	}
	for _, k := range doneKeys(t, d) {
		if strings.HasPrefix(k, "pause-") {
			t.Errorf("pause key %s", k)
		}
	}
	if exists(d.Resume) {
		t.Error("resume dir created")
	}
}

func TestPauseOnlyInTool(t *testing.T) {
	for _, stand := range []string{"prompt", "stop"} {
		d, tmp := testDirs(t)
		c := pauseCtx(t, tmp, nil)
		c.Stand = stand
		if dec, _ := decideWith(c, d, optsController); dec.Output != nil {
			t.Errorf("%s spoke: %v", stand, dec.Output)
		}
		if got := doneKeys(t, d); len(got) != 0 {
			t.Errorf("%s keys = %v", stand, got)
		}
	}
}

func TestPauseThenAlarmKeepsTheLaterPauseEnd(t *testing.T) {
	d, tmp := testDirs(t)
	c := pauseCtx(t, tmp, nil)
	c.Alarm = testAlarm // resets 2026-09-06T12:30Z, before the pause end
	if r := reasonOf(t, mustDecide(t, c, d).Output); !strings.Contains(r, "21:00") {
		t.Errorf("first = %s", r)
	}
	second := mustDecide(t, c, d) // the alarm path, a pause marker exists
	if r := reasonOf(t, second.Output); !strings.Contains(r, "resumes this session after 21:00.") {
		t.Errorf("second = %s", r)
	}
	checkPause := func() {
		m := readMarkerFile(t, d, "s1")
		if m["window"] != "pause" || m["pause"] != true || m["reset_at"] != testUntil || m["controller"] != true {
			t.Errorf("marker = %v", m)
		}
	}
	checkPause()
	stop := pauseCtx(t, tmp, nil)
	stop.Alarm = testAlarm
	stop.Stand = "stop"
	if r := reasonOf(t, mustDecide(t, stop, d).Output); !strings.Contains(r, "resumes this session after 21:00.") {
		t.Errorf("stop = %s", r)
	}
	checkPause()
}

func mustDecide(t *testing.T, c Context, d Dirs) Decision {
	t.Helper()
	dec, warns := decideWith(c, d, optsController)
	if len(warns) != 0 {
		t.Errorf("warnings: %q", warns)
	}
	return dec
}

func TestAlarmThenEarlierPauseKeepsTheAlarmEnd(t *testing.T) {
	d, tmp := testDirs(t)
	late := *testAlarm
	late.ResetAt = "2026-10-03T22:30:00Z" // after the pause end (19:00Z)
	c := runCtx(t, "tool", tmp, nil)
	c.Alarm = &late
	mustDecide(t, c, d)
	if m := readMarkerFile(t, d, "s1"); m["window"] != "venster" {
		t.Fatalf("marker = %v", m)
	}
	p := pauseCtx(t, tmp, nil)
	dec := mustDecide(t, p, d)
	m := readMarkerFile(t, d, "s1")
	if _, ok := m["pause"]; ok || m["window"] != "venster" || m["reset_at"] != late.ResetAt || m["controller"] != true {
		t.Errorf("marker = %v", m)
	}
	if r := reasonOf(t, dec.Output); !strings.Contains(r, "resumes this session after 22:30.") { // Loc UTC
		t.Errorf("reason = %s", r)
	}
}

func TestUnreadableMarkerIsOverwritten(t *testing.T) {
	d, tmp := testDirs(t)
	if err := os.MkdirAll(d.Resume, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Resume, "s1.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustDecide(t, runCtx(t, "tool", tmp, nil), d)
	if m := readMarkerFile(t, d, "s1"); m["window"] != "venster" {
		t.Errorf("marker = %v", m)
	}
}

// The wake-up cron and the clock of a winning alarm marker are in Context.Loc, like local time
// in the Python.
func TestLocalTimeFollowsLoc(t *testing.T) {
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	d, tmp := testDirs(t)
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1"})
	c.Loc = ams
	dec, _ := decideWith(c, d, optsResume)
	if r := reasonOf(t, dec.Output); !strings.Contains(r, `cron "32 14 6 9 *"`) {
		t.Errorf("resume reason = %s", r)
	}

	late := *testAlarm
	late.ResetAt = "2026-10-03T22:30:00Z"
	run := runCtx(t, "tool", tmp, map[string]any{"session_id": "s2"})
	run.Alarm = &late
	mustDecide(t, run, d)
	p := pauseCtx(t, tmp, map[string]any{"session_id": "s2"})
	p.Loc = ams
	if r := reasonOf(t, mustDecide(t, p, d).Output); !strings.Contains(r, "resumes this session after 00:30.") {
		t.Errorf("pause reason = %s", r)
	}
}
