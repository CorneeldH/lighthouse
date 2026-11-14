package usageguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// keyMaxAge is how long a key lives; stop prunes older ones.
const keyMaxAge = 14 * 24 * time.Hour

// Decision is the one JSON object to print, or nil, and for stop the key the hook writes after
// printing it.
type Decision struct {
	Output   map[string]any
	KeyAfter string
}

// Decide picks who speaks. opts is read at most once, and only when a step needs it. warn gets
// one line per error; an error never blocks.
func Decide(c Context, d Dirs, opts func() Options, warn func(string)) Decision {
	var cached *Options
	options := func() Options {
		if cached == nil {
			o := opts()
			cached = &o
		}
		return *cached
	}

	// 1. The pause of a controller run: only in tool, also without an alarm.
	if c.Stand == "tool" && c.RunID != "" && c.PauseUntil != "" && c.AgentID == "" &&
		pauseDue(c, d) && options().Controller {
		if out, ok := speakPause(c, d, warn); ok {
			return Decision{Output: out}
		}
	}

	// 2. The alarm.
	if c.Alarm == nil {
		return Decision{}
	}

	// 3. Stop speaks once per window, and never within a stop hook.
	stand := EffectiveStand(c)
	if stand == "stop" {
		if c.StopHookActive || exists(AlarmKey(c, "stop", d)) {
			return Decision{}
		}
		// Without a key dir the stop key never lands: stop would block every turn.
		if err := os.MkdirAll(d.State, 0o777); err != nil {
			warn(fmt.Sprintf("cannot write keys in %s: %v", d.State, err))
			return Decision{}
		}
	}

	// 4. Claim the key before speaking; a parallel call of the same session spoke.
	claimed := ""
	if stand == "tool" || stand == "subagent" {
		claimed = AlarmKey(c, stand, d)
		if !claim(claimed, d, warn) {
			return Decision{}
		}
	}

	// 5. The speaker. 6. A failing speaker releases the claim.
	out, err := speak(c, stand, d, options)
	if err != nil {
		if claimed != "" {
			_ = os.Remove(claimed)
		}
		warn(fmt.Sprintf("%s: speaker failed: %v", stand, err))
		return Decision{}
	}

	// 7. Stop writes its key only after the block is out (the hook does that).
	if stand == "stop" {
		pruneKeys(d)
		return Decision{Output: out, KeyAfter: AlarmKey(c, "stop", d)}
	}
	return Decision{Output: out}
}

func speak(c Context, stand string, d Dirs, options func() Options) (map[string]any, error) {
	switch {
	case stand == "subagent":
		return subagentSpeak(c), nil
	case c.RunID != "" && options().Controller:
		return controllerSpeak(c, stand, d)
	case options().Resume:
		return resumeSpeak(c, stand, d)
	}
	return baseSpeak(c, stand, d, "", ""), nil
}

// speakPause parks the run for the PAUSE flag and writes the pause key; false after a
// warning, and the alarm may speak instead.
func speakPause(c Context, d Dirs, warn func(string)) (map[string]any, bool) {
	if err := os.MkdirAll(d.State, 0o777); err != nil {
		warn(fmt.Sprintf("pause: cannot write keys in %s: %v", d.State, err))
		return nil, false
	}
	out, err := pauseSpeak(c, d)
	if err == nil {
		err = os.WriteFile(PauseKey(c, d), nil, 0o666)
	}
	if err != nil {
		warn(fmt.Sprintf("pause: speaker failed: %v", err))
		return nil, false
	}
	return out, true
}

// claim creates key exclusively; false when it exists (another call spoke) or cannot be made.
func claim(key string, d Dirs, warn func(string)) bool {
	if err := os.MkdirAll(d.State, 0o777); err != nil {
		warn(fmt.Sprintf("cannot write keys in %s: %v", d.State, err))
		return false
	}
	f, err := os.OpenFile(key, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if errors.Is(err, fs.ErrExist) {
		return false
	}
	if err != nil {
		warn(fmt.Sprintf("cannot claim %s: %v", key, err))
		return false
	}
	_ = f.Close()
	return true
}

// pauseDue reports whether the pause key is missing, or older than the flag.
func pauseDue(c Context, d Dirs) bool {
	key, err := os.Stat(PauseKey(c, d))
	if err != nil {
		return true
	}
	flag, err := os.Stat(c.PauseFlag)
	if err != nil {
		return false
	}
	return key.ModTime().Before(flag.ModTime())
}

// pruneKeys removes keys older than keyMaxAge, by the clock of this machine.
func pruneKeys(d Dirs) {
	paths, _ := filepath.Glob(filepath.Join(d.State, "*.done"))
	cutoff := time.Now().Add(-keyMaxAge)
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.Remove(p)
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
