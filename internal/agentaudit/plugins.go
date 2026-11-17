package agentaudit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// managedReason is the fixed reason of the managed status: the audit never reads
// settings an organisation manages.
const managedReason = "managed settings are not read"

// pluginRootVars are the spellings of the variable Claude Code sets to a plugin's
// installPath when it runs one of its hooks.
var pluginRootVars = []string{"${CLAUDE_PLUGIN_ROOT}", "$CLAUDE_PLUGIN_ROOT"}

// enabledPlugins is the set of plugin keys (name@marketplace) that are on: per key, the
// settings file with the highest precedence that names it decides: the local settings
// over the project settings over the user settings. A value that is no boolean fails that source and decides nothing.
func (c *collector) enabledPlugins() []string {
	on := map[string]bool{}
	for _, f := range []struct{ source, path string }{
		{SourceUser, filepath.Join(c.env.ConfigDir, "settings.json")},
		{SourceProject, filepath.Join(c.env.Root, ".claude", "settings.json")},
		{SourceLocal, filepath.Join(c.env.Root, ".claude", "settings.local.json")},
	} {
		doc, _, err := readJSON(f.path)
		if err != nil || doc == nil {
			continue // settings already reported an unreadable file
		}
		v, ok := doc.Get("enabledPlugins")
		if !ok {
			continue
		}
		shown := c.display(f.path)
		obj, ok := v.(*orderedjson.Object)
		if !ok {
			c.fail(f.source, shown+": enabledPlugins is not an object")
			continue
		}
		for _, key := range obj.Keys() {
			bv, _ := obj.Get(key)
			b, ok := bv.(bool)
			if !ok {
				c.fail(f.source, fmt.Sprintf("%s: enabledPlugins[%q] is not a boolean", shown, key))
				continue
			}
			on[key] = b // a later file has the higher precedence
		}
	}
	var keys []string
	for key, b := range on {
		if b {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// installEntry is one install of a plugin in installed_plugins.json.
type installEntry struct{ scope, projectPath, installPath string }

// readInstalled reads <ConfigDir>/plugins/installed_plugins.json into its installs per
// plugin key. A missing file is no installs and no error.
func (c *collector) readInstalled() (map[string][]installEntry, error) {
	path := filepath.Join(c.env.ConfigDir, "plugins", "installed_plugins.json")
	shown := c.display(path)
	doc, _, err := readJSON(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", shown, err)
	}
	out := map[string][]installEntry{}
	if doc == nil {
		return out, nil
	}
	pv, ok := doc.Get("plugins")
	if !ok {
		return out, nil
	}
	plugins, ok := pv.(*orderedjson.Object)
	if !ok {
		return nil, fmt.Errorf(`%s: "plugins" is not an object`, shown)
	}
	for _, key := range plugins.Keys() {
		v, _ := plugins.Get(key)
		var raw []any
		switch t := v.(type) {
		case []any: // version 2: a list of installs
			raw = t
		case *orderedjson.Object: // version 1: one install, user scope
			raw = []any{t}
		}
		for _, r := range raw {
			obj, ok := r.(*orderedjson.Object)
			if !ok {
				continue
			}
			out[key] = append(out[key], installEntry{
				scope:       str(obj, "scope"),
				projectPath: str(obj, "projectPath"),
				installPath: str(obj, "installPath"),
			})
		}
	}
	return out, nil
}

func str(obj *orderedjson.Object, key string) string {
	v, _ := obj.Get(key)
	s, _ := v.(string)
	return s
}

// install picks the install of a plugin that applies to this repository: one with scope
// project or local whose projectPath is Root or MainRoot, else one with scope user.
func (c *collector) install(entries []installEntry) (installEntry, bool) {
	var user *installEntry
	for i, e := range entries {
		switch e.scope {
		case "project", "local":
			if e.projectPath != "" && (e.projectPath == c.env.Root || e.projectPath == c.env.MainRoot) {
				return e, true
			}
		default: // "user", or no scope in version 1
			if user == nil {
				user = &entries[i]
			}
		}
	}
	if user != nil {
		return *user, true
	}
	return installEntry{}, false
}

// plugins reads every enabled plugin as source plugin:<name> and returns those sources,
// sorted. A plugin it cannot locate is COULD NOT CHECK; the others are still read.
func (c *collector) plugins() []string {
	keys := c.enabledPlugins()
	if len(keys) == 0 {
		return nil
	}
	shownInstalled := c.display(filepath.Join(c.env.ConfigDir, "plugins", "installed_plugins.json"))
	installs, installErr := c.readInstalled()
	var sources []string
	for _, key := range keys {
		name, _, _ := strings.Cut(key, "@")
		source := SourcePluginPrefix + name
		sources = append(sources, source)
		entries, listed := installs[key]
		e, ok := c.install(entries)
		switch {
		case installErr != nil:
			c.fail(source, installErr.Error())
		case !listed:
			c.fail(source, fmt.Sprintf("%s is not in %s", key, shownInstalled))
		case !ok:
			c.fail(source, fmt.Sprintf("%s is not installed for this project in %s", key, shownInstalled))
		case e.installPath == "":
			c.fail(source, key+" has no installPath")
		default:
			c.plugin(source, e.installPath)
		}
	}
	sort.Strings(sources)
	return compact(sources)
}

// compact drops adjacent duplicates: two marketplaces may carry a plugin of one name.
func compact(sorted []string) []string {
	var out []string
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// plugin reads what an installed plugin brings: skills, commands, agents, hooks and MCP
// servers.
func (c *collector) plugin(source, root string) {
	info, err := os.Stat(root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.fail(source, "installPath "+root+" does not exist")
		return
	case err != nil:
		c.fail(source, err.Error())
		return
	case !info.IsDir():
		c.fail(source, "installPath "+root+" is not a directory")
		return
	}
	c.seen(source)
	c.skills(source, "claude", filepath.Join(root, "skills"))
	c.markdown(source, "command", filepath.Join(root, "commands"))
	c.markdown(source, "agent", filepath.Join(root, "agents"))
	c.hookFile(source, filepath.Join(root, "hooks", "hooks.json"), root)
	c.mcpFile(source, filepath.Join(root, ".mcp.json"))
}

// hookFile reads the hooks of a plugin's hooks.json, with the plugin root variable
// resolved to root when the script is looked up.
func (c *collector) hookFile(source, path, root string) {
	shown := c.display(path)
	doc, exists, err := readJSON(path)
	if !exists {
		return
	}
	if err != nil {
		c.fail(source, shown+": "+err.Error())
		return
	}
	c.registrations(source, shown, doc, root)
}
