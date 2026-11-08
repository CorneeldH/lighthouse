package kpi

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Character classes as Python 3 defines them on str; Go's \s, \S and \d are ASCII-only.
const (
	wsClass  = `[\t\n\v\f\r\x{1c}-\x{1f}\x{85}\p{Z}]`
	nwsClass = `[^\t\n\v\f\r\x{1c}-\x{1f}\x{85}\p{Z}]`
	digClass = `\p{Nd}`
)

var classReplacer = strings.NewReplacer("⟨ws⟩", wsClass, "⟨nws⟩", nwsClass, "⟨d⟩", digClass)

// pyRe compiles pattern after expanding the ⟨ws⟩, ⟨nws⟩ and ⟨d⟩ placeholders.
func pyRe(pattern string) *regexp.Regexp {
	return regexp.MustCompile(classReplacer.Replace(pattern))
}

var (
	pickupRe = pyRe(`(?i)` +
		`(?:pak|pik)⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}⟨ws⟩+op` +
		`|(?:begin(?:nen)?|start(?:en)?|ga⟨ws⟩+verder|ga⟨ws⟩+aan⟨ws⟩+de⟨ws⟩+slag|werk)⟨ws⟩+` +
		`(?:aan|met)⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}` +
		`|kijk⟨ws⟩+(?:eens⟨ws⟩+)?naar⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}` +
		`|pick⟨ws⟩+up⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}` +
		`|pick⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}⟨ws⟩+up` +
		`|(?:start(?:ing)?|continue|get⟨ws⟩+started|work(?:ing)?)⟨ws⟩+` +
		`(?:on|with)⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}` +
		`|look⟨ws⟩+at⟨ws⟩+(?:issue⟨ws⟩*)?#?⟨d⟩{2,4}`)

	fixRequestRe = pyRe(`(?i)` +
		`(?:fix|repareer|debug|herstel)⟨ws⟩+(?:de⟨ws⟩+)?(?:pr⟨ws⟩*)?#?⟨d⟩{2,4}\b` +
		`|#?⟨d⟩{2,4}⟨ws⟩+(?:pr⟨ws⟩+)?(?:heeft|is)⟨ws⟩+(?:een⟨ws⟩+)?` +
		`(?:conflict|conflicts|conflictje|error|errors|fout|rood)` +
		`|conflict(?:en|je)?⟨ws⟩+(?:van⟨ws⟩+|in⟨ws⟩+|op⟨ws⟩+)?#?⟨d⟩{2,4}`)

	// mutatingRe leaves out Python's (?!<) after <<: it is a no-op there, because the next
	// character must be '-', whitespace, a quote, a capital or '_', never '<'. So
	// `cat <<< 'HELLO'` matches in both.
	mutatingRe = pyRe(`(?i)` +
		`git⟨ws⟩+(rebase|merge|cherry-pick|add|commit|push|checkout|restore|worktree⟨ws⟩+add|reset|switch)\b` +
		`|git⟨ws⟩+branch⟨ws⟩+(?:⟨nws⟩+⟨ws⟩+)*(?:-[dDmMcC]|--delete|--move|--copy)\b` +
		`|git⟨ws⟩+stash⟨ws⟩+(?:pop|apply)\b` +
		`|sed⟨ws⟩+-i` +
		`|<<-?⟨ws⟩*(?-i:['"]?[A-Z_][A-Z0-9_]*['"]?)` +
		`|gh⟨ws⟩+(run⟨ws⟩+rerun|pr⟨ws⟩+merge|pr⟨ws⟩+edit)\b`)

	diagnosticRe = pyRe(`(?i)` +
		`gh⟨ws⟩+pr⟨ws⟩+(view|checks|diff)\b` +
		`|gh⟨ws⟩+run⟨ws⟩+(view|list)\b` +
		`|git⟨ws⟩+stash⟨ws⟩+(?:list|show)\b` +
		`|git⟨ws⟩+(worktree⟨ws⟩+list|status|log|branch|diff|show|fetch|rev-parse|merge-tree)\b`)
)

func isPickup(text string) bool     { return pickupRe.MatchString(text) }
func isFixRequest(text string) bool { return fixRequestRe.MatchString(text) }

// commandOf is the Bash command of a tool input; anything but a string counts as "".
func commandOf(input map[string]any) string {
	s, _ := input["command"].(string)
	return s
}

func isMutating(name string, input map[string]any) bool {
	switch name {
	case "Write", "Edit", "NotebookEdit":
		return true
	case "Bash":
		return mutatingRe.MatchString(commandOf(input))
	}
	return false
}

func isDiagnostic(name string, input map[string]any) bool {
	if name != "Bash" {
		return false
	}
	command := commandOf(input)
	return !mutatingRe.MatchString(command) && diagnosticRe.MatchString(command)
}

// isWord is Python's \w on str.
func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

func isDigit(r rune) bool { return unicode.Is(unicode.Nd, r) }

func isNameRune(r rune) bool { return r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') }

// namesASkill is SLASH_SKILL_RE.search: (?<![\w/])/([a-z][a-z0-9-]*(?::[a-z0-9-]+)?)(?![\w/]).
// RE2 has no lookaround, so every end the backtracking could reach is tried.
func namesASkill(text string) bool {
	rs := []rune(text)
	notEdge := func(i int) bool { return i >= len(rs) || (!isWord(rs[i]) && rs[i] != '/') }
	for i, r := range rs {
		if r != '/' || (i > 0 && (isWord(rs[i-1]) || rs[i-1] == '/')) {
			continue
		}
		start := i + 1
		if start >= len(rs) || rs[start] < 'a' || rs[start] > 'z' {
			continue
		}
		coreEnd := start + 1
		for coreEnd < len(rs) && isNameRune(rs[coreEnd]) {
			coreEnd++
		}
		for end := start + 1; end <= coreEnd; end++ {
			if notEdge(end) {
				return true
			}
		}
		if coreEnd < len(rs) && rs[coreEnd] == ':' {
			subEnd := coreEnd + 1
			for subEnd < len(rs) && isNameRune(rs[subEnd]) {
				subEnd++
			}
			for end := coreEnd + 2; end <= subEnd; end++ {
				if notEdge(end) {
					return true
				}
			}
		}
	}
	return false
}

// hasIssueNumber is ISSUE_NUMBER_RE.search: (?<![\w.])#?\d{2,4}(?![\w.]).
func hasIssueNumber(text string) bool {
	rs := []rune(text)
	for i := range rs {
		if i > 0 && (isWord(rs[i-1]) || rs[i-1] == '.') {
			continue
		}
		starts := []int{i}
		if rs[i] == '#' {
			starts = append(starts, i+1)
		}
		for _, s := range starts {
			run := 0
			for s+run < len(rs) && isDigit(rs[s+run]) {
				run++
			}
			for n := min(run, 4); n >= 2; n-- {
				end := s + n
				if end >= len(rs) || (!isWord(rs[end]) && rs[end] != '.') {
					return true
				}
			}
		}
	}
	return false
}

// headingRes are the heading-dependent patterns: the gate line and the scout-skipped line.
type headingRes struct {
	gateRe, subgateRe, scoutSkipRe *regexp.Regexp
}

// flexibleVariants is every variant of heading key as a regex group; a space matches any
// whitespace, also none.
func flexibleVariants(key string) (string, error) {
	variants, err := config.Variants(key)
	if err != nil {
		return "", err
	}
	alts := make([]string, len(variants))
	for i, v := range variants {
		parts := strings.Split(v, " ")
		for j, p := range parts {
			parts[j] = regexp.QuoteMeta(p)
		}
		alts[i] = strings.Join(parts, wsClass+"*")
	}
	return "(?:" + strings.Join(alts, "|") + ")", nil
}

func loadHeadingRes() (headingRes, error) {
	gate, err := flexibleVariants("gate_line")
	if err != nil {
		return headingRes{}, err
	}
	skip, err := flexibleVariants("scout_skipped")
	if err != nil {
		return headingRes{}, err
	}
	return headingRes{
		gateRe:      pyRe(`\b` + gate + `⟨ws⟩*(?P<gate>[0-4])[abc]?\b`),
		subgateRe:   pyRe(`\b` + gate + `⟨ws⟩*(?P<gate>[0-4])⟨ws⟩*(?P<sub>[abc])?\b`),
		scoutSkipRe: pyRe(`(?m)^` + skip + `⟨ws⟩*—⟨ws⟩*(?P<reden>.+?)⟨ws⟩*$`),
	}, nil
}

// gate is the announced gate number in text.
func (h headingRes) gate(text string) (int, bool) {
	m := h.gateRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[h.gateRe.SubexpIndex("gate")])
	return n, true
}

// subgate is the announced gate with its sub-letter ("4b"), or only the digit.
func (h headingRes) subgate(text string) (string, bool) {
	m := h.subgateRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[h.subgateRe.SubexpIndex("gate")] + m[h.subgateRe.SubexpIndex("sub")], true
}

// scoutMinReason is the shortest reason that counts: "duidelijk." and "simpel." do not.
const scoutMinReason = 20

// skipReasonOK reports whether text has a scout-skipped line with a real reason.
func (h headingRes) skipReasonOK(text string) bool {
	m := h.scoutSkipRe.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	reason := strings.Trim(m[h.scoutSkipRe.SubexpIndex("reden")], " .")
	return utf8.RuneCountInString(reason) >= scoutMinReason
}
