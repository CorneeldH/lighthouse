package importcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFiles are the files that declare the dependencies of a project of l.
func ManifestFiles(l Lang) []string {
	switch l {
	case "python":
		return []string{"pyproject.toml", "uv.lock"}
	case "go":
		return []string{"go.mod", "go.sum"}
	case "node":
		return []string{"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock"}
	}
	return nil
}

// InScope says how files relate to p. touched: a file lies under p.Dir. all: a manifest or
// lockfile of p is among files, so every import of p is in scope.
func InScope(p Project, files []string) (all bool, touched bool) {
	manifests := map[string]bool{}
	for _, m := range ManifestFiles(p.Lang) {
		manifests[path.Join(p.Dir, m)] = true
	}
	for _, f := range files {
		f = path.Clean(f)
		if p.Dir == "." || f == p.Dir || strings.HasPrefix(f, p.Dir+"/") {
			touched = true
		}
		if manifests[f] {
			return true, true
		}
	}
	return all, touched
}

// Discover lists the projects of l among the tracked files of the repo at root, by directory.
// Paths with a testdata or node_modules segment are fixtures or installed packages, not
// projects. A node project that is a workspace of a package.json above it is not a project.
func Discover(root string, l Lang) ([]Project, error) {
	files, err := trackedFiles(root)
	if err != nil {
		return nil, err
	}
	var marker string
	switch l {
	case "python":
		marker = "pyproject.toml"
	case "go":
		marker = "go.mod"
	case "node":
		marker = "package.json"
	default:
		return nil, fmt.Errorf("unknown language %q", l)
	}
	var dirs []string
	for _, f := range files {
		if skipPath(f) || path.Base(f) != marker {
			continue
		}
		dirs = append(dirs, path.Dir(f))
	}
	sort.Strings(dirs)
	if l == "node" {
		dirs = dropWorkspaces(root, dirs)
	}
	var out []Project
	for _, d := range dirs {
		out = append(out, Project{Lang: l, Dir: d})
	}
	return out, nil
}

func skipPath(f string) bool {
	for _, seg := range strings.Split(f, "/") {
		if seg == "testdata" || seg == "node_modules" {
			return true
		}
	}
	return false
}

func trackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// dropWorkspaces removes the package.json directories that fall under the workspaces of a
// package.json or pnpm-workspace.yaml in a directory above them.
func dropWorkspaces(root string, dirs []string) []string {
	patterns := map[string][]string{}
	for _, d := range dirs {
		if p := workspacePatterns(root, d); len(p) > 0 {
			patterns[d] = p
		}
	}
	var out []string
	for _, d := range dirs {
		if !isWorkspace(d, patterns) {
			out = append(out, d)
		}
	}
	return out
}

func isWorkspace(dir string, patterns map[string][]string) bool {
	for parent, pats := range patterns {
		if parent == dir {
			continue
		}
		rel := dir
		if parent != "." {
			if !strings.HasPrefix(dir, parent+"/") {
				continue
			}
			rel = strings.TrimPrefix(dir, parent+"/")
		}
		member := false
		for _, p := range pats {
			neg := strings.HasPrefix(p, "!")
			if matchWorkspace(strings.TrimPrefix(p, "!"), rel) {
				member = !neg
			}
		}
		if member {
			return true
		}
	}
	return false
}

func matchWorkspace(pattern, rel string) bool {
	pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
	ps, rs := strings.Split(pattern, "/"), strings.Split(rel, "/")
	return matchSegments(ps, rs)
}

func matchSegments(ps, rs []string) bool {
	if len(ps) == 0 {
		return len(rs) == 0
	}
	if ps[0] == "**" {
		for i := 0; i <= len(rs); i++ {
			if matchSegments(ps[1:], rs[i:]) {
				return true
			}
		}
		return false
	}
	if len(rs) == 0 {
		return false
	}
	ok, err := path.Match(ps[0], rs[0])
	return err == nil && ok && matchSegments(ps[1:], rs[1:])
}

func workspacePatterns(root, dir string) []string {
	base := filepath.Join(root, filepath.FromSlash(dir))
	var pats []string
	if data, err := os.ReadFile(filepath.Join(base, "package.json")); err == nil {
		var pkg struct {
			Workspaces json.RawMessage `json:"workspaces"`
		}
		if json.Unmarshal(data, &pkg) == nil && len(pkg.Workspaces) > 0 {
			var list []string
			var obj struct {
				Packages []string `json:"packages"`
			}
			if json.Unmarshal(pkg.Workspaces, &list) == nil {
				pats = append(pats, list...)
			} else if json.Unmarshal(pkg.Workspaces, &obj) == nil {
				pats = append(pats, obj.Packages...)
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(base, "pnpm-workspace.yaml")); err == nil {
		var ws struct {
			Packages []string `yaml:"packages"`
		}
		if yaml.Unmarshal(data, &ws) == nil {
			pats = append(pats, ws.Packages...)
		}
	}
	return pats
}
