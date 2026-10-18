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
//	skills/<name>/<rel>     .claude/skills/lighthouse-<name>/<rel> and
//	                        .agents/skills/lighthouse-<name>/<rel>
//	skills/<name>/claude/SKILL.md, skills/<name>/agents/SKILL.md
//	                        the SKILL.md of one harness only, when the two differ
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

// installDirs are the directories a file unit may live in; agents are files named
// .claude/agents/lighthouse-<name>.md directly.
var installDirs = []string{HookDir, ".lighthouse/lib/lighthouse_lib/", ".lighthouse/scripts/"}

// skillDirs are the two harness directories a skill lands in (spec, Indeling).
var skillDirs = []string{".claude/skills/lighthouse-", ".agents/skills/lighthouse-"}

// IsInstallPath reports whether p is a path lighthouse may own for a unit of kind:
// a clean relative path under a lighthouse directory, AGENTS.md for a block, and
// .claude/settings.json for a hook. A manifest is committed and may be edited in a
// PR, so every path read from it passes this check before anything is written or
// removed.
func IsInstallPath(kind Kind, p string) bool {
	if p == "" || path.IsAbs(p) || path.Clean(p) != p || strings.Contains(p, "\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return false
		}
	}
	switch kind {
	case KindBlock:
		return p == agentsMDPath
	case KindHook:
		return p == settingsPath
	case KindFile:
		for _, dir := range installDirs {
			if strings.HasPrefix(p, dir) && len(p) > len(dir) {
				return true
			}
		}
		for _, dir := range skillDirs {
			if rest := strings.TrimPrefix(p, dir); rest != p {
				name, file, ok := strings.Cut(rest, "/")
				return ok && name != "" && file != ""
			}
		}
		const agents = ".claude/agents/lighthouse-"
		return strings.HasPrefix(p, agents) && !strings.Contains(p[len(agents):], "/") && path.Ext(p) == ".md"
	}
	return false
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
	skills := map[string]*skill{}
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
			case "hooks", "lib", "scripts", "agents", "instructions", "skills":
				return nil
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
		case "skills":
			return collectSkill(skills, name, rel, rest, data)
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
		u, err := fileUnit(name, version, target, data)
		if err != nil {
			return err
		}
		p.Units = append(p.Units, u)
		return nil
	})
	if err != nil {
		return Pack{}, err
	}
	units, err := skillUnits(skills, name, version)
	if err != nil {
		return Pack{}, err
	}
	p.Units = append(p.Units, units...)
	if hooksJSON != nil {
		hooks, err := parseHooks(name, hooksJSON, scripts)
		if err != nil {
			return Pack{}, err
		}
		p.Units = append(p.Units, hooks...)
	}
	return p, nil
}

// fileUnit marks data for target and gives it its mode.
func fileUnit(pkg, version, target string, data []byte) (Unit, error) {
	content, err := Mark(target, data, pkg, version)
	if err != nil {
		return Unit{}, fmt.Errorf("package %s: %w", pkg, err)
	}
	mode := fs.FileMode(0o644)
	if bytes.HasPrefix(content, []byte("#!")) {
		mode = 0o755
	}
	return Unit{Kind: KindFile, Package: pkg, Path: target, Content: content, Mode: mode}, nil
}

// skill holds the files of one skills/<name>/ directory while the package is walked.
type skill struct {
	shared map[string][]byte // rel within the skill -> content, for both harnesses
	claude []byte            // claude/SKILL.md
	agents []byte            // agents/SKILL.md
}

func collectSkill(skills map[string]*skill, pkg, rel, rest string, data []byte) error {
	dir, file, ok := strings.Cut(rest, "/")
	if !ok {
		return fmt.Errorf("package %s: a skill is skills/<name>/..., not %s", pkg, rel)
	}
	s := skills[dir]
	if s == nil {
		s = &skill{shared: map[string][]byte{}}
		skills[dir] = s
	}
	switch {
	case file == "claude/SKILL.md":
		s.claude = data
	case file == "agents/SKILL.md":
		s.agents = data
	case strings.HasPrefix(file, "claude/") || strings.HasPrefix(file, "agents/"):
		return fmt.Errorf("package %s: %s: claude/ and agents/ hold only SKILL.md", pkg, rel)
	default:
		s.shared[file] = data
	}
	return nil
}

// frontmatterName returns the value of `name:` in the frontmatter of a SKILL.md.
func frontmatterName(data []byte) string {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimRight(line, "\r")
		if line == "---" {
			break
		}
		if v, ok := strings.CutPrefix(line, "name:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// skillUnits checks each collected skill and maps it into both harness directories.
func skillUnits(skills map[string]*skill, pkg, version string) ([]Unit, error) {
	names := make([]string, 0, len(skills))
	for n := range skills {
		names = append(names, n)
	}
	sort.Strings(names)
	var units []Unit
	for _, n := range names {
		s := skills[n]
		where := "skills/" + n + "/"
		shared, hasShared := s.shared["SKILL.md"]
		switch {
		case hasShared && (s.claude != nil || s.agents != nil):
			return nil, fmt.Errorf("package %s: %s: SKILL.md next to claude/ or agents/SKILL.md", pkg, where)
		case !hasShared && (s.claude == nil || s.agents == nil):
			return nil, fmt.Errorf("package %s: %s: needs SKILL.md, or both claude/SKILL.md and agents/SKILL.md", pkg, where)
		}
		variants := [][]byte{s.claude, s.agents}
		if hasShared {
			variants = [][]byte{shared, shared}
		}
		for _, v := range variants {
			if got := frontmatterName(v); got != "lighthouse-"+n {
				return nil, fmt.Errorf("package %s: %s: frontmatter name is %q, want %q", pkg, where, got, "lighthouse-"+n)
			}
		}
		rels := make([]string, 0, len(s.shared))
		for rel := range s.shared {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for i, dir := range skillDirs {
			base := dir + n + "/"
			for _, rel := range rels {
				data := s.shared[rel]
				if rel == "SKILL.md" {
					data = variants[i]
				}
				u, err := fileUnit(pkg, version, base+rel, data)
				if err != nil {
					return nil, err
				}
				units = append(units, u)
			}
			if !hasShared {
				u, err := fileUnit(pkg, version, base+"SKILL.md", variants[i])
				if err != nil {
					return nil, err
				}
				units = append(units, u)
			}
		}
	}
	return units, nil
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
