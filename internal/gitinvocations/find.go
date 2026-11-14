package gitinvocations

import "strings"

// Options tunes Find. An empty DefaultBranch means "main". With ReportNewDirs a `commit` in a
// directory that does not exist yet is reported with NewDir as its target, instead of the
// existing ancestor or nothing at all. A push is never affected: see settleTarget.
type Options struct {
	DefaultBranch string
	ReportNewDirs bool
}

// Invocation is one `git commit` or `git push` that Find found. ToMain is "yes", "no", "all" or
// "unresolved". Target is a directory, Unresolved or NewDir; Raw is the path as written.
type Invocation struct {
	Action, Target, ToMain, Raw string
}

var globalWithValue = set("-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path")
var pathOptions = set("-C", "--git-dir", "--work-tree")

func refspecTarget(arg string) string {
	spec := strings.TrimPrefix(arg, "+")
	if _, after, ok := strings.Cut(spec, ":"); ok {
		spec = after
	}
	return strings.TrimPrefix(spec, "refs/heads/")
}

// pushDestination is the destination of a push: "yes" (defaultBranch), "all" (all branches), "no"
// or "unresolved". The first positional argument is the remote, not the destination, so an
// untraceable remote says nothing about where the push goes.
func pushDestination(words []string, literal []bool, defaultBranch string) string {
	result := "no"
	positional := 0
	for k, arg := range words {
		if strings.HasPrefix(arg, "<") || strings.HasPrefix(arg, ">") {
			break
		}
		if arg == "--all" || arg == "--mirror" {
			result = "all"
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		positional++
		if !literal[k] {
			if result == "no" && positional > 1 {
				result = "unresolved"
			}
			continue
		}
		if refspecTarget(arg) == defaultBranch {
			return "yes"
		}
	}
	return result
}

// GitTarget runs the global-option loop of `git` (-C, --git-dir, --work-tree) over a step whose
// Idx points at the word `git`. known is false when the directory cannot be traced (Python
// None). actionIdx is the word after the last global option, so len(s.Words) if no subcommand
// follows.
func GitTarget(s Step) (target string, known bool, raw string, fromNewDir bool, actionIdx int) {
	words := s.Words // Walk guarantees s.Idx < len(words)
	target, known = s.Here, s.Here != ""
	raw, fromNewDir = s.Raw, s.NewDir
	idx := s.Idx + 1
	for idx < len(words) && strings.HasPrefix(words[idx], "-") {
		name, inline, sep := strings.Cut(words[idx], "=")
		var value string
		var has bool
		valueIdx := 0
		switch {
		case pathOptions[name] && sep:
			value, has, valueIdx = inline, true, idx
			idx++
		case globalWithValue[name] && !sep:
			if pathOptions[name] && idx+1 < len(words) {
				value, has, valueIdx = words[idx+1], true, idx+1
			}
			idx += 2
		default:
			idx++
		}
		if !has {
			continue
		}
		resolved, ok := Resolve(value, s.Literal[valueIdx], s.Tildes[valueIdx])
		switch {
		case !ok:
			target, known, raw, fromNewDir = "", false, value, false
		case isAbs(resolved):
			target, known, raw = pyNormpath(resolved), true, value
		case known:
			target = pyNormpath(pyJoin(target, resolved))
			raw = target
		default:
			raw = value
		}
	}
	return target, known, raw, fromNewDir, idx
}

// settleTarget resolves a target directory that does not exist. keep is false when the
// invocation falls outside the protection and is skipped.
func settleTarget(target string, known, fromNewDir bool, action, toMain string, reportNewDirs bool) (string, bool, bool) {
	if known && !isDir(target) {
		// Not no-man's-land: walk up to the nearest directory that exists. In a repo, the fresh
		// directory is a subdirectory of it, with the same branch, so `mkdir -p sub && cd sub &&
		// git commit` lands on main. Only an ancestor outside every repo is unprotectable.
		ancestor, found := ExistingAncestor(target)
		inRepo := false
		if found {
			_, inRepo = RepoOf(ancestor)
		}
		switch {
		case !inRepo:
			target, known, fromNewDir = "", false, true
		case reportNewDirs && action == "commit":
			target = NewDir
		default:
			target = ancestor
		}
	}
	if !known && fromNewDir && toMain != "yes" {
		// The directory comes into being only during this command and lies in no repo, so there
		// is no branch to protect. A push with a literal main destination always counts. This is
		// deliberately asymmetric with an untraceable -C path (F3).
		if reportNewDirs && action == "commit" {
			return NewDir, true, true
		}
		return target, known, false
	}
	return target, known, true
}

// Find finds the git commit/push invocations in command, executed from cwd. On an open quote it
// returns ErrUnparseable and no invocations.
func Find(command, cwd string, opts Options) ([]Invocation, error) {
	defaultBranch := opts.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	steps, err := Walk(command, cwd)
	if err != nil {
		return nil, err
	}
	var found []Invocation
	for _, step := range steps {
		words := step.Words
		if Basename(words[step.Idx]) != "git" { // Walk guarantees step.Idx < len(words)
			continue
		}
		target, known, raw, fromNewDir, idx := GitTarget(step)
		if idx >= len(words) {
			continue
		}
		action := words[idx]
		if action != "commit" && action != "push" {
			continue
		}
		toMain := "no"
		if action == "push" {
			toMain = pushDestination(words[idx+1:], step.Literal[idx+1:], defaultBranch)
		}
		target, known, keep := settleTarget(target, known, fromNewDir, action, toMain, opts.ReportNewDirs)
		if !keep {
			continue
		}
		if !known {
			target = Unresolved
		}
		found = append(found, Invocation{action, target, toMain, raw})
	}
	return found, nil
}
