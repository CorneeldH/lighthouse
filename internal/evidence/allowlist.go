package evidence

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Allowlist holds the constants of evidence.py:49-79 under their Python names, as
// tests/pack/fixtures/evidence/allowlist.json records them: options without a value per command
// (`…_FLAGS`), options that take one value, separate or with `=` (`…_VAL`), and the git
// subcommands. Extending it is a change of this code and of the table in /lighthouse-write-pr,
// never something a PR body can do.
var Allowlist = map[string][]string{
	"GREP_FLAGS": {
		"-F", "-E", "-G", "-i", "-n", "-c", "-l", "-L", "-q", "-r", "-R", "-w", "-x",
		"-o", "-s", "-h", "-H", "-v",
		"--fixed-strings", "--extended-regexp", "--basic-regexp", "--ignore-case",
		"--line-number", "--count", "--files-with-matches", "--files-without-match",
		"--quiet", "--recursive", "--dereference-recursive", "--word-regexp",
		"--line-regexp", "--only-matching", "--no-messages", "--no-filename",
		"--with-filename", "--invert-match",
	},
	"GREP_VAL": {"-e", "--regexp", "--include", "--exclude"},
	// GREP_FLAGS without -r, -R, -s, --recursive, --dereference-recursive and --no-messages.
	"GIT_GREP_FLAGS": {
		"-F", "-E", "-G", "-i", "-n", "-c", "-l", "-L", "-q", "-w", "-x",
		"-o", "-h", "-H", "-v",
		"--fixed-strings", "--extended-regexp", "--basic-regexp", "--ignore-case",
		"--line-number", "--count", "--files-with-matches", "--files-without-match",
		"--quiet", "--word-regexp",
		"--line-regexp", "--only-matching", "--no-filename",
		"--with-filename", "--invert-match",
	},
	"GIT_GREP_VAL": {"-e"},
	"LS_FLAGS":     {"-l", "-a", "-A", "-1", "-d", "-R", "-h"},
	"WC_FLAGS":     {"-l", "-w", "-c", "-m"},
	"CAT_FLAGS":    {"-n"},
	// test: all arguments are one expression; none of them writes or executes.
	"GIT_LOG_FLAGS": {"--oneline", "--stat", "--name-only", "--"},
	// -n only with a number; -n5 is allowed too.
	"GIT_LOG_VAL":         {"-n", "--format", "--pretty", "--grep"},
	"GIT_LOG_EQUALS_ONLY": {"--format", "--pretty"},
	"GIT_SUBS":            {"log", "show", "ls-files", "grep"},
	// Paths: the normpath starts with `[test_gate] evidence_test_dir`.
	"PYTEST_FLAGS": {"-q", "-v"},
	// graphql: plus exactly one -f query=….
	"GH_API_VAL":    {"--jq", "-q"},
	"GH_VIEW_FLAGS": {"--comments", "-c"},
	"GH_VIEW_VAL":   {"--json", "--jq", "-q", "--repo", "-R"},
}

type set map[string]bool

func setOf(name string) set {
	s := set{}
	for _, v := range Allowlist[name] {
		s[v] = true
	}
	return s
}

var (
	grepFlags        = setOf("GREP_FLAGS")
	grepVal          = setOf("GREP_VAL")
	gitGrepFlags     = setOf("GIT_GREP_FLAGS")
	gitGrepVal       = setOf("GIT_GREP_VAL")
	lsFlags          = setOf("LS_FLAGS")
	wcFlags          = setOf("WC_FLAGS")
	catFlags         = setOf("CAT_FLAGS")
	gitLogFlags      = setOf("GIT_LOG_FLAGS")
	gitLogVal        = setOf("GIT_LOG_VAL")
	gitLogEqualsOnly = setOf("GIT_LOG_EQUALS_ONLY")
	gitSubs          = setOf("GIT_SUBS")
	pytestFlags      = setOf("PYTEST_FLAGS")
	ghAPIVal         = setOf("GH_API_VAL")
	ghViewFlags      = setOf("GH_VIEW_FLAGS")
	ghViewVal        = setOf("GH_VIEW_VAL")
)

// jqForbidden finds the words gojq (`gh --jq`) reads the environment or stdin with; the word
// boundaries are checked by hand, because Python's \b is Unicode-aware and Go's is not.
var jqForbidden = regexp.MustCompile(`env|\$ENV|inputs?`)

// refForbidden make a ref branch-relative.
var refForbidden = []string{"..", "@{", "~", "^"}

// parsed is the result of parse: positional args, args after `--`, and (option, value) pairs.
type parsed struct {
	pos   []string
	after []string
	vals  [][2]string
}

// value is the value of option name: (value, new index) or a reason. attached: the value is
// attached to the option (`--name=rest`, `-nrest`); then that is the whole value, even when rest
// is empty: git, GNU grep and pflag read `--name=` as an empty value and parse the next word as a
// new option. If we swallowed that word as the value, `--output=x` would slip past. An empty
// value is therefore refused, as is a short value that starts with `=` (`-q=x`: pflag strips the
// `=`, getopt does not).
func value(args []string, i int, name, rest string, attached bool) (string, int, string) {
	if attached {
		if rest == "" {
			return "", 0, "`" + name + "=` with an empty value is not allowed"
		}
		if !strings.HasPrefix(name, "--") && strings.HasPrefix(rest, "=") {
			return "", 0, "`" + name + rest + "`: a value that starts with `=` is not allowed"
		}
		return rest, i, ""
	}
	if i >= len(args) {
		return "", 0, "`" + name + "` lacks a value"
	}
	if args[i] == "" {
		return "", 0, "`" + name + "` with an empty value is not allowed"
	}
	return args[i], i + 1, ""
}

// parseLong handles one `--long` word (args[i-1]); the new index or a reason.
func parseLong(args []string, i int, flags, val set, out *parsed) (int, string) {
	tok := args[i-1]
	name, rest, equals := strings.Cut(tok, "=")
	if !val[name] {
		if flags[tok] {
			return i, ""
		}
		return 0, "option `" + tok + "` is not in the allowlist"
	}
	v, next, reason := value(args, i, name, rest, equals)
	if reason != "" {
		return 0, reason
	}
	out.vals = append(out.vals, [2]string{name, v})
	return next, ""
}

// parseShort handles one short cluster (`-nF`, `-n5`, `-efoo`; args[i-1]); the new index or a
// reason.
func parseShort(args []string, i int, flags, val set, out *parsed) (int, string) {
	tok := args[i-1]
	letters := []rune(tok)
	for pos := 1; pos < len(letters); pos++ {
		short := "-" + string(letters[pos])
		if val[short] {
			glued := string(letters[pos+1:])
			v, next, reason := value(args, i, short, glued, glued != "")
			if reason != "" {
				return 0, reason
			}
			out.vals = append(out.vals, [2]string{short, v})
			return next, ""
		}
		if !flags[short] {
			return 0, "option `" + short + "` (in `" + tok + "`) is not in the allowlist"
		}
	}
	return i, ""
}

// parse splits args into flags, value options and positional args; a reason for anything else.
// A word that starts with `-` and is on neither list is refused. Short clusters (`-nF`) are
// allowed when every letter is in flags; a value letter (`-n5`, `-efoo`) swallows the rest of the
// word, or else the next word.
func parse(args []string, flags, val set, ddOK bool) (*parsed, string) {
	out := &parsed{}
	i := 0
	for i < len(args) {
		tok := args[i]
		i++
		if tok == "--" {
			if !ddOK {
				return nil, "`--` is not allowed here"
			}
			out.after = args[i:]
			return out, ""
		}
		if tok == "-" || !strings.HasPrefix(tok, "-") {
			out.pos = append(out.pos, tok)
			continue
		}
		if flags[tok] {
			continue
		}
		step := parseShort
		if strings.HasPrefix(tok, "--") {
			step = parseLong
		}
		next, reason := step(args, i, flags, val, out)
		if reason != "" {
			return nil, reason
		}
		i = next
	}
	return out, ""
}

func reasonOnly(_ *parsed, reason string) string { return reason }

// isWordRune is a character of Python's \w in a str pattern.
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

// boundedAt is a Python \b between s[:i] and s[i:].
func boundedAt(s string, i int) bool {
	var before, after bool
	if i > 0 {
		r := []rune(s[:i])
		before = isWordRune(r[len(r)-1])
	}
	if i < len(s) {
		after = isWordRune([]rune(s[i:])[0])
	}
	return before != after
}

// jqReadsOutside is _JQ_FORBIDDEN_RE.search: `\benv\b`, `$ENV` or `\binputs?\b`.
func jqReadsOutside(expr string) bool {
	for start := 0; start <= len(expr); {
		loc := jqForbidden.FindStringIndex(expr[start:])
		if loc == nil {
			return false
		}
		from, to := start+loc[0], start+loc[1]
		word := expr[from:to]
		if word == "$ENV" {
			return true
		}
		// `inputs?` backtracking to `input` cannot help: `t` and `s` are both word characters.
		if boundedAt(expr, from) && boundedAt(expr, to) {
			return true
		}
		start = from + 1
	}
	return false
}

func jqReason(vals [][2]string) string {
	for _, nv := range vals {
		if (nv[0] == "--jq" || nv[0] == "-q") && jqReadsOutside(nv[1]) {
			return "`" + nv[0] + "` expression reads the environment or input"
		}
	}
	return ""
}

func refReason(args []string) string {
	for _, a := range args {
		for _, t := range refForbidden {
			if strings.Contains(a, t) {
				return "`" + a + "` is branch-relative (`..`, `@{`, `~`, `^`)"
			}
		}
	}
	return ""
}

func checkGrep(args []string) string { return reasonOnly(parse(args, grepFlags, grepVal, true)) }

// isDigitRunes are the characters beyond Nd that Python's str.isdigit accepts (Numeric_Type
// Digit: superscripts, circled digits and the like), from Python's Unicode 15.0 tables.
var isDigitRunes = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0xb2, Hi: 0xb3, Stride: 1}, {Lo: 0xb9, Hi: 0xb9, Stride: 1},
		{Lo: 0x1369, Hi: 0x1371, Stride: 1}, {Lo: 0x19da, Hi: 0x19da, Stride: 1},
		{Lo: 0x2070, Hi: 0x2070, Stride: 1}, {Lo: 0x2074, Hi: 0x2079, Stride: 1},
		{Lo: 0x2080, Hi: 0x2089, Stride: 1}, {Lo: 0x2460, Hi: 0x2468, Stride: 1},
		{Lo: 0x2474, Hi: 0x247c, Stride: 1}, {Lo: 0x2488, Hi: 0x2490, Stride: 1},
		{Lo: 0x24ea, Hi: 0x24ea, Stride: 1}, {Lo: 0x24f5, Hi: 0x24fd, Stride: 1},
		{Lo: 0x24ff, Hi: 0x24ff, Stride: 1}, {Lo: 0x2776, Hi: 0x277e, Stride: 1},
		{Lo: 0x2780, Hi: 0x2788, Stride: 1}, {Lo: 0x278a, Hi: 0x2792, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x10a40, Hi: 0x10a43, Stride: 1}, {Lo: 0x10e60, Hi: 0x10e68, Stride: 1},
		{Lo: 0x11052, Hi: 0x1105a, Stride: 1}, {Lo: 0x1f100, Hi: 0x1f10a, Stride: 1},
	},
}

// pyIsDigit is Python's str.isdigit: non-empty, every character a digit.
func pyIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) && !unicode.Is(isDigitRunes, r) {
			return false
		}
	}
	return true
}

func checkGit(args []string) string {
	if len(args) == 0 || !gitSubs[args[0]] {
		return "git: only log, show, ls-files and grep, without global options"
	}
	sub, rest := args[0], args[1:]
	if sub == "grep" {
		p, reason := parse(rest, gitGrepFlags, gitGrepVal, true)
		if reason != "" {
			return reason
		}
		hasE := slices.ContainsFunc(p.vals, func(nv [2]string) bool { return nv[0] == "-e" })
		// The first positional word without `-e` is the pattern, not a ref.
		pos := p.pos
		if !hasE {
			pos = pos[min(1, len(pos)):]
		}
		return refReason(append(slices.Clone(pos), p.after...))
	}
	// Without `=`, git reads `--pretty` as a bare flag and `--format` as an error: the next word
	// is then an option, not a value. Only the `=` form is unambiguous.
	for _, a := range rest {
		if gitLogEqualsOnly[a] {
			return "`" + a + "` only as `" + a + "=<value>`"
		}
	}
	p, reason := parse(rest, gitLogFlags, gitLogVal, true)
	if reason != "" {
		return reason
	}
	for _, nv := range p.vals {
		if nv[0] == "-n" && !pyIsDigit(nv[1]) {
			return "`-n` accepts only a number"
		}
		if (nv[0] == "--format" || nv[0] == "--pretty") && strings.Contains(nv[1], "%G") {
			return "`" + nv[0] + "` with `%G` starts gpg"
		}
	}
	return refReason(append(slices.Clone(p.pos), p.after...))
}

// pyNormpath is Python's posixpath.normpath: path.Clean, but a leading `//` (exactly two
// slashes) is kept, as POSIX allows.
func pyNormpath(p string) string {
	cleaned := path.Clean(p)
	if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
		return "/" + cleaned
	}
	return cleaned
}

// testDirFunc is `[test_gate] evidence_test_dir`, read only when a pytest row needs it.
type testDirFunc func() (string, error)

func checkPytest(args []string, testDir testDirFunc) (string, error) {
	if len(args) < 2 || args[0] != "run" || args[1] != "pytest" {
		return "uv: only `uv run pytest`, without options between `run` and `pytest`", nil
	}
	p, reason := parse(args[2:], pytestFlags, set{}, false)
	if reason != "" {
		return reason, nil
	}
	if len(p.pos) == 0 {
		return "uv run pytest without a path would run the whole suite", nil
	}
	raw, err := testDir()
	if err != nil {
		return "", err
	}
	dir := pyNormpath(raw) + "/"
	for _, tok := range p.pos {
		file, _, _ := strings.Cut(tok, "::")
		if !strings.HasPrefix(pyNormpath(file), dir) {
			return "path `" + tok + "` is not under `" + dir + "`", nil
		}
	}
	return "", nil
}

// pyLower is Python's str.lower as far as "mutation" can tell: İ lowers to `i̇`, two code points.
func pyLower(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

// graphqlField takes the one `-f query=…` out of args: (rest, "") or (nil, reason).
func graphqlField(args []string) ([]string, string) {
	var fields, rest []string
	for i := 0; i < len(args); {
		switch {
		case args[i] != "-f":
			rest = append(rest, args[i])
			i++
		case i+1 < len(args):
			fields = append(fields, args[i+1])
			i += 2
		default:
			return nil, "`-f` lacks a value"
		}
	}
	if len(fields) != 1 {
		return nil, "gh api graphql: exactly one `-f query=…`"
	}
	if !strings.HasPrefix(fields[0], "query=") {
		return nil, "gh api graphql: the `-f` value must start with `query=`"
	}
	if strings.Contains(pyLower(fields[0]), "mutation") {
		return nil, "gh api graphql: a mutation is not allowed"
	}
	return rest, ""
}

func checkGHAPI(args []string) string {
	// `-f` is for graphql only; without `graphql`, parse refuses it.
	graphql := slices.Contains(args, "graphql")
	if graphql {
		rest, reason := graphqlField(args)
		if reason != "" {
			return reason
		}
		args = rest
	}
	p, reason := parse(args, set{}, ghAPIVal, false)
	if reason != "" {
		return reason
	}
	if len(p.pos) != 1 {
		return "gh api: exactly one path or `graphql`"
	}
	target := p.pos[0]
	if graphql && target != "graphql" {
		return "gh api graphql: `graphql` must be the only path"
	}
	if strings.Contains(target, "://") || strings.HasPrefix(target, "//") {
		return "gh api: a path only, not a URL"
	}
	return jqReason(p.vals)
}

func checkGHView(args []string) string {
	p, reason := parse(args, ghViewFlags, ghViewVal, false)
	if reason != "" {
		return reason
	}
	// Without a number, gh reads the branch of the cwd. Entry C measures in a detached worktree
	// on `origin/main`: there is no branch there, and the row fails after the merge.
	if len(p.pos) != 1 {
		return "gh view: exactly one issue or PR number (or github.com URL)"
	}
	for _, tok := range p.pos {
		if strings.Contains(tok, "://") && !strings.HasPrefix(tok, "https://github.com/") {
			return "gh view: a github.com URL only"
		}
	}
	return jqReason(p.vals)
}

func checkGH(args []string) string {
	if len(args) >= 1 && args[0] == "api" {
		return checkGHAPI(args[1:])
	}
	if len(args) >= 2 && (args[0] == "issue" || args[0] == "pr") && args[1] == "view" {
		return checkGHView(args[2:])
	}
	return "gh: only `gh api`, `gh issue view` and `gh pr view`"
}

func flagsOnly(args []string, flags set) string { return reasonOnly(parse(args, flags, set{}, false)) }

// check is Check with the pytest directory read lazily; an error only from testDir.
func check(argv []string, testDir testDirFunc) (string, error) {
	if len(argv) == 0 {
		return "empty command", nil
	}
	name, args := argv[0], argv[1:]
	switch name {
	case "grep":
		return checkGrep(args), nil
	case "git":
		return checkGit(args), nil
	case "ls":
		return flagsOnly(args, lsFlags), nil
	case "wc":
		return flagsOnly(args, wcFlags), nil
	case "cat":
		return flagsOnly(args, catFlags), nil
	case "test":
		return "", nil
	case "uv":
		return checkPytest(args, testDir)
	case "gh":
		return checkGH(args), nil
	}
	return fmt.Sprintf("`%s` is not in the allowlist", name), nil
}

// Check is "" when argv is allowed, otherwise the reason. argv[0] is the bare name, no path or
// assignment; testDir is `[test_gate] evidence_test_dir`.
func Check(argv []string, testDir string) string {
	reason, _ := check(argv, func() (string, error) { return testDir, nil })
	return reason
}
