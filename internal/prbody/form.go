// Package prbody checks a `gh pr create` body for a valid form. It is the port of
// pr_body_gate.py; its fixtures are tests/pack/fixtures/pr_body_gate.
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

// closeWords are GitHub's closing words; gitlabCloseWords those of GitLab's default
// closing pattern, which adds the -ing forms and implement.
const (
	closeWords       = `(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)`
	gitlabCloseWords = `(?:close[sd]?|closing|fix(?:e[sd]|ing)?|resolve[sd]?|resolving|implement(?:s|ed|ing)?)`
)

// gitlabDefaultHost is the host of a GitLab repo without forge.host.
const gitlabDefaultHost = "gitlab.com"

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

// Repo is the forge of `forge.repo` in lighthouse.toml: Kind ("" is github), Slug (forge.repo,
// "" when not set), Host (forge.host, "" for the default host), TokenEnv (forge.token_env) and
// PRGate (pr_gate.issue: "" is "optional").
type Repo struct{ Kind, Slug, Host, TokenEnv, PRGate string }

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
// <repo>#N or the full issue URL; without a repo (Slug "") only #N. On GitLab a target may follow
// `issue`/`issues`, and more targets may follow the first: next is then the regex of one more
// target of that list, anchored at the end of the previous one; nil on GitHub.
func closesRe(repo Repo) (first, next *regexp.Regexp) {
	target := `#(\p{Nd}+)`
	gitlab := repo.Kind == "gitlab"
	if repo.Slug != "" {
		quoted := regexp.QuoteMeta(repo.Slug)
		url := `https://github\.com/` + quoted + `/issues/`
		if gitlab {
			host := repo.Host
			if host == "" {
				host = gitlabDefaultHost
			}
			url = `https://` + regexp.QuoteMeta(host) + `/` + quoted + `/-/issues/`
		}
		target = `(?:` + url + `|` + quoted + `#|#)(\p{Nd}+)`
	}
	if !gitlab {
		return regexp.MustCompile(`(?i)` + closeWords + `:?[ \t]+` + target), nil
	}
	target = `(?:issues?[ \t]+)?` + target
	return regexp.MustCompile(`(?i)` + gitlabCloseWords + `:?[ \t]+` + target),
		regexp.MustCompile(`(?i)^(?:[ \t]*,?[ \t]+and[ \t]+|[ \t]*,?[ \t]*)` + target)
}

// ClosesTargets is every issue with a closing word in body, unique, in order of appearance. repo
// is the forge of `forge.repo`: its kind picks the closing words and the targets.
func ClosesTargets(body string, repo Repo) []int {
	var seen []int
	add := func(digits string) {
		if nr, ok := pyInt(digits); ok && !slices.Contains(seen, nr) {
			seen = append(seen, nr)
		}
	}
	s := StripComments(body)
	first, next := closesRe(repo)
	for _, loc := range findBounded(first, s) {
		add(s[loc[2]:loc[3]])
		// GitLab closes the whole list after one closing word: `Closes #1, #2 and #3`.
		for pos := loc[1]; next != nil; {
			m := next.FindStringSubmatchIndex(s[pos:])
			if m == nil {
				break
			}
			if after, _ := utf8.DecodeRuneInString(s[pos+m[3]:]); pos+m[3] < len(s) && boundary(after) {
				break
			}
			add(s[pos+m[2] : pos+m[3]])
			pos += m[1]
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
func CheckBody(body string, repo Repo, acCount func(nr int) (int, bool, string)) Verdict {
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

	gate := repo.PRGate
	strict := gate == "required" || gate == "evidence"
	refs := RefsIssues(body)
	for _, nr := range refs {
		if slices.Contains(targets, nr) {
			continue
		}
		if !HasEvidenceBlock(body, nr) {
			if gate == "evidence" {
				v.Blockers = append(v.Blockers, fmt.Sprintf(
					"pr_gate.issue = evidence: Refs #%d has no `### Evidence #%d` table", nr, nr))
			}
			continue
		}
		// Form only: the rows are not counted, so the issue is not fetched.
		if _, reason := EvidenceRows(body, nr); reason != "" {
			v.Blockers = append(v.Blockers, fmt.Sprintf("Refs #%d: %s", nr, reason))
		}
	}

	if len(targets) == 0 && len(refs) == 0 {
		switch {
		case NoIssue(body) && strict:
			v.Blockers = append(v.Blockers, fmt.Sprintf(
				"pr_gate.issue = %s: the body has no issue — `No issue: <reason>` is not allowed; name an issue %s",
				gate, issueForms(gate)))
		case !NoIssue(body):
			hint := ""
			if len(allRefs(body)) > 0 {
				hint = fmt.Sprintf(" `Refs #N` lacks a reason of at least %d characters"+
					" (`Refs #N — <reason>`; separator %s).", MinReason, strings.Join(refsSeps, ", "))
			}
			prefix := ""
			if strict {
				prefix = "pr_gate.issue = " + gate + ": "
			}
			v.Blockers = append(v.Blockers, prefix+"no valid form in the PR body: "+formList(gate)+"."+hint)
		}
	}
	return v
}

// issueForms is the allowed forms that name an issue under gate, for the No-issue blocker.
func issueForms(gate string) string {
	if gate == "evidence" {
		return "with `Closes #N` + `### Evidence #N` or `Refs #N` plus its `### Evidence #N` table"
	}
	return "with `Closes #N` + `### Evidence #N` or `Refs #N — <reason>`"
}

// formList is the sentence of the forms the body may take under gate.
func formList(gate string) string {
	switch gate {
	case "required":
		return "`Closes #N` with `### Evidence #N` or `Refs #N — <reason>`"
	case "evidence":
		return "`Closes #N` with `### Evidence #N` or `Refs #N` with its `### Evidence #N` table"
	}
	return "`Closes #N` with `### Evidence #N`, `Refs #N — <reason>` or `No issue: <reason>`"
}
