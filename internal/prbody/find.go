package prbody

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
)

// valueLong and valueShort are the options that take a value (separate or `=`); `--recover`
// takes one too.
var (
	valueLong = set("--base", "--head", "--title", "--body", "--body-file", "--assignee",
		"--label", "--milestone", "--project", "--reviewer", "--repo", "--template", "--recover")
	valueShort = set("-B", "-H", "-t", "-b", "-F", "-a", "-l", "-m", "-p", "-r", "-R", "-T")
	// noBodyLong and noBodyShort yield no checkable body: the PR text then comes from elsewhere.
	noBodyLong  = set("--fill", "--fill-first", "--fill-verbose", "--web", "--editor")
	noBodyShort = map[rune]bool{'f': true, 'w': true, 'e': true}
)

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// PrCreate is one `gh pr create` (or `gh pr new`) with the state of its own step. A nil pointer is
// Python's None.
//
// Cwd is the directory of the step (nil: unknown); BodyFile is already resolved against it.
// Problem is a reason to block before any content check. RepoProblem blocks regardless of the
// cwd: with a non-literal repo we do not know whether the PR goes here. Repo comes from
// `--repo`/`-R` or a `GH_REPO=` before `gh`; the GH_REPO of the process is Env.ProcessGHRepo,
// which Main adds.
type PrCreate struct {
	Cwd, Repo, Body, BodyFile *string
	Problem, RepoProblem      string
}

// option is the value of an option: the word, whether it is literal and whether its tilde expands.
type option struct {
	value          string
	literal, tilde bool
}

// parseOption reads the word at k as an option: its name ("" for none), its value (nil without
// one), the no-body flags it carries and the next k.
func parseOption(step gitinvocations.Step, k int) (string, *option, []string, int) {
	words, literal, tildes := step.Words, step.Literal, step.Tildes
	w := words[k]
	var name string
	switch {
	case strings.HasPrefix(w, "--"):
		heading, rest, eq := strings.Cut(w, "=")
		switch {
		case valueLong[heading] && eq:
			return heading, &option{rest, literal[k], false}, nil, k + 1
		case valueLong[heading]:
			name = heading
		case noBodyLong[w]:
			return "", nil, []string{w}, k + 1
		default:
			return "", nil, nil, k + 1
		}
	case strings.HasPrefix(w, "-") && len(w) > 1:
		if len(w) > 2 && valueShort[w[:2]] { // `-Fbody.md`, `-F=body.md`
			return w[:2], &option{strings.TrimPrefix(w[2:], "="), literal[k], false}, nil, k + 1
		}
		if !valueShort[w] {
			var flags []string
			for _, c := range w[1:] {
				if !noBodyShort[c] {
					return "", nil, nil, k + 1
				}
				flags = append(flags, "-"+string(c))
			}
			return "", nil, flags, k + 1
		}
		name = w
	default:
		return "", nil, nil, k + 1
	}
	if k+1 >= len(words) { // an option without a value
		return name, nil, nil, k + 1
	}
	return name, &option{words[k+1], literal[k+1], tildes[k+1]}, nil, k + 2
}

// options is the option values per option name and the no-body flags found.
func options(step gitinvocations.Step) (map[string]option, []string) {
	values := map[string]option{}
	var flags []string
	k := step.Idx + 3
	for k < len(step.Words) && step.Words[k] != "--" {
		var name string
		var value *option
		var newFlags []string
		name, value, newFlags, k = parseOption(step, k)
		flags = append(flags, newFlags...)
		if name != "" && value != nil {
			values[name] = *value
		}
	}
	return values, flags
}

func firstValue(values map[string]option, names ...string) *option {
	for _, n := range names {
		if v, ok := values[n]; ok {
			return &v
		}
	}
	return nil
}

// The path helpers work on `/`, as gitinvocations does.

// pyNormpath is Python's os.path.normpath: path.Clean on the `/` form, but a leading `//`
// (exactly two slashes) is kept, as POSIX allows.
func pyNormpath(p string) string {
	p = filepath.ToSlash(p)
	cleaned := path.Clean(p)
	if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
		return "/" + cleaned
	}
	return cleaned
}

// isAbs is an absolute path: `/…`, or on Windows also a path with a drive.
func isAbs(p string) bool { return strings.HasPrefix(p, "/") || filepath.IsAbs(p) }

// bodySource is (body, body file, problem) per the body-source table of the spec.
func bodySource(cwd *string, bodyW, fileW *option, flags []string) (*string, *string, string) {
	if len(flags) > 0 {
		return nil, nil, flags[0] + " gives no body to check"
	}
	if bodyW != nil && fileW != nil {
		return nil, nil, "choose one body source"
	}
	source := fileW
	if source == nil {
		source = bodyW
	}
	if source == nil {
		return nil, nil, "no body"
	}
	if fileW != nil && source.value == "-" {
		return nil, nil, "a body via stdin cannot be checked"
	}
	if !source.literal {
		return nil, nil, "non-literal body — use --body-file"
	}
	if fileW == nil {
		value := source.value
		return &value, nil, ""
	}
	p, ok := gitinvocations.Resolve(source.value, source.literal, source.tilde)
	if ok && isAbs(p) {
		file := pyNormpath(p)
		return nil, &file, ""
	}
	if !ok || cwd == nil {
		return nil, nil, "path of the body file cannot be resolved"
	}
	joined := *cwd + "/" + p
	if strings.HasSuffix(*cwd, "/") {
		joined = *cwd + p
	}
	file := pyNormpath(joined)
	return nil, &file, ""
}

// ghRepoEnv is a `GH_REPO=…` before `gh` (also after `env`), as an option value; the last wins.
func ghRepoEnv(step gitinvocations.Step) *option {
	var found *option
	for k := range step.Idx {
		name, value, eq := strings.Cut(step.Words[k], "=")
		if name == "GH_REPO" && eq {
			found = &option{value, step.Literal[k], false}
		}
	}
	return found
}

// prCreate reads one step. unknownStart is a walk from an unknown cwd: Walk then reports a relative
// `cd` as a relative Here, where Python keeps None.
func prCreate(step gitinvocations.Step, unknownStart bool) PrCreate {
	var cwd *string
	if step.Here != "" && (!unknownStart || isAbs(step.Here)) {
		here := step.Here
		cwd = &here
	}
	values, flags := options(step)
	// gh: `--repo`/`-R` beats `GH_REPO`.
	flag := firstValue(values, "--repo", "-R")
	repoW := flag
	if repoW == nil {
		repoW = ghRepoEnv(step)
	}
	body, bodyFile, problem := bodySource(cwd,
		firstValue(values, "--body", "-b"), firstValue(values, "--body-file", "-F"), flags)
	pc := PrCreate{Cwd: cwd, Body: body, BodyFile: bodyFile, Problem: problem}
	if repoW != nil && !repoW.literal {
		// Does the PR go to this repo or another? We cannot tell, not even from the cwd: block,
		// wherever the command runs.
		source := "GH_REPO"
		if flag != nil {
			source = "--repo"
		}
		pc.RepoProblem = "non-literal " + source + " — spell the repo out"
		return pc
	}
	if repoW != nil {
		value := repoW.value
		pc.Repo = &value
	}
	return pc
}

// FindPrCreates is every `gh pr create`/`gh pr new` in command, run from cwd ("" for unknown).
// An open quote gives gitinvocations.ErrUnparseable. The GH_REPO of the process is not read here.
func FindPrCreates(command, cwd string) ([]PrCreate, error) {
	steps, err := gitinvocations.Walk(command, cwd)
	if err != nil {
		return nil, err
	}
	var found []PrCreate
	for _, step := range steps {
		w := step.Words
		if gitinvocations.Basename(w[step.Idx]) == "gh" && step.Idx+2 < len(w) &&
			w[step.Idx+1] == "pr" && (w[step.Idx+2] == "create" || w[step.Idx+2] == "new") {
			found = append(found, prCreate(step, cwd == ""))
		}
	}
	return found, nil
}
