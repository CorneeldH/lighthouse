// Package usageguard is the decider of the usage-gate hook: who speaks, once per window, when
// the usage alarm is up or the controller's PAUSE flag holds. It reads the hook payload into a
// Context, builds the paths and keys of the contract (docs/usage-gate.md), claims the "once per
// window" key and picks the speaker: the base reasons, the subagent reason, the resume speaker
// or the controller speaker. An error never blocks: it is a warning and silence.
package usageguard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

var (
	// Payload ids end up in file names; anything else counts as absent.
	safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	// LIGHTHOUSE_CONTROLLER_RUN counts only in the shape of a uuid.
	runID = regexp.MustCompile(`^[0-9a-f-]+$`)
)

// Context is one call of the hook.
type Context struct {
	Stand          string       // prompt | tool | subagent | stop
	Alarm          *usage.Alarm // nil without a valid alarm
	SessionID      string       // "" when absent or not a safe id
	AgentID        string       // same
	Cwd            string       // payload cwd, else the working directory of the hook
	RunID          string       // "" outside a controller run
	PauseUntil     string       // "" without a valid pause flag
	PauseFlag      string       // path of the flag, "" without
	StopHookActive bool         // true as well for an unreadable or non-object payload
	PayloadOK      bool
	Now            time.Time
	Loc            *time.Location // local time: the wake-up cron and the clock of an alarm marker
}

func safe(v any) string {
	if s, ok := v.(string); ok && safeID.MatchString(s) {
		return s
	}
	return ""
}

// BuildContext reads the hook payload; getenv supplies LIGHTHOUSE_CONTROLLER_RUN.
func BuildContext(stand string, alarm *usage.Alarm, pauseUntil, pauseFlag string, payload []byte, now time.Time, getenv func(string) string) Context {
	var parsed any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		parsed = nil
	}
	data, ok := parsed.(map[string]any)
	cwd, _ := data["cwd"].(string)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	run := getenv("LIGHTHOUSE_CONTROLLER_RUN")
	if !runID.MatchString(run) {
		run = ""
	}
	// An unreadable payload must not block at Stop: it would loop.
	active := true
	if ok {
		active = data["stop_hook_active"] == true
	}
	return Context{
		Stand:          stand,
		Alarm:          alarm,
		SessionID:      safe(data["session_id"]),
		AgentID:        safe(data["agent_id"]),
		Cwd:            cwd,
		RunID:          run,
		PauseUntil:     pauseUntil,
		PauseFlag:      pauseFlag,
		StopHookActive: active,
		PayloadOK:      ok,
		Now:            now,
		Loc:            time.Local,
	}
}

// Dirs are the key directory and the resume marker directory.
type Dirs struct{ State, Resume string }

// DirsFromEnv is State <XDG_STATE_HOME|$HOME/.local/state>/lighthouse/usage-guard and Resume
// park.MarkerDir. An XDG variable that is not an absolute path counts as not set.
func DirsFromEnv(getenv func(string) string) (Dirs, error) {
	base := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return Dirs{}, errors.New("neither XDG_STATE_HOME nor HOME is an absolute path: no place for the usage-guard keys")
		}
		base = filepath.Join(home, ".local", "state")
	}
	resume, err := park.MarkerDir(getenv)
	if err != nil {
		return Dirs{}, err
	}
	// The name stays usage-guard through the hook rename: a new name resets every key.
	return Dirs{State: filepath.Join(base, "lighthouse", "usage-guard"), Resume: resume}, nil
}

// EffectiveStand handles a tool call from inside a subagent as stand subagent.
func EffectiveStand(c Context) string {
	if c.Stand == "tool" && c.AgentID != "" {
		return "subagent"
	}
	return c.Stand
}

// AlarmKey is <kind>-<reset_at>.<stand>[.<session_id>][.<run_id>|.<agent_id>].done in State;
// "" without an alarm.
func AlarmKey(c Context, stand string, d Dirs) string {
	if c.Alarm == nil {
		return ""
	}
	parts := []string{c.Alarm.Kind + "-" + c.Alarm.ResetAt, stand}
	if c.SessionID != "" {
		parts = append(parts, c.SessionID)
	}
	if stand == "subagent" && c.AgentID != "" {
		parts = append(parts, c.AgentID)
	} else if c.RunID != "" {
		parts = append(parts, c.RunID)
	}
	return filepath.Join(d.State, strings.Join(parts, ".")+".done")
}

// PauseKey is pause-<until>.<run_id>.done in State.
func PauseKey(c Context, d Dirs) string {
	return filepath.Join(d.State, "pause-"+c.PauseUntil+"."+c.RunID+".done")
}

// HandoffPath is <session_id>.handoff.md in Resume, or no-session-<unix now>.handoff.md.
func HandoffPath(c Context, d Dirs) string {
	name := c.SessionID
	if name == "" {
		name = "no-session-" + strconv.FormatInt(c.Now.Unix(), 10)
	}
	return filepath.Join(d.Resume, name+".handoff.md")
}

// Options are the [usage_guard] options of lighthouse.toml.
type Options struct{ Resume, Controller bool }

// ReadOptions reads [usage_guard]; a config error is a warning and counts as both off.
func ReadOptions(warn func(string)) Options {
	path, err := config.Path()
	if err != nil {
		warn("config: " + err.Error())
		return Options{}
	}
	cfg, err := config.Load(path)
	if err != nil {
		warn("config: " + err.Error())
		return Options{}
	}
	var o Options
	for key, dst := range map[string]*bool{"usage_guard.resume": &o.Resume, "usage_guard.controller": &o.Controller} {
		v, err := config.Lookup(cfg, key)
		if err != nil {
			warn("config: " + err.Error())
			return Options{}
		}
		*dst, _ = v.(bool)
	}
	return o
}
