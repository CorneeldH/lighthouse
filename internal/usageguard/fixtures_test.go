package usageguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the wake_cron fixtures name zones

	"github.com/CorneeldH/lighthouse/internal/usage"
)

// The fixtures are shared with tests/pack/test_usage_gate_lib.py; the Python is the oracle.
const fixtureDir = "../../tests/pack/fixtures/usage_guard"

func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name+".json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
}

type fixtureAlarm struct {
	Kind       string `json:"kind"`
	Pct        string `json:"pct"`
	ResetLocal string `json:"reset_local"`
	ResetAt    string `json:"reset_at"`
}

type fixtureCase struct {
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	Speaker    string          `json:"speaker"`
	Stand      string          `json:"stand"`
	Payload    map[string]any  `json:"payload"`
	SessionID  *string         `json:"session_id"`
	AgentID    *string         `json:"agent_id"`
	Alarm      *fixtureAlarm   `json:"alarm"`
	RunID      string          `json:"run_id"`
	PauseUntil string          `json:"pause_until"`
	Now        string          `json:"now"`
	Expect     json.RawMessage `json:"expect"`
}

// fixtureCtx is the Context a keys or speech case describes, like _fixture_ctx in Python.
func fixtureCtx(t *testing.T, fc fixtureCase) Context {
	t.Helper()
	payload := map[string]any{}
	for k, v := range fc.Payload {
		payload[k] = v
	}
	if fc.SessionID != nil {
		payload["session_id"] = *fc.SessionID
	}
	if fc.AgentID != nil {
		payload["agent_id"] = *fc.AgentID
	}
	fa := fc.Alarm
	if fc.Kind == "alarm" {
		fa = &fixtureAlarm{Kind: "venster", Pct: "92", ResetLocal: "14:30", ResetAt: "2026-09-06T12:30:00Z"}
	}
	var alarm *usage.Alarm
	if fa != nil {
		pct, err := strconv.Atoi(fa.Pct)
		if err != nil {
			t.Fatal(err)
		}
		alarm = &usage.Alarm{Kind: fa.Kind, Pct: pct, ResetLocal: fa.ResetLocal, ResetAt: fa.ResetAt}
	}
	now, err := time.Parse("2006-01-02T15:04:05Z", fc.Now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	c := BuildContext(fc.Stand, alarm, fc.PauseUntil, "", b, now, envFunc(nil))
	c.RunID = fc.RunID
	c.Loc = time.UTC
	return c
}

func TestKeysFixtures(t *testing.T) {
	var cases []fixtureCase
	loadFixture(t, "keys", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, fc := range cases {
		t.Run(fc.Name, func(t *testing.T) {
			d, _ := testDirs(t)
			c := fixtureCtx(t, fc)
			var path, base string
			switch fc.Kind {
			case "alarm":
				path, base = AlarmKey(c, fc.Stand, d), d.State
			case "pause":
				path, base = PauseKey(c, d), d.State
			default:
				path, base = HandoffPath(c, d), d.Resume
			}
			var want string
			if err := json.Unmarshal(fc.Expect, &want); err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				t.Fatal(err)
			}
			if rel != want {
				t.Errorf("got %q, want %q", rel, want)
			}
		})
	}
}

func TestSpeechFixtures(t *testing.T) {
	var cases []fixtureCase
	loadFixture(t, "speech", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, fc := range cases {
		t.Run(fc.Name, func(t *testing.T) {
			d, _ := testDirs(t)
			c := fixtureCtx(t, fc)
			var out map[string]any
			var err error
			switch fc.Speaker {
			case "base":
				out = baseSpeak(c, fc.Stand, d, "", "")
			case "subagent":
				out = subagentSpeak(c)
			case "resume":
				out, err = resumeSpeak(c, fc.Stand, d)
			case "controller":
				out, err = controllerSpeak(c, fc.Stand, d)
			default:
				out, err = pauseSpeak(c, d)
			}
			if err != nil {
				t.Fatalf("speaker failed: %v", err)
			}
			var want map[string]any
			expect := strings.ReplaceAll(string(fc.Expect), "{resume}", d.Resume)
			if err := json.Unmarshal([]byte(expect), &want); err != nil {
				t.Fatal(err)
			}
			// Compare as JSON, the form the hook prints.
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %v\nwant %v", got, want)
			}
		})
	}
}

func TestWakeCronFixtures(t *testing.T) {
	var cases []struct {
		ResetAt string `json:"reset_at"`
		TZ      string `json:"tz"`
		Expect  string `json:"expect"`
	}
	loadFixture(t, "wake_cron", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, fc := range cases {
		t.Run(fc.ResetAt+" "+fc.TZ, func(t *testing.T) {
			loc, err := time.LoadLocation(fc.TZ)
			if err != nil {
				t.Fatal(err)
			}
			got, err := WakeCron(fc.ResetAt, loc)
			if err != nil {
				t.Fatal(err)
			}
			if got != fc.Expect {
				t.Errorf("got %q, want %q", got, fc.Expect)
			}
		})
	}
}
