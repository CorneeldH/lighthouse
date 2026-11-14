package bashwrites

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/repoidentity"
)

// tokenRE is the fallback tokenizer for a command with an open quote: no shell parse is possible
// any more, so a rough split on what is never a path character.
var tokenRE = regexp.MustCompile(`[^\s'"=;&|<>()]+`)

// rootOf is the repo root of path: the nearest existing ancestor, then RepoOf.
func rootOf(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	ancestor, ok := gitinvocations.ExistingAncestor(path)
	if !ok {
		return "", false
	}
	return gitinvocations.RepoOf(ancestor)
}

// resolvedPath resolves word, then makes it absolute against base.
func resolvedPath(word string, literal, tilde bool, base string) (string, bool) {
	resolved, ok := gitinvocations.Resolve(word, literal, tilde)
	if !ok {
		return "", false
	}
	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved), true
	}
	return filepath.Clean(filepath.Join(base, resolved)), true
}

// realpath is os.path.realpath: symlinks resolved on the part that exists.
func realpath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// DeriveRoots are the repo roots a command may have written to: the cwd; per step of Walk the
// directory state; for `git` its target; every word with a "/" or starting with "~". An open quote
// falls back to the cwd plus a rough tokenisation. Every root goes through realpath; the project
// filter is ProjectFilter's.
func DeriveRoots(command, cwd string) map[string]bool {
	roots := map[string]bool{}
	add := func(path string) {
		if root, ok := rootOf(path); ok {
			roots[root] = true
		}
	}
	add(cwd)
	steps, err := gitinvocations.Walk(command, cwd)
	for _, step := range steps {
		add(step.Here)
		if gitinvocations.Basename(step.Words[step.Idx]) == "git" {
			target, known, _, _, _ := gitinvocations.GitTarget(step)
			if known {
				add(target)
			}
		}
		base := step.Here
		if base == "" {
			base = cwd
		}
		for k, word := range step.Words {
			if strings.Contains(word, "/") || strings.HasPrefix(word, "~") {
				if p, ok := resolvedPath(word, step.Literal[k], step.Tildes[k], base); ok {
					add(p)
				}
			}
		}
	}
	if errors.Is(err, gitinvocations.ErrUnparseable) {
		for _, token := range tokenRE.FindAllString(command, -1) {
			if strings.Contains(token, "/") || strings.HasPrefix(token, "~") {
				if p, ok := resolvedPath(token, true, strings.HasPrefix(token, "~"), cwd); ok {
					add(p)
				}
			}
		}
	}
	real := map[string]bool{}
	for root := range roots {
		real[realpath(root)] = true
	}
	return real
}

// ProjectFilter limits roots to those with the same --git-common-dir as projectDir, or the same
// origin (repoidentity.SameOrigin). A root for which git fails becomes an error line, not an
// error; when it fails for projectDir itself, the *GitError is returned.
func ProjectFilter(roots map[string]bool, projectDir string, deadline time.Time) (kept map[string]bool, errs []string, err error) {
	commonDir := func(root string) (string, error) {
		out, err := Git(root, deadline, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return "", err
		}
		return realpath(strings.TrimSpace(decodeReplace(out))), nil
	}
	ref, err := commonDir(projectDir)
	if err != nil {
		return nil, nil, err
	}
	ordered := make([]string, 0, len(roots))
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	kept = map[string]bool{}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	for _, root := range ordered {
		common, err := commonDir(root)
		if err != nil {
			errs = append(errs, "COULD NOT CHECK: "+root+" ("+err.Error()+")")
			continue
		}
		if common == ref {
			kept[root] = true
			continue
		}
		// A worktree of a separate clone (forge) with the same origin belongs too (#36).
		same, _, err := repoidentity.SameOrigin(ctx, projectDir, root)
		if err != nil {
			errs = append(errs, "COULD NOT CHECK: "+root+" ("+err.Error()+")")
			continue
		}
		if same {
			kept[root] = true
		}
	}
	return kept, errs, nil
}
