package lintcheck

import (
	"path"
	"regexp"
	"strings"
)

// Verdict is the judgement of one linter call in CI.
type Verdict int

const (
	// NotFound: no CI step runs the linter.
	NotFound Verdict = iota
	// Valid: the call makes CI red on a finding, with the floor on and the version pinned.
	Valid
	// Unrecognised: the check cannot show that the exit status of the call fails the step.
	Unrecognised
	// Narrowed: a flag or a config turns a floor rule off or narrows the selection.
	Narrowed
	// Unpinned: no install on a pinned version precedes the call in its job.
	Unpinned
)

func (v Verdict) String() string {
	return [...]string{"NotFound", "Valid", "Unrecognised", "Narrowed", "Unpinned"}[v]
}

// Call is one recognised call of a linter in CI. Args are the arguments after the subcommand
// (`ruff check`, `golangci-lint run`, `expo lint`), or the `args:` input of an action. Dir is the
// directory the call runs in, slash-separated and relative to the repo root, after a cd or a
// package.json script.
type Call struct {
	Step    Step
	Linter  string
	Args    []string
	Dir     string
	Verdict Verdict
	Reason  string
}

// Calls returns every call of linter ("ruff", "golangci-lint", "shellcheck" or "eslint") in steps,
// each with its verdict. pkgJSON gives the scripts of the package.json in a directory, for
// `npm run <script>` (one level); floor are the floor codes; cfgNarrows asks the checker whether a
// `--config <file>` (relative to dir) misses the floor or makes no choice. A step that never runs
// (`if: false`) has no calls.
func Calls(steps []Step, linter string, pkgJSON func(dir string) map[string]string, floor []string, cfgNarrows func(dir, cfgPath string) bool) []Call {
	if pkgJSON == nil {
		pkgJSON = func(string) map[string]string { return nil }
	}
	if cfgNarrows == nil {
		cfgNarrows = func(string, string) bool { return true }
	}
	r := resolver{linter: linter, pkgJSON: pkgJSON, floor: floor, cfgNarrows: cfgNarrows}
	var out []Call
	for i, s := range steps {
		if s.IfFalse {
			continue
		}
		var found []found
		if s.Uses != "" {
			found = r.actionCalls(s)
		} else if s.Run != "" {
			found = r.runCalls(s)
		}
		for _, f := range found {
			if f.Verdict == Valid && !r.pinned(steps, i, f) {
				f.Verdict, f.Reason = Unpinned, "CI version not pinned"
			}
			f.Dir = f.dir
			out = append(out, f.Call)
		}
	}
	return out
}

// wiredCall applies the aggregation rule: the linter is wired when one call is Valid; otherwise
// the first call is reported, and without a call a NotFound call says no CI step runs it.
func wiredCall(calls []Call, linter string) (Call, bool) {
	for _, c := range calls {
		if c.Verdict == Valid {
			return c, true
		}
	}
	if len(calls) > 0 {
		return calls[0], false
	}
	name := linter
	if linter == "ruff" {
		name = "ruff check"
	}
	return Call{Linter: linter, Verdict: NotFound, Reason: "no CI step runs " + name}, false
}

// found is a call with the directory it runs in, after a cd or a package.json script.
type found struct {
	Call
	dir string
}

type resolver struct {
	linter     string
	pkgJSON    func(dir string) map[string]string
	floor      []string
	cfgNarrows func(dir, cfgPath string) bool
}

// The actions the check recognises, by the part of `uses:` before the @.
const (
	ruffAction     = "astral-sh/ruff-action"
	golangciAction = "golangci/golangci-lint-action"
)

func actionName(uses string) string {
	if i := strings.IndexByte(uses, '@'); i >= 0 {
		uses = uses[:i]
	}
	return strings.TrimSpace(uses)
}

func (r resolver) actionCalls(s Step) []found {
	name := actionName(s.Uses)
	var args []string
	if a := strings.TrimSpace(s.With["args"]); a != "" {
		sc, err := parseScript(a)
		if err != nil || len(sc.lists) != 1 || len(sc.lists[0].pipes) != 1 || len(sc.lists[0].pipes[0].cmds) != 1 {
			if (name == ruffAction && r.linter == "ruff") || (name == golangciAction && r.linter == "golangci-lint") {
				return []found{{Call: Call{Step: s, Linter: r.linter, Verdict: Unrecognised, Reason: "step not recognised: action args not placeable"}}}
			}
			return nil
		}
		args = texts(sc.lists[0].pipes[0].cmds[0].words)
	}
	switch {
	case name == ruffAction && r.linter == "ruff":
		// the action runs `ruff <args>`, `check` by default
		if len(args) > 0 {
			if args[0] != "check" {
				return nil // ruff format or another subcommand: no lint call
			}
			args = args[1:]
		}
	case name == golangciAction && r.linter == "golangci-lint":
		if strings.TrimSpace(s.With["install-only"]) == "true" {
			return nil
		}
	default:
		return nil
	}
	if infoOnly(r.linter, args) {
		return nil
	}
	c := Call{Step: s, Linter: r.linter, Args: args, Verdict: Valid}
	if why := stepGate(s); why != "" {
		c.Verdict, c.Reason = Unrecognised, why
	} else {
		c.Verdict, c.Reason = r.judgeArgs(args, workDir(s), s.Env)
	}
	return []found{{Call: c, dir: workDir(s)}}
}

func workDir(s Step) string { return joinDir(".", s.WorkDir) }

// joinDir resolves a cd target against dir, slash-separated and relative to the repo root.
func joinDir(dir, to string) string {
	if dir == "" {
		dir = "."
	}
	if to == "" {
		return path.Clean(dir)
	}
	if strings.HasPrefix(to, "/") || strings.ContainsAny(to, "$`~") {
		return to // not placeable in the repo; a lookup there finds nothing
	}
	return path.Clean(path.Join(dir, to))
}

// runCalls finds the calls in a run script.
func (r resolver) runCalls(s Step) []found {
	sc, err := parseScript(s.Run)
	if err != nil {
		if r.mentions(s.Run) {
			return []found{{Call: Call{Step: s, Linter: r.linter, Verdict: Unrecognised, Reason: "step not recognised: script not placeable"}}}
		}
		return nil
	}
	gate := stepGate(s)
	if gate == "" {
		gate = runGate(s)
	}
	name, _, _ := effectiveShell(s)
	start := shellState{errexit: true, pipefail: startsWithPipefail(name), dir: workDir(s)}
	var out []found
	sc.walk(start, func(v visit) {
		for _, m := range r.match(v.words, v.state.dir, true) {
			c := Call{Step: s, Linter: r.linter, Args: m.args, Verdict: Valid}
			switch {
			case m.unplaced != "":
				c.Verdict, c.Reason = Unrecognised, m.unplaced
			case gate != "":
				c.Verdict, c.Reason = Unrecognised, gate
			default:
				if why := sc.positionGate(v.pos, v.state, true); why != "" {
					c.Verdict, c.Reason = Unrecognised, why
				} else {
					c.Verdict, c.Reason = r.judgeArgs(m.args, m.dir, s.Env)
				}
			}
			out = append(out, found{Call: c, dir: m.dir})
		}
	})
	return out
}

// match is one call found in a command.
type match struct {
	args     []string
	dir      string
	unplaced string // non-empty: a call the check cannot place, with the reason
}

// runners run another command whose exit status the check does not follow.
var runners = map[string]bool{"xargs": true, "find": true, "parallel": true, "eval": true, "watch": true, "bash": true, "sh": true, "zsh": true, "dash": true}

// match recognises the linter in one command. follow allows one level of package.json scripts.
func (r resolver) match(words []string, dir string, follow bool) []match {
	argv := stripWrappers(words)
	if len(argv) == 0 {
		return nil
	}
	if args, ok := r.direct(argv); ok {
		if infoOnly(r.linter, args) {
			return nil // prints help, a version or settings: lints nothing
		}
		return []match{{args: args, dir: dir}}
	}
	name := binName(argv[0])
	switch r.linter {
	case "eslint":
		if script, extra, sdir, ok := scriptRun(argv, dir); ok {
			if !follow {
				return nil
			}
			return r.followScript(script, extra, sdir)
		}
	}
	if runners[name] && r.mentions(strings.Join(argv[1:], " ")) {
		return []match{{dir: dir, unplaced: "step not recognised: call through " + name}}
	}
	return nil
}

// direct recognises a call of the linter itself and returns its arguments after the subcommand.
func (r resolver) direct(argv []string) ([]string, bool) {
	name := binName(argv[0])
	sub := ""
	if len(argv) > 1 {
		sub = argv[1]
	}
	switch {
	case r.linter == "ruff" && name == "ruff" && sub == "check",
		r.linter == "golangci-lint" && name == "golangci-lint" && sub == "run",
		r.linter == "eslint" && name == "expo" && sub == "lint":
		return argv[2:], true
	case r.linter == "shellcheck" && (name == "shellcheck" || name == "shellcheck-repo.sh"),
		r.linter == "eslint" && name == "eslint":
		return argv[1:], true
	}
	return nil, false
}

// infoFlags are the options with which a linter prints information and lints nothing.
var infoFlags = map[string][]string{
	"ruff":          {"--help", "-h", "--show-settings", "--show-files"},
	"golangci-lint": {"--help", "-h"},
	"shellcheck":    {"--help", "--version", "-V", "--check-floor"}, // --check-floor: shellcheck-repo.sh probes the floor
	"eslint":        {"--version", "-v", "--help", "-h", "--print-config", "--env-info", "--init"},
}

func infoOnly(linter string, args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if j := strings.IndexByte(a, '='); j > 0 && strings.HasPrefix(a, "--") {
			a = a[:j]
		}
		if hasAny([]string{a}, infoFlags[linter]...) {
			return true
		}
	}
	return false
}

// followScript recognises the linter in a package.json script. npm runs it with `sh -c`, without
// errexit, so only the last command's status counts.
func (r resolver) followScript(name string, extra []string, dir string) []match {
	body := r.pkgJSON(dir)[name]
	if body == "" {
		return nil
	}
	sc, err := parseScript(body)
	if err != nil {
		if r.mentions(body) {
			return []match{{dir: dir, unplaced: "step not recognised: package.json script not placeable"}}
		}
		return nil
	}
	var out []match
	sc.walk(shellState{dir: dir}, func(v visit) {
		for _, m := range r.match(v.words, v.state.dir, false) {
			if m.unplaced == "" {
				if why := sc.positionGate(v.pos, v.state, false); why != "" {
					m.unplaced = why + " (package.json script " + name + ")"
				}
				m.args = append(append([]string{}, m.args...), extra...)
				if infoOnly(r.linter, m.args) {
					continue // the passed-on arguments make it print information only
				}
			}
			out = append(out, m)
		}
	})
	return out
}

// npmBuiltins are pnpm and yarn subcommands that are not package.json scripts.
var npmBuiltins = map[string]bool{
	"install": true, "i": true, "add": true, "remove": true, "ci": true, "exec": true, "dlx": true,
	"x": true, "run": true, "test": true, "t": true, "why": true, "update": true, "up": true,
	"link": true, "publish": true, "pack": true, "init": true, "create": true, "config": true,
}

// scriptRun recognises `npm run <s>`, `npm test`, `pnpm [run] <s>`, `yarn [run] <s>` and
// `bun run <s>`, with --prefix, -C, --dir or --cwd moving the directory. Arguments after the
// script (npm: after `--`) are passed on.
func scriptRun(argv []string, dir string) (name string, extra []string, sdir string, ok bool) {
	tool := binName(argv[0])
	if tool != "npm" && tool != "pnpm" && tool != "yarn" && tool != "bun" {
		return "", nil, "", false
	}
	sdir = dir
	rest := argv[1:]
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		f := rest[0]
		rest = rest[1:]
		switch {
		case (f == "--prefix" || f == "-C" || f == "--dir" || f == "--cwd") && len(rest) > 0:
			sdir = joinDir(dir, rest[0])
			rest = rest[1:]
		case strings.HasPrefix(f, "--prefix="), strings.HasPrefix(f, "--dir="), strings.HasPrefix(f, "--cwd="):
			sdir = joinDir(dir, f[strings.IndexByte(f, '=')+1:])
		}
	}
	if len(rest) == 0 {
		return "", nil, "", false
	}
	switch {
	case rest[0] == "run" || rest[0] == "run-script":
		if len(rest) < 2 {
			return "", nil, "", false
		}
		name, rest = rest[1], rest[2:]
	case tool == "npm" && (rest[0] == "test" || rest[0] == "t"):
		name, rest = "test", rest[1:]
	case (tool == "pnpm" || tool == "yarn") && !npmBuiltins[rest[0]]:
		name, rest = rest[0], rest[1:]
	default:
		return "", nil, "", false
	}
	if tool == "npm" {
		for i, a := range rest {
			if a == "--" {
				return name, rest[i+1:], sdir, true
			}
		}
		return name, nil, sdir, true
	}
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	return name, rest, sdir, true
}

// mentions reports whether text names the linter, for text the check could not place.
func (r resolver) mentions(text string) bool {
	switch r.linter {
	case "eslint":
		return strings.Contains(text, "eslint") || strings.Contains(text, "expo lint") || strings.Contains(text, "run lint")
	case "shellcheck":
		return strings.Contains(text, "shellcheck")
	}
	return strings.Contains(text, r.linter)
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// stripWrappers drops leading assignments and the commands that only run another one with its
// exit status: env, sudo, time, timeout, uvx, uv run, npx, pnpm exec, yarn exec, bunx,
// poetry run, python -m, go run and a shell running a script file.
func stripWrappers(words []string) []string {
	w := words
	for {
		for len(w) > 0 && assignment.MatchString(w[0]) {
			w = w[1:]
		}
		if len(w) == 0 {
			return nil
		}
		next, ok := unwrap(w)
		if !ok {
			return w
		}
		w = next
	}
}

// valueFlags are wrapper flags that take the next word as their value.
var valueFlags = map[string]bool{
	"--from": true, "--with": true, "--python": true, "-p": true, "--package": true, "-u": true,
	"--directory": true, "--project": true, "--group": true, "--extra": true, "-k": true, "-s": true,
	"--signal": true, "--kill-after": true, "-n": true, "--adjustment": true, "-C": true,
}

func skipFlags(w []string) []string {
	for len(w) > 0 && strings.HasPrefix(w[0], "-") {
		if valueFlags[w[0]] && len(w) > 1 {
			w = w[2:]
			continue
		}
		w = w[1:]
	}
	return w
}

func unwrap(w []string) ([]string, bool) {
	name := binName(w[0])
	sub := ""
	if len(w) > 1 {
		sub = w[1]
	}
	switch {
	case name == "env":
		rest := skipFlags(w[1:])
		for len(rest) > 0 && assignment.MatchString(rest[0]) {
			rest = rest[1:]
		}
		return rest, true
	case name == "command" && (sub == "-v" || sub == "-V"):
		return nil, true // looks the command up, runs nothing
	case name == "sudo" || name == "time" || name == "command" || name == "exec" || name == "nice" || name == "nohup":
		return skipFlags(w[1:]), true
	case name == "timeout":
		rest := skipFlags(w[1:])
		if len(rest) > 0 {
			rest = rest[1:] // the duration
		}
		return rest, true
	case name == "uvx" || name == "bunx" || name == "pnpx" || name == "npx":
		return skipFlags(w[1:]), true
	case name == "uv" && sub == "run":
		return skipFlags(w[2:]), true
	case name == "uv" && sub == "tool" && len(w) > 2 && w[2] == "run":
		return skipFlags(w[3:]), true
	case (name == "pnpm" || name == "yarn") && (sub == "exec" || sub == "dlx"):
		return skipFlags(w[2:]), true
	case name == "bun" && sub == "x":
		return skipFlags(w[2:]), true
	case (name == "poetry" || name == "pipx" || name == "pdm" || name == "hatch" || name == "rye") && sub == "run":
		return skipFlags(w[2:]), true
	case strings.HasPrefix(name, "python") && sub == "-m" && len(w) > 2:
		return w[2:], true
	case name == "go" && sub == "run" && len(w) > 2:
		rest := skipFlags(w[2:])
		return rest, len(rest) > 0
	case name == "yarn" && len(w) > 1 && (sub == "eslint"):
		return w[1:], true
	case (name == "bash" || name == "sh") && len(w) > 1 && !strings.HasPrefix(sub, "-"):
		return w[1:], true // a script file: its name decides
	}
	return nil, false
}

// binName is the command name of a word: the base of a path, without a version suffix
// (ruff@0.9.0, ruff==0.9.0, …/golangci-lint@v2.15.0).
func binName(w string) string {
	w = path.Base(w)
	if i := strings.Index(w, "@"); i > 0 {
		w = w[:i]
	}
	if i := strings.Index(w, "=="); i > 0 {
		w = w[:i]
	}
	return w
}

func hasAny(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

// flag is one parsed option: --name=value, --name value or -xvalue.
type flag struct{ name, value string }

// parseFlags splits args into options; takesValue names the options whose value is the next word.
func parseFlags(args []string, takesValue map[string]bool) []flag {
	var out []flag
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if a == "--" {
			break
		}
		if j := strings.IndexByte(a, '='); j > 0 && strings.HasPrefix(a, "--") {
			out = append(out, flag{a[:j], a[j+1:]})
			continue
		}
		if takesValue[a] && i+1 < len(args) {
			out = append(out, flag{a, args[i+1]})
			i++
			continue
		}
		// a short option with its value attached: -eSC2164
		if len(a) > 2 && a[1] != '-' && takesValue[a[:2]] {
			out = append(out, flag{a[:2], a[2:]})
			continue
		}
		out = append(out, flag{a, ""})
	}
	return out
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// judgeArgs returns the verdict from the arguments of a call: Unrecognised for a flag that masks
// the exit status, Narrowed for one that turns the floor off or narrows it. env carries
// SHELLCHECK_OPTS, which shellcheck reads as extra arguments.
func (r resolver) judgeArgs(args []string, dir string, env map[string]string) (Verdict, string) {
	switch r.linter {
	case "ruff":
		return r.judgeRuff(args, dir)
	case "golangci-lint":
		return r.judgeGolangci(args, dir)
	case "shellcheck":
		if opts := strings.TrimSpace(env["SHELLCHECK_OPTS"]); opts != "" {
			sc, err := parseScript(opts)
			if err != nil || len(sc.lists) != 1 || len(sc.lists[0].pipes) != 1 || len(sc.lists[0].pipes[0].cmds) != 1 {
				return Unrecognised, "step not recognised: SHELLCHECK_OPTS not placeable"
			}
			v, why := r.judgeShellcheck(texts(sc.lists[0].pipes[0].cmds[0].words), dir)
			if v != Valid {
				return v, why + " (SHELLCHECK_OPTS)"
			}
		}
		return r.judgeShellcheck(args, dir)
	case "eslint":
		return r.judgeEslint(args, dir)
	}
	return Unrecognised, "step not recognised: unknown linter " + r.linter
}

// ruffCovers reports whether a ruff selector (a code, a prefix or ALL) covers code.
func ruffCovers(selector, code string) bool {
	return selector == "ALL" || strings.HasPrefix(code, selector)
}

func (r resolver) judgeRuff(args []string, dir string) (Verdict, string) {
	takes := map[string]bool{"--select": true, "--ignore": true, "--extend-ignore": true, "--per-file-ignores": true,
		"--extend-per-file-ignores": true, "--config": true, "--exclude": true, "--extend-exclude": true,
		"--extend-select": true, "--output-format": true, "--target-version": true, "--line-length": true}
	for _, f := range parseFlags(args, takes) {
		switch f.name {
		case "--exit-zero", "-e", "--fix", "--fix-only":
			return Unrecognised, "step not recognised: " + f.name + " hides findings from the exit status"
		case "--isolated":
			return Narrowed, "CI flag --isolated drops the config"
		case "--select":
			sel := splitList(f.value)
			for _, code := range r.floor {
				covered := false
				for _, s := range sel {
					covered = covered || ruffCovers(s, code)
				}
				if !covered {
					return Narrowed, "CI flag --select drops floor rule " + code
				}
			}
		case "--ignore", "--extend-ignore":
			// --per-file-ignores is path scope, which does not count; it only takes a value
			for _, s := range splitList(f.value) {
				for _, code := range r.floor {
					if ruffCovers(s, code) {
						return Narrowed, "CI flag " + f.name + " turns off floor rule " + code
					}
				}
			}
		case "--config":
			if strings.Contains(f.value, "=") {
				return Narrowed, "CI flag --config overrides the selection inline"
			}
			if r.cfgNarrows(dir, f.value) {
				return Narrowed, "CI config " + f.value + " misses the floor"
			}
		}
	}
	return Valid, ""
}

func (r resolver) judgeGolangci(args []string, dir string) (Verdict, string) {
	takes := map[string]bool{"--disable": true, "-D": true, "--enable-only": true, "--default": true,
		"--config": true, "-c": true, "--issues-exit-code": true, "--enable": true, "-E": true, "--timeout": true}
	for _, f := range parseFlags(args, takes) {
		switch f.name {
		case "--issues-exit-code":
			if strings.TrimSpace(f.value) == "0" {
				return Unrecognised, "step not recognised: --issues-exit-code=0 hides findings from the exit status"
			}
		case "--disable", "-D":
			for _, l := range splitList(f.value) {
				if contains(r.floor, l) {
					return Narrowed, "CI flag " + f.name + " turns off floor linter " + l
				}
			}
		case "--enable-only":
			on := splitList(f.value)
			for _, l := range r.floor {
				if !contains(on, l) {
					return Narrowed, "CI flag --enable-only drops floor linter " + l
				}
			}
		case "--default":
			if v := strings.TrimSpace(f.value); v != "standard" && v != "all" {
				return Narrowed, "CI flag --default " + v + " narrows the floor"
			}
		case "--disable-all", "--fast", "--fast-only":
			return Narrowed, "CI flag " + f.name + " narrows the floor"
		case "--no-config":
			return Narrowed, "CI flag --no-config drops the config"
		case "--config", "-c":
			if r.cfgNarrows(dir, f.value) {
				return Narrowed, "CI config " + f.value + " misses the floor"
			}
		}
	}
	return Valid, ""
}

// shellcheckSeverities in rising order.
var shellcheckSeverities = []string{"style", "info", "warning", "error"}

// shellcheckFloorSeverity maps the floor codes of shell-lint-check.sh to the severity shellcheck
// 0.11.0 reports them with (checked against the pinned binary). A code that is not here counts as
// style, the lowest, so any raised --severity narrows it.
var shellcheckFloorSeverity = map[string]string{
	"SC2164": "warning", "SC2046": "warning", "SC2115": "warning", "SC2155": "warning", "SC2034": "warning",
}

// shellcheckCodeSeverity returns the severity of a shellcheck code: style, info, warning or error.
func shellcheckCodeSeverity(code string) string {
	if s, ok := shellcheckFloorSeverity[normSC(code)]; ok {
		return s
	}
	return "style"
}

func severityRank(s string) int {
	for i, v := range shellcheckSeverities {
		if v == s {
			return i
		}
	}
	return len(shellcheckSeverities) // unknown: above everything, so it narrows
}

func normSC(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !strings.HasPrefix(code, "SC") {
		code = "SC" + code
	}
	return code
}

func (r resolver) judgeShellcheck(args []string, dir string) (Verdict, string) {
	takes := map[string]bool{"-e": true, "--exclude": true, "-i": true, "--include": true, "-S": true,
		"--severity": true, "--rcfile": true, "-f": true, "--format": true, "-s": true, "--shell": true,
		"-o": true, "--enable": true, "-P": true, "--source-path": true, "-W": true, "--wiki-link-count": true}
	for _, f := range parseFlags(args, takes) {
		switch f.name {
		case "-e", "--exclude":
			for _, c := range splitList(f.value) {
				if strings.EqualFold(c, "all") {
					return Narrowed, "CI flag " + f.name + " all turns off the floor"
				}
				if contains(r.floor, normSC(c)) {
					return Narrowed, "CI flag " + f.name + " turns off floor rule " + normSC(c)
				}
			}
		case "-i", "--include":
			on := splitList(f.value)
			for i := range on {
				on[i] = normSC(on[i])
			}
			for _, c := range r.floor {
				if !contains(on, c) {
					return Narrowed, "CI flag " + f.name + " drops floor rule " + c
				}
			}
		case "-S", "--severity":
			min := severityRank(strings.ToLower(strings.TrimSpace(f.value)))
			for _, c := range r.floor {
				if severityRank(shellcheckCodeSeverity(c)) < min {
					return Narrowed, "CI flag " + f.name + " " + f.value + " drops floor rule " + c
				}
			}
		case "--rcfile":
			if r.cfgNarrows(dir, f.value) {
				return Narrowed, "CI rc file " + f.value + " turns off the floor"
			}
		}
	}
	return Valid, ""
}

func (r resolver) judgeEslint(args []string, dir string) (Verdict, string) {
	takes := map[string]bool{"--config": true, "-c": true, "--ignore-pattern": true, "--max-warnings": true,
		"--format": true, "-f": true, "--rule": true, "--ext": true, "--output-file": true, "-o": true}
	flags := parseFlags(args, takes)
	ownConfig := false
	for _, f := range flags {
		if f.name == "--config" || f.name == "-c" {
			ownConfig = true
		}
	}
	for _, f := range flags {
		switch f.name {
		case "--fix":
			return Unrecognised, "step not recognised: --fix hides fixable findings from the exit status"
		case "--no-config-lookup", "--no-eslintrc":
			if !ownConfig {
				return Narrowed, "CI flag " + f.name + " drops the config"
			}
		case "--config", "-c":
			if r.cfgNarrows(dir, f.value) {
				return Narrowed, "CI config " + f.value + " makes no choice"
			}
		}
	}
	return Valid, ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var (
	versionNumber = regexp.MustCompile(`\d+\.\d+\.\d+`)
	ruffPinned    = regexp.MustCompile(`(^|[^A-Za-z0-9_-])ruff(@|==)(\S+)`)
	golangciPkgV  = regexp.MustCompile(`golangci-lint\S*@(\S+)`)
)

// pinned reports whether the call has an install on a pinned version: an action's own version
// input, or a pin-proving install in the same step or an earlier step of the same job.
func (r resolver) pinned(steps []Step, at int, c found) bool {
	if c.Step.Uses != "" {
		return versionInput(c.Step.With["version"])
	}
	call := steps[at]
	for i := 0; i <= at; i++ {
		s := steps[i]
		if s.IfFalse || s.File != call.File || s.Job != call.Job || s.Index > call.Index {
			continue
		}
		if r.stepPins(s, c) {
			return true
		}
	}
	return false
}

func versionInput(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && v != "latest"
}

func (r resolver) stepPins(s Step, c found) bool {
	if s.Uses != "" {
		name := actionName(s.Uses)
		return (r.linter == "ruff" && name == ruffAction || r.linter == "golangci-lint" && name == golangciAction) &&
			versionInput(s.With["version"])
	}
	sc, err := parseScript(s.Run)
	if err != nil {
		return false
	}
	pins := false
	sc.walk(shellState{dir: workDir(s)}, func(v visit) {
		if pins || isEcho(v.words) {
			return
		}
		// the whole pipeline: an install script is often `curl … | sh -s -- <version>`
		var all []string
		for _, cmd := range sc.lists[v.pos.list].pipes[v.pos.pipe].cmds {
			all = append(all, texts(cmd.words)...)
		}
		pins = r.pinProof(v.words, all, v.state.dir, c.dir)
	})
	return pins
}

func isEcho(words []string) bool {
	n := binName(words[0])
	return n == "echo" || n == "printf"
}

// pinProof reports whether one command installs the linter on a pinned version.
func (r resolver) pinProof(words, pipe []string, dir, callDir string) bool {
	joined := strings.Join(words, " ")
	switch r.linter {
	case "ruff":
		for _, m := range ruffPinned.FindAllStringSubmatch(joined, -1) {
			if m[3] != "latest" {
				return true
			}
		}
	case "golangci-lint":
		if m := golangciPkgV.FindStringSubmatch(joined); m != nil {
			return m[1] != "latest"
		}
		p := strings.Join(pipe, " ")
		return strings.Contains(p, "golangci") && strings.Contains(p, "install.sh") && versionNumber.MatchString(p)
	case "shellcheck":
		if len(words) > 0 && binName(stripWrappersOrSelf(words)[0]) == "shellcheck-repo.sh" {
			return true // the script provisions the pinned binary itself
		}
		p := strings.Join(pipe, " ")
		return strings.Contains(p, "shellcheck") && versionNumber.MatchString(p)
	case "eslint":
		return lockfileInstall(words, dir) == callDir
	}
	return false
}

func stripWrappersOrSelf(words []string) []string {
	if w := stripWrappers(words); len(w) > 0 {
		return w
	}
	return words
}

// lockfileInstall returns the directory of an install from the lockfile (npm ci,
// pnpm install --frozen-lockfile, yarn install --immutable or --frozen-lockfile,
// bun install --frozen-lockfile), or "" when the command is none.
func lockfileInstall(words []string, dir string) string {
	argv := stripWrappers(words)
	if len(argv) == 0 {
		return ""
	}
	tool := binName(argv[0])
	rest := argv[1:]
	d := dir
	var sub string
	var flags []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case (a == "--prefix" || a == "-C" || a == "--dir" || a == "--cwd") && i+1 < len(rest):
			d = joinDir(dir, rest[i+1])
			i++
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
		case sub == "":
			sub = a
		}
	}
	frozen := hasAny(flags, "--frozen-lockfile")
	ok := false
	switch tool {
	case "npm":
		ok = sub == "ci" || sub == "clean-install"
	case "pnpm":
		ok = (sub == "install" || sub == "i") && frozen
	case "yarn":
		ok = (sub == "install" || sub == "") && (frozen || hasAny(flags, "--immutable"))
	case "bun":
		ok = (sub == "install" || sub == "i") && frozen
	}
	if !ok {
		return ""
	}
	return d
}
