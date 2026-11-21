package protocol

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Heading is the text of key in language. An unknown language falls back to "en". An unknown
// key panics: the keys are in the embedded table, so a missing one is a programming error.
func Heading(key, language string) string {
	byLanguage, ok := headingTable()[key]
	if !ok {
		panic("protocol: unknown heading key " + key)
	}
	if text, ok := byLanguage[language]; ok {
		return text
	}
	return byLanguage["en"]
}

// Variants is every text of key, ordered by language code, for reading.
func Variants(key string) []string {
	byLanguage := headingTable()[key]
	languages := make([]string, 0, len(byLanguage))
	for language := range byLanguage {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	out := make([]string, 0, len(languages))
	for _, language := range languages {
		out = append(out, byLanguage[language])
	}
	return out
}

// reasonVariant is one text of a reason_* heading with its key.
type reasonVariant struct{ key, text string }

// reasonVariants holds every text of every reason_* heading, longest first, so a text that
// starts with a shorter one is read as its own key.
var reasonVariants = sync.OnceValue(func() []reasonVariant {
	var out []reasonVariant
	for key := range headingTable() {
		if !strings.HasPrefix(key, "reason_") {
			continue
		}
		for _, text := range Variants(key) {
			out = append(out, reasonVariant{key: key, text: text})
		}
	}
	slices.SortFunc(out, func(a, b reasonVariant) int {
		return cmp.Or(len(b.text)-len(a.text), strings.Compare(a.key, b.key))
	})
	return out
})

// ReasonKey reads the reason_* key back from the reason field of a 🤖 Returned: the heading
// text in any language, alone or followed by ": <argument>". false when no heading matches.
func ReasonKey(field string) (string, bool) {
	field = strings.TrimSpace(field)
	for _, v := range reasonVariants() {
		if rest, ok := strings.CutPrefix(field, v.text); ok && (rest == "" || strings.HasPrefix(rest, ": ")) {
			return v.key, true
		}
	}
	return "", false
}
