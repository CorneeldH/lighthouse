// Package packs turns a package directory into units: the files, the managed block in
// AGENTS.md and the hook registrations that `lighthouse init` installs.
//
// The package layout follows a Claude Code plugin (spec, besluit 5):
//
//	hooks/hooks.json        hook registrations (plugin format); not copied
//	hooks/<rel>             .claude/hooks/lighthouse/<rel>
//	lib/<rel>               .lighthouse/lib/lighthouse_lib/<rel>
//	scripts/<rel>           .lighthouse/scripts/<rel>
//	agents/<name>.md        .claude/agents/lighthouse-<name>.md
//	instructions/AGENTS.md  the package's managed block in AGENTS.md
//	requires.toml           tool -> the units that need it
package packs

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// Kind is the kind of a unit.
type Kind string

// The three kinds of unit (spec, Versiemarker en manifest).
const (
	KindFile  Kind = "file"
	KindBlock Kind = "block"
	KindHook  Kind = "hook"
)

// Key identifies a unit in a repository.
type Key struct {
	Kind Kind
	Path string
	ID   string
}

// Registration is one hook entry under an event and a matcher in .claude/settings.json.
// Matcher "" means a group without a matcher field.
type Registration struct {
	Event   string
	Matcher string
	Entry   *orderedjson.Object
}

// Unit is one thing `lighthouse init` installs, with its desired content.
type Unit struct {
	Kind    Kind
	Package string
	Path    string      // target path, slash-separated, relative to the repo root
	ID      string      // block: package name; hook: HookID(...); file: ""
	Content []byte      // file: content with marker; block: block body; hook: orderedjson.Canonical(Entry)
	Mode    fs.FileMode // file: 0o755 if Content starts with "#!", else 0o644
	Hook    *Registration
}

// Key returns the key of u.
func (u Unit) Key() Key { return Key{Kind: u.Kind, Path: u.Path, ID: u.ID} }

// Requirement is a tool and the units of a package that need it.
type Requirement struct {
	Tool     string
	NeededBy []string
}

// Pack is a loaded package.
type Pack struct {
	Name     string
	Units    []Unit
	Requires []Requirement
}

// HookDir is where the hook scripts of every package land. A registration whose command
// points here belongs to lighthouse; any other registration belongs to the project.
const HookDir = ".claude/hooks/lighthouse/"

const (
	settingsPath  = ".claude/settings.json"
	agentsMDPath  = "AGENTS.md"
	pluginHooks   = "${CLAUDE_PLUGIN_ROOT}/hooks/"
	projectHooks  = `"$CLAUDE_PROJECT_DIR"/` + HookDir
	hooksJSONPath = "hooks/hooks.json"
)

// HookID identifies a hook registration by event, matcher and command.
func HookID(event, matcher, command string) string {
	return event + "|" + matcher + "|" + command
}

// IsLighthouseCommand reports whether a hook command points into HookDir.
func IsLighthouseCommand(command string) bool {
	return strings.Contains(command, HookDir)
}

// junk is never installed: caches the test lanes leave in the package tree.
func junk(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == "__pycache__" || part == ".pytest_cache" || part == ".DS_Store" {
			return true
		}
	}
	return strings.HasSuffix(p, ".pyc")
}

// Load reads the package rooted at fsys.
func Load(fsys fs.FS, name, version string) (Pack, error) {
	p := Pack{Name: name}
	scripts := map[string]bool{}
	var hooksJSON []byte
	err := fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." || junk(rel) {
			if d.IsDir() && rel != "." {
				return fs.SkipDir
			}
			return nil
		}
		top, rest, nested := strings.Cut(rel, "/")
		if d.IsDir() {
			switch top {
			case "hooks", "lib", "scripts", "agents", "instructions":
				return nil
			case "skills":
				return fmt.Errorf("package %s: skills/ is not supported yet", name)
			default:
				return fmt.Errorf("package %s: unknown directory %s/", name, rel)
			}
		}
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return err
		}
		if !nested {
			if rel == "requires.toml" {
				reqs, err := parseRequires(data)
				if err != nil {
					return fmt.Errorf("package %s: requires.toml: %w", name, err)
				}
				p.Requires = reqs
				return nil
			}
			return fmt.Errorf("package %s: unknown file %s", name, rel)
		}
		var target string
		switch top {
		case "hooks":
			if rel == hooksJSONPath {
				hooksJSON = data
				return nil
			}
			scripts[rest] = true
			target = HookDir + rest
		case "lib":
			target = ".lighthouse/lib/lighthouse_lib/" + rest
		case "scripts":
			target = ".lighthouse/scripts/" + rest
		case "agents":
			if strings.Contains(rest, "/") || path.Ext(rest) != ".md" {
				return fmt.Errorf("package %s: an agent is agents/<name>.md, not %s", name, rel)
			}
			target = ".claude/agents/lighthouse-" + rest
		case "instructions":
			if rest != "AGENTS.md" {
				return fmt.Errorf("package %s: instructions/ holds only AGENTS.md, not %s", name, rel)
			}
			body := data
			if !bytes.HasSuffix(body, []byte("\n")) {
				body = append(append([]byte{}, body...), '\n')
			}
			p.Units = append(p.Units, Unit{Kind: KindBlock, Package: name, Path: agentsMDPath, ID: name, Content: body})
			return nil
		}
		content, err := Mark(target, data, name, version)
		if err != nil {
			return fmt.Errorf("package %s: %w", name, err)
		}
		mode := fs.FileMode(0o644)
		if bytes.HasPrefix(content, []byte("#!")) {
			mode = 0o755
		}
		p.Units = append(p.Units, Unit{Kind: KindFile, Package: name, Path: target, Content: content, Mode: mode})
		return nil
	})
	if err != nil {
		return Pack{}, err
	}
	if hooksJSON != nil {
		hooks, err := parseHooks(name, hooksJSON, scripts)
		if err != nil {
			return Pack{}, err
		}
		p.Units = append(p.Units, hooks...)
	}
	return p, nil
}

func parseRequires(data []byte) ([]Requirement, error) {
	var raw map[string][]string
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	reqs := make([]Requirement, 0, len(raw))
	for tool, by := range raw {
		reqs = append(reqs, Requirement{Tool: tool, NeededBy: by})
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Tool < reqs[j].Tool })
	return reqs, nil
}

// parseHooks reads hooks/hooks.json in plugin format and rewrites each command from
// ${CLAUDE_PLUGIN_ROOT}/hooks/<script> to "$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/<script>.
func parseHooks(name string, data []byte, scripts map[string]bool) ([]Unit, error) {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("package %s: %s: %s", name, hooksJSONPath, fmt.Sprintf(format, a...))
	}
	v, err := orderedjson.Parse(data)
	if err != nil {
		return nil, fail("%v", err)
	}
	root, ok := v.(*orderedjson.Object)
	if !ok {
		return nil, fail("not an object")
	}
	hooksV, _ := root.Get("hooks")
	events, ok := hooksV.(*orderedjson.Object)
	if !ok {
		return nil, fail(`"hooks" is not an object`)
	}
	var units []Unit
	for _, event := range events.Keys() {
		groupsV, _ := events.Get(event)
		groups, ok := groupsV.([]any)
		if !ok {
			return nil, fail("hooks.%s is not an array", event)
		}
		for gi, gv := range groups {
			group, ok := gv.(*orderedjson.Object)
			if !ok {
				return nil, fail("hooks.%s[%d] is not an object", event, gi)
			}
			matcher := ""
			if mv, ok := group.Get("matcher"); ok {
				if matcher, ok = mv.(string); !ok {
					return nil, fail("hooks.%s[%d].matcher is not a string", event, gi)
				}
			}
			entriesV, _ := group.Get("hooks")
			entries, ok := entriesV.([]any)
			if !ok {
				return nil, fail("hooks.%s[%d].hooks is not an array", event, gi)
			}
			for hi, ev := range entries {
				entry, ok := ev.(*orderedjson.Object)
				if !ok {
					return nil, fail("hooks.%s[%d].hooks[%d] is not an object", event, gi, hi)
				}
				cv, _ := entry.Get("command")
				command, ok := cv.(string)
				if !ok || !strings.HasPrefix(command, pluginHooks) {
					return nil, fail("hooks.%s[%d].hooks[%d].command must start with %s", event, gi, hi, pluginHooks)
				}
				script := strings.TrimPrefix(command, pluginHooks)
				if word, _, _ := strings.Cut(script, " "); !scripts[word] {
					return nil, fail("hooks.%s[%d].hooks[%d] runs hooks/%s, which is not in the package", event, gi, hi, word)
				}
				installed := projectHooks + script
				entry.Set("command", installed)
				canonical, err := orderedjson.Canonical(entry)
				if err != nil {
					return nil, fail("%v", err)
				}
				units = append(units, Unit{
					Kind:    KindHook,
					Package: name,
					Path:    settingsPath,
					ID:      HookID(event, matcher, installed),
					Content: canonical,
					Hook:    &Registration{Event: event, Matcher: matcher, Entry: entry},
				})
			}
		}
	}
	return units, nil
}
