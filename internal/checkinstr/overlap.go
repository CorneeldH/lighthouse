package checkinstr

import (
	"fmt"
	"regexp"
	"strings"
)

// overlapThreshold is the number of shared trigger words from which two descriptions overlap.
const overlapThreshold = 5

// stopwords appear in almost every description and so distinguish nothing.
var stopwords = func() map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(`gebruik wanneer welke wordt worden binnen deze dit die daar want
		zodat alleen altijd nooit iemand iedere elke andere anders about after before
		which where their there these those would could should using while whenever
		without within always never asks user users something anything wants needs`) {
		set[w] = true
	}
	return set
}()

// scopeWords are the bounding phrases, each matched as separate words: a bare scope word
// inside a longer word does not count (`buiten` in "buitenwereld"). // lighthouse: allow-language — a Dutch example
var scopeWords = func() []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, w := range []string{"let op", "distinct from", "in plaats van", "gebruik dan",
		"niet de skill", "niet voor", "nooit voor", "instead", "not for"} {
		res = append(res, regexp.MustCompile(`(^|[^a-z])`+strings.ReplaceAll(w, " ", `[[:space:]]`)+`([^a-z]|$)`))
	}
	return res
}()

// hyphenToken is a hyphenated name, such as another skill.
var hyphenToken = func() *regexp.Regexp {
	re := regexp.MustCompile(`[a-z0-9]+(-[a-z0-9]+)+`)
	re.Longest()
	return re
}()

// lowerASCII lowercases A-Z only, as `tr '[:upper:]' '[:lower:]'` does on bytes.
func lowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

// triggerTokens are the trigger words of a description: lowercase runs of a-z0-9 of at least
// five characters that are not a stopword.
func triggerTokens(desc string) map[string]bool {
	tokens := map[string]bool{}
	words := strings.FieldsFunc(lowerASCII(desc), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, w := range words {
		if len(w) >= 5 && !stopwords[w] {
			tokens[w] = true
		}
	}
	return tokens
}

// hasExclusionClause is true when desc bounds itself: a scope phrase as separate words AND
// another skill by name (a hyphenated token of at least six characters that is not own). Either
// one alone does not bound: a widening phrase enlarges the trigger instead.
func hasExclusionClause(desc, own string) bool {
	descLines := strings.Split(lowerASCII(desc), "\n")
	scoped := false
	for _, re := range scopeWords {
		for _, l := range descLines {
			scoped = scoped || re.MatchString(l)
		}
	}
	if !scoped {
		return false
	}
	for _, l := range descLines {
		for _, token := range hyphenToken.FindAllString(l, -1) {
			if len(token) >= 6 && token != own {
				return true
			}
		}
	}
	return false
}

// Check 4: two descriptions that share trigger words without either one bounding. The model
// then picks at random; the fix is a clause in that description, not a higher threshold.
func checkOverlap(rp *repo, r *report) {
	names := skillDirs(rp.claudeSkills)
	descs := make([]string, len(names))
	tokens := make([]map[string]bool, len(names))
	for i, name := range names {
		descs[i] = frontmatterField(rp.claudeSkills+"/"+name+"/SKILL.md", "description")
		tokens[i] = triggerTokens(descs[i])
	}
	found := false
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			shared := 0
			for w := range tokens[i] {
				if tokens[j][w] {
					shared++
				}
			}
			if shared < overlapThreshold || hasExclusionClause(descs[i], names[i]) ||
				hasExclusionClause(descs[j], names[j]) {
				continue
			}
			found = true
			r.fail(fmt.Sprintf("overlap without a bounding clause: '%s' and '%s' share %d trigger words",
				names[i], names[j], shared))
		}
	}
	if !found {
		r.ok("no unbounded overlap between descriptions")
	}
}
