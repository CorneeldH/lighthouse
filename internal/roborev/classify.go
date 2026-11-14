package roborev

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

var (
	heading  = regexp.MustCompile("(?m)^## roborev: (.+?) \\(`([^`]+)`\\)\\s*$")
	severity = regexp.MustCompile(`^### (?:\d+\. )?(\S.*?)\s*$`)
	numbered = regexp.MustCompile(`^### \d+\. `)
	bullet   = regexp.MustCompile(`^- (\S+?):(\d+):\s*(.*)$`)
	location = regexp.MustCompile(`^\*\*Location:\*\*\s*(\S+?):(\d+)`)
	problem  = regexp.MustCompile(`^\*\*Problem:\*\*\s*(.*)$`)
	// roborev's own log lines on stderr: never the reason.
	logLine   = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	doneKinds = []string{"Review Complete", "Combined Review", "Review Passed"}
)

// Finding is one finding of a review; a nil Path or Line is JSON null.
type Finding struct {
	Path     *string
	Line     *int
	Severity string
	Text     string
}

// Classification is the verdict on raw review output: "done" with Sha, Kind and Findings, or
// "skip" with Reason.
type Classification struct {
	Verdict  string
	Sha      string
	Kind     string
	Reason   string
	Findings []Finding
}

// JSON is the object `classify` prints, in the key order of the Python script.
func (c Classification) JSON() *orderedjson.Object {
	out := orderedjson.NewObject()
	out.Set("verdict", c.Verdict)
	if c.Verdict == "skip" {
		out.Set("reason", c.Reason)
		return out
	}
	out.Set("sha", c.Sha)
	out.Set("kind", c.Kind)
	findings := []any{}
	for _, f := range c.Findings {
		o := orderedjson.NewObject()
		o.Set("path", ptrValue(f.Path))
		o.Set("line", ptrValue(f.Line))
		o.Set("severity", f.Severity)
		o.Set("text", f.Text)
		findings = append(findings, o)
	}
	out.Set("findings", findings)
	return out
}

func ptrValue[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// splitLines splits as Python's str.splitlines does, so the same text gives the same lines.
func splitLines(s string) []string {
	var lines []string
	runes := []rune(s)
	var cur strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\r':
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			fallthrough
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func skipBecause(reason string) Classification {
	return Classification{Verdict: "skip", Reason: reason}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func strPtr(s string) *string { return &s }
func intPtr(n int) *int       { return &n }

// findings reads both forms: CI `### <Severity>` + `- <path>:<line>: <text>` bullets, and the
// local `### <n>. <Severity>` + `**Location:** <path>:<line>` + `**Problem:** <text>`.
func findings(raw string) []Finding {
	var found []Finding
	sev := ""
	local := -1 // index in found of the local finding being filled
	for _, line := range splitLines(raw) {
		if m := severity.FindStringSubmatch(line); m != nil {
			sev = strings.ToLower(m[1])
			local = -1
			if numbered.MatchString(line) {
				found = append(found, Finding{Severity: sev})
				local = len(found) - 1
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			sev, local = "", -1
			continue
		}
		if sev == "" {
			continue
		}
		if local >= 0 {
			if m := location.FindStringSubmatch(line); m != nil {
				found[local].Path, found[local].Line = strPtr(m[1]), intPtr(atoi(m[2]))
			} else if m := problem.FindStringSubmatch(line); m != nil {
				found[local].Text = strings.TrimSpace(m[1])
			}
		} else if m := bullet.FindStringSubmatch(line); m != nil {
			found = append(found, Finding{Path: strPtr(m[1]), Line: intPtr(atoi(m[2])), Severity: sev, Text: strings.TrimSpace(m[3])})
		} else if strings.HasPrefix(line, "- ") {
			found = append(found, Finding{Severity: sev, Text: strings.TrimSpace(line[2:])})
		}
	}
	return found
}

// Classify turns raw review output into a verdict; the exit code of roborev plays no part.
//
// Heading: ## roborev: <kind> (`<sha>`). Review Complete, Combined Review and Review Passed give
// done (a pass has no findings; a complete review that parses to none is
// `review faalde: bevindingen onleesbaar`); any other kind gives skip. No heading: the reason
// names the first non-empty line that is no roborev log line, else the first line; 200
// characters at most; empty output: `geen output`.
func Classify(raw string) Classification {
	loc := heading.FindStringSubmatchIndex(raw)
	if loc == nil {
		var lines []string
		for _, ln := range splitLines(raw) {
			if ln = strings.TrimSpace(ln); ln != "" {
				lines = append(lines, ln)
			}
		}
		first := ""
		if len(lines) > 0 {
			first = lines[0]
			for _, ln := range lines {
				if !logLine.MatchString(ln) {
					first = ln
					break
				}
			}
		}
		if r := []rune(first); len(r) > 200 {
			first = string(r[:200])
		}
		if first == "" {
			first = "geen output"
		}
		return skipBecause("review faalde: " + first)
	}
	kind, sha := raw[loc[2]:loc[3]], raw[loc[4]:loc[5]]
	known := false
	for _, k := range doneKinds {
		known = known || k == kind
	}
	if !known {
		return skipBecause("review faalde: " + kind)
	}
	var found []Finding
	if kind != "Review Passed" {
		if found = findings(raw[loc[1]:]); len(found) == 0 {
			return skipBecause("review faalde: bevindingen onleesbaar")
		}
	}
	return Classification{Verdict: "done", Sha: sha, Kind: kind, Findings: found}
}
