package archscan

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	lhconfig "github.com/CorneeldH/lighthouse/internal/config"
)

// headings is the table the fixed result headings come from. It is loaded from the headings
// file of the target repository, never from the process cwd.
type headings = *lhconfig.HeadingsTable

// Claim marks.
const (
	markMeasured     = "nagemeten"      // lighthouse: allow-language
	markNotMeasured  = "niet-nagemeten" // lighthouse: allow-language
	markUnmeasurable = "niet-meetbaar"  // lighthouse: allow-language
)

// The literal marks a claim ends on, as the result format fixes them.
const (
	litNotMeasured   = "[niet nagemeten]"           // lighthouse: allow-language
	litUnmeasurable  = "[niet meetbaar vanaf hier]" // lighthouse: allow-language
	litMeasuredOpen  = "[nagemeten: `"              // lighthouse: allow-language
	litMeasuredArrow = "` → "
	checkboxLabel    = "Afgewezen" // lighthouse: allow-language
	checkboxOpen     = "- [ ] " + checkboxLabel
	checkboxChecked  = "- [x] " + checkboxLabel
	candidateLevel   = "### "
	sectionLevel     = "## "
)

// Claim is one bullet of a candidate with its mark. Mark is one of the mark constants above;
// Command and Outcome are set for the measured mark only.
type Claim struct{ Text, Mark, Command, Outcome string }

// Candidate is one `###` block. Raw is the whole block, heading to the line before the next one.
type Candidate struct {
	Key, Name string
	Rejected  bool
	Claims    []Claim
	Raw       string
}

// Result is a parsed result file; Body is the file as written.
type Result struct {
	Candidates []Candidate
	StillOpen  []int
	Body       string
}

var (
	// candidateRE reads `### <name> (`<key>`)`.
	candidateRE = regexp.MustCompile("^### (.+?) \\(`([^`]+)`\\)\\s*$")
	stillOpenRE = regexp.MustCompile(`^[-*] #(\d+)\b`)
)

// parseMark reads the single mark at the end of a claim bullet. It parses from the end so that
// brackets inside the command do not matter.
func parseMark(line string) (Claim, error) {
	line = strings.TrimRight(line, " \t\r")
	c := Claim{Text: line}
	rest := ""
	switch {
	case strings.HasSuffix(line, litNotMeasured):
		c.Mark, rest = markNotMeasured, strings.TrimSuffix(line, litNotMeasured)
	case strings.HasSuffix(line, litUnmeasurable):
		c.Mark, rest = markUnmeasurable, strings.TrimSuffix(line, litUnmeasurable)
	case strings.HasSuffix(line, "]") && strings.Contains(line, litMeasuredOpen):
		i := strings.LastIndex(line, litMeasuredOpen)
		body := strings.TrimSuffix(line[i+len(litMeasuredOpen):], "]")
		j := strings.Index(body, litMeasuredArrow)
		if j < 0 {
			return c, errors.New("measured claim without outcome")
		}
		c.Command = body[:j]
		c.Outcome = strings.TrimSpace(body[j+len(litMeasuredArrow):])
		if strings.TrimSpace(c.Command) == "" || c.Outcome == "" {
			return c, errors.New("measured claim without command or outcome")
		}
		c.Mark, rest = markMeasured, line[:i]
	default:
		return c, errors.New("claim without a mark")
	}
	if endsWithMark(rest) {
		return c, errors.New("claim with more than one mark")
	}
	return c, nil
}

// endsWithMark reports whether s, ignoring trailing blanks, ends on a complete mark.
func endsWithMark(s string) bool {
	s = strings.TrimRight(s, " \t\r")
	if strings.HasSuffix(s, litNotMeasured) || strings.HasSuffix(s, litUnmeasurable) {
		return true
	}
	i := strings.LastIndex(s, litMeasuredOpen)
	return strings.HasSuffix(s, "]") && i >= 0 && strings.Contains(s[i:], litMeasuredArrow)
}

// sectionOf names the required section a `## ` line opens, or "".
func sectionOf(line string, names map[string][]string) string {
	if !strings.HasPrefix(line, sectionLevel) {
		return ""
	}
	text := strings.TrimSpace(strings.TrimPrefix(line, sectionLevel))
	for key, variants := range names {
		for _, v := range variants {
			if strings.EqualFold(text, v) {
				return key
			}
		}
	}
	return ""
}

// parseResult reads a result file line by line. A claim is every top-level bullet of a candidate
// other than the checkbox: the result format has no claims heading, so the field lines of a
// candidate must not be bullets.
func parseResult(b []byte, h headings) (Result, error) {
	body := string(b)
	if strings.TrimSpace(body) == "" {
		return Result{}, errors.New("result file is empty")
	}
	keys := []string{"scan_candidates", "scan_still_open", "scan_method", "scan_top"}
	names := map[string][]string{}
	for _, k := range keys {
		v, err := h.Variants(k)
		if err != nil {
			return Result{}, err
		}
		names[k] = v
	}
	res := Result{Body: body}
	seenSection := map[string]bool{}
	hasContent := map[string]bool{}
	seenKey := map[string]bool{}
	var errs []error
	section := ""
	var cur *Candidate
	var raw []string
	// boxes counts the checkbox lines of cur, from its heading on line curLine: exactly one, or
	// the candidate could never be rejected (none) or be read two ways (more).
	boxes, curLine := 0, 0
	flush := func() {
		if cur != nil {
			if boxes != 1 {
				errs = append(errs, fmt.Errorf("line %d: candidate %s has %d checkbox lines %q, want exactly one", curLine, cur.Key, boxes, checkboxOpen))
			}
			cur.Raw = strings.TrimRight(strings.Join(raw, "\n"), "\n")
			res.Candidates = append(res.Candidates, *cur)
			cur, raw = nil, nil
		}
	}
	for n, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if s := sectionOf(line, names); s != "" {
			flush()
			section = s
			seenSection[s] = true
			continue
		}
		if strings.HasPrefix(line, sectionLevel) || strings.HasPrefix(line, "# ") {
			flush()
			section = ""
			continue
		}
		if strings.TrimSpace(line) != "" {
			hasContent[section] = true
		}
		switch section {
		case "scan_candidates":
			if strings.HasPrefix(line, candidateLevel) {
				flush()
				m := candidateRE.FindStringSubmatch(line)
				if m == nil {
					errs = append(errs, fmt.Errorf("line %d: candidate heading needs a name and a key", n+1))
					continue
				}
				if seenKey[m[2]] {
					errs = append(errs, fmt.Errorf("line %d: duplicate key %s", n+1, m[2]))
				}
				seenKey[m[2]] = true
				cur = &Candidate{Key: m[2], Name: m[1]}
				raw = []string{line}
				boxes, curLine = 0, n+1
				continue
			}
			if cur == nil {
				continue
			}
			raw = append(raw, line)
			switch {
			case strings.HasPrefix(line, checkboxOpen):
				boxes++
			case strings.HasPrefix(line, checkboxChecked):
				boxes++
				cur.Rejected = true
			case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
				c, err := parseMark(line[2:])
				if err != nil {
					errs = append(errs, fmt.Errorf("line %d: %w", n+1, err))
					continue
				}
				cur.Claims = append(cur.Claims, c)
			}
		case "scan_still_open":
			if m := stillOpenRE.FindStringSubmatch(line); m != nil {
				nr, err := strconv.Atoi(m[1])
				if err != nil {
					errs = append(errs, fmt.Errorf("line %d: issue number %s: %w", n+1, m[1], err))
					continue
				}
				res.StillOpen = append(res.StillOpen, nr)
			}
		}
	}
	flush()
	for _, k := range keys {
		if !seenSection[k] {
			errs = append(errs, fmt.Errorf("missing heading %s (%s)", k, names[k][0]))
		} else if !hasContent[k] {
			errs = append(errs, fmt.Errorf("heading %s (%s) has no content", k, names[k][0]))
		}
	}
	if len(errs) > 0 {
		return Result{}, errors.Join(errs...)
	}
	return res, nil
}

// rewrite walks the candidate blocks of body and lets fn decide, per checkbox line, the new line.
// Candidates live in the first `## ` section that holds a candidate heading; a `###` block in
// any other section (method, still open) is not a candidate.
func rewrite(body string, fn func(key, line string) string) string {
	lines := strings.Split(body, "\n")
	key := ""
	inSection, done := false, false
	for i, l := range lines {
		t := strings.TrimSuffix(l, "\r")
		switch {
		case strings.HasPrefix(t, sectionLevel) || strings.HasPrefix(t, "# "):
			key = ""
			if inSection {
				done, inSection = true, false
			}
		case strings.HasPrefix(t, candidateLevel):
			key = ""
			if m := candidateRE.FindStringSubmatch(t); m != nil && !done {
				key, inSection = m[2], true
			}
		case key != "" && (strings.HasPrefix(t, checkboxOpen) || strings.HasPrefix(t, checkboxChecked)):
			lines[i] = fn(key, l)
		}
	}
	return strings.Join(lines, "\n")
}

// WithCheckboxes renders the body with a ticked checkbox for every key in live; a box that is
// ticked in the body stays ticked.
func (r Result) WithCheckboxes(live map[string]bool) string {
	return rewrite(r.Body, func(key, line string) string {
		if live[key] {
			return checkboxChecked + strings.TrimPrefix(strings.TrimPrefix(line, checkboxChecked), checkboxOpen)
		}
		return line
	})
}

// RejectedKeys is the keys whose checkbox is ticked in an existing body.
func RejectedKeys(body string) map[string]bool {
	out := map[string]bool{}
	rewrite(body, func(key, line string) string {
		if strings.HasPrefix(line, checkboxChecked) {
			out[key] = true
		}
		return line
	})
	return out
}
