package checkinstr

import (
	"regexp"
	"sort"
	"strings"
)

// pySpace is Python's `\s` on str: RE2's `\s` is ASCII only and has no `\v`.
const pySpace = `[\p{Z}\t\n\v\f\r\x1c-\x1f\x85]`

// lighthouseHook is a command of a lighthouse hook. For `run.sh <name>` the hook is `<name>`.
var lighthouseHook = regexp.MustCompile(`\.claude/hooks/lighthouse/(?:[A-Za-z0-9_-]+/)*([A-Za-z0-9_-]+)\.sh(?:` +
	pySpace + `+([A-Za-z0-9_-]+))?`)

// hookEvents collects per hook name the events it is registered on.
func hookEvents(node any, event string, events map[string]map[string]bool) {
	switch n := node.(type) {
	case map[string]any:
		for key, value := range n {
			s, ok := value.(string)
			if key != "command" || !ok {
				hookEvents(value, event, events)
				continue
			}
			m := lighthouseHook.FindStringSubmatch(s)
			if m == nil {
				continue
			}
			name := m[1]
			if name == "run" && m[2] != "" {
				name = m[2]
			}
			if events[name] == nil {
				events[name] = map[string]bool{}
			}
			events[name][event] = true
		}
	case []any:
		for _, item := range n {
			hookEvents(item, event, events)
		}
	}
}

// hooksObject is the `hooks` block of settings, empty when absent; errSettings when it is
// present but not an object (null included).
func hooksObject(settings map[string]any) (map[string]any, error) {
	v, ok := settings["hooks"]
	if !ok {
		return map[string]any{}, nil
	}
	hooks, ok := v.(map[string]any)
	if !ok {
		return nil, errSettings
	}
	return hooks, nil
}

// Check 14: a lighthouse hook is named after what it does. `-gate` can block, so it sits on
// PreToolUse, Stop or SubagentStop; `-check` reports after the fact and never sits on Stop or
// SubagentStop. Only commands under `.claude/hooks/lighthouse/` count: a hook of the project
// itself is not ours to name.
func checkHookNames(rp *repo, r *report) {
	settingsPath := rp.root + "/.claude/settings.json"
	if !isFile(settingsPath) {
		r.skip("hook-names: '" + settingsPath + "' does not exist, so hook names cannot be checked here")
		return
	}
	settings, err := loadSettings(settingsPath)
	var hooks map[string]any
	if err == nil {
		hooks, err = hooksObject(settings)
	}
	if err != nil {
		r.fail("hook-names: '" + settingsPath + "' is not valid JSON (or not an object)")
		return
	}

	events := map[string]map[string]bool{}
	for event, entries := range hooks {
		hookEvents(entries, event, events)
	}
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)
	found := false
	for _, name := range names {
		on := events[name]
		switch {
		case strings.HasSuffix(name, "-gate"):
			if !on["PreToolUse"] && !on["Stop"] && !on["SubagentStop"] {
				found = true
				r.fail("hook-names: '" + name + "' is a gate but no registration is on PreToolUse, Stop or SubagentStop")
			}
		case strings.HasSuffix(name, "-check"):
			if on["Stop"] || on["SubagentStop"] {
				found = true
				r.fail("hook-names: '" + name + "' is a check but is registered on Stop or SubagentStop")
			}
		default:
			found = true
			r.fail("hook-names: '" + name + "' ends on neither -gate nor -check")
		}
	}
	if !found {
		r.ok("every lighthouse hook is named after what it does")
	}
}
