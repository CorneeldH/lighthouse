package importcheck

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GoChecker checks a Go module with `go mod tidy -diff`.
type GoChecker struct{}

var goVersionRe = regexp.MustCompile(`^go1\.(\d+)`)

// Check runs `go mod tidy -diff` in the module and classifies the diff. Only a module that
// shifts from "// indirect" to direct (indirect-import) and a direct require that is missing
// while the module imports it (missing-require) are findings; other drift is left alone.
func (GoChecker) Check(p Project, env Env) ([]Finding, error) {
	unchecked := func(format string, a ...any) error {
		return UncheckedError(fmt.Sprintf(format, a...))
	}
	if _, err := env.LookPath("go"); err != nil {
		return nil, unchecked("go not found on PATH, needed in %s: %v", p.Dir, err)
	}
	version, err := env.GoVersion()
	if err != nil {
		return nil, unchecked("cannot read the go version in %s: %v", p.Dir, err)
	}
	m := goVersionRe.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return nil, unchecked("cannot read the go version %q in %s", version, p.Dir)
	}
	if minor, _ := strconv.Atoi(m[1]); minor < 23 {
		return nil, unchecked("go mod tidy -diff needs go 1.23 or newer, found %s (%s)", strings.TrimSpace(version), p.Dir)
	}

	dir := filepath.Join(env.Root, filepath.FromSlash(p.Dir))
	stdout, stderr, code, err := env.Exec(dir, []string{"go", "mod", "tidy", "-diff"}, nil)
	if err != nil {
		return nil, unchecked("go mod tidy -diff could not start in %s: %v", p.Dir, err)
	}
	reason := strings.TrimSpace(string(stderr))
	switch {
	case code == 0 && len(bytes.TrimSpace(stdout)) == 0:
		return nil, nil
	case code == 0:
		return nil, unchecked("go mod tidy -diff failed in %s: exit 0 with a diff on stdout: %s", p.Dir, reason)
	case code == 1 && len(bytes.TrimSpace(stdout)) == 0:
		return nil, unchecked("go mod tidy -diff failed in %s: exit 1 without a diff: %s", p.Dir, reason)
	case code != 1:
		return nil, unchecked("go mod tidy -diff failed in %s: exit %d: %s", p.Dir, code, reason)
	}

	shifted, missing := parseTidyDiff(string(stdout))
	if len(shifted) == 0 && len(missing) == 0 {
		return nil, nil
	}
	return locateGo(p, dir, shifted, missing), nil
}

type goImport struct {
	file string // slash-separated, relative to the project
	line int
	path string
}

// locateGo finds the places that import a module of the sets; see Check for the fallbacks.
func locateGo(p Project, dir string, shifted, missing []string) []Finding {
	kinds := map[string]string{}
	for _, mod := range missing {
		kinds[mod] = "missing-require"
	}
	for _, mod := range shifted {
		kinds[mod] = "indirect-import"
	}
	reasons := map[string]string{
		"indirect-import": "module is imported directly but go.mod marks it // indirect",
		"missing-require": "module is imported directly but go.mod has no require for it",
	}
	found := map[string]bool{}
	var out []Finding
	for _, imp := range goImports(dir) {
		mod := owningModule(imp.path, kinds)
		if mod == "" {
			continue
		}
		found[mod] = true
		out = append(out, Finding{
			File: path.Join(p.Dir, imp.file), Line: imp.line,
			ID: kinds[mod], Import: imp.path, Reason: reasons[kinds[mod]],
		})
	}
	for _, mod := range shifted {
		if !found[mod] {
			out = append(out, Finding{
				File: path.Join(p.Dir, "go.mod"), Line: goModLine(dir, mod),
				ID: "indirect-import", Import: mod, Reason: reasons["indirect-import"],
			})
		}
	}
	return out
}

// owningModule returns the longest module of kinds that contains the import path.
func owningModule(importPath string, kinds map[string]string) string {
	best := ""
	for mod := range kinds {
		if (importPath == mod || strings.HasPrefix(importPath, mod+"/")) && len(mod) > len(best) {
			best = mod
		}
	}
	return best
}

// goImports lists the imports of every .go file of the module in dir, build constraints
// ignored, nested modules, vendor, testdata and hidden directories left out.
func goImports(dir string) []goImport {
	var out []goImport
	fset := token.NewFileSet()
	_ = filepath.WalkDir(dir, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if full != dir && (name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if full != dir {
				if _, statErr := os.Stat(filepath.Join(full, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, full, nil, parser.ImportsOnly)
		if parseErr != nil || f == nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, full)
		for _, spec := range f.Imports {
			p, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				continue
			}
			out = append(out, goImport{file: filepath.ToSlash(rel), line: fset.Position(spec.Path.Pos()).Line, path: p})
		}
		return nil
	})
	return out
}

// goModLine is the line of the require of mod in go.mod, 1 when it is not found.
func goModLine(dir, mod string) int {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return 1
	}
	for i, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "require" {
			fields = fields[1:]
		}
		if len(fields) >= 2 && fields[0] == mod && strings.HasPrefix(fields[1], "v") {
			return i + 1
		}
	}
	return 1
}

type tidyEntry struct{ indirect bool }

// parseTidyDiff reads the output of `go mod tidy -diff`. shifted are the module paths that go
// from "// indirect" to direct, missing the direct requires that have no counterpart on the
// removed side. Both are paired on module path over the whole diff, not on adjacent lines.
func parseTidyDiff(diff string) (shifted, missing []string) {
	removed, added := map[string]tidyEntry{}, map[string]tidyEntry{}
	inGoMod := true
	block := ""
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff "):
			inGoMod = strings.HasSuffix(strings.TrimSpace(line), "go.mod")
			block = ""
			continue
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			continue
		case strings.HasPrefix(line, "@@"):
			block = ""
			continue
		}
		if !inGoMod || line == "" {
			continue
		}
		prefix, body := line[0], strings.TrimSpace(line[1:])
		if prefix != '+' && prefix != '-' && prefix != ' ' {
			continue
		}
		fields := strings.Fields(body)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == ")" {
			block = ""
			continue
		}
		if len(fields) == 2 && fields[1] == "(" {
			block = fields[0]
			continue
		}
		if prefix == ' ' {
			continue
		}
		if fields[0] == "require" {
			fields = fields[1:]
		} else if block != "" && block != "require" {
			continue
		} else if block == "" && isGoModKeyword(fields[0]) {
			continue
		}
		e, mod, ok := tidyRequire(fields)
		if !ok {
			continue
		}
		if prefix == '-' {
			removed[mod] = e
		} else {
			added[mod] = e
		}
	}
	for mod, e := range added {
		if e.indirect {
			continue
		}
		if old, ok := removed[mod]; !ok {
			missing = append(missing, mod)
		} else if old.indirect {
			shifted = append(shifted, mod)
		}
	}
	sort.Strings(shifted)
	sort.Strings(missing)
	return shifted, missing
}

func isGoModKeyword(s string) bool {
	switch s {
	case "module", "go", "toolchain", "godebug", "replace", "exclude", "retract", "tool", "ignore":
		return true
	}
	return false
}

// tidyRequire reads "path version [// indirect]"; anything else (a replace, a go.sum line) is not
// a require.
func tidyRequire(fields []string) (tidyEntry, string, bool) {
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
		return tidyEntry{}, "", false
	}
	if len(fields) > 2 && fields[2] != "//" {
		return tidyEntry{}, "", false
	}
	return tidyEntry{indirect: len(fields) > 3 && fields[3] == "indirect"}, fields[0], true
}
