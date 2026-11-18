package maprefresh

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// config is the validated params of one schedule of job map-refresh.
type config struct {
	command, fastArgs   string
	mapDir, publishDir  string // slash form, relative to the work tree; publishDir "" = no mirror
	documents           []string
	minLines            int
	driftPaths          []string
	driftLow, driftHigh int
	newDirWeight        int
	tools               []string
	maxFast, maxFull    time.Duration
}

func defaults() config {
	return config{
		command:      "/gsd-map-codebase",
		fastArgs:     "--fast --focus tech+arch",
		mapDir:       ".planning/codebase",
		documents:    []string{"STACK", "INTEGRATIONS", "ARCHITECTURE", "STRUCTURE", "CONVENTIONS", "TESTING", "CONCERNS"},
		minLines:     20,
		driftPaths:   []string{"src", "apps", "scripts", "deploy", "tests"},
		driftLow:     25,
		driftHigh:    150,
		newDirWeight: 10,
		tools:        []string{"Read", "Write", "Glob", "Grep", "Bash", "Agent"},
		maxFast:      45 * time.Minute,
		maxFull:      120 * time.Minute,
	}
}

var documentName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// parse validates params over the defaults. It reports every problem; an unknown key is a
// problem (a typo would else run with the default silently).
func parse(params map[string]any) (config, error) {
	c := defaults()
	var errs []error
	bad := func(key, format string, a ...any) {
		errs = append(errs, fmt.Errorf("params %s: %s", key, fmt.Sprintf(format, a...)))
	}
	known := []string{"command", "fast_args", "map_dir", "publish_dir", "documents", "min_lines", "drift_paths",
		"drift_low", "drift_high", "new_dir_weight", "tools", "max_runtime_fast", "max_runtime_full"}
	for k := range params {
		if !slices.Contains(known, k) {
			errs = append(errs, fmt.Errorf("params: unknown key %s", scheduled.ShowPath(k)))
		}
	}
	str := func(key string, dst *string, allowEmpty bool) {
		v, ok := params[key]
		if !ok {
			return
		}
		s, isStr := v.(string)
		switch {
		case !isStr:
			bad(key, "must be a string")
		case strings.ContainsAny(s, "\r\n"):
			bad(key, "must be one line")
		case strings.TrimSpace(s) == "" && !allowEmpty:
			bad(key, "must not be empty")
		default:
			*dst = strings.TrimSpace(s)
		}
	}
	num := func(key string, dst *int, min int) {
		v, ok := params[key]
		if !ok {
			return
		}
		var n int64
		switch x := v.(type) {
		case int64:
			n = x
		case int:
			n = int64(x)
		default:
			bad(key, "must be a whole number")
			return
		}
		if n < int64(min) || n > 1_000_000 {
			bad(key, "must be between %d and 1000000", min)
			return
		}
		*dst = int(n)
	}
	dur := func(key string, dst *time.Duration) {
		v, ok := params[key]
		if !ok {
			return
		}
		s, isStr := v.(string)
		if !isStr {
			bad(key, "must be a duration string such as \"45m\"")
			return
		}
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			bad(key, "must be a positive duration such as \"45m\"")
			return
		}
		*dst = d
	}
	list := func(key string, dst *[]string) {
		v, ok := params[key]
		if !ok {
			return
		}
		var out []string
		switch x := v.(type) {
		case []string:
			out = x
		case []any:
			for _, e := range x {
				s, isStr := e.(string)
				if !isStr {
					bad(key, "must be a list of strings")
					return
				}
				out = append(out, s)
			}
		default:
			bad(key, "must be a list of strings")
			return
		}
		if len(out) == 0 {
			bad(key, "must not be empty")
			return
		}
		*dst = out
	}

	str("command", &c.command, false)
	str("fast_args", &c.fastArgs, true)
	str("map_dir", &c.mapDir, false)
	str("publish_dir", &c.publishDir, true)
	list("documents", &c.documents)
	num("min_lines", &c.minLines, 1)
	list("drift_paths", &c.driftPaths)
	num("drift_low", &c.driftLow, 0)
	num("drift_high", &c.driftHigh, 0)
	num("new_dir_weight", &c.newDirWeight, 0)
	if v, ok := params["tools"]; ok {
		switch x := v.(type) {
		case string:
			c.tools = strings.Fields(x)
			if len(c.tools) == 0 {
				bad("tools", "must not be empty")
			}
		default:
			list("tools", &c.tools)
		}
	}
	dur("max_runtime_fast", &c.maxFast)
	dur("max_runtime_full", &c.maxFull)

	errs = append(errs, c.validate()...)
	return c, errors.Join(errs...)
}

// validate checks the values together: shapes of the paths and the names, the thresholds.
func (c config) validate() []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf("params: "+format, a...)) }
	if c.driftLow > c.driftHigh {
		bad("drift_low %d is above drift_high %d", c.driftLow, c.driftHigh)
	}
	for _, d := range []struct{ key, p string }{{"map_dir", c.mapDir}, {"publish_dir", c.publishDir}} {
		if d.p != "" && !cleanDir(d.p) {
			bad("%s %s must be a clean relative path inside the repository, outside .git", d.key, scheduled.ShowPath(d.p))
		}
	}
	if c.publishDir != "" && cleanDir(c.mapDir) && cleanDir(c.publishDir) &&
		(c.mapDir == c.publishDir || strings.HasPrefix(c.mapDir, c.publishDir+"/") || strings.HasPrefix(c.publishDir, c.mapDir+"/")) {
		bad("map_dir and publish_dir must not be the same or inside one another")
	}
	for _, p := range c.driftPaths {
		if !cleanDir(p) {
			bad("drift_paths entry %s must be a clean relative path inside the repository", scheduled.ShowPath(p))
		}
	}
	seen := map[string]bool{}
	for _, n := range c.documents {
		switch {
		case !documentName.MatchString(n):
			bad("documents entry %s is not a file name stem", scheduled.ShowPath(n))
		case seen[n]:
			bad("documents entry %s is listed twice", scheduled.ShowPath(n))
		}
		seen[n] = true
	}
	return errs
}

// cleanDir reports whether p is a clean, local, slash-separated path that is not .git.
func cleanDir(p string) bool {
	return p != "" && p != "." && filepath.IsLocal(p) && path.Clean(p) == p && !strings.Contains(p, `\`) &&
		p != ".git" && !strings.HasPrefix(p, ".git/")
}
