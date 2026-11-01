// Package protocol reads the `🤖 <kind>` comments of the lighthouse protocol. It is the Go
// twin of pack/core/lib/protocol.py; tests/pack/fixtures/protocol holds the cases both share.
package protocol

import (
	"regexp"
	"slices"
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
	KindParked   Kind = "parked"
)

var kinds = []Kind{KindHandoff, KindStarted, KindQuestion, KindResumed, KindDone, KindReturned, KindRebased, KindParked}

// Header is the kind and the fields of a protocol comment. Keys and values are canonical:
// the legacy ones are translated.
type Header struct {
	Kind   Kind
	Fields map[string]string
	// Session is the id of the last `<sub>session <uuid>` footer in the body, "" when none.
	Session string
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

var sessionFooter = regexp.MustCompile(
	`<sub>(?:session|sessie) ([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

var fieldLine = regexp.MustCompile(`^([a-z_]+):\s?(.*)$`)

type variant struct {
	kind Kind
	text string
}

// headingTable is the embedded heading table: key, then language, then text. It is part of
// the binary, so an unreadable one is a programming error.
var headingTable = sync.OnceValue(func() map[string]map[string]string {
	raw, err := pack.Files.ReadFile("core/headings.toml")
	if err != nil {
		panic("protocol: read core/headings.toml: " + err.Error())
	}
	var table map[string]map[string]string
	if err := toml.Unmarshal(raw, &table); err != nil {
		panic("protocol: decode core/headings.toml: " + err.Error())
	}
	return table
})

// kindVariants holds every heading variant of every kind, longest first.
var kindVariants = sync.OnceValue(func() []variant {
	var out []variant
	for _, kind := range kinds {
		for _, text := range Variants("kind_" + string(kind)) {
			out = append(out, variant{kind: kind, text: text})
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
	header := Header{Kind: kind, Fields: fields}
	if sessions := sessionFooter.FindAllStringSubmatch(body, -1); len(sessions) > 0 {
		header.Session = sessions[len(sessions)-1][1]
	}
	return header, true
}
