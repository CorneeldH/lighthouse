// Package unblock is `lighthouse unblock-dependents`: it releases the issues that waited on a
// just-closed issue, as the bot from `[ci]`.
//
// The release lane (`.github/workflows/lighthouse-unblock-dependents.yml`) runs it on every
// `issues: closed`. For each open issue that the closed issue blocked (`dependencies/blocking`)
// it counts the blockers that are still open. If none are open any more and the issue carries
// the blocking label of the config, that label is removed and the issue gets a comment. The
// closed issue gets one comment back with what was released and what still waits.
//
// Norms, binding for every change to this package:
//
//   - Only `completed` releases. An issue that was closed as `not_planned` or `duplicate` did
//     not do the work; the block stays and a human decides.
//   - Only what carries the label. A dependent without the blocking label is not read, not
//     edited and not mentioned; a second run after a successful release therefore posts
//     nothing new. One that still waits is mentioned again.
//   - Comments first, then the label. First the reply comment on the closed issue, then per
//     dependent the comment and only then the DELETE. If a comment fails, the label stays and
//     a restart catches it; a 404 on the DELETE means "already gone".
//   - Not read is not zero. If reading the blockers of one issue fails, that label stays and
//     the run is red; "could not read" never counts as released.
//
// It replaces unblock_dependents.py: the summary lines, the comments and the exit codes are the
// script's, and tests/pack/fixtures/scripts/unblock-dependents prove it. Output and comments are
// English; the reply on the closed issue starts with the fixed line ReleaseLine, which
// `close-issue` reads.
package unblock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// GhTimeout is the timeout of every `gh` call; the summary line of a timeout names it.
const GhTimeout = 60 * time.Second

const (
	// repoPath is left to `gh`, which fills `{owner}` and `{repo}` from the repository.
	repoPath = "repos/{owner}/{repo}"
	// ReleaseLine opens the reply on the closed issue; close-issue reads it.
	ReleaseLine = "Released lane after this close:"
)

// ghError is a failed `gh` call: a nonzero exit, or a timeout (Code -1, the message in Stderr).
type ghError struct {
	Argv   []string
	Code   int
	Stderr string
}

func (e *ghError) Error() string {
	base := fmt.Sprintf("Command '%s' returned non-zero exit status %d.", pyRepr(e.Argv), e.Code)
	if strings.TrimSpace(e.Stderr) != "" {
		return base + "\n" + strings.TrimSpace(e.Stderr)
	}
	return base
}

// jsonError is the error of a reply that is not JSON, worded as json.loads words it.
type jsonError struct{ Msg string }

func (e *jsonError) Error() string { return e.Msg }

// gh runs `gh args...` and returns its stdout.
type gh func(args ...string) (string, error)

func newGh(ctx context.Context, run procrun.Func) gh {
	return func(args ...string) (string, error) {
		argv := append([]string{"gh"}, args...)
		res, err := run(ctx, "", nil, "gh", args...)
		var timeout *procrun.Timeout
		var missing *procrun.NotFound
		switch {
		case errors.As(err, &timeout):
			return "", &ghError{Argv: argv, Code: -1,
				Stderr: fmt.Sprintf("gh %s: timed out after %.0f s", args[0], GhTimeout.Seconds())}
		case errors.As(err, &missing):
			return "", &ghError{Argv: argv, Code: 127, Stderr: "gh: not found"}
		case err != nil:
			return "", err
		case res.Code != 0:
			return "", &ghError{Argv: argv, Code: res.Code, Stderr: res.Stderr}
		}
		return res.Stdout, nil
	}
}

// errorLine is the cause on one line: for a `gh` error the first line of its stderr, not the
// `Command [...]` header.
func errorLine(err error) string {
	var g *ghError
	if errors.As(err, &g) {
		for _, line := range strings.Split(strings.ReplaceAll(g.Stderr, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				return strings.TrimSpace(line)
			}
		}
	}
	var key *procrun.KeyError
	if errors.As(err, &key) {
		return "missing field " + pyRepr(key.Key)
	}
	return err.Error()
}

// quoteAll is quote(s, safe=”): everything but `A-Za-z0-9_.-~` becomes `%XX`, per UTF-8 byte.
func quoteAll(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '_', c == '.', c == '-', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// pyRepr is repr() of a string or of a list of strings.
func pyRepr(v any) string {
	switch x := v.(type) {
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = pyRepr(s)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		quote := "'"
		if strings.Contains(x, "'") && !strings.Contains(x, `"`) {
			quote = `"`
		}
		var b strings.Builder
		b.WriteString(quote)
		for _, r := range x {
			switch {
			case r == '\\':
				b.WriteString(`\\`)
			case string(r) == quote:
				b.WriteString(`\` + quote)
			case r == '\n':
				b.WriteString(`\n`)
			case r == '\r':
				b.WriteString(`\r`)
			case r == '\t':
				b.WriteString(`\t`)
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, `\x%02x`, r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteString(quote)
		return b.String()
	}
	return fmt.Sprint(v)
}

// pyType is type(v).__name__ of a decoded JSON value.
func pyType(v any) string {
	switch x := v.(type) {
	case nil:
		return "NoneType"
	case string:
		return "str"
	case bool:
		return "bool"
	case json.Number:
		if strings.ContainsAny(string(x), ".eE") {
			return "float"
		}
		return "int"
	case []any:
		return "list"
	}
	return "dict"
}

// pyStr is str(v) of a decoded JSON value, as an f-string prints it.
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case json.Number:
		return string(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// index is v[k]: a missing key is a KeyError, a value that is not an object a TypeError.
func index(v any, k string) (any, error) {
	if m, ok := v.(map[string]any); ok {
		got, ok := m[k]
		if !ok {
			return nil, &procrun.KeyError{Key: k}
		}
		return got, nil
	}
	switch v.(type) {
	case string:
		return nil, &procrun.TypeError{Msg: "string indices must be integers, not 'str'"}
	case []any:
		return nil, &procrun.TypeError{Msg: "list indices must be integers or slices, not str"}
	}
	return nil, &procrun.TypeError{Msg: fmt.Sprintf("'%s' object is not subscriptable", pyType(v))}
}

// is reports whether v is the string s.
func is(v any, s string) bool {
	got, ok := v.(string)
	return ok && got == s
}

// items iterates v as a list; any other value is a TypeError.
func items(v any) ([]any, error) {
	if list, ok := v.([]any); ok {
		return list, nil
	}
	return nil, &procrun.TypeError{Msg: fmt.Sprintf("'%s' object is not iterable", pyType(v))}
}

// decode is json.loads: one JSON value and nothing after it.
func decode(doc string) (any, error) {
	trimmed := strings.TrimLeft(doc, " \t\n\r")
	pos := utf8.RuneCountInString(doc) - utf8.RuneCountInString(trimmed)
	starts := trimmed != "" && (strings.ContainsRune(`{["-0123456789`, rune(trimmed[0])) ||
		strings.HasPrefix(trimmed, "true") || strings.HasPrefix(trimmed, "false") || strings.HasPrefix(trimmed, "null"))
	if !starts {
		return nil, &jsonError{Msg: "Expecting value: " + position(doc, pos)}
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, &jsonError{Msg: "Invalid JSON: " + err.Error()}
	}
	rest := strings.TrimLeft(trimmed[dec.InputOffset():], " \t\n\r")
	if rest != "" {
		at := pos + utf8.RuneCountInString(trimmed) - utf8.RuneCountInString(rest)
		return nil, &jsonError{Msg: "Extra data: " + position(doc, at)}
	}
	return v, nil
}

// position is `line L column C (char N)` of the code point N of doc.
func position(doc string, n int) string {
	before := string([]rune(doc)[:n])
	line := 1 + strings.Count(before, "\n")
	col := n + 1
	if i := strings.LastIndex(before, "\n"); i >= 0 {
		col = n - utf8.RuneCountInString(before[:i+1]) + 1
	}
	return fmt.Sprintf("line %d column %d (char %d)", line, col, n)
}

func api(g gh, path string) (any, error) {
	out, err := g("api", repoPath+"/"+path)
	if err != nil {
		return nil, err
	}
	return decode(out)
}

// getList is a dependency list, or a TypeError if `gh` gives anything other than a list.
func getList(g gh, path string) ([]any, error) {
	v, err := api(g, path+"?per_page=100")
	if err != nil {
		return nil, err
	}
	list, ok := v.([]any)
	if !ok {
		return nil, &procrun.TypeError{Msg: fmt.Sprintf("%s returned no JSON list (%s)", path, pyType(v))}
	}
	return list, nil
}

func comment(g gh, nr, text string) error {
	_, err := g("api", "-X", "POST", repoPath+"/issues/"+nr+"/comments", "-f", "body="+text)
	return err
}

// hasLabel reports whether the issue carries label: `label in {l["name"] for l in issue["labels"]}`.
func hasLabel(issue any, label string) (bool, error) {
	raw, err := index(issue, "labels")
	if err != nil {
		return false, err
	}
	list, err := items(raw)
	if err != nil {
		return false, err
	}
	found := false
	for _, l := range list {
		name, err := index(l, "name")
		if err != nil {
			return false, err
		}
		found = found || is(name, label)
	}
	return found, nil
}

// removeLabel is the DELETE; a 404 (removed by hand in the meantime) is not an error.
func removeLabel(g gh, nr, label string) error {
	_, err := g("api", "-X", "DELETE", repoPath+"/issues/"+nr+"/labels/"+quoteAll(label))
	var failed *ghError
	if errors.As(err, &failed) && strings.Contains(failed.Stderr, "HTTP 404") {
		return nil
	}
	return err
}

// readDependents is (message, open dependents); a message means: nothing to do, and why.
func readDependents(g gh, nr int) (string, []any, error) {
	issue, err := api(g, fmt.Sprintf("issues/%d", nr))
	if err != nil {
		return "", nil, err
	}
	state, err := index(issue, "state")
	if err != nil {
		return "", nil, err
	}
	if !is(state, "closed") {
		return fmt.Sprintf("#%d is open: nothing to release.", nr), nil, nil
	}
	reason, err := index(issue, "state_reason")
	if err != nil {
		return "", nil, err
	}
	if !is(reason, "completed") {
		return fmt.Sprintf("#%d closed as `%s`: blocks stay.", nr, pyStr(reason)), nil, nil
	}
	blocking, err := getList(g, fmt.Sprintf("issues/%d/dependencies/blocking", nr))
	if err != nil {
		return "", nil, err
	}
	var dependents []any
	for _, i := range blocking {
		st, err := index(i, "state")
		if err != nil {
			return "", nil, err
		}
		if is(st, "open") {
			dependents = append(dependents, i)
		}
	}
	if len(dependents) == 0 {
		return fmt.Sprintf("#%d blocked no open issues.", nr), nil, nil
	}
	return "", dependents, nil
}

const (
	free  = "vrij"
	waits = "wacht"
	known = "al"
)

// judge is (kind, text) for one dependent, without writing: free, waits or known.
func judge(g gh, dep any, label string) (kind, text string, err error) {
	has, err := hasLabel(dep, label)
	if err != nil {
		return "", "", err
	}
	if !has {
		return known, fmt.Sprintf("does not carry `%s`, nothing to do", label), nil
	}
	number, err := index(dep, "number")
	if err != nil {
		return "", "", err
	}
	blockers, err := getList(g, fmt.Sprintf("issues/%s/dependencies/blocked_by", pyStr(number)))
	if err != nil {
		return "", "", err
	}
	var open []string
	for _, b := range blockers {
		st, err := index(b, "state")
		if err != nil {
			return "", "", err
		}
		if !is(st, "open") {
			continue
		}
		n, err := index(b, "number")
		if err != nil {
			return "", "", err
		}
		open = append(open, "#"+pyStr(n))
	}
	if len(open) > 0 {
		return waits, "still waits for " + strings.Join(open, ", "), nil
	}
	return free, fmt.Sprintf("released, `%s` comes off", label), nil
}

// judgeAll is phase 1: the exit code, the numbers to release and the lines for the reply
// comment. It appends one summary line per dependent to lines.
func judgeAll(g gh, dependents []any, lines *[]string, label string) (int, []string, []string) {
	code := 0
	var released, reply []string
	for _, dep := range dependents {
		number, err := index(dep, "number")
		if err != nil {
			code = 1
			*lines = append(*lines, "- #?: **Error:** "+errorLine(err))
			continue
		}
		m := pyStr(number)
		kind, text, err := judge(g, dep, label)
		if err != nil {
			code = 1
			*lines = append(*lines, fmt.Sprintf("- #%s: **Error:** %s", m, errorLine(err)))
			continue
		}
		*lines = append(*lines, fmt.Sprintf("- #%s: %s", m, text))
		switch kind {
		case free:
			released = append(released, m)
			reply = append(reply, fmt.Sprintf("- #%s is released: `%s` comes off", m, label))
		case waits:
			reply = append(reply, fmt.Sprintf("- #%s %s", m, text))
		}
	}
	return code, released, reply
}

func release(g gh, nr int, m, label string) error {
	text := fmt.Sprintf("Released: the last open blocker #%d is closed; the release lane removes `%s`.", nr, label)
	if err := comment(g, m, text); err != nil {
		return err
	}
	return removeLabel(g, m, label)
}

// Run releases the dependents of issue nr and returns the exit code and the summary lines.
//
// Three phases: read, the reply comment on nr, only then the dependents. If the reply comment
// fails, nothing has been touched yet and a restart redoes everything. A `gh` that cannot start
// (not installed) is an error line like any other read error, where the script crashed.
func Run(ctx context.Context, run procrun.Func, nr int, label string, dryRun bool) (int, []string) {
	g := newGh(ctx, run)
	lines := []string{fmt.Sprintf("## Release lane for #%d", nr), ""}
	message, dependents, err := readDependents(g, nr)
	if err != nil {
		return 1, append(lines, fmt.Sprintf("**Error:** #%d not read: %s", nr, errorLine(err)))
	}
	if message != "" {
		return 0, append(lines, message)
	}
	code, released, reply := judgeAll(g, dependents, &lines, label)
	if dryRun {
		return code, append(lines, "", "_dry-run: nothing written._")
	}
	if len(reply) == 0 {
		return code, lines
	}
	body := strings.Join(append([]string{ReleaseLine, ""}, reply...), "\n")
	if err := comment(g, fmt.Sprint(nr), body); err != nil {
		lines = append(lines, fmt.Sprintf("**Error:** comment on #%d: %s; nothing touched", nr, errorLine(err)))
		return 1, lines
	}
	for _, m := range released {
		if err := release(g, nr, m, label); err != nil {
			code = 1
			lines = append(lines, fmt.Sprintf("- #%s: **Error:** %s", m, errorLine(err)))
		}
	}
	return code, lines
}

// Write prints the summary lines to out, as the script did.
func Write(out io.Writer, lines []string) error {
	_, err := io.WriteString(out, strings.Join(lines, "\n")+"\n")
	return err
}
