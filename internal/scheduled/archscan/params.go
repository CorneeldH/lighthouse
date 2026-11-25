package archscan

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// allowedTools are the only tools the unattended agent may get: no Bash, no Agent.
var allowedTools = []string{"Read", "Glob", "Grep", "Write"}

// config is the validated params of one schedule of job architecture-scan.
type config struct {
	command    string
	window     string
	labels     []string
	tools      []string
	maxRuntime time.Duration
}

func defaults() config {
	return config{
		command:    "/lighthouse-architecture-scan",
		window:     "3 months",
		tools:      slices.Clone(allowedTools),
		maxRuntime: 60 * time.Minute,
	}
}

// strList reads a list of strings from a TOML list or one space-separated string.
func strList(v any) ([]string, bool) {
	switch x := v.(type) {
	case string:
		return strings.Fields(x), true
	case []string:
		return x, true
	case []any:
		var out []string
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// parse validates params over the defaults. It reports every problem; an unknown key is a
// problem (a typo would else run with the default silently).
func parse(params map[string]any) (config, error) {
	c := defaults()
	var errs []error
	bad := func(key, format string, a ...any) {
		errs = append(errs, fmt.Errorf("params %s: %s", key, fmt.Sprintf(format, a...)))
	}
	known := []string{"command", "window", "labels", "tools", "max_runtime"}
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
	str("command", &c.command)
	str("window", &c.window)
	if v, ok := params["max_runtime"]; ok {
		s, isStr := v.(string)
		d, err := time.ParseDuration(s)
		if !isStr || err != nil || d <= 0 {
			bad("max_runtime", "must be a positive duration such as \"60m\"")
		} else {
			c.maxRuntime = d
		}
	}
	if v, ok := params["labels"]; ok {
		labels, isList := strList(v)
		if !isList {
			bad("labels", "must be a list of strings")
		}
		for _, l := range labels {
			if strings.TrimSpace(l) == "" || strings.ContainsAny(l, "\r\n") {
				bad("labels", "must hold non-empty one-line labels")
				break
			}
		}
		if isList {
			c.labels = labels
		}
	}
	if v, ok := params["tools"]; ok {
		tools, isList := strList(v)
		switch {
		case !isList:
			bad("tools", "must be a list of strings or one string")
		case len(tools) == 0:
			bad("tools", "must not be empty")
		default:
			for _, t := range tools {
				if !slices.Contains(allowedTools, t) {
					bad("tools", "%s is not allowed; only %s", scheduled.ShowPath(t), strings.Join(allowedTools, ", "))
				}
			}
			c.tools = tools
		}
	}
	return c, errors.Join(errs...)
}
