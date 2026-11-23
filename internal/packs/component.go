package packs

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/levels"
)

// Component says which component a unit belongs to: a hook name or "skill:<name>", or
// base for a unit that no level switches. A package without a levels.toml (f.Levels is
// nil, as in a test fixture) has only base units. In every other package a unit with no
// place, or with two, is an error.
func Component(u Unit, f levels.File) (name string, base bool, err error) {
	if f.Levels == nil {
		return "", true, nil
	}
	switch u.Kind {
	case KindBlock, KindPermission:
		return "", true, nil
	case KindHook:
		name, err = hookComponent(u)
	case KindFile:
		var isBase bool
		name, isBase, err = fileComponent(u, f)
		if err == nil && isBase {
			return "", true, nil
		}
	default:
		err = fmt.Errorf("unit kind %q has no component", u.Kind)
	}
	if err != nil {
		return "", false, err
	}
	if name == "base" {
		return "", true, nil
	}
	if slices.Contains(f.Base, name) {
		if listed(f, name) {
			return "", false, fmt.Errorf("component %q is in base and in a level table or on", name)
		}
		return "", true, nil
	}
	return name, false, nil
}

// listed reports whether f names component in a level table or in on.
func listed(f levels.File, component string) bool {
	if slices.Contains(f.On, component) {
		return true
	}
	for _, t := range f.Levels {
		if _, ok := t.Components[component]; ok {
			return true
		}
	}
	return false
}

// hookComponent reads the component from a registration: the command `run.sh <name> ...`
// or `<name>.sh`.
func hookComponent(u Unit) (string, error) {
	_, command, ok := strings.Cut(u.ID, HookDir)
	if !ok {
		return "", fmt.Errorf("hook registration %q does not point into %s", u.ID, HookDir)
	}
	words := strings.Fields(command)
	switch {
	case len(words) == 0:
		return "", fmt.Errorf("hook registration %q has no command", u.ID)
	case words[0] == "run.sh" && len(words) > 1:
		return words[1], nil
	default:
		return strings.TrimSuffix(words[0], ".sh"), nil
	}
}

// fileComponent places a file unit. It returns base true for the files no level switches.
func fileComponent(u Unit, f levels.File) (name string, base bool, err error) {
	p := u.Path
	switch {
	case p == HeadingsPath, p == PRTemplatePath, strings.HasPrefix(p, WorkflowPrefix):
		return "", true, nil
	case p == HookDir+"run.sh", p == HookDir+"blocking":
		return "", true, nil
	case strings.HasPrefix(p, HookDir):
		return strings.TrimSuffix(strings.TrimPrefix(p, HookDir), ".sh"), false, nil
	}
	for _, dir := range skillDirs {
		if rest, ok := strings.CutPrefix(p, dir); ok {
			skill, _, _ := strings.Cut(rest, "/")
			return "skill:" + skill, false, nil
		}
	}
	var key string
	if rest, ok := strings.CutPrefix(p, ".claude/agents/lighthouse-"); ok {
		key = "agents/" + rest
	} else if rest, ok := strings.CutPrefix(p, ".lighthouse/scripts/"); ok {
		key = "scripts/" + path.Clean(rest)
	} else {
		return "", false, fmt.Errorf("file %s has no component", p)
	}
	owner, ok := f.Owners[key]
	if !ok {
		return "", false, fmt.Errorf("%s has no owner in levels.toml [owners]", key)
	}
	return owner, false, nil
}
