package reflect

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// config is the validated params of one schedule of job reflect.
type config struct {
	command              string
	outputDir            string // slash form, relative to the work tree
	minSessions          int
	windowMin, windowMax int // days
	minLines             int
	maxCorpus            int // bytes
	maxPrompt            int // runes
	tools                []string
	maxRuntime           time.Duration
}

func defaults() config {
	return config{
		command:     "/paia-reflect-skills",
		outputDir:   "docs/developer/reflect",
		minSessions: 20,
		windowMin:   14,
		windowMax:   90,
		minLines:    20,
		maxCorpus:   400000,
		maxPrompt:   400,
		tools:       []string{"Read", "Glob", "Grep", "Write"},
		maxRuntime:  60 * time.Minute,
	}
}

// parse validates params over the defaults. It reports every problem; an unknown key is a
// problem (a typo would else run with the default silently).
func parse(params map[string]any) (config, error) {
	c := defaults()
	var errs []error
	bad := func(key, format string, a ...any) {
		errs = append(errs, fmt.Errorf("params %s: %s", key, fmt.Sprintf(format, a...)))
	}
	known := []string{"command", "output_dir", "min_sessions", "window_min_days", "window_max_days", "min_lines",
		"max_corpus_bytes", "max_prompt_chars", "tools", "max_runtime"}
	for k := range params {
		if !slices.Contains(known, k) {
			errs = append(errs, fmt.Errorf("params: unknown key %s", scheduled.ShowPath(k)))
		}
	}
	str := func(key string, dst *string) {
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
		case strings.TrimSpace(s) == "":
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
		if n < int64(min) || n > 100_000_000 {
			bad(key, "must be between %d and 100000000", min)
			return
		}
		*dst = int(n)
	}
	str("command", &c.command)
	str("output_dir", &c.outputDir)
	num("min_sessions", &c.minSessions, 1)
	num("window_min_days", &c.windowMin, 1)
	num("window_max_days", &c.windowMax, 1)
	num("min_lines", &c.minLines, 1)
	num("max_corpus_bytes", &c.maxCorpus, 1000)
	num("max_prompt_chars", &c.maxPrompt, 1)
	if v, ok := params["max_runtime"]; ok {
		s, isStr := v.(string)
		d, err := time.ParseDuration(s)
		if !isStr || err != nil || d <= 0 {
			bad("max_runtime", "must be a positive duration such as \"60m\"")
		} else {
			c.maxRuntime = d
		}
	}
	if v, ok := params["tools"]; ok {
		var tools []string
		switch x := v.(type) {
		case string:
			tools = strings.Fields(x)
		case []string:
			tools = x
		case []any:
			for _, e := range x {
				s, isStr := e.(string)
				if !isStr {
					bad("tools", "must be a list of strings")
					tools = nil
					break
				}
				tools = append(tools, s)
			}
		default:
			bad("tools", "must be a list of strings or one string")
		}
		if len(tools) == 0 {
			bad("tools", "must not be empty")
		} else {
			c.tools = tools
		}
	}
	if c.windowMin > c.windowMax {
		errs = append(errs, fmt.Errorf("params: window_min_days %d is above window_max_days %d", c.windowMin, c.windowMax))
	}
	if !cleanDir(c.outputDir) {
		errs = append(errs, fmt.Errorf("params: output_dir %s must be a clean relative path inside the repository, outside .git", scheduled.ShowPath(c.outputDir)))
	}
	return c, errors.Join(errs...)
}

// cleanDir reports whether p is a clean, local, slash-separated path that is not .git.
func cleanDir(p string) bool {
	return p != "" && p != "." && filepath.IsLocal(p) && path.Clean(p) == p && !strings.Contains(p, `\`) &&
		p != ".git" && !strings.HasPrefix(p, ".git/")
}
