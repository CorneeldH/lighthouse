package checkinstr

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
)

// errSettings is a settings.json that is not valid JSON, or not an object.
var errSettings = errors.New("settings.json is not valid JSON (or not an object)")

// hookPath is a hook script named in a skill body, also in a subdirectory of `.claude/hooks/`.
var hookPath = func() *regexp.Regexp {
	re := regexp.MustCompile(`\.claude/hooks/([A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.sh`)
	re.Longest()
	return re
}()

// loadSettings parses settings.json; errSettings when it cannot be read, is not valid JSON, or
// is not an object.
func loadSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errSettings
	}
	var doc any
	if json.Unmarshal(data, &doc) != nil {
		return nil, errSettings
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, errSettings
	}
	return obj, nil
}

// pathChar is a character of a path or file name: a registration must not have one right
// before or after the hook path.
func pathChar(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '-'
}

// namesWhole is true when hook occurs in s as a whole path, not as part of a longer one
// (`streng.sh.disabled` does not count for `streng.sh`).
func namesWhole(s, hook string) bool {
	for from := 0; ; {
		i := strings.Index(s[from:], hook)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(hook)
		if (start == 0 || !pathChar(s[start-1])) && (end == len(s) || !pathChar(s[end])) {
			return true
		}
		from = start + 1
	}
}

// registered is true when a `command` field somewhere under node names hook as a whole path.
func registered(node any, hook string) bool {
	switch n := node.(type) {
	case map[string]any:
		for key, value := range n {
			if s, ok := value.(string); ok && key == "command" && namesWhole(s, hook) {
				return true
			}
			if registered(value, hook) {
				return true
			}
		}
	case []any:
		for _, item := range n {
			if registered(item, hook) {
				return true
			}
		}
	}
	return false
}

// Check 8: `binding: hard` claims an enforcement exists. The skill names its hook in the body,
// and that hook must exist AND be registered in the `hooks` block of settings.json: a hook
// script registered nowhere never runs.
func checkPairing(rp *repo, r *report) {
	settingsPath := rp.root + "/.claude/settings.json"
	settingsExists := isFile(settingsPath)
	var settings map[string]any
	var settingsErr error
	parsed := false

	hard, found := 0, false
	skipReason := ""
	for _, dir := range skillDirs(rp.claudeSkills) {
		if skillMeta(rp.claudeSkills+"/"+dir, "binding") != "hard" {
			continue
		}
		hard++
		noCounterpart := "pairing: '" + dir + "' carries 'binding: hard' but names no counterpart that exists and is registered in settings.json"
		hooks := namedHooks(rp.claudeSkills + "/" + dir + "/SKILL.md")
		// No hook path named at all is a finding, whatever settings.json says.
		if len(hooks) == 0 {
			found = true
			r.fail(noCounterpart)
			continue
		}
		// Only this skill is unmeasurable: the loop goes on to the next.
		if !settingsExists {
			skipReason = "pairing: '" + settingsPath + "' does not exist, so registration cannot be checked here"
			continue
		}
		covered, broken := false, false
		for _, hook := range hooks {
			if !isFile(rp.root + "/" + hook) {
				continue
			}
			if !parsed {
				settings, settingsErr = loadSettings(settingsPath)
				parsed = true
			}
			if settingsErr != nil {
				broken = true
				continue
			}
			hooksBlock, ok := settings["hooks"]
			if !ok {
				hooksBlock = map[string]any{}
			}
			if registered(hooksBlock, hook) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		found = true
		// A broken settings.json is measurable, and its cause is settings.json, not the skill body.
		if broken {
			r.fail("pairing: '" + settingsPath + "' is not valid JSON (or not an object), so '" + dir + "' cannot be checked for a registered hook")
		} else {
			r.fail(noCounterpart)
		}
	}

	if hard == 0 {
		r.ok("no skill claims 'binding: hard'")
		return
	}
	// Always, also next to a FAIL elsewhere: an unmeasurable skill is reported loudly.
	if skipReason != "" {
		r.skip(skipReason)
	}
	if !found && skipReason == "" {
		r.ok("every binding: hard has a registered hook")
	}
}

// namedHooks are the hook paths a SKILL.md names, sorted and unique.
func namedHooks(file string) []string {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var hooks []string
	for _, h := range hookPath.FindAllString(string(data), -1) {
		if !seen[h] {
			seen[h] = true
			hooks = append(hooks, h)
		}
	}
	sort.Strings(hooks)
	return hooks
}
