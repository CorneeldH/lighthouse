package initcmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/packs"
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
		case strings.HasPrefix(f, packs.WorkflowPrefix):
			// installed and excluded from the block; they say nothing about the repo
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

const dependabotPath = ".github/dependabot.yml"

// lighthouseWorkflows is what Dependabot must leave alone: the installed workflows are
// manifest units with pinned actions, and a bump would make the installation locally
// modified.
const lighthouseWorkflows = ".github/workflows/lighthouse-*.yml"

// renderDependabot writes the dependabot.yml for eco, sorted by name: weekly, one group.
func renderDependabot(eco []ecosystem) []byte {
	eco = append([]ecosystem(nil), eco...)
	sort.Slice(eco, func(i, j int) bool { return eco[i].Name < eco[j].Name })
	var b strings.Builder
	b.WriteString("# Seeded by `lighthouse init`; this file belongs to the repo and is never overwritten again.\n")
	b.WriteString("version: 2\nupdates:\n")
	for _, e := range eco {
		fmt.Fprintf(&b, "  - package-ecosystem: %q\n", e.Name)
		if e.Name == "github-actions" {
			b.WriteString("    directory: \"/\"\n")
			fmt.Fprintf(&b, "    exclude-paths:\n      - %q\n", lighthouseWorkflows)
		} else {
			b.WriteString("    directories:\n")
			for _, d := range e.Dirs {
				fmt.Fprintf(&b, "      - %q\n", d)
			}
		}
		b.WriteString("    schedule:\n      interval: \"weekly\"\n")
		b.WriteString("    groups:\n      all:\n        patterns:\n          - \"*\"\n")
	}
	return []byte(b.String())
}

// dependabotPlan seeds .github/dependabot.yml into the plan, once: an existing file is the
// project's and is never written, also with --force. Detection reads the disk before
// apply, so workflows that this run installs are not counted yet.
func dependabotPlan(root string, o Options, load func(string) (*planned, error)) error {
	// dependabot.yaml is the same config under its other name: never seed next to it.
	if _, err := os.Stat(filepath.Join(root, ".github", "dependabot.yaml")); err == nil {
		_, _ = fmt.Fprintf(o.Err, "dependabot: %s exists; left as it is\n", ".github/dependabot.yaml")
		return warnSelfHosted(root, o)
	}
	p, err := load(dependabotPath)
	if err != nil {
		return err
	}
	if p.existed {
		_, _ = fmt.Fprintf(o.Err, "dependabot: %s exists; left as it is\n", dependabotPath)
		return warnSelfHosted(root, o)
	}
	eco, err := detectEcosystems(root)
	if err != nil {
		return err
	}
	if len(eco) == 0 {
		_, _ = fmt.Fprintln(o.Err, "dependabot: no ecosystems found; nothing seeded")
		return warnSelfHosted(root, o)
	}
	p.new, p.mode = renderDependabot(eco), 0o644
	return warnSelfHosted(root, o)
}

// warnSelfHosted names the workflows that a Dependabot PR would send to a self-hosted runner.
func warnSelfHosted(root string, o Options) error {
	files, err := selfHostedWorkflows(root)
	if err != nil {
		return err
	}
	for _, f := range files {
		_, _ = fmt.Fprintf(o.Err, "dependabot: %s runs on a self-hosted runner; guard it so Dependabot PRs use a hosted runner (runs-on: ${{ github.actor == 'dependabot[bot]' && 'ubuntu-latest' || 'self-hosted' }})\n", f)
	}
	return nil
}

// selfHostedWorkflows lists the workflows git knows whose runs-on names self-hosted
// and that do not mention dependabot[bot] anywhere (an unguarded Dependabot PR would
// wait for a runner it cannot use). Comment text never counts.
func selfHostedWorkflows(root string) ([]string, error) {
	files, err := lsFiles(root, []string{".github/workflows/*.yml", ".github/workflows/*.yaml"})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if errors.Is(err, fs.ErrNotExist) {
			continue // tracked but deleted from the work tree
		}
		if err != nil {
			return nil, err
		}
		selfHosted, guarded := false, false
		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.Index(line, "#"); i >= 0 && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				line = line[:i]
			}
			if strings.Contains(line, "dependabot[bot]") {
				guarded = true
			}
			if t := strings.TrimLeft(line, " \t-"); strings.HasPrefix(t, "runs-on:") && strings.Contains(t, "self-hosted") {
				selfHosted = true
			}
		}
		if selfHosted && !guarded {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}
