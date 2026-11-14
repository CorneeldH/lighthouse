package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// GoPackage is the part of `go list -json` the gate reads.
type GoPackage struct {
	ImportPath, Dir string
	GoFiles         []string
	CgoFiles        []string
	TestGoFiles     []string
	XTestGoFiles    []string
	IgnoredGoFiles  []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
}

func (p GoPackage) lists() [][]string {
	return [][]string{p.GoFiles, p.CgoFiles, p.TestGoFiles, p.XTestGoFiles, p.IgnoredGoFiles,
		p.EmbedFiles, p.TestEmbedFiles, p.XTestEmbedFiles}
}

// resolve follows symlinks so a worktree under a symlink compares equal to its listing; when
// that fails the path is made absolute only.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// GoPackages returns the import paths of the packages the changed files belong to, sorted and
// unique. Importers of a touched package are not selected: the gate tests too little rather
// than too much, CI tests everything.
func GoPackages(files []string, listing []GoPackage, root string) []string {
	root = resolve(root)
	selected := map[string]bool{}
	for _, name := range files {
		if name == "" || name == "go.mod" || name == "go.sum" {
			continue
		}
		path := filepath.Join(root, name)
		for _, pkg := range listing {
			if belongs(path, pkg) {
				selected[pkg.ImportPath] = true
			}
		}
	}
	out := make([]string, 0, len(selected))
	for p := range selected {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func belongs(path string, pkg GoPackage) bool {
	rel, err := filepath.Rel(resolve(pkg.Dir), path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.ToSlash(rel)
	if strings.SplitN(rel, "/", 2)[0] == "testdata" {
		return true
	}
	for _, list := range pkg.lists() {
		if slices.Contains(list, rel) {
			return true
		}
	}
	// A deleted .go file is in no listing; it counts for the package of its directory.
	return strings.HasSuffix(rel, ".go") && !strings.Contains(rel, "/")
}

func goList(env Env) ([]GoPackage, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = env.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		_, _ = env.Stderr.Write(stderr.Bytes())
		return nil, unchecked("go list failed (exit %d), see above", exit.ExitCode())
	case err != nil:
		if errors.Is(err, exec.ErrNotFound) {
			return nil, unchecked("go not on PATH")
		}
		return nil, unchecked("go cannot start: %s", reason(err))
	}
	dec := json.NewDecoder(&stdout)
	var out []GoPackage
	for {
		var pkg GoPackage
		if err := dec.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, unchecked("go list gave output that is not JSON: %s", err)
		}
		out = append(out, pkg)
	}
}

const goUsage = "usage: lighthouse gate go -- CMD..."

// Go selects the Go packages of the commit and runs `CMD <import paths>`; without a file
// list it runs `CMD ./...`. args is everything after `gate go`, `--` included. The exit code
// is that of CMD, or Unchecked with a line on stderr when nothing was tested.
func Go(args []string, env Env) int {
	rc, err := selectAndRunGo(args, env)
	return finish("gate-go", env, rc, err)
}

func selectAndRunGo(args []string, env Env) (int, error) {
	cmd, err := splitCommand(args, goUsage)
	if err != nil {
		return 0, err
	}
	if env.FilesPath == "" {
		return run(append(slices.Clone(cmd), "./..."), env)
	}
	data, err := os.ReadFile(env.FilesPath)
	if err != nil {
		return 0, unchecked("cannot read the changed files: %s", reason(err))
	}
	listing, err := goList(env)
	if err != nil {
		return 0, err
	}
	pkgs := GoPackages(strings.Split(string(data), "\n"), listing, env.Dir)
	if len(pkgs) == 0 {
		_, _ = fmt.Fprintln(env.Stdout, "gate-go: skipped — the commit touches no Go package")
		return 0, nil
	}
	_, _ = fmt.Fprintf(env.Stdout, "gate-go: %s\n", strings.Join(pkgs, " "))
	return run(append(slices.Clone(cmd), pkgs...), env)
}
