// Package protocol reads the `🤖 <kind>` comments of the lighthouse protocol. It is the Go
// twin of pack/core/lib/protocol.py; tests/pack/fixtures/protocol holds the cases both share.
package protocol

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/pack"
)

// Robot opens every comment of the protocol.
const Robot = "🤖"

// Kind is the canonical kind of a protocol comment, whatever language its heading is in.
type Kind string

const (
	KindHandoff  Kind = "handoff"
	KindStarted  Kind = "started"
	KindQuestion Kind = "question"
	KindResumed  Kind = "resumed"
	KindDone     Kind = "done"
	KindReturned Kind = "returned"
	KindRebased  Kind = "rebased"
)

var kinds = []Kind{KindHandoff, KindStarted, KindQuestion, KindResumed, KindDone, KindReturned, KindRebased}

// Header is the kind and the fields of a protocol comment. Keys and values are canonical:
// the legacy ones are translated.
type Header struct {
	Kind   Kind
	Fields map[string]string
}

// The keys and values of comments from before the move, as in _LEGACY_KEYS and
// _LEGACY_VALUES of protocol.py.
var legacyKeys = map[string]string{
	"volgende":   "next",
	"wanneer":    "when",
	"anker":      "anchor",
	"basis":      "base",
	"sessie":     "session",
	"commando":   "command",
	"stapel":     "stack",
	"blokkade":   "blocker",
	"sprong":     "skip",
	"terugval":   "fallback",
	"voorganger": "predecessor",
	"op":         "onto",
	"antwoord":   "answer",
	"reden":      "reason",
}

var legacyValues = map[[2]string]string{
	{"when", "nu"}:           "now",
	{"when", "nacht"}:        "night",
	{"blocker", "verrekend"}: "accounted",
}

var fieldLine = regexp.MustCompile(`^([a-z_]+):\s?(.*)$`)

type variant struct {
	kind Kind
	text string
}

// kindVariants holds every heading variant of every kind, longest first. The embedded
// table is part of the binary, so an unreadable one is a programming error.
var kindVariants = sync.OnceValue(func() []variant {
	raw, err := pack.Files.ReadFile("core/headings.toml")
	if err != nil {
		panic("protocol: read core/headings.toml: " + err.Error())
	}
	var table map[string]map[string]string
	if err := toml.Unmarshal(raw, &table); err != nil {
		panic("protocol: decode core/headings.toml: " + err.Error())
	}
	var out []variant
	for _, kind := range kinds {
		byLanguage := table["kind_"+string(kind)]
		languages := make([]string, 0, len(byLanguage))
		for language := range byLanguage {
			languages = append(languages, language)
		}
		sort.Strings(languages)
		for _, language := range languages {
			out = append(out, variant{kind: kind, text: byLanguage[language]})
		}
	}
	slices.SortStableFunc(out, func(a, b variant) int { return len(b.text) - len(a.text) })
	return out
})

// kindOf maps the text after `🤖 ` to a kind: a variant, then nothing, whitespace or `·`.
func kindOf(title string) (Kind, bool) {
	for _, v := range kindVariants() {
		rest, found := strings.CutPrefix(title, v.text)
		if !found {
			continue
		}
		if rest == "" {
			return v.kind, true
		}
		r, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsSpace(r) || r == '·' {
			return v.kind, true
		}
	}
	return "", false
}

// ParseHeader reads the kind and the fields of a protocol comment. The second result is
// false when the body has no protocol header.
func ParseHeader(body string) (Header, bool) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	title, found := strings.CutPrefix(lines[0], Robot+" ")
	if !found {
		return Header{}, false
	}
	kind, ok := kindOf(strings.TrimSpace(title))
	if !ok {
		return Header{}, false
	}
	fields := map[string]string{}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		m := fieldLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		if canonical, ok := legacyKeys[key]; ok {
			key = canonical
		}
		value := strings.TrimSpace(m[2])
		if canonical, ok := legacyValues[[2]string{key, value}]; ok {
			value = canonical
		}
		fields[key] = value
	}
	return Header{Kind: kind, Fields: fields}, true
}
