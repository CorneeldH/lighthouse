package prbody

import (
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
)

// tool is one command that creates a pull or merge request, with the options the gate reads.
// valueLong and valueShort are the options that take a value (separate or `=`); noBodyLong and
// noBodyShort yield no checkable body: the text then comes from elsewhere. body, bodyFile and
// repo name the options of each source, the first found wins; ghRepo is a tool that reads
// GH_REPO.
type tool struct {
	name                  string
	valueLong, valueShort map[string]bool
	noBodyLong            map[string]bool
	noBodyShort           map[rune]bool
	body, bodyFile, repo  []string
	ghRepo                bool
}

var (
	// ghTool is `gh pr create`/`gh pr new`; `--recover` takes a value too.
	ghTool = &tool{
		name: "gh",
		valueLong: set("--base", "--head", "--title", "--body", "--body-file", "--assignee",
			"--label", "--milestone", "--project", "--reviewer", "--repo", "--template", "--recover"),
		valueShort:  set("-B", "-H", "-t", "-b", "-F", "-a", "-l", "-m", "-p", "-r", "-R", "-T"),
		noBodyLong:  set("--fill", "--fill-first", "--fill-verbose", "--web", "--editor"),
		noBodyShort: map[rune]bool{'f': true, 'w': true, 'e': true},
		body:        []string{"--body", "-b"},
		bodyFile:    []string{"--body-file", "-F"},
		repo:        []string{"--repo", "-R"},
		ghRepo:      true,
	}
	// glabTool is `glab mr create`/`glab mr new`: the body is the description; `-b` is the
	// target branch, not a body.
	glabTool = &tool{
		name: "glab",
		valueLong: set("--title", "--description", "--repo", "--assignee", "--reviewer",
			"--target-branch", "--source-branch", "--label", "--milestone", "--related-issue",
			"--head", "--recover"),
		valueShort:  set("-t", "-d", "-R", "-a", "-b", "-s", "-l", "-m", "-H"),
		noBodyLong:  set("--fill", "--fill-commit-body", "--web"),
		noBodyShort: map[rune]bool{'f': true, 'w': true},
		body:        []string{"--description", "-d"},
		repo:        []string{"--repo", "-R"},
	}
	// lighthouseTool is `lighthouse forge pr create`: only --body-file, and the repo is always
	// forge.repo of the checkout it runs in.
	lighthouseTool = &tool{
		name:      "lighthouse",
		valueLong: set("--title", "--body-file", "--head", "--base", "--milestone"),
		bodyFile:  []string{"--body-file"},
	}
)

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// PrCreate is one `gh pr create` (or `gh pr new`), `glab mr create` (or `glab mr new`) or
// `lighthouse forge pr create`, with the state of its own step. A nil pointer is Python's None.
//
// Tool is "gh", "glab" or "lighthouse". Cwd is the directory of the step (nil: unknown);
// BodyFile is already resolved against it. Problem is a reason to block before any content
// check. RepoProblem blocks regardless of the cwd: with a non-literal repo we do not know
// whether the PR goes here. Repo comes from `--repo`/`-R` (gh and glab) or a `GH_REPO=` before
// `gh`; the GH_REPO of the process is Env.ProcessGHRepo, which Main adds for gh only. For
// lighthouse the repo is always forge.repo of the checkout: Repo stays nil.
type PrCreate struct {
	Cwd, Repo, Body, BodyFile *string
	Problem, RepoProblem      string
	Tool                      string
}

// option is the value of an option: the word, whether it is literal and whether its tilde expands.
type option struct {
	value          string
	literal, tilde bool
}

// parseOption reads the word at k as an option: its name ("" for none), its value (nil without
// one), the no-body flags it carries and the next k.
func parseOption(tl *tool, step gitinvocations.Step, k int) (string, *option, []string, int) {
	words, literal, tildes := step.Words, step.Literal, step.Tildes
	w := words[k]
	var name string
	switch {
	case strings.HasPrefix(w, "--"):
		heading, rest, eq := strings.Cut(w, "=")
		switch {
		case tl.valueLong[heading] && eq:
			return heading, &option{rest, literal[k], false}, nil, k + 1
		case tl.valueLong[heading]:
			name = heading
		case tl.noBodyLong[w]:
			return "", nil, []string{w}, k + 1
		default:
			return "", nil, nil, k + 1
		}
	case strings.HasPrefix(w, "-") && len(w) > 1:
		if len(w) > 2 && tl.valueShort[w[:2]] { // `-Fbody.md`, `-F=body.md`
			return w[:2], &option{strings.TrimPrefix(w[2:], "="), literal[k], false}, nil, k + 1
		}
		if !tl.valueShort[w] {
			var flags []string
			for _, c := range w[1:] {
				if !tl.noBodyShort[c] {
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

// options is the option values per option name and the no-body flags found, from word start on.
func options(tl *tool, step gitinvocations.Step, start int) (map[string]option, []string) {
	values := map[string]option{}
	var flags []string
	k := start
	for k < len(step.Words) && step.Words[k] != "--" {
		var name string
		var value *option
		var newFlags []string
		name, value, newFlags, k = parseOption(tl, step, k)
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

// prCreate reads one step of tl whose options start at word start. unknownStart is a walk from an
// unknown cwd: Walk then reports a relative `cd` as a relative Here, where Python keeps None.
func prCreate(tl *tool, step gitinvocations.Step, start int, unknownStart bool) PrCreate {
	var cwd *string
	if step.Here != "" && (!unknownStart || isAbs(step.Here)) {
		here := step.Here
		cwd = &here
	}
	values, flags := options(tl, step, start)
	// gh: `--repo`/`-R` beats `GH_REPO`.
	flag := firstValue(values, tl.repo...)
	repoW := flag
	if repoW == nil && tl.ghRepo {
		repoW = ghRepoEnv(step)
	}
	body, bodyFile, problem := bodySource(cwd,
		firstValue(values, tl.body...), firstValue(values, tl.bodyFile...), flags)
	pc := PrCreate{Tool: tl.name, Cwd: cwd, Body: body, BodyFile: bodyFile, Problem: problem}
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

// recognise is the tool of the command at word idx of w and the word its options start at, or
// nil: `gh pr create|new`, `glab mr create|new`, and `lighthouse forge pr create`, also as
// `go run <…/lighthouse> forge pr create`.
func recognise(w []string, idx int) (*tool, int) {
	has := func(i int, words ...string) bool { return idx+i < len(w) && slices.Contains(words, w[idx+i]) }
	switch gitinvocations.Basename(w[idx]) {
	case "gh":
		if has(1, "pr") && has(2, "create", "new") {
			return ghTool, idx + 3
		}
	case "glab":
		if has(1, "mr") && has(2, "create", "new") {
			return glabTool, idx + 3
		}
	case "lighthouse":
		if has(1, "forge") && has(2, "pr") && has(3, "create") {
			return lighthouseTool, idx + 4
		}
	case "go":
		if has(1, "run") && idx+2 < len(w) && gitinvocations.Basename(w[idx+2]) == "lighthouse" &&
			has(3, "forge") && has(4, "pr") && has(5, "create") {
			return lighthouseTool, idx + 6
		}
	}
	return nil, 0
}

// FindPrCreates is every pull or merge request create (see recognise) in command, run from cwd
// ("" for unknown). An open quote gives gitinvocations.ErrUnparseable. The GH_REPO of the process
// is not read here.
func FindPrCreates(command, cwd string) ([]PrCreate, error) {
	steps, err := gitinvocations.Walk(command, cwd)
	if err != nil {
		return nil, err
	}
	var found []PrCreate
	for _, step := range steps {
		if tl, start := recognise(step.Words, step.Idx); tl != nil {
			found = append(found, prCreate(tl, step, start, cwd == ""))
		}
	}
	return found, nil
}
