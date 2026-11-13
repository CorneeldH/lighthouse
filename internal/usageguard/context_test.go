package usageguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPathsFollowXDG(t *testing.T) {
	d, tmp := testDirs(t)
	if want := filepath.Join(tmp, "state", "lighthouse", "usage-guard"); d.State != want {
		t.Errorf("State = %q, want %q", d.State, want)
	}
	if want := filepath.Join(tmp, "data", "lighthouse", "resume"); d.Resume != want {
		t.Errorf("Resume = %q, want %q", d.Resume, want)
	}
}

func TestStateDirKeepsOldName(t *testing.T) {
	// The state dir keeps its old name through the hook rename: a new name resets every
	// "once per window" key on every machine. Built from parts so a text replacement of the
	// hook name cannot touch this expectation.
	d, tmp := testDirs(t)
	if filepath.Base(d.State) != "usage"+"-guard" {
		t.Errorf("name = %q", filepath.Base(d.State))
	}
	if filepath.Base(filepath.Dir(d.State)) != "lighthouse" {
		t.Errorf("parent = %q", filepath.Dir(d.State))
	}
	if filepath.Dir(filepath.Dir(d.State)) != filepath.Join(tmp, "state") {
		t.Errorf("grandparent = %q", filepath.Dir(filepath.Dir(d.State)))
	}
}

func TestRelativeXDGIsIgnored(t *testing.T) {
	d, err := DirsFromEnv(envFunc(map[string]string{
		"XDG_STATE_HOME": "rel",
		"XDG_DATA_HOME":  "rel",
		"HOME":           "/home/u",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/u/.local/state/lighthouse/usage-guard"; d.State != want {
		t.Errorf("State = %q, want %q", d.State, want)
	}
	if want := "/home/u/.local/share/lighthouse/resume"; d.Resume != want {
		t.Errorf("Resume = %q, want %q", d.Resume, want)
	}
}

func TestDirsWithoutHomeIsAnError(t *testing.T) {
	if _, err := DirsFromEnv(envFunc(map[string]string{"HOME": "rel"})); err == nil {
		t.Error("want an error without an absolute XDG_STATE_HOME or HOME")
	}
}

func TestRunIDFromEnv(t *testing.T) {
	build := func(run string) Context {
		return BuildContext("tool", testAlarm, "", "", []byte("{}"), testNow,
			envFunc(map[string]string{"LIGHTHOUSE_CONTROLLER_RUN": run}))
	}
	if got := build(testRun).RunID; got != testRun {
		t.Errorf("RunID = %q, want %q", got, testRun)
	}
	for _, bad := range []string{"XYZ", testRun + "\n", "../x", ""} {
		if got := build(bad).RunID; got != "" {
			t.Errorf("RunID for %q = %q, want empty", bad, got)
		}
	}
}

func TestUnsafeIDsCountAsAbsent(t *testing.T) {
	for _, bad := range []any{"../x", "a/b", strings.Repeat("s", 129), "", 7} {
		c := mkCtx(t, "tool", map[string]any{"session_id": bad, "agent_id": bad})
		if c.SessionID != "" || c.AgentID != "" {
			t.Errorf("%v: SessionID=%q AgentID=%q, want both empty", bad, c.SessionID, c.AgentID)
		}
		if !c.PayloadOK {
			t.Errorf("%v: PayloadOK false", bad)
		}
	}
}

func TestSafeIDsAreKept(t *testing.T) {
	c := mkCtx(t, "tool", map[string]any{"session_id": strings.Repeat("s", 128), "agent_id": "a_1-B"})
	if c.SessionID != strings.Repeat("s", 128) {
		t.Errorf("SessionID = %q", c.SessionID)
	}
	if c.AgentID != "a_1-B" {
		t.Errorf("AgentID = %q", c.AgentID)
	}
}

func TestCwdFromPayloadElseHook(t *testing.T) {
	if got := mkCtx(t, "tool", map[string]any{"cwd": "/repo"}).Cwd; got != "/repo" {
		t.Errorf("Cwd = %q, want /repo", got)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := mkCtx(t, "tool", map[string]any{}).Cwd; got != wd {
		t.Errorf("Cwd = %q, want %q", got, wd)
	}
}

func TestBuildContextLocIsLocal(t *testing.T) {
	c := BuildContext("tool", nil, "", "", []byte("{}"), testNow, envFunc(nil))
	if c.Loc != time.Local {
		t.Errorf("Loc = %v, want time.Local", c.Loc)
	}
}

func TestToolWithAgentIDIsSubagentStand(t *testing.T) {
	c := mkCtx(t, "tool", map[string]any{"session_id": "s1", "agent_id": "a1"})
	if got := EffectiveStand(c); got != "subagent" {
		t.Errorf("EffectiveStand = %q, want subagent", got)
	}
	c = mkCtx(t, "stop", map[string]any{"session_id": "s1", "agent_id": "a1"})
	if got := EffectiveStand(c); got != "stop" {
		t.Errorf("EffectiveStand = %q, want stop", got)
	}
}

func writeToml(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	minimal := "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"
	if err := os.WriteFile(path, []byte(minimal+body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
}

func TestOptionsRead(t *testing.T) {
	writeToml(t, "\n[usage_guard]\nresume = true\n")
	r := &recorder{}
	if got := ReadOptions(r.warn); got != (Options{Resume: true}) {
		t.Errorf("ReadOptions = %+v", got)
	}
	if len(r.msgs) != 0 {
		t.Errorf("warnings: %v", r.msgs)
	}
}

func TestOptionsConfigError(t *testing.T) {
	writeToml(t, "\n[usage_guard]\nresume = \"yes\"\n")
	r := &recorder{}
	if got := ReadOptions(r.warn); got != (Options{}) {
		t.Errorf("ReadOptions = %+v, want both off", got)
	}
	if len(r.msgs) != 1 || !strings.HasPrefix(r.msgs[0], "config: ") {
		t.Errorf("warnings = %q, want one starting with %q", r.msgs, "config: ")
	}
}
