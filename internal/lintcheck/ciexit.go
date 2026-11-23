package lintcheck

import (
	"errors"
	"strings"
)

// The exit status of a CI step is an allowlist (spec §3, "Red in CI"): a recognised call counts
// only when its exit status is provably the status of the step. Everything this file cannot place
// is Unrecognised, never Valid.

// effectiveShell returns the shell a run step runs in, whether that shell starts with errexit, and
// whether the check could read it at all. The shell is `shell:` on the step, else
// defaults.run.shell on the job, else on the workflow (Steps resolves that), else the default of
// the runner: GitHub runs `bash -e {0}` on a Linux or macOS runner. A runner the check cannot read
// (an expression, a list of labels, a self-hosted label) or a Windows runner without a shell is
// not ok. GitLab's default shell stops at the first failing script line.
func effectiveShell(s Step) (name string, errexit bool, ok bool) {
	if s.GitLab {
		return "gitlab", true, true
	}
	sh := strings.TrimSpace(s.Shell)
	if sh == "" {
		ro := strings.TrimSpace(s.RunsOn)
		if strings.HasPrefix(ro, "ubuntu-") || strings.HasPrefix(ro, "macos-") {
			return "bash -e {0}", true, true
		}
		return "", false, false
	}
	if strings.Contains(sh, "${{") {
		return "", false, false
	}
	switch sh {
	case "bash", "sh": // GitHub runs `bash --noprofile --norc -eo pipefail {0}` and `sh -e {0}`
		return sh, true, true
	}
	return sh, false, true
}

// startsWithPipefail: only an explicit `shell: bash` sets pipefail; the runner default
// `bash -e {0}`, `sh` and GitLab's shell do not.
func startsWithPipefail(shell string) bool { return shell == "bash" }

// stepGate returns why a step cannot make CI red whatever it runs, or "" when it can.
func stepGate(s Step) string {
	if s.Unplaced != "" {
		return s.Unplaced
	}
	if !literalFalseOrAbsent(s.ContinueOnError) {
		return "step has continue-on-error"
	}
	if !literalFalseOrAbsent(s.JobContinueOnError) {
		return "job has continue-on-error"
	}
	if s.AllowFailure {
		return "job has allow_failure"
	}
	if s.Manual {
		return "job does not run on every change (when, rules, only or except)"
	}
	return ""
}

func literalFalseOrAbsent(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "false"
}

// runGate returns why the shell of a run step cannot make CI red, or "".
func runGate(s Step) string {
	name, errexit, ok := effectiveShell(s)
	if !ok {
		return "step not recognised: shell or runner not readable"
	}
	if !errexit {
		return "step not recognised: shell " + name + " does not stop on a failing command"
	}
	return ""
}

// A word of a shell command after quote removal. startQuoted marks a word whose first character
// was a quote, so `"A=b"` is no assignment.
type word struct {
	text        string
	startQuoted bool
}

type command struct{ words []word }

type pipeline struct{ cmds []command }

// andOr is one list of pipelines joined by && and ||, ended by sep: ";", "&" or a newline (also
// the end). sameLine marks a ";" followed by another command on the same line.
type andOr struct {
	pipes    []pipeline
	ops      []string
	sep      string
	sameLine bool
}

// script is a parsed run script. complex marks a construct the check does not place (a subshell,
// a function, a case); every call in such a script is Unrecognised. trap marks a `trap` anywhere
// in the script: its handler can replace the exit status, so no call in it is placeable either.
type script struct {
	lists   []andOr
	complex bool
	trap    bool
}

var errUnplaceable = errors.New("script not placeable")

// parseScript is a small tokenizer, no shell parser: it joins `\` continuations, drops comments and
// redirections, keeps quoted text and $(…) inside one word, and splits on &&, ||, |, ;, & and
// newlines. A heredoc or an unbalanced quote is errUnplaceable.
func parseScript(src string) (script, error) {
	lx := lexer{src: strings.ReplaceAll(src, "\\\r\n", ""), atStart: true}
	lx.src = strings.ReplaceAll(lx.src, "\\\n", "")
	toks, err := lx.run()
	if err != nil {
		return script{}, err
	}
	var sc script
	var cur andOr
	var pipe pipeline
	var cmd command
	endCmd := func() {
		if len(cmd.words) > 0 {
			pipe.cmds = append(pipe.cmds, cmd)
		}
		cmd = command{}
	}
	endPipe := func() {
		endCmd()
		if len(pipe.cmds) > 0 {
			cur.pipes = append(cur.pipes, pipe)
		}
		pipe = pipeline{}
	}
	endList := func(sep string) {
		endPipe()
		if len(cur.pipes) > 0 {
			cur.sep = sep
			sc.lists = append(sc.lists, cur)
		}
		cur = andOr{}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.op {
		case "":
			cmd.words = append(cmd.words, t.w)
		case "|":
			endCmd()
			i = skipNewlines(toks, i)
		case "&&", "||":
			endPipe()
			cur.ops = append(cur.ops, t.op)
			i = skipNewlines(toks, i)
		case ";", "&":
			endList(t.op)
			if j := i + 1; j < len(toks) && toks[j].op != "\n" && len(sc.lists) > 0 {
				sc.lists[len(sc.lists)-1].sameLine = true
			}
		case "\n":
			endList("\n")
		default: // ( ) ;;
			sc.complex = true
			endCmd()
		}
	}
	endList("\n")
	for _, l := range sc.lists {
		if len(l.ops) != len(l.pipes)-1 {
			sc.complex = true // an operator without a command on one side
		}
		for _, p := range l.pipes {
			for _, c := range p.cmds {
				sc.trap = sc.trap || isTrap(texts(c.words))
			}
		}
	}
	return sc, nil
}

// isTrap reports a `trap` command, also after compound keywords or `builtin` / `command`.
func isTrap(words []string) bool {
	for _, w := range words {
		switch {
		case compoundOpen[w], compoundClose[w], compoundInner[w], w == "builtin", w == "command":
			continue
		}
		return w == "trap"
	}
	return false
}

func skipNewlines(toks []token, i int) int {
	for i+1 < len(toks) && toks[i+1].op == "\n" {
		i++
	}
	return i
}

type token struct {
	op string // "" for a word
	w  word
}

type lexer struct {
	src     string
	i       int
	toks    []token
	buf     strings.Builder
	inWord  bool
	quoted  bool
	atStart bool
}

func (lx *lexer) endWord() {
	if lx.inWord {
		lx.toks = append(lx.toks, token{w: word{text: lx.buf.String(), startQuoted: lx.quoted}})
	}
	lx.buf.Reset()
	lx.inWord, lx.quoted = false, false
}

func (lx *lexer) op(o string) {
	lx.endWord()
	lx.toks = append(lx.toks, token{op: o})
}

func (lx *lexer) peek(n int) byte {
	if lx.i+n < len(lx.src) {
		return lx.src[lx.i+n]
	}
	return 0
}

func (lx *lexer) run() ([]token, error) {
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			lx.endWord()
			lx.i++
		case c == '\n':
			lx.op("\n")
			lx.i++
		case c == '#' && !lx.inWord:
			for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
				lx.i++
			}
		case c == '\'':
			end := strings.IndexByte(lx.src[lx.i+1:], '\'')
			if end < 0 {
				return nil, errUnplaceable
			}
			lx.startWord(true)
			lx.buf.WriteString(lx.src[lx.i+1 : lx.i+1+end])
			lx.i += end + 2
		case c == '"':
			lx.startWord(true)
			lx.i++
			if err := lx.doubleQuoted(); err != nil {
				return nil, err
			}
		case c == '\\':
			lx.startWord(false)
			if lx.i+1 < len(lx.src) {
				lx.buf.WriteByte(lx.src[lx.i+1])
			}
			lx.i += 2
		case c == '$' && (lx.peek(1) == '(' || lx.peek(1) == '{'):
			lx.startWord(false)
			if err := lx.substitution(); err != nil {
				return nil, err
			}
		case c == '`':
			end := strings.IndexByte(lx.src[lx.i+1:], '`')
			if end < 0 {
				return nil, errUnplaceable
			}
			lx.startWord(false)
			lx.buf.WriteString(lx.src[lx.i : lx.i+2+end])
			lx.i += end + 2
		case c == '&' && lx.peek(1) == '&':
			lx.op("&&")
			lx.i += 2
		case c == '&' && lx.peek(1) == '>':
			lx.endWord()
			lx.i++
			if err := lx.redirect(); err != nil {
				return nil, err
			}
		case c == '&':
			lx.op("&")
			lx.i++
		case c == '|' && lx.peek(1) == '|':
			lx.op("||")
			lx.i += 2
		case c == '|':
			lx.op("|")
			lx.i++
			if lx.peek(0) == '&' { // |& pipes stderr too
				lx.i++
			}
		case c == ';' && lx.peek(1) == ';':
			lx.op(";;")
			lx.i += 2
		case c == ';':
			lx.op(";")
			lx.i++
		case c == '(' || c == ')':
			lx.op(string(c))
			lx.i++
		case c == '>' || c == '<':
			if lx.inWord && isDigits(lx.buf.String()) && !lx.quoted { // 2>, 1>&2
				lx.buf.Reset()
				lx.inWord = false
			}
			lx.endWord()
			if err := lx.redirect(); err != nil {
				return nil, err
			}
		default:
			lx.startWord(false)
			lx.buf.WriteByte(c)
			lx.i++
		}
	}
	lx.endWord()
	return lx.toks, nil
}

func (lx *lexer) startWord(quoted bool) {
	if !lx.inWord {
		lx.inWord, lx.quoted = true, quoted
	}
}

// doubleQuoted reads up to the closing quote; $(…) inside keeps its own quotes.
func (lx *lexer) doubleQuoted() error {
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == '"':
			lx.i++
			return nil
		case c == '\\' && lx.i+1 < len(lx.src):
			lx.buf.WriteByte(lx.src[lx.i+1])
			lx.i += 2
		case c == '$' && (lx.peek(1) == '(' || lx.peek(1) == '{'):
			if err := lx.substitution(); err != nil {
				return err
			}
		default:
			lx.buf.WriteByte(c)
			lx.i++
		}
	}
	return errUnplaceable
}

// substitution copies $(…) or ${…} into the word, with nesting and quotes inside.
func (lx *lexer) substitution() error {
	open, closing := lx.src[lx.i+1], byte(')')
	if open == '{' {
		closing = '}'
	}
	start := lx.i
	depth := 0
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch c {
		case open:
			depth++
		case closing:
			depth--
			if depth == 0 {
				lx.i++
				lx.buf.WriteString(lx.src[start:lx.i])
				return nil
			}
		case '\'', '"':
			end := strings.IndexByte(lx.src[lx.i+1:], c)
			if end < 0 {
				return errUnplaceable
			}
			lx.i += end + 1
		}
		lx.i++
	}
	return errUnplaceable
}

// redirect drops a redirection and its target. A heredoc is not placeable.
func (lx *lexer) redirect() error {
	if strings.HasPrefix(lx.src[lx.i:], "<<") && !strings.HasPrefix(lx.src[lx.i:], "<<<") {
		return errUnplaceable
	}
	for lx.i < len(lx.src) && strings.IndexByte("<>&|", lx.src[lx.i]) >= 0 {
		lx.i++
	}
	for lx.i < len(lx.src) && (lx.src[lx.i] == ' ' || lx.src[lx.i] == '\t') {
		lx.i++
	}
	// The target is the next word; lex it into a throwaway token.
	mark := len(lx.toks)
	for lx.i < len(lx.src) && strings.IndexByte(" \t\n;&|()<>", lx.src[lx.i]) < 0 {
		c := lx.src[lx.i]
		switch {
		case c == '\'' || c == '"':
			end := strings.IndexByte(lx.src[lx.i+1:], c)
			if end < 0 {
				return errUnplaceable
			}
			lx.i += end + 2
		case c == '$' && (lx.peek(1) == '(' || lx.peek(1) == '{'):
			if err := lx.substitution(); err != nil {
				return err
			}
		default:
			lx.i++
		}
	}
	lx.buf.Reset()
	lx.inWord, lx.quoted = false, false
	lx.toks = lx.toks[:mark]
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shellState is what a walk over a script knows at a command: errexit, pipefail, the directory
// after `cd`, and the depth of compound commands.
type shellState struct {
	errexit, pipefail bool
	dir               string
	depth             int
}

// applySet updates errexit and pipefail for `set -e`, `set +e`, `set -o pipefail`,
// `set -euo pipefail` and the like. With allowOn false it only turns them off.
func (st *shellState) applySet(args []string, allowOn bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || (a[0] != '-' && a[0] != '+') {
			continue
		}
		on := a[0] == '-'
		if on && !allowOn {
			continue
		}
		for _, f := range a[1:] {
			switch f {
			case 'e':
				st.errexit = on
			case 'o':
				if i+1 < len(args) {
					i++
					switch args[i] {
					case "errexit":
						st.errexit = on
					case "pipefail":
						st.pipefail = on
					}
				}
			}
		}
	}
}

// position is where a command sits in a script.
type position struct {
	list, pipe, cmd int
	nested          bool // inside an if, a loop, a brace group, or after `!`
}

// positionGate returns why the exit status of the command at p does not reach the end of the
// script, or "". inStep is true for a run step (errexit required) and false for a package.json
// script, which npm runs with `sh -c`: there only the last command counts.
func (sc script) positionGate(p position, st shellState, inStep bool) string {
	if sc.complex {
		return "step not recognised: shell construct the check does not place"
	}
	if sc.trap {
		return "step not recognised: trap can change the exit status"
	}
	if p.nested {
		return "step not recognised: call inside a compound command"
	}
	l := sc.lists[p.list]
	if l.sep == "&" {
		return "step not recognised: call runs in the background"
	}
	for _, op := range l.ops {
		if op == "||" {
			return "step not recognised: || masks the exit status"
		}
	}
	if p.cmd < len(l.pipes[p.pipe].cmds)-1 && !st.pipefail {
		return "step not recognised: pipe without pipefail masks the exit status"
	}
	last := p.list == len(sc.lists)-1
	if p.pipe < len(l.pipes)-1 && !last {
		return "step not recognised: && chain followed by more commands masks the exit status"
	}
	if !inStep {
		if !last {
			return "step not recognised: script command that is not the last one"
		}
		return ""
	}
	if !st.errexit {
		return "step not recognised: set +e before the call"
	}
	if l.sep == ";" && l.sameLine {
		return "step not recognised: ; after the call"
	}
	return ""
}

// compoundOpen and compoundClose change the depth; compoundInner starts a command inside one.
var (
	compoundOpen  = map[string]bool{"if": true, "while": true, "until": true, "for": true, "case": true, "select": true, "{": true}
	compoundClose = map[string]bool{"fi": true, "done": true, "esac": true, "}": true}
	compoundInner = map[string]bool{"then": true, "do": true, "else": true, "elif": true, "!": true}
)

// visit is one command of a walk: its words after keywords, its position and the state before it.
type visit struct {
	words []string
	pos   position
	state shellState
}

// walk calls f for every command of the script in order, tracking `set`, `cd` and compound depth.
func (sc script) walk(start shellState, f func(v visit)) {
	st := start
	for li, l := range sc.lists {
		for pi, p := range l.pipes {
			for ci, c := range p.cmds {
				words, nested := st.stripKeywords(texts(c.words))
				if len(words) == 0 {
					continue
				}
				f(visit{words: words, pos: position{li, pi, ci, nested}, state: st})
				plain := !nested && len(p.cmds) == 1
				switch {
				case words[0] == "set":
					// a set that may run turns errexit or pipefail off, wherever it sits; only
					// a plain one turns them on
					st.applySet(words[1:], plain && len(l.pipes) == 1)
				case words[0] == "cd" && len(words) == 2 && plain:
					// also inside an && chain: the rest of the chain runs there
					st.dir = joinDir(st.dir, words[1])
				}
			}
		}
	}
}

// stripKeywords drops the compound keywords that start a command and updates the depth; nested
// is true when the command sits inside a compound command or after one of those keywords.
func (st *shellState) stripKeywords(words []string) (rest []string, nested bool) {
	nested = st.depth > 0
	for len(words) > 0 {
		w := words[0]
		switch {
		case compoundOpen[w]:
			st.depth++
		case compoundClose[w]:
			if st.depth > 0 {
				st.depth--
			}
		case compoundInner[w]:
		default:
			return words, nested
		}
		nested = true
		words = words[1:]
	}
	return nil, nested
}

func texts(ws []word) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.text
	}
	return out
}
