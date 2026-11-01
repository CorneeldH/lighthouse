package protocol

import (
	"sort"
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
