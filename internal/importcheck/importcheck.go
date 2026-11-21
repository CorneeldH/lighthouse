// Package importcheck is the gate on undeclared direct imports: it finds the projects of a
// repository per language, hands each to a Checker and combines the results into one exit code.
package importcheck

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gate"
	"github.com/CorneeldH/lighthouse/internal/manifest"
)

// Lang is a language of the gate: "python", "go" or "node".
type Lang string // lighthouse: allow-language — name fixed by the spec

// Finding is one undeclared import. File is relative to the repo root, slash-separated.
type Finding struct {
	File   string
	Line   int
	ID     string
	Import string
	Reason string
}

// Project is one project of a language. Dir is relative to the repo root, "." for the root.
type Project struct {
	Lang Lang // lighthouse: allow-language
	Dir  string
}

// Checker checks one project.
type Checker interface {
	// Check returns the findings of one project, or an UncheckedError.
	Check(p Project, env Env) ([]Finding, error)
}

// UncheckedError means a project could not be checked; the text is the reason after
// "COULD NOT CHECK — ".
type UncheckedError string

func (e UncheckedError) Error() string { return string(e) }

// Env is what a checker reads besides the project.
type Env struct {
	Root           string
	Stdout, Stderr io.Writer
	// Exec runs argv in dir with extraEnv added; err is set only when the process cannot start.
	Exec      func(dir string, argv []string, extraEnv []string) (stdout, stderr []byte, code int, err error)
	LookPath  func(string) (string, error)
	GoVersion func() (string, error)
}

// Options is what Run needs.
type Options struct {
	Root string
	// Langs empty: the packs of .lighthouse/manifest.toml (expo gives node). // lighthouse: allow-language
	Langs []Lang // lighthouse: allow-language
	// Files empty: everything.
	Files []string
	// Checkers has a checker per language; a language without one is unchecked, never a pass.
	Checkers map[Lang]Checker
}

// FormatFinding is the stdout line of a finding.
func FormatFinding(f Finding) string {
	return fmt.Sprintf("%s:%d: %s %s — %s", f.File, f.Line, f.ID, f.Import, f.Reason)
}

// Run checks every project in scope. It returns 0 (clean), 1 (a finding) or gate.Unchecked
// (something could not be checked and nothing was found); findings win over unchecked.
func Run(o Options, env Env) int {
	env = withDefaults(env, o.Root)
	languages := o.Langs
	if len(languages) == 0 {
		var err error
		if languages, err = manifestLanguages(o.Root); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "COULD NOT CHECK — %v\n", err)
			return gate.Unchecked
		}
	}
	found, unchecked := false, false
	for _, l := range languages {
		projects, err := Discover(o.Root, l)
		if err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "COULD NOT CHECK — %v\n", err)
			unchecked = true
			continue
		}
		for _, p := range projects {
			all, touched := InScope(p, ownFiles(p, projects, o.Files))
			if len(o.Files) > 0 && !all && !touched {
				continue
			}
			findings, err := checkOne(o.Checkers[l], l, p, env)
			if err != nil {
				_, _ = fmt.Fprintf(env.Stderr, "COULD NOT CHECK — %v\n", err)
				unchecked = true
				continue
			}
			if len(o.Files) > 0 && !all {
				findings = onlyFiles(findings, o.Files)
			}
			sort.SliceStable(findings, func(i, j int) bool {
				if findings[i].File != findings[j].File {
					return findings[i].File < findings[j].File
				}
				return findings[i].Line < findings[j].Line
			})
			for _, f := range findings {
				_, _ = fmt.Fprintln(env.Stdout, FormatFinding(f))
				found = true
			}
		}
	}
	switch {
	case found:
		return 1
	case unchecked:
		return gate.Unchecked
	}
	return 0
}

func checkOne(c Checker, l Lang, p Project, env Env) ([]Finding, error) {
	if c == nil {
		return nil, UncheckedError(fmt.Sprintf("no checker for %s (%s)", l, p.Dir))
	}
	findings, err := c.Check(p, env)
	if err != nil {
		var u UncheckedError
		if errors.As(err, &u) {
			return nil, u
		}
		// Any other error is also a project that could not be checked, never a pass.
		return nil, UncheckedError(err.Error())
	}
	return findings, nil
}

func onlyFiles(findings []Finding, files []string) []Finding {
	keep := map[string]bool{}
	for _, f := range files {
		keep[path.Clean(f)] = true
	}
	var out []Finding
	for _, f := range findings {
		if keep[f.File] {
			out = append(out, f)
		}
	}
	return out
}

func manifestLanguages(root string) ([]Lang, error) {
	m, err := manifest.Read(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	seen := map[Lang]bool{}
	var languages []Lang
	for _, pk := range m.Packages {
		var l Lang
		switch pk {
		case "python", "go":
			l = Lang(pk)
		case "node", "expo":
			l = "node"
		default:
			continue
		}
		if !seen[l] {
			seen[l] = true
			languages = append(languages, l)
		}
	}
	return languages, nil
}

func withDefaults(env Env, root string) Env {
	if env.Root == "" {
		env.Root = root
	}
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	if env.Exec == nil {
		env.Exec = defaultExec
	}
	if env.LookPath == nil {
		env.LookPath = exec.LookPath
	}
	if env.GoVersion == nil {
		env.GoVersion = func() (string, error) {
			out, err := exec.Command("go", "env", "GOVERSION").Output()
			return string(bytes.TrimSpace(out)), err
		}
	}
	return env
}

func defaultExec(dir string, argv []string, extraEnv []string) ([]byte, []byte, int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), extraEnv...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), errb.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	return out.Bytes(), errb.Bytes(), 0, nil
}

// ownFiles leaves out the files that belong to a project nested deeper than p: a file is
// checked with the project closest to it.
func ownFiles(p Project, projects []Project, files []string) []string {
	var out []string
	for _, f := range files {
		f = path.Clean(f)
		nested := false
		for _, q := range projects {
			if q.Dir != p.Dir && q.Dir != "." && (p.Dir == "." || strings.HasPrefix(q.Dir, p.Dir+"/")) &&
				strings.HasPrefix(f, q.Dir+"/") {
				nested = true
			}
		}
		if !nested {
			out = append(out, f)
		}
	}
	return out
}
