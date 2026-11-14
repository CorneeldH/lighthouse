// Package prbody checks a `gh pr create` body for a valid form. It ports
// pack/core/lib/pr_body_gate.py, which stays frozen for worktree_audit.py; both read the shared
// fixtures in tests/pack/fixtures/pr_body_gate.
//
// A body must have at least one of three forms (mixing is allowed):
//
//   - `Closes #N` with a heading `### Evidence #N` and a table `AC | Command | Result` with one
//     row per acceptance criterion of the issue;
//   - `Refs #N — <reason>` (reason at least MinReason characters); a `### Evidence #N` table
//     under it is optional, is checked on form only and is not counted against the issue;
//   - `No issue: <reason>` (same).
//
// The repo is `forge.repo` of lighthouse.toml; without it only `#N` counts, and the AC count
// reports that it is not measurable.
package prbody

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// The constants of pr_body_gate.py.
const (
	// DeadlineS is the total deadline in seconds of one Main call, across every gh and git call.
	// It stays under the hook timeout (15 s): a command hook that expires lets the tool call
	// through, and that would be fail-open.
	DeadlineS = 12
	// ACTimeoutS bounds the `gh api` call of the AC count, in seconds.
	ACTimeoutS = 5
	// GitTimeoutS bounds one git call of the scope check, in seconds.
	GitTimeoutS = 10
	// MinReason is the minimum length in characters, after stripping, of a Refs or No issue reason.
	MinReason = 10
)

const closeWords = `(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)`

// refsSeps are the separators between `Refs #N` and its reason, on the same line.
var refsSeps = []string{"—", "-", ":"}

var evidenceHeader = [3]string{"AC", "Command", "Result"}

var (
	commentRe       = regexp.MustCompile(`(?s)<!--.*?-->`)
	refsRe          = regexp.MustCompile(`(?i)Refs[ \t]+#(\p{Nd}+)(.*)`)
	noIssueRe       = regexp.MustCompile(`(?im)^[ \t]*No issue[ \t]*:(.*)$`)
	separatorCellRe = regexp.MustCompile(`^:?-+:?$`)
	headingRe       = regexp.MustCompile(`^#{1,6}[ \t]`)
	checkboxRe      = regexp.MustCompile(`^[ \t]*[-*][ \t]+\[[ xX]\]`)
)

const fallbackNotice = "pr-create-gate: AC count not measurable for #%d (%s) — structure checked only"

// Row is one data row of an Evidence block; Command is the raw cell, with backticks.
type Row struct{ AC, Command, Result string }

// Verdict is the outcome of CheckBody.
type Verdict struct{ Blockers, Notices []string }

// pyIsSpace is Python's str.isspace: Go's unicode.IsSpace plus the separators U+001C–U+001F.
func pyIsSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// pyStrip is Python's str.strip().
func pyStrip(s string) string { return strings.TrimFunc(s, pyIsSpace) }

// isWord is a character of Python's `\w` on str: alphanumeric or `_`.
func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

// boundary is `[\w/]`, the class of the lookbehind and lookahead of the Python regexes.
func boundary(r rune) bool { return isWord(r) || r == '/' }

// findBounded is re.finditer for a pattern that starts with `(?<![\w/])` and has `(?![\w/])` right
// after its group 1 (the issue number), which RE2 lacks: the submatch indexes of every match whose
// neighbours are not `[\w/]`. A rejected match does not block a later overlapping one: the search
// goes on just after its start. The greedy digits of group 1 never need backtracking here: a
// shorter run is followed by a digit, which is `\w`.
func findBounded(re *regexp.Regexp, s string) [][]int {
	var out [][]int
	pos := 0
	for pos <= len(s) {
		loc := re.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			break
		}
		for i := range loc {
			if loc[i] >= 0 {
				loc[i] += pos
			}
		}
		start, end := loc[0], loc[1]
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		after, _ := utf8.DecodeRuneInString(s[loc[3]:])
		if (start > 0 && boundary(before)) || (loc[3] < len(s) && boundary(after)) {
			_, size := utf8.DecodeRuneInString(s[start:])
			pos = start + max(size, 1)
			continue
		}
		out = append(out, loc)
		pos = end
	}
	return out
}

// digitValue is the value of a Unicode decimal digit (Nd): every Nd range is a run of 0–9.
func digitValue(r rune) int {
	start := r
	for unicode.Is(unicode.Nd, start-1) {
		start--
	}
	return int(r-start) % 10
}

// pyInt is int() of a run of Unicode decimal digits; false when it does not fit an int.
func pyInt(digits string) (int, bool) {
	n := 0
	for _, r := range digits {
		d := digitValue(r)
		if n > (int(^uint(0)>>1)-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}

// StripComments turns `\r\n` into `\n` (a web UI edit), then drops `<!-- … -->`.
func StripComments(body string) string {
	return commentRe.ReplaceAllString(strings.ReplaceAll(body, "\r\n", "\n"), "")
}

// closesRe is the closing-word regex without its lookbehind and lookahead. The target is #N,
// <repo>#N or the full issue URL; without a repo ("") only #N.
func closesRe(repo string) *regexp.Regexp {
	target := `#(\p{Nd}+)`
	if repo != "" {
		quoted := regexp.QuoteMeta(repo)
		target = `(?:https://github\.com/` + quoted + `/issues/|` + quoted + `#|#)(\p{Nd}+)`
	}
	return regexp.MustCompile(`(?i)` + closeWords + `:?[ \t]+` + target)
}

// ClosesTargets is every issue with a closing word in body, unique, in order of appearance.
// repo is `forge.repo`, "" when it is not set.
func ClosesTargets(body, repo string) []int {
	var seen []int
	s := StripComments(body)
	for _, loc := range findBounded(closesRe(repo), s) {
		nr, ok := pyInt(s[loc[2]:loc[3]])
		if ok && !slices.Contains(seen, nr) {
			seen = append(seen, nr)
		}
	}
	return seen
}

// parseReason is the reason after a separator, or "" without a separator.
func parseReason(rest string) string {
	rest = pyStrip(rest)
	for _, sep := range refsSeps {
		if strings.HasPrefix(rest, sep) {
			return pyStrip(rest[len(sep):])
		}
	}
	return ""
}

type ref struct {
	nr     int
	reason string
}

// allRefs is every `Refs #N` with its reason ("" when the separator or the reason is missing).
func allRefs(body string) []ref {
	var out []ref
	s := StripComments(body)
	for _, loc := range findBounded(refsRe, s) {
		if nr, ok := pyInt(s[loc[2]:loc[3]]); ok {
			out = append(out, ref{nr, parseReason(s[loc[4]:loc[5]])})
		}
	}
	return out
}

// RefsTargets is every issue number after a `Refs #N` outside comments, in order, with no reason
// required: whoever only wants to know whether a body refers to an issue reads it this way.
func RefsTargets(body string) []int {
	var out []int
	for _, r := range allRefs(body) {
		out = append(out, r.nr)
	}
	return out
}

// RefsIssues is only the `Refs #N` with a reason of at least MinReason characters, unique.
func RefsIssues(body string) []int {
	var found []int
	for _, r := range allRefs(body) {
		if utf8.RuneCountInString(r.reason) >= MinReason && !slices.Contains(found, r.nr) {
			found = append(found, r.nr)
		}
	}
	return found
}

// NoIssue reports a `No issue: <reason>` line with a reason of at least MinReason characters.
func NoIssue(body string) bool {
	for _, m := range noIssueRe.FindAllStringSubmatch(StripComments(body), -1) {
		if utf8.RuneCountInString(pyStrip(m[1])) >= MinReason {
			return true
		}
	}
	return false
}

// cells splits a table line on unescaped `|`; the outer pipes are dropped.
func cells(line string) []string {
	s := strings.TrimPrefix(pyStrip(line), "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && (i == 0 || s[i-1] != '\\') {
			out = append(out, pyStrip(s[start:i]))
			start = i + 1
		}
	}
	return append(out, pyStrip(s[start:]))
}

// row is one data row, or the reason why it is invalid.
func row(line string, seq int) (Row, string) {
	c := cells(line)
	if len(c) != len(evidenceHeader) {
		return Row{}, fmt.Sprintf("row %d has %d cells, expected %d (write a `|` in a cell as `\\|`)",
			seq, len(c), len(evidenceHeader))
	}
	if slices.Contains(c, "") {
		return Row{}, fmt.Sprintf("row %d has an empty cell", seq)
	}
	command := c[1]
	if utf8.RuneCountInString(command) < 3 || !strings.HasPrefix(command, "`") || !strings.HasSuffix(command, "`") {
		return Row{}, fmt.Sprintf("the Command cell of row %d must be in backticks", seq)
	}
	return Row{c[0], command, c[2]}, ""
}

func evidenceHeadingRe(nr int) *regexp.Regexp {
	return regexp.MustCompile(`^###[ \t]+Evidence[ \t]+#` + strconv.Itoa(nr) + `[ \t]*$`)
}

// HasEvidenceBlock reports whether body, comments stripped, has a `### Evidence #nr` heading.
func HasEvidenceBlock(body string, nr int) bool {
	heading := evidenceHeadingRe(nr)
	return slices.ContainsFunc(strings.Split(StripComments(body), "\n"), heading.MatchString)
}

// EvidenceRows is the rows of `### Evidence #nr` and "", or no rows and the reason why the block
// is wrong.
func EvidenceRows(body string, nr int) ([]Row, string) {
	lines := strings.Split(StripComments(body), "\n")
	start := slices.IndexFunc(lines, evidenceHeadingRe(nr).MatchString)
	if start < 0 {
		return nil, fmt.Sprintf("heading `### Evidence #%d` is missing", nr)
	}
	i := start + 1
	for i < len(lines) && pyStrip(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !strings.HasPrefix(pyStrip(lines[i]), "|") {
		return nil, fmt.Sprintf("no table under `### Evidence #%d`", nr)
	}
	if !slices.Equal(cells(lines[i]), evidenceHeader[:]) {
		return nil, "the table header must be `| " + strings.Join(evidenceHeader[:], " | ") + " |`"
	}
	i++
	if i >= len(lines) || !allSeparators(cells(lines[i])) {
		return nil, "the separator line `|---|---|---|` under the table header is missing"
	}
	var rows []Row
	for _, line := range lines[i+1:] {
		if !strings.HasPrefix(pyStrip(line), "|") {
			break
		}
		r, reason := row(line, len(rows)+1)
		if reason != "" {
			return nil, reason
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return nil, "the Evidence block has no data row"
	}
	return rows, ""
}

func allSeparators(c []string) bool {
	for _, cell := range c {
		if !separatorCellRe.MatchString(cell) {
			return false
		}
	}
	return true
}

// acHeadingRe is the AC heading in every language of headings.toml: an issue can have another
// language than the repo.
func acHeadingRe() (*regexp.Regexp, error) {
	variants, err := config.Variants("acceptance_criteria")
	if err != nil {
		return nil, err
	}
	quoted := make([]string, len(variants))
	for i, v := range variants {
		quoted[i] = regexp.QuoteMeta(v)
	}
	return regexp.MustCompile(`(?i)^#{2,4}[ \t]+(?:` + strings.Join(quoted, "|") + `)[ \t]*$`), nil
}

// countACs is CountACs with the error of a headings table that cannot be read.
func countACs(issueBody string) (int, bool, error) {
	heading, err := acHeadingRe()
	if err != nil {
		return 0, false, err
	}
	lines := strings.Split(strings.ReplaceAll(issueBody, "\r\n", "\n"), "\n")
	start := slices.IndexFunc(lines, heading.MatchString)
	if start < 0 {
		return 0, false, nil
	}
	count := 0
	for _, line := range lines[start+1:] {
		if headingRe.MatchString(line) {
			break
		}
		if checkboxRe.MatchString(line) {
			count++
		}
	}
	return count, count > 0, nil
}

// CountACs is the number of checkbox lines under the AC heading (`#{2,4}`, any language); false
// for no heading, zero lines, or a headings table that cannot be read.
func CountACs(issueBody string) (int, bool) {
	n, ok, err := countACs(issueBody)
	return n, ok && err == nil
}

// CheckBody checks the forms of body. acCount gives the AC count of an issue, or false and the
// cause why it is not measurable; it is asked only for a Closes target with a valid Evidence block.
func CheckBody(body, repo string, acCount func(nr int) (int, bool, string)) Verdict {
	var v Verdict
	targets := ClosesTargets(body, repo)
	for _, nr := range targets {
		rows, reason := EvidenceRows(body, nr)
		if reason != "" {
			v.Blockers = append(v.Blockers, fmt.Sprintf("Closes #%d: %s", nr, reason))
			continue
		}
		count, ok, cause := acCount(nr)
		switch {
		case !ok:
			v.Notices = append(v.Notices, fmt.Sprintf(fallbackNotice, nr, cause))
		case count != len(rows):
			v.Blockers = append(v.Blockers, fmt.Sprintf(
				"Closes #%d: the issue has %d acceptance criteria, the Evidence block %d rows — one row per AC",
				nr, count, len(rows)))
		}
	}

	refs := RefsIssues(body)
	for _, nr := range refs {
		if slices.Contains(targets, nr) || !HasEvidenceBlock(body, nr) {
			continue
		}
		// Form only: the rows are not counted, so the issue is not fetched.
		if _, reason := EvidenceRows(body, nr); reason != "" {
			v.Blockers = append(v.Blockers, fmt.Sprintf("Refs #%d: %s", nr, reason))
		}
	}

	if len(targets) == 0 && len(refs) == 0 && !NoIssue(body) {
		hint := ""
		if len(allRefs(body)) > 0 {
			hint = fmt.Sprintf(" `Refs #N` lacks a reason of at least %d characters"+
				" (`Refs #N — <reason>`; separator %s).", MinReason, strings.Join(refsSeps, ", "))
		}
		v.Blockers = append(v.Blockers, "no valid form in the PR body: `Closes #N` with `### Evidence #N`,"+
			" `Refs #N — <reason>` or `No issue: <reason>`."+hint)
	}
	return v
}
