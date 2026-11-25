package pins

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitpath"
)

// installedPlugins is the part of Claude Code's installed_plugins.json this package reads:
// plugin key `<name>@<marketplace>` to its installs, one per scope.
type installedPlugins struct {
	Plugins map[string][]installedPlugin `json:"plugins"`
}

type installedPlugin struct {
	Scope       string `json:"scope"`
	ProjectPath string `json:"projectPath"`
	Version     string `json:"version"`
}

// MeasurePlugin compares the installed version of plugin p with the pin. The scope follows the
// repo, in order: `project` for worktree, `project` for the main checkout of worktree, `user`;
// the first scope that has the plugin decides. Lower is behind, higher is ahead (not judged, so
// never current), equal is current. A missing or unreadable file, no matching install, several
// marketplaces in the chosen scope, or an unparsable version is an error.
func MeasurePlugin(p Pin, installedPluginsPath, worktree string) Result {
	data, err := os.ReadFile(installedPluginsPath)
	if err != nil {
		return pluginError("cannot read %s: %v", installedPluginsPath, err)
	}
	var file installedPlugins
	if err := json.Unmarshal(data, &file); err != nil {
		return pluginError("cannot parse %s: %v", installedPluginsPath, err)
	}
	pinned, err := ParseVersion(p.Version)
	if err != nil {
		return pluginError("pin %s: %v", p.Name, err)
	}

	type scope struct {
		label, projectPath string
		user               bool
	}
	scopes := []scope{{label: "project (worktree)", projectPath: worktree}}
	if main := mainCheckout(worktree); main != "" {
		scopes = append(scopes, scope{label: "project (main checkout)", projectPath: main})
	}
	scopes = append(scopes, scope{label: "user", user: true})

	prefix := p.Name + "@"
	for _, sc := range scopes {
		var keys []string
		versions := map[string]string{}
		for key, installs := range file.Plugins {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			for _, in := range installs {
				if sc.user && in.Scope == "user" || !sc.user && in.Scope == "project" && samePath(in.ProjectPath, sc.projectPath) {
					keys = append(keys, key)
					versions[key] = in.Version
				}
			}
		}
		sort.Strings(keys)
		switch {
		case len(keys) == 0:
			continue
		case len(keys) > 1:
			return pluginError("scope %s has %s: ambiguous marketplace", sc.label, strings.Join(keys, " and "))
		}
		installed, err := ParseVersion(versions[keys[0]])
		if err != nil {
			return pluginError("%s in scope %s: %v", keys[0], sc.label, err)
		}
		detail := fmt.Sprintf("installed %s (%s, scope %s), pinned %s", installed, keys[0], sc.label, pinned)
		status := StatusCurrent
		switch installed.Compare(pinned) {
		case -1:
			status = StatusBehind
		case 1:
			status = StatusAhead
		}
		return Result{Status: status, Detail: detail}
	}
	return pluginError("no %s install found for scope worktree, main checkout or user in %s", prefix+"<marketplace>", installedPluginsPath)
}

func pluginError(format string, args ...any) Result {
	return Result{Status: StatusError, Detail: fmt.Sprintf(format, args...)}
}

// mainCheckout is the main checkout of the repository that contains dir, or "" when git cannot
// tell (not a repository, git missing); the caller then skips that scope.
func mainCheckout(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := gitpath.Native(string(out))
	if common == "" {
		return ""
	}
	return filepath.Dir(common)
}

// samePath compares two paths after cleaning and resolving symlinks where possible.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return resolve(a) == resolve(b)
}

func resolve(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
