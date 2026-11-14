package turnroots

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
)

// pathKeys are the tools that write a file, and the input key that names it.
var pathKeys = map[string]string{
	"Edit":         "file_path",
	"Write":        "file_path",
	"MultiEdit":    "file_path",
	"NotebookEdit": "notebook_path",
}

// deadline is the time all git calls of one run share.
const deadline = 20 * time.Second

// realpath is os.path.realpath: absolute, with the symlinks of the part that exists resolved and
// the rest kept.
func realpath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	var rest []string
	for current := abs; ; current = filepath.Dir(current) {
		if real, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(append([]string{real}, rest...)...)
		}
		if filepath.Dir(current) == current {
			return abs
		}
		rest = append([]string{filepath.Base(current)}, rest...)
	}
}

// rootOf is the realpath of the checkout path lies in.
func rootOf(path string) (string, bool) {
	ancestor, ok := gitinvocations.ExistingAncestor(path)
	if !ok {
		return "", false
	}
	root, ok := gitinvocations.RepoOf(ancestor)
	if !ok {
		return "", false
	}
	return realpath(root), true
}

// toolPath is the absolute path a writing tool call names; false for any other call.
func toolPath(use toolUse) (string, bool) {
	key, ok := pathKeys[use.name]
	if !ok {
		return "", false
	}
	path, ok := use.input[key].(string)
	if !ok || !filepath.IsAbs(path) {
		return "", false
	}
	return path, true
}

// touchedRoots are the checkout roots the tool calls in turn name, not yet per project: the root
// of every Edit, Write, MultiEdit and NotebookEdit path, and per Bash call bashwrites.DeriveRoots
// with the cwd of its entry.
func touchedRoots(turn []map[string]any) map[string]bool {
	roots := map[string]bool{}
	for _, use := range toolUses(turn) {
		if _, writes := pathKeys[use.name]; writes {
			if path, ok := toolPath(use); ok {
				if root, ok := rootOf(path); ok {
					roots[root] = true
				}
			}
			continue
		}
		if use.name == "Bash" {
			if command, ok := use.input["command"].(string); ok && use.hasWd {
				for root := range bashwrites.DeriveRoots(command, use.cwd) {
					roots[root] = true
				}
			}
		}
	}
	return roots
}

// toolPaths are the realpaths of every absolute path an Edit, Write, MultiEdit or NotebookEdit
// call in turn names.
func toolPaths(turn []map[string]any) map[string]bool {
	paths := map[string]bool{}
	for _, use := range toolUses(turn) {
		if path, ok := toolPath(use); ok {
			paths[realpath(path)] = true
		}
	}
	return paths
}

// changedSince are the paths, relative to root, that git shows as changed since since (whole
// epoch seconds): dirty with a recent mtime, deleted, or in the diff over the reflog of the turn.
// Every git failure is returned.
func changedSince(root string, since int64, until time.Time) (map[string]bool, error) {
	data, err := bashwrites.Git(root, until, "status", "--porcelain", "-z", "-uall")
	if err != nil {
		return nil, err
	}
	changed, removed := bashwrites.ParsePorcelain(data)
	paths := removed
	for rel := range changed {
		info, err := os.Lstat(filepath.Join(root, rel))
		// Changed but gone from disk: counts, like a deleted path.
		if err != nil || info.ModTime().Unix() >= since {
			paths[rel] = true
		}
	}

	head, err := bashwrites.HeadOf(root, until)
	if err != nil {
		return nil, err
	}
	if head == "unborn" {
		return paths, nil
	}
	reflog, err := bashwrites.ReadReflog(root, until)
	if err != nil {
		return nil, err
	}
	if len(reflog) == 0 {
		return paths, nil
	}
	// Newest first: the first entry from before the turn, else the oldest one.
	base := reflog[len(reflog)-1].SHA
	for _, entry := range reflog {
		if entry.Epoch < since {
			base = entry.SHA
			break
		}
	}
	diff, err := bashwrites.Git(root, until, "diff", "--name-only", "--no-renames", "-z", base, "HEAD")
	if err != nil {
		return nil, err
	}
	for _, name := range strings.Split(string(diff), "\x00") {
		if name != "" {
			paths[name] = true
		}
	}
	return paths, nil
}

// turnState is what one Stop payload gives: the roots to check (sorted), the entries of the turn
// and the moment of its prompt (hasStart false without a readable timestamp).
type turnState struct {
	roots    []string
	entries  []map[string]any
	start    time.Time
	hasStart bool
}

// readTurn is `_turn` of turn_roots.py on the raw payload. projectDir is CLAUDE_PROJECT_DIR.
func readTurn(raw []byte, projectDir string, until time.Time) (turnState, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return turnState{}, &Undetermined{"the payload is not valid JSON."}
	}
	payload, ok := v.(map[string]any)
	if !ok {
		return turnState{}, &Undetermined{"the payload is no JSON object."}
	}
	cwd, _ := payload["cwd"].(string)
	if cwd == "" {
		cwd = projectDir
	}
	transcript, _ := payload["transcript_path"].(string)
	if transcript == "" {
		return turnState{}, &Undetermined{"the payload has no transcript_path."}
	}

	var cwdRoot string
	hasCwdRoot := false
	if cwd != "" {
		cwdRoot, hasCwdRoot = rootOf(cwd)
	}
	projectRoot, hasProject := "", false
	if projectDir != "" {
		projectRoot, hasProject = rootOf(projectDir)
	}
	if !hasProject {
		projectRoot, hasProject = cwdRoot, hasCwdRoot
	}
	if !hasProject {
		return turnState{}, &Undetermined{"not inside a git repository (" + cwd + ")."}
	}

	turn, start, hasStart, err := turnEntries(transcript)
	if err != nil {
		return turnState{}, err
	}
	kept, errs, err := bashwrites.ProjectFilter(touchedRoots(turn), projectRoot, until)
	if err != nil {
		return turnState{}, &Undetermined{fmt.Sprintf("git cannot read %s (%v).", projectRoot, err)}
	}
	if len(errs) > 0 {
		return turnState{}, &Undetermined{strings.Join(errs, " ")}
	}
	state := turnState{entries: turn, start: start, hasStart: hasStart}
	if len(kept) > 0 {
		for root := range kept {
			state.roots = append(state.roots, root)
		}
		sort.Strings(state.roots)
		return state, nil
	}
	if !hasCwdRoot {
		return turnState{}, &Undetermined{"not inside a git repository (" + cwd + ")."}
	}
	state.roots = []string{cwdRoot}
	return state, nil
}

// Resolve is the sorted list of checkout roots to check for the Stop payload raw: those of this
// project the turn wrote in, else the root of the payload cwd. projectDir is CLAUDE_PROJECT_DIR.
// Every error is an *Undetermined.
func Resolve(raw []byte, projectDir string) ([]string, error) {
	state, err := readTurn(raw, projectDir, time.Now().Add(deadline))
	if err != nil {
		return nil, err
	}
	return state.roots, nil
}

// Verdict says whether the turn touched a path in Root that matches a pattern.
type Verdict struct {
	Root    string
	Touched bool
}

// Classify gives each root of Resolve a verdict: did the turn touch a path in that root that
// matches one of patterns (fnmatchCase on the path relative to the root)? In doubt, a root is
// touched: a prompt without a readable timestamp, or a git call that fails for that root.
func Classify(raw []byte, projectDir string, patterns []string) ([]Verdict, error) {
	until := time.Now().Add(deadline)
	state, err := readTurn(raw, projectDir, until)
	if err != nil {
		return nil, err
	}
	verdicts := make([]Verdict, 0, len(state.roots))
	if !state.hasStart {
		for _, root := range state.roots {
			verdicts = append(verdicts, Verdict{Root: root, Touched: true})
		}
		return verdicts, nil
	}
	since := state.start.Unix()
	compiled := compilePatterns(patterns)
	// A path belongs only to its own checkout: a worktree lies under the main checkout, so a
	// prefix test on the path would give it to both.
	owners := map[string]string{}
	for path := range toolPaths(state.entries) {
		owner, _ := rootOf(path)
		owners[path] = owner
	}

	for _, root := range state.roots {
		rels := map[string]bool{}
		for path, owner := range owners {
			if owner == root {
				if rel, err := filepath.Rel(root, path); err == nil {
					rels[rel] = true
				}
			}
		}
		changed, err := changedSince(root, since, until)
		var gitErr *bashwrites.GitError
		if errors.As(err, &gitErr) {
			verdicts = append(verdicts, Verdict{Root: root, Touched: true})
			continue
		}
		if err != nil {
			return nil, err
		}
		for rel := range changed {
			rels[rel] = true
		}
		verdicts = append(verdicts, Verdict{Root: root, Touched: matchesAny(rels, compiled)})
	}
	return verdicts, nil
}

// compilePatterns is each pattern as fnmatchCase's regexp, compiled once. A pattern that does not
// compile is left out: fnmatchCase matches nothing with it.
func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if re, err := regexp.Compile(translate(pattern)); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// matchesAny says whether one of rels matches one of the patterns compiled by compilePatterns.
func matchesAny(rels map[string]bool, patterns []*regexp.Regexp) bool {
	for rel := range rels {
		for _, re := range patterns {
			if re.MatchString(rel) {
				return true
			}
		}
	}
	return false
}

// fnmatchCase is Python's fnmatch.fnmatchcase: the whole name against a shell pattern, case
// sensitive. `*` matches anything, `/` included; `?` one character; `[...]` a set, `[!...]` its
// negation, with ranges. An unclosed `[` is a literal. path.Match differs: its `*` stops at `/`.
func fnmatchCase(name, pattern string) bool {
	return matchesAny(map[string]bool{name: true}, compilePatterns([]string{pattern}))
}

// translate is fnmatch.translate as a Go regexp.
func translate(pattern string) string {
	pat := []rune(pattern)
	var b strings.Builder
	b.WriteString(`(?s)^`)
	for i := 0; i < len(pat); {
		c := pat[i]
		i++
		switch c {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		case '[':
			j := i
			if j < len(pat) && pat[j] == '!' {
				j++
			}
			if j < len(pat) && pat[j] == ']' {
				j++
			}
			for j < len(pat) && pat[j] != ']' {
				j++
			}
			if j >= len(pat) {
				b.WriteString(`\[`)
				continue
			}
			b.WriteString(charClass(pat[i:j]))
			i = j + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`$`)
	return b.String()
}

// charClass is the regexp for the inside of a `[...]` set. A reversed range is dropped, as in
// Python; a set left empty never matches, and an empty negated set matches any character.
func charClass(set []rune) string {
	negate := len(set) > 0 && set[0] == '!'
	if negate {
		set = set[1:]
	}
	var items strings.Builder
	for k := 0; k < len(set); {
		if k+2 < len(set) && set[k+1] == '-' {
			if set[k] <= set[k+2] {
				fmt.Fprintf(&items, `\x{%x}-\x{%x}`, set[k], set[k+2])
			}
			k += 3
			continue
		}
		fmt.Fprintf(&items, `\x{%x}`, set[k])
		k++
	}
	switch {
	case items.Len() == 0 && negate:
		return `.`
	case items.Len() == 0:
		return `[^\x00-\x{10FFFF}]`
	case negate:
		return `[^` + items.String() + `]`
	}
	return `[` + items.String() + `]`
}
