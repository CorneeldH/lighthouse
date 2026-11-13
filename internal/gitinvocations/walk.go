package gitinvocations

import (
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

type wrapper struct {
	withValue  map[string]bool
	positional int
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

// wrappers are words that may come before the actual command without being it.
var wrappers = map[string]wrapper{
	"sudo":    {set("-u", "-g", "-p", "-C", "--user", "--group", "--prompt"), 0},
	"doas":    {set("-u", "-C"), 0},
	"command": {set(), 0},
	"nice":    {set("-n", "--adjustment"), 0},
	"time":    {set(), 0},
	"env":     {set("-u", "--unset", "-C", "--chdir"), 0},
	"rtk":     {set(), 0},
	"exec":    {set("-a"), 0},
	"nohup":   {set(), 0},
	"stdbuf":  {set("-i", "-o", "-e"), 0},
	"timeout": {set("-k", "--kill-after", "-s", "--signal"), 1},
	"xargs":   {set("-I", "-n", "-P", "-s", "-d", "-E", "-a", "--replace", "--max-args"), 0},
}

// shellWrappers receive a command as a string: `bash -c <script>`.
var shellWrappers = set("bash", "sh", "zsh", "dash", "ksh")

var skipWords = set("{", "}", "!", "if", "then", "else", "elif", "fi", "while", "until", "do",
	"done", "for", "case", "esac")

// The path helpers below work on `/` only, as the shell command does. On Windows a path is
// turned to `/` first (filepath.ToSlash); filepath.Clean would turn it back to `\`, after which
// pyDirname finds no parent and a fresh directory falls outside every repo.

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

// isAbs is an absolute path: `/…`, or on Windows also a path with a drive (`C:/…`, `C:\…`).
func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || filepath.IsAbs(p)
}

// pyJoin is Python's os.path.join for two parts.
func pyJoin(a, b string) string {
	switch {
	case isAbs(b):
		return b
	case a == "" || strings.HasSuffix(a, "/"):
		return a + b
	}
	return a + "/" + b
}

// pyDirname is Python's os.path.dirname.
func pyDirname(path string) string {
	head := path[:strings.LastIndex(path, "/")+1]
	if head != "" && strings.Trim(head, "/") != "" {
		head = strings.TrimRight(head, "/")
	}
	return head
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isWordChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// expandVars is Python's os.path.expandvars: `$NAME` and `${NAME}` that are set are replaced, an
// unknown or malformed one stays as it is.
func expandVars(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			out.WriteByte(s[i])
			i++
			continue
		}
		end, name := 0, ""
		if i+1 < len(s) && s[i+1] == '{' {
			if k := strings.IndexByte(s[i+2:], '}'); k >= 0 {
				end, name = i+2+k+1, s[i+2:i+2+k]
			}
		} else {
			k := i + 1
			for k < len(s) && isWordChar(s[k]) {
				k++
			}
			if k > i+1 {
				end, name = k, s[i+1:k]
			}
		}
		if end == 0 {
			out.WriteByte('$')
			i++
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			out.WriteString(value)
		} else {
			out.WriteString(s[i:end])
		}
		i = end
	}
	return out.String()
}

// expandUser is Python's os.path.expanduser: `~` is HOME (else the passwd entry), `~name` the
// home directory of that user. A path it cannot expand stays as it is.
func expandUser(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	i := strings.IndexByte(path[1:], '/')
	if i < 0 {
		i = len(path)
	} else {
		i++
	}
	var home string
	if i == 1 {
		if value, ok := os.LookupEnv("HOME"); ok {
			home = value
		} else {
			u, err := user.Current()
			if err != nil {
				return path
			}
			home = u.HomeDir
		}
	} else {
		u, err := user.Lookup(path[1:i])
		if err != nil {
			return path
		}
		home = u.HomeDir
	}
	if joined := strings.TrimRight(home, "/") + path[i:]; joined != "" {
		return joined
	}
	return "/"
}

// resolve resolves a path word. ok is false when it cannot be resolved with certainty. The tilde
// expands only if it was unquoted.
func resolve(w string, literal, tilde bool) (string, bool) {
	text := w
	if !literal {
		text = expandVars(w)
	}
	if tilde {
		text = expandUser(text)
	}
	if strings.ContainsAny(text, "$`*?") {
		return "", false
	}
	return text, true
}

func basename(w string) string {
	return w[strings.LastIndex(w, "/")+1:]
}

func isAssignment(w string) bool {
	name, _, found := strings.Cut(w, "=")
	if !found || name == "" {
		return false
	}
	for k, ch := range name {
		if k == 0 && !unicode.IsLetter(ch) && ch != '_' {
			return false
		}
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && !unicode.IsNumber(ch) && ch != '_' {
			return false
		}
	}
	return true
}

// commandStart skips assignments, wrappers and shell keywords.
func commandStart(words []string) int {
	idx := 0
	moved := true
	for moved && idx < len(words) {
		moved = false
		for idx < len(words) && isAssignment(words[idx]) {
			idx++
			moved = true
		}
		for idx < len(words) && skipWords[words[idx]] {
			idx++
			moved = true
		}
		if idx < len(words) {
			w, ok := wrappers[basename(words[idx])]
			if !ok {
				continue
			}
			idx++
			for idx < len(words) && strings.HasPrefix(words[idx], "-") && words[idx] != "-" {
				if !strings.Contains(words[idx], "=") && w.withValue[words[idx]] {
					idx += 2
				} else {
					idx++
				}
			}
			for range w.positional {
				if idx < len(words) && !strings.HasPrefix(words[idx], "-") {
					idx++
				}
			}
			moved = true
		}
	}
	return idx
}

// ExistingAncestor is the nearest directory that exists now, going up from path.
func ExistingAncestor(path string) (string, bool) {
	current := path
	for {
		if isDir(current) {
			return current, true
		}
		parent := pyDirname(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// RepoOf is the repo root that path lies in: it walks up until a `.git` is found, without
// calling git. A worktree has a file there instead of a directory. A `.git` directory without
// `HEAD` is a stray directory, not a repo, and is walked past; a garbage `HEAD` still counts.
func RepoOf(path string) (string, bool) {
	current := path
	for {
		dotGit := pyJoin(current, ".git")
		if info, err := os.Stat(dotGit); err == nil && info.Mode().IsRegular() {
			return current, true
		}
		if _, err := os.Lstat(pyJoin(dotGit, "HEAD")); err == nil {
			return current, true
		}
		parent := pyDirname(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// stackEntry is a (here, raw) pair; here is nil when the directory is unknown.
type stackEntry struct {
	here *string
	raw  string
}

// dirState is the directory state of the shell: where we are, where we came from, the stack.
type dirState struct {
	here   *string
	raw    string
	old    *string
	oldRaw string
	oldSet bool
	stack  []stackEntry
	newDir bool
	stale  stackEntry
}

func newDirs(cwd string) *dirState {
	cwd = filepath.ToSlash(cwd) // a no-op outside Windows
	return &dirState{here: &cwd, raw: cwd}
}

func (d *dirState) clone() *dirState {
	c := *d
	c.stack = append([]stackEntry(nil), d.stack...)
	return &c
}

func (d *dirState) applyCd(here *string, raw string) {
	d.old, d.oldRaw, d.oldSet = d.here, d.raw, true
	d.here, d.raw = here, raw
	d.newDir = false
}

// stackMove does `popd`, `cd -` and `pushd`; true if the move is done with that. `pushd` with an
// argument only puts (here, raw) on the stack and returns false: cdTo does the move.
func (d *dirState) stackMove(verb string, words []string, args []int) bool {
	if verb == "popd" {
		if n := len(d.stack); n > 0 {
			previous := d.stack[n-1]
			d.stack = d.stack[:n-1]
			d.applyCd(previous.here, previous.raw)
		} else {
			d.applyCd(nil, "popd")
		}
		return true
	}
	if len(args) > 0 && words[args[0]] == "-" {
		if d.oldSet {
			d.applyCd(d.old, d.oldRaw)
		} else {
			// We do not know the OLDPWD from before this command.
			d.applyCd(nil, "cd -")
		}
		return true
	}
	if verb == "pushd" {
		if len(args) == 0 {
			// `pushd` without an argument swaps the top two: cannot be followed.
			d.applyCd(nil, "pushd")
			return true
		}
		d.stack = append(d.stack, stackEntry{d.here, d.raw})
	}
	return false
}

// cdTo moves to w: resolve, join with here, and note a directory that does not exist yet.
func (d *dirState) cdTo(w string, literal, tilde bool) {
	target, ok := resolve(w, literal, tilde)
	if !ok {
		d.applyCd(nil, w)
		return
	}
	switch {
	case isAbs(target):
		target = pyNormpath(target)
	case d.here != nil:
		target = pyNormpath(pyJoin(*d.here, target))
	default:
		d.raw = w
		return
	}
	if !isDir(target) {
		// Either the command creates it itself (`mkdir -p X && cd X`) and bash will soon be in
		// that fresh directory, or the `cd` fails and bash stays where it is. After `&&` we keep
		// calculating with the new path; settleNewDir puts the old directory back otherwise.
		stale := stackEntry{d.here, d.raw}
		d.applyCd(&target, target)
		d.newDir, d.stale = true, stale
		return
	}
	d.applyCd(&target, target)
}

// changeDir follows `cd`, `pushd` and `popd` the way bash executes them.
func (d *dirState) changeDir(verb string, words []string, literal, tildes []bool, idx int) {
	var args []int
	for k := idx + 1; k < len(words); k++ {
		if !strings.HasPrefix(words[k], "-") || len(words[k]) <= 1 {
			args = append(args, k)
		}
	}
	if d.stackMove(verb, words, args) {
		return
	}
	if len(args) == 0 {
		home := expandUser("~")
		d.applyCd(&home, "~")
		return
	}
	k := args[0]
	d.cdTo(words[k], literal[k], tildes[k])
}

// shellScript is the literal `-c` string of a shell.
func shellScript(words []string, literal []bool, idx int) (string, bool) {
	for j := idx + 1; j < len(words); j++ {
		w := words[j]
		if !strings.HasPrefix(w, "-") || w == "-" {
			return "", false
		}
		flags := ""
		if !strings.HasPrefix(w, "--") {
			flags = w[1:]
		}
		if w == "-c" || strings.Contains(flags, "c") {
			if j+1 < len(words) && literal[j+1] {
				return words[j+1], true
			}
			return "", false
		}
	}
	return "", false
}

// isName reports whether w looks like a function name.
func isName(w string) bool {
	if w == "" || unicode.IsDigit([]rune(w)[0]) {
		return false
	}
	for _, ch := range w {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && !unicode.IsNumber(ch) && !strings.ContainsRune("_-.", ch) {
			return false
		}
	}
	return true
}

// bodyEnd is the index just after the `{ ... }` block with which a function body begins.
func bodyEnd(queue []segment) int {
	depth := 0
	for k, item := range queue {
		if item.kind != segCommand {
			continue
		}
		for _, w := range item.words {
			switch w.text {
			case "{":
				depth++
			case "}":
				depth--
			}
		}
		if depth <= 0 {
			return k + 1
		}
	}
	return len(queue)
}

// functionBody checks for a `foo() { ... }` definition and, if so, isolates the body in a
// sub-shell: it runs only at a call, so its `cd` must not move the caller. It returns the new
// queue and whether it was a definition.
func functionBody(words []string, queue []segment) ([]segment, bool) {
	if len(queue) < 3 || queue[0].kind != segSubEnter || queue[2].kind != segSubLeave {
		return queue, false
	}
	if queue[1].kind != segCommand || len(queue[1].words) > 0 {
		return queue, false
	}
	if !isName(words[len(words)-1]) {
		return queue, false
	}
	if len(words) > 2 || (len(words) == 2 && words[0] != "function") {
		return queue, false
	}
	rest := append([]segment(nil), queue[3:]...)
	end := bodyEnd(rest)
	out := make([]segment, 0, len(rest)+2)
	out = append(out, segment{kind: segSubEnter})
	out = append(out, rest[:end]...)
	out = append(out, segment{kind: segSubLeave})
	out = append(out, rest[end:]...)
	return out, true
}

// Step is one segment during Walk, with the directory state that holds at that moment. Idx is the
// word where the actual command begins. Here, Raw and NewDir are copies of the directory state at
// the moment of the step; Here is "" when the directory cannot be traced.
type Step struct {
	Words   []string
	Literal []bool
	Tildes  []bool
	Idx     int
	Here    string
	Raw     string
	NewDir  bool
}

// walkState is the state that Walk carries from segment to segment.
type walkState struct {
	queue    []segment
	dirs     *dirState
	saved    []*dirState
	afterAnd bool
}

// takeMarker handles an `&&` or sub-shell marker; true if it was one.
func (s *walkState) takeMarker(seg segment) bool {
	switch seg.kind {
	case segAndSep:
		s.afterAnd = true
	case segSubEnter:
		s.saved = append(s.saved, s.dirs.clone())
	case segSubLeave:
		if n := len(s.saved); n > 0 {
			s.dirs = s.saved[n-1]
			s.saved = s.saved[:n-1]
		}
	default:
		return false
	}
	return true
}

// settleNewDir falls back to the old directory if no `&&` preceded a fresh directory.
func (s *walkState) settleNewDir() {
	if s.dirs.newDir && !s.afterAnd {
		// No `&&` before it: this also runs if that `cd` failed, and then bash is still in the
		// old directory.
		s.dirs.here, s.dirs.raw = s.dirs.stale.here, s.dirs.stale.raw
		s.dirs.newDir = false
	}
	s.afterAnd = false
}

// applyEffect applies the effect of a `cd`/`pushd`/`popd` or a shell (`bash -c`).
func (s *walkState) applyEffect(words []string, literal, tildes []bool, idx int) error {
	verb := basename(words[idx])
	switch {
	case verb == "cd" || verb == "pushd" || verb == "popd":
		s.dirs.changeDir(verb, words, literal, tildes, idx)
	case shellWrappers[verb]:
		if script, ok := shellScript(words, literal, idx); ok {
			// A shell of its own with a cwd of its own: brackets around it.
			inner, err := splitSegments(stripHeredocs(script))
			if err != nil {
				return err
			}
			queue := []segment{{kind: segSubEnter}}
			queue = append(queue, inner...)
			queue = append(queue, segment{kind: segSubLeave})
			s.queue = append(queue, s.queue...)
		}
	}
	return nil
}

// Walk walks every command segment of command, executed from cwd. It returns one Step per
// non-empty segment that is not a function definition, also for `cd`/`pushd`/`popd` and a shell
// (`bash -c`), with the directory state as it was when the segment ran. On an open quote it
// returns ErrUnparseable together with the steps found before it.
func Walk(command, cwd string) ([]Step, error) {
	queue, err := splitSegments(stripHeredocs(command))
	if err != nil {
		return nil, err
	}
	state := &walkState{queue: queue, dirs: newDirs(cwd)}
	var steps []Step
	for len(state.queue) > 0 {
		seg := state.queue[0]
		state.queue = state.queue[1:]
		if state.takeMarker(seg) {
			continue
		}
		var words []string
		var literal, tildes []bool
		for _, w := range seg.words {
			words = append(words, w.text)
			literal = append(literal, w.literal)
			tildes = append(tildes, w.tilde)
		}
		if len(words) == 0 {
			continue
		}
		state.settleNewDir()
		var isFunc bool
		if state.queue, isFunc = functionBody(words, state.queue); isFunc {
			continue
		}
		idx := commandStart(words)
		if idx >= len(words) {
			continue
		}
		here := ""
		if state.dirs.here != nil {
			here = *state.dirs.here
		}
		steps = append(steps, Step{words, literal, tildes, idx, here, state.dirs.raw, state.dirs.newDir})
		if err := state.applyEffect(words, literal, tildes, idx); err != nil {
			return steps, err
		}
	}
	return steps, nil
}
