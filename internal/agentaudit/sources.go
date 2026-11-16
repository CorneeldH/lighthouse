package agentaudit

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/frontmatter"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/settings"
)

// blocks matches what makes a hook script block (spec §4): exit 2, a block decision or a
// deny permission decision, with the JSON quotes optionally escaped for the shell.
var blocks = regexp.MustCompile(`\bexit\s+2\b|\\?"decision\\?"\s*:\s*\\?"block\\?"|\\?"permissionDecision\\?"\s*:\s*\\?"deny\\?"`)

// sourceState is what a source showed while it was read.
type sourceState struct {
	present  bool
	problems []string
}

type collector struct {
	env       Env
	home      string
	inv       *Inventory
	installed map[string]bool // file paths in the manifest
	lhRules   map[string]bool // permission rules of the installed packages
	blocking  map[string]bool // lighthouse hook names that block
	states    map[string]*sourceState
}

// Collect reads every source of env and returns what it found. It errs only when
// env.Root does not exist; a source it cannot read is a status, never an error. It writes
// nothing.
func Collect(env Env) (*Inventory, error) {
	info, err := os.Stat(env.Root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", env.Root)
	}
	home, _ := os.UserHomeDir()
	c := &collector{
		env:       env,
		home:      home,
		inv:       &Inventory{},
		installed: map[string]bool{},
		lhRules:   map[string]bool{},
		blocking:  map[string]bool{},
		states:    map[string]*sourceState{},
	}
	c.fromPacks()
	c.lighthouse()

	claude := filepath.Join(env.Root, ".claude")
	c.settings(SourceProject, filepath.Join(claude, "settings.json"))
	c.settings(SourceLocal, filepath.Join(claude, "settings.local.json"))
	c.skills(SourceProject, filepath.Join(claude, "skills"))
	c.agents(SourceProject, filepath.Join(claude, "agents"))
	c.rules(SourceProject, filepath.Join(claude, "rules"))
	c.mcpFile(SourceProject, filepath.Join(env.Root, ".mcp.json"))
	c.scripts(SourceProject, filepath.Join(claude, "hooks"))

	if !env.RepoOnly {
		c.settings(SourceUser, filepath.Join(env.ConfigDir, "settings.json"))
		c.skills(SourceUser, filepath.Join(env.ConfigDir, "skills"))
		c.agents(SourceUser, filepath.Join(env.ConfigDir, "agents"))
		c.rules(SourceUser, filepath.Join(env.ConfigDir, "rules"))
		c.claudeJSON()
		c.scripts(SourceUser, filepath.Join(env.ConfigDir, "hooks"))
		// Plugins (plugin:<name>) are read here once collectPlugins exists.
	}

	for _, source := range []string{SourceLighthouse, SourceProject, SourceLocal, SourceUser} {
		c.inv.Statuses = append(c.inv.Statuses, c.status(source))
	}
	return c.inv, nil
}

func (c *collector) state(source string) *sourceState {
	s, ok := c.states[source]
	if !ok {
		s = &sourceState{}
		c.states[source] = s
	}
	return s
}

func (c *collector) seen(source string) { c.state(source).present = true }

// fail marks source as not fully checked; the rest of it is still read.
func (c *collector) fail(source, reason string) {
	s := c.state(source)
	s.present = true
	s.problems = append(s.problems, reason)
}

func (c *collector) status(source string) Status {
	if source == SourceUser && c.env.RepoOnly {
		return Status{Source: source, State: StateSkipped}
	}
	s := c.state(source)
	switch {
	case len(s.problems) > 0:
		return Status{Source: source, State: StateCouldNotCh, Reason: strings.Join(s.problems, "; ")}
	case s.present:
		return Status{Source: source, State: StateOK}
	default:
		return Status{Source: source, State: StateAbsent}
	}
}

// display is how a path shows in the output: relative to the repository root, under ~
// for the home directory, else absolute; always with slashes.
func (c *collector) display(abs string) string {
	if rel, err := filepath.Rel(c.env.Root, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	if c.home != "" {
		if rel, err := filepath.Rel(c.home, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(abs)
}

// fromPacks reads from the installed packages which rules are lighthouse's and which
// lighthouse hooks block: those named in hooks/blocking, and those with a guard.
func (c *collector) fromPacks() {
	for _, p := range c.env.Packs {
		for _, g := range p.Guards {
			c.blocking[g.Name] = true
		}
		for _, u := range p.Units {
			switch {
			case u.Kind == packs.KindPermission:
				c.lhRules[u.ID] = true
			case u.Kind == packs.KindFile && u.Path == packs.HookDir+"blocking":
				sc := bufio.NewScanner(bytes.NewReader(u.Content))
				for sc.Scan() {
					if name := strings.TrimSpace(sc.Text()); name != "" && !strings.HasPrefix(name, "#") {
						c.blocking[name] = true
					}
				}
			}
		}
	}
}

func (c *collector) lighthouse() {
	m, err := manifest.Read(c.env.Root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		c.fail(SourceLighthouse, err.Error())
		return
	}
	c.seen(SourceLighthouse)
	for _, u := range m.Units {
		if u.Kind == packs.KindFile {
			c.installed[u.Path] = true
		}
	}
}

// owner is the source of a file found in source's directories: lighthouse when the
// manifest lists it.
func (c *collector) owner(source, abs string) string {
	if source == SourceProject && c.installed[c.display(abs)] {
		return SourceLighthouse
	}
	return source
}

// readJSON reads an object from path. exists is false when there is no file.
func readJSON(path string) (doc *orderedjson.Object, exists bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	v, err := orderedjson.Parse(data)
	if err != nil {
		return nil, true, fmt.Errorf("not valid JSON: %w", err)
	}
	obj, ok := v.(*orderedjson.Object)
	if !ok {
		return nil, true, errors.New("not a JSON object")
	}
	return obj, true, nil
}

// settings reads the hooks and the permissions of one settings file. A section of the
// wrong shape fails the source, and the other section is still read.
func (c *collector) settings(source, path string) {
	shown := c.display(path)
	doc, exists, err := readJSON(path)
	if !exists {
		return
	}
	if err != nil {
		c.fail(source, shown+": "+err.Error())
		return
	}
	c.seen(source)
	if regs, err := settings.Registrations(doc); err != nil {
		c.fail(source, shown+": "+strings.TrimPrefix(err.Error(), ".claude/settings.json: "))
	} else {
		for _, r := range regs {
			cv, _ := r.Entry.Get("command")
			command, _ := cv.(string)
			if command == "" {
				continue // not a command hook
			}
			c.inv.Hooks = append(c.inv.Hooks, c.hook(source, r.Event, r.Matcher, command))
		}
	}
	c.permissions(source, shown, doc)
}

func (c *collector) permissions(source, shown string, doc *orderedjson.Object) {
	pv, ok := doc.Get("permissions")
	if !ok {
		return
	}
	obj, ok := pv.(*orderedjson.Object)
	if !ok {
		c.fail(source, shown+`: "permissions" is not an object`)
		return
	}
	for _, kind := range []string{"allow", "deny", "ask"} {
		v, ok := obj.Get(kind)
		if !ok {
			continue
		}
		rules, ok := stringList(v)
		if !ok {
			c.fail(source, fmt.Sprintf("%s: permissions.%s is not an array of strings", shown, kind))
			continue
		}
		for _, rule := range rules {
			owner := source
			// Lighthouse only ever ships allow rules; a deny or ask next to one is the project's.
			if source == SourceProject && kind == "allow" && (c.lhRules[rule] || packs.IsLighthouseRule(rule)) {
				owner = SourceLighthouse
			}
			c.inv.Permissions = append(c.inv.Permissions, Permission{Source: owner, Kind: kind, Rule: rule})
		}
	}
}

// stringList is v as a list of strings.
func stringList(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func (c *collector) hook(source, event, matcher, command string) Hook {
	if source == SourceProject && packs.IsLighthouseCommand(command) {
		source = SourceLighthouse
	}
	h := Hook{Source: source, Event: event, Matcher: matcher, Command: command}
	h.Script = scriptPath(command, c.env.Root, c.home)
	if h.Script == "" {
		h.Content = []byte(command)
	} else if data, err := os.ReadFile(h.Script); err != nil {
		h.Unreadable = err.Error()
	} else {
		h.Content = data
	}
	switch {
	case source == SourceLighthouse:
		h.Blocking = c.blocking[lighthouseHookName(command)]
	case h.Unreadable == "":
		h.Blocking = blocks.Match(h.Content)
	}
	return h
}

// lighthouseHookName is the hook a command into HookDir runs: the word after run.sh, or
// the script name without its extension.
func lighthouseHookName(command string) string {
	_, rest, _ := strings.Cut(command, packs.HookDir)
	words := strings.Fields(rest)
	if len(words) == 0 {
		return ""
	}
	first := unquote.Replace(words[0])
	if first == "run.sh" && len(words) > 1 {
		return unquote.Replace(words[1])
	}
	return strings.TrimSuffix(first, filepath.Ext(first))
}

// readDir lists dir; a missing dir is no entries and no problem.
func (c *collector) readDir(source, dir string) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		c.fail(source, c.display(dir)+": "+err.Error())
		return nil
	}
	c.seen(source)
	return entries
}

func (c *collector) skills(source, dir string) {
	for _, e := range c.readDir(source, dir) {
		p := filepath.Join(dir, e.Name(), "SKILL.md")
		if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
			continue
		}
		c.inv.Skills = append(c.inv.Skills, c.skill(c.owner(source, p), "skill", e.Name(), p))
	}
}

func (c *collector) agents(source, dir string) {
	for _, e := range c.readDir(source, dir) {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		c.inv.Skills = append(c.inv.Skills, c.skill(c.owner(source, p), "agent", strings.TrimSuffix(e.Name(), ".md"), p))
	}
}

// skill counts the name (from the frontmatter, else fallback) and the description in
// characters, not bytes.
func (c *collector) skill(source, kind, fallback, path string) Skill {
	name := frontmatter.Field(path, "name")
	if name == "" {
		name = fallback
	}
	desc := frontmatter.Field(path, "description")
	return Skill{
		Source: source, Name: name, Kind: kind, Path: c.display(path),
		NameChars: len([]rune(name)), DescChars: len([]rune(desc)),
	}
}

func (c *collector) rules(source, dir string) {
	c.walk(source, dir, func(p string, d fs.DirEntry) error {
		if !d.IsDir() && filepath.Ext(p) == ".md" {
			c.inv.Rules = append(c.inv.Rules, Rule{Source: c.owner(source, p), Path: c.display(p), HasPaths: hasKey(p, "paths")})
		}
		return nil
	})
}

// scripts lists every file under dir, outside lighthouse's own hook directory.
func (c *collector) scripts(source, dir string) {
	own := filepath.Join(c.env.Root, filepath.FromSlash(strings.TrimSuffix(packs.HookDir, "/")))
	c.walk(source, dir, func(p string, d fs.DirEntry) error {
		if d.IsDir() && p == own {
			return fs.SkipDir
		}
		if !d.IsDir() {
			c.inv.Scripts = append(c.inv.Scripts, source+"/"+c.display(p))
		}
		return nil
	})
}

// walk visits every entry under dir in lexical order; a missing dir is no problem, an
// unreadable one fails source.
func (c *collector) walk(source, dir string, visit func(p string, d fs.DirEntry) error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return
	}
	c.seen(source)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		return visit(p, d)
	})
	if err != nil {
		c.fail(source, c.display(dir)+": "+err.Error())
	}
}

// hasKey reports whether the frontmatter of file has key, with a value on its line or a
// list below it.
func hasKey(file, key string) bool {
	data, err := os.ReadFile(file)
	if err != nil || !bytes.HasPrefix(data, []byte("---\n")) {
		return false
	}
	for _, l := range strings.Split(string(data), "\n")[1:] {
		if l == "---" {
			return false
		}
		if strings.HasPrefix(l, key+":") {
			return true
		}
	}
	return false
}

func (c *collector) mcpFile(source, path string) {
	doc, exists, err := readJSON(path)
	if !exists {
		return
	}
	if err != nil {
		c.fail(source, c.display(path)+": "+err.Error())
		return
	}
	c.seen(source)
	c.servers(source, c.display(path), "mcpServers", doc)
}

// servers adds the keys of doc.mcpServers as MCP servers of source; where names that
// object in a reason.
func (c *collector) servers(source, origin, where string, doc *orderedjson.Object) {
	v, ok := doc.Get("mcpServers")
	if !ok {
		return
	}
	obj, ok := v.(*orderedjson.Object)
	if !ok {
		c.fail(source, origin+": "+where+" is not an object")
		return
	}
	for _, name := range obj.Keys() {
		c.inv.MCP = append(c.inv.MCP, MCPServer{Source: source, Name: name, Origin: origin})
	}
}

// claudeJSON reads ~/.claude.json: the servers under projects[<Root or MainRoot>] are
// lokaal, the servers at the top are gebruiker's.
func (c *collector) claudeJSON() {
	shown := c.display(c.env.ClaudeJSON)
	doc, exists, err := readJSON(c.env.ClaudeJSON)
	if !exists {
		return
	}
	if err != nil {
		c.fail(SourceUser, shown+": "+err.Error())
		c.fail(SourceLocal, shown+": "+err.Error())
		return
	}
	c.seen(SourceUser)
	if pv, ok := doc.Get("projects"); ok {
		projects, ok := pv.(*orderedjson.Object)
		if !ok {
			c.fail(SourceLocal, shown+`: "projects" is not an object`)
		} else {
			c.projectServers(shown, projects)
		}
	}
	c.servers(SourceUser, shown, "mcpServers", doc)
}

func (c *collector) projectServers(shown string, projects *orderedjson.Object) {
	seen := map[string]bool{}
	for _, root := range []string{c.env.Root, c.env.MainRoot} {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		v, ok := projects.Get(root)
		if !ok {
			continue
		}
		entry, ok := v.(*orderedjson.Object)
		if !ok {
			c.fail(SourceLocal, fmt.Sprintf("%s: projects[%q] is not an object", shown, root))
			continue
		}
		c.seen(SourceLocal)
		c.servers(SourceLocal, shown, fmt.Sprintf("projects[%q].mcpServers", root), entry)
	}
}
