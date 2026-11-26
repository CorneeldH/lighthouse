package initcmd

import (
	"path"
	"sort"
	"strings"
)

// ecosystem is one Dependabot package ecosystem with the directories (Dependabot
// paths: "/" or "/web") in which it was found.
type ecosystem struct {
	Name string
	Dirs []string
}

var ecosystemSpecs = []string{
	"**/go.mod", "**/uv.lock", "**/pyproject.toml", "**/requirements*.txt",
	"**/Pipfile", "**/setup.py", "**/package.json", "**/Dockerfile", "**/Dockerfile.*",
	".github/workflows/*.yml", ".github/workflows/*.yaml", "action.yml", "action.yaml",
}

// detectEcosystems finds the ecosystems of the repository at root, sorted by name,
// with sorted, unique directories. Fixtures and vendored code are skipped.
func detectEcosystems(root string) ([]ecosystem, error) {
	files, err := lsFiles(root, ecosystemSpecs)
	if err != nil {
		return nil, err
	}
	dirs := map[string]map[string]bool{}
	add := func(name, dir string) {
		if dirs[name] == nil {
			dirs[name] = map[string]bool{}
		}
		dirs[name][dir] = true
	}
	pyproject := map[string]bool{}
	uvlock := map[string]bool{}
	for _, f := range files {
		if skippedPath(f) {
			continue
		}
		dir, base := path.Split(f)
		dir = path.Clean("/" + dir)
		switch {
		case strings.HasPrefix(f, ".github/workflows/"), dir == "/" && (base == "action.yml" || base == "action.yaml"):
			add("github-actions", "/")
		case base == "go.mod":
			add("gomod", dir)
		case base == "uv.lock":
			uvlock[dir] = true
			add("uv", dir)
		case base == "pyproject.toml":
			pyproject[dir] = true
		case base == "Pipfile", base == "setup.py", strings.HasPrefix(base, "requirements"):
			add("pip", dir)
		case base == "package.json":
			add("npm", dir)
		case base == "Dockerfile", strings.HasPrefix(base, "Dockerfile."):
			add("docker", dir)
		}
	}
	for dir := range pyproject {
		if !uvlock[dir] {
			add("pip", dir)
		}
	}
	var out []ecosystem
	for name, set := range dirs {
		e := ecosystem{Name: name}
		for d := range set {
			e.Dirs = append(e.Dirs, d)
		}
		sort.Strings(e.Dirs)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func skippedPath(f string) bool {
	for _, seg := range strings.Split(f, "/") {
		switch seg {
		case "testdata", "node_modules", "vendor":
			return true
		}
	}
	return false
}
