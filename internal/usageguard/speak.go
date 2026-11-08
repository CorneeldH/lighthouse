package usageguard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var kindLabel = map[string]string{"venster": "session window", "week": "weekly window"}

// parkSteps is what a controller run is told to do when it parks.
const parkSteps = "commit the work in progress, then stop: do not start a new step, do not dispatch a " +
	"subagent, do not schedule a wake-up and do not write a handoff"

// wakeDelay is how long after the reset the wake-up fires.
const wakeDelay = 2 * time.Minute

func block(reason string) map[string]any {
	return map[string]any{"decision": "block", "reason": reason}
}

func promptNote(text string) map[string]any {
	return map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "UserPromptSubmit",
		"additionalContext": text,
	}}
}

func headline(c Context) string {
	label, ok := kindLabel[c.Alarm.Kind]
	if !ok {
		label = c.Alarm.Kind
	}
	return fmt.Sprintf("Usage is at %d%% of the %s, which resets at %s.", c.Alarm.Pct, label, c.Alarm.ResetLocal)
}

// baseSpeak gives the base reasons for prompt, tool and stop. wake is a lowercase clause for a
// step before the handoff ("schedule ..."), note a sentence after the handoff step.
func baseSpeak(c Context, stand string, d Dirs, wake, note string) map[string]any {
	handoff := "`/lighthouse-write-handoff " + HandoffPath(c, d) + "`"
	head := headline(c)
	tail := ""
	if note != "" {
		tail = " " + note
	}
	step := ""
	if wake != "" {
		step = wake + ", "
	}
	switch stand {
	case "prompt":
		return promptNote(head + " Wrap up: finish only the current step, commit the work in progress, " +
			step + "run " + handoff + ", and do not start a new step." + tail)
	case "stop":
		if wake != "" {
			step = strings.ToUpper(wake[:1]) + wake[1:] + ". "
		}
		return block(head + " Commit nothing more. " + step + "Run " + handoff + " and note in it what is " +
			"uncommitted and when to resume (after " + c.Alarm.ResetLocal + ")." + tail + " Then stop.")
	}
	return block(head + " Before anything else: commit the work in progress, " + step + "run " + handoff +
		", and report where the work stands (worktree, branch, what is still open)." +
		tail + " Do not start a new step and do not dispatch a subagent.")
}

func subagentSpeak(c Context) map[string]any {
	return block(headline(c) + " Stop now and report to the main thread what you did and what " +
		"is still open. Do not commit, do not schedule a wake-up and do not write a " +
		"handoff: the main thread wraps up the session.")
}

// parseISO reads an ISO time with an offset (aware) or without one (naive, in UTC), like
// Python's datetime.fromisoformat for the forms a reset_at or a pause flag takes.
func parseISO(s string) (t time.Time, aware bool, err error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, false, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("not an ISO time: %q", s)
}

// awareTime is an ISO time with an offset; false when it is none of that.
func awareTime(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	t, aware, err := parseISO(s)
	return t, err == nil && aware
}

// WakeCron is resetAt plus 2 minutes in loc as "M H D Mon *", without leading zeros.
func WakeCron(resetAt string, loc *time.Location) (string, error) {
	t, aware, err := parseISO(resetAt)
	if err != nil {
		return "", err
	}
	if !aware {
		return "", fmt.Errorf("reset_at without an offset: %q", resetAt)
	}
	at := t.Add(wakeDelay).In(loc)
	return fmt.Sprintf("%d %d %d %d *", at.Minute(), at.Hour(), at.Day(), int(at.Month())), nil
}

// marker is one resume marker. Window is any: a marker that wins keeps its own window as read.
type marker struct {
	SessionID   string `json:"session_id"`
	Cwd         string `json:"cwd"`
	Window      any    `json:"window"`
	ResetAt     string `json:"reset_at"`
	CreatedAt   string `json:"created_at"`
	Source      string `json:"source"`
	HandoffPath string `json:"handoff_path,omitempty"`
	Controller  bool   `json:"controller,omitempty"`
	Pause       bool   `json:"pause,omitempty"`
}

func newMarker(c Context, sessionID, window, resetAt, source string) marker {
	return marker{
		SessionID: sessionID,
		Cwd:       c.Cwd,
		Window:    window,
		ResetAt:   resetAt,
		CreatedAt: c.Now.UTC().Format("2006-01-02T15:04:05Z"),
		Source:    source,
	}
}

// writeMarker writes <Resume>/<session_id>.json atomically: a tmp file in that dir, then
// rename; JSON with an indent of 2 and a trailing newline.
func writeMarker(d Dirs, m marker) error {
	if err := os.MkdirAll(d.Resume, 0o777); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d.Resume, ".marker-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(buf.Bytes())
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(d.Resume, m.SessionID+".json")); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// resumeSpeak is the wording of an ordinary session with [usage_guard] resume: a venster alarm
// adds a one-shot wake-up, and tool and stop write a marker for a session with an id.
func resumeSpeak(c Context, stand string, d Dirs) (map[string]any, error) {
	var out map[string]any
	if c.Alarm.Kind == "venster" {
		cron, err := WakeCron(c.Alarm.ResetAt, c.Loc)
		if err != nil {
			return nil, err
		}
		wake := `schedule one wake-up with CronCreate (cron "` + cron + `", recurring false, prompt = ` +
			"the first step from the handoff); if you already set a wake-up in this window, " +
			"check with CronList and take over that job ID instead of setting a second one"
		out = baseSpeak(c, stand, d, wake, "Name the wake-up's job ID in the handoff.")
	} else {
		out = baseSpeak(c, stand, d, "", "")
	}
	if stand != "prompt" && c.SessionID != "" {
		m := newMarker(c, c.SessionID, c.Alarm.Kind, c.Alarm.ResetAt, stand)
		m.HandoffPath = HandoffPath(c, d)
		if err := writeMarker(d, m); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// clock is HH:MM of an ISO time as written; the raw text when it does not parse.
func clock(until string) string {
	t, _, err := parseISO(until)
	if err != nil {
		return until
	}
	return t.Format("15:04")
}

// existingMarker is the marker of this session when it is a readable object with an aware
// reset_at; nil otherwise.
func existingMarker(d Dirs, sessionID string) map[string]any {
	b, err := os.ReadFile(filepath.Join(d.Resume, sessionID+".json"))
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil
	}
	if _, ok := awareTime(m["reset_at"]); !ok {
		return nil
	}
	return m
}

// parkRun writes the marker of a controller run; the later resume moment wins over an
// existing marker of the session, so a run resumes after the later of a pause and an alarm,
// and a pause marker keeps its pause field. It returns the clock time of the moment that won.
func parkRun(c Context, d Dirs, window, resetAt, source string, pause bool) (string, error) {
	sessionID := c.SessionID
	if sessionID == "" {
		sessionID = c.RunID
	}
	m := newMarker(c, sessionID, window, resetAt, source)
	m.Controller = true
	m.Pause = pause
	var at string
	if window == "pause" {
		at = clock(resetAt)
	} else {
		at = c.Alarm.ResetLocal
	}
	if old := existingMarker(d, sessionID); old != nil {
		oldAt, _ := awareTime(old["reset_at"])
		newAt, ok := awareTime(resetAt)
		if !ok {
			return "", fmt.Errorf("cannot compare reset_at %q with the marker of %s", resetAt, sessionID)
		}
		if oldAt.After(newAt) {
			m.Window = old["window"]
			m.ResetAt = old["reset_at"].(string)
			m.Pause = old["pause"] == true
			// A pause keeps the offset of its flag; an alarm's UTC time is shown local.
			if old["window"] != "pause" {
				oldAt = oldAt.In(c.Loc)
			}
			at = oldAt.Format("15:04")
		}
	}
	if err := writeMarker(d, m); err != nil {
		return "", err
	}
	return at, nil
}

// controllerSpeak is the wording of a controller run: prompt warns, tool and stop park.
func controllerSpeak(c Context, stand string, d Dirs) (map[string]any, error) {
	if stand == "prompt" {
		return promptNote(headline(c) + " This controller run parks at the next tool call: finish only " +
			"the current step and commit the work in progress."), nil
	}
	at, err := parkRun(c, d, c.Alarm.Kind, c.Alarm.ResetAt, stand, false)
	if err != nil {
		return nil, err
	}
	return block(headline(c) + " Now " + parkSteps + ". The controller resumes this session after " + at + "."), nil
}

// pauseSpeak parks the run until the time in the controller's PAUSE flag.
func pauseSpeak(c Context, d Dirs) (map[string]any, error) {
	at, err := parkRun(c, d, "pause", c.PauseUntil, "tool", true)
	if err != nil {
		return nil, err
	}
	return block("The controller has paused this run until " + clock(c.PauseUntil) + ". Now " + parkSteps +
		". The controller resumes this session after " + at + "."), nil
}
