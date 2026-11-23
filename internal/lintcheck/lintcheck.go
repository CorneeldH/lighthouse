// Package lintcheck is the gate on a repository whose linter is not wired: per language it asks a
// Checker whether the linter, its pin and the CI step are in place, and combines the answers into
// one exit code.
package lintcheck

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"

	"github.com/CorneeldH/lighthouse/internal/gate"
	"github.com/CorneeldH/lighthouse/internal/importcheck"
	"github.com/CorneeldH/lighthouse/internal/manifest"
)

// Lang is a language of the gate: "python", "go", "node" or "shell".
type Lang string // lighthouse: allow-language — name fixed by the spec

// Finding is one missing part of the wiring. File is relative to the repo root, slash-separated;
// Line 0 means no line.
type Finding struct {
	File        string
	Line        int
	Linter, Msg string
}

// FormatFinding is the stdout line of a finding: File[:Line]: Linter — Msg.
func FormatFinding(f Finding) string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s — %s", f.File, f.Line, f.Linter, f.Msg)
	}
	return fmt.Sprintf("%s: %s — %s", f.File, f.Linter, f.Msg)
}

// UncheckedError means the wiring could not be checked; the text is the reason after
// "COULD NOT CHECK — ".
type UncheckedError string

func (e UncheckedError) Error() string { return string(e) }

// Env is what a checker reads besides the directory.
type Env struct {
	Root           string
	Stdout, Stderr io.Writer
	ForgeKind      string
}

// Checker checks one directory: "." or a Node root. wired lists the linters that are fully wired.
type Checker interface {
	Check(dir string, env Env) (findings []Finding, wired []string, err error)
}

// Options is what Run needs.
type Options struct {
	Root string
	// Langs empty: the packs of .lighthouse/manifest.toml. // lighthouse: allow-language
	Langs []Lang // lighthouse: allow-language
	// Dirs overrides the discovery of Node roots; empty: importcheck.Discover.
	Dirs []string
	// Checkers has a checker per language; a language without one is unchecked, never a pass.
	Checkers map[Lang]Checker
}

// Run checks every language in scope. It returns 0 (all wired), 1 (a finding) or gate.Unchecked
// (something could not be checked and nothing was found); 1 wins over 3, 3 over 0.
func Run(o Options, env Env) int {
	if env.Root == "" {
		env.Root = o.Root
	}
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	languages := o.Langs
	if len(languages) == 0 {
		var err error
		if languages, err = manifestLanguages(o.Root); err != nil {
			unchecked(env, err)
			return gate.Unchecked
		}
	}
	found, notChecked := false, false
	for _, l := range languages {
		dirs, err := dirsOf(o, l)
		if err != nil {
			unchecked(env, err)
			notChecked = true
			continue
		}
		for _, d := range dirs {
			findings, wired, err := checkOne(o.Checkers[l], l, d, env)
			if err != nil {
				unchecked(env, err)
				notChecked = true
				continue
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
			for _, w := range wired {
				_, _ = fmt.Fprintf(env.Stdout, "%s: wired\n", w)
			}
		}
	}
	switch {
	case found:
		return 1
	case notChecked:
		return gate.Unchecked
	}
	return 0
}

func unchecked(env Env, err error) {
	_, _ = fmt.Fprintf(env.Stderr, "lint check: COULD NOT CHECK — %v\n", err)
}

func dirsOf(o Options, l Lang) ([]string, error) {
	if l != "node" {
		return []string{"."}, nil
	}
	if len(o.Dirs) > 0 {
		return o.Dirs, nil
	}
	projects, err := importcheck.Discover(o.Root, "node")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, p := range projects {
		dirs = append(dirs, p.Dir)
	}
	return dirs, nil
}

func checkOne(c Checker, l Lang, dir string, env Env) ([]Finding, []string, error) {
	if c == nil {
		return nil, nil, UncheckedError(fmt.Sprintf("no checker for %s (%s)", l, dir))
	}
	findings, wired, err := c.Check(dir, env)
	if err != nil {
		var u UncheckedError
		if errors.As(err, &u) {
			return nil, nil, u
		}
		// Any other error is also a directory that could not be checked, never a pass.
		return nil, nil, UncheckedError(err.Error())
	}
	return findings, wired, nil
}

// manifestLanguages maps the packs of the manifest to languages; python, go and shell stay, node
// and expo give node. A missing manifest is no languages. importcheck's own list lacks shell, so
// this is a separate switch.
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
		case "python", "go", "shell":
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
