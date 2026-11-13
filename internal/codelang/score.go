// Package codelang detects code that is not written in English. This file is layer 1: it
// scores a name or a text per word, from word frequencies (zipf x 10) embedded in the binary.
package codelang

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Verdict is the outcome of judging one name or text.
type Verdict string

// The three verdicts.
const (
	VerdictClean   Verdict = "clean"
	VerdictDoubt   Verdict = "doubt"
	VerdictFinding Verdict = "finding"
)

// Thresholds from the Python original. Scores are kept in whole tenths (zipf nl minus en, so
// 10 is 1.0): a float sum can round differently on the boundary, an integer sum cannot.
const (
	thresholdTenths = 10 // a unit is a finding when its sum is strictly above this
	compoundTenths  = 30 // score of an unlisted word that splits into Dutch words
	partMin         = 3  // letters of a part of a compound
	partNLFloor     = 25 // zipf x 10 NL a part of a compound needs
	doubtLean       = 20 // nl - en of a word that makes a unit doubtful
	doubtNL         = 30 // nl of a word that makes a unit doubtful
	nameMin         = 4  // shortest word counted in a name
	splitMax        = 40 // longest word DutchSplit tries
	textMin         = 2  // shortest word counted in prose
)

//go:embed data/freq.tsv.gz data/jargon.txt
var dataFS embed.FS

// The Python file names stay in the messages, so the text of an error does not change.
const (
	freqName   = "code_language_freq.tsv.gz"
	jargonName = "code_language_jargon.txt"
)

// CannotCheckError says the check could not run; the Reason is the message after
// "COULD NOT CHECK — ".
type CannotCheckError struct{ Reason string }

func (e *CannotCheckError) Error() string { return e.Reason }

// Scorer judges names and texts against a frequency table and a jargon list.
type Scorer struct {
	freq   map[string][2]int // word to {english, dutch}, zipf x 10
	jargon map[string]bool
}

// NewScorer builds a Scorer from a table of word -> {en, nl} and a list of jargon words.
func NewScorer(freq map[string][2]int, jargon []string) *Scorer {
	skip := make(map[string]bool, len(jargon))
	for _, w := range jargon {
		skip[w] = true
	}
	return &Scorer{freq: freq, jargon: skip}
}

var defaultScorer = sync.OnceValues(func() (*Scorer, error) {
	raw, err := dataFS.ReadFile("data/freq.tsv.gz")
	if err != nil {
		return nil, &CannotCheckError{Reason: fmt.Sprintf("cannot read %s: %v", freqName, err)}
	}
	freq, err := loadFreq(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	rawJargon, err := dataFS.ReadFile("data/jargon.txt")
	if err != nil {
		return nil, &CannotCheckError{Reason: fmt.Sprintf("cannot read %s: %v", jargonName, err)}
	}
	jargon, err := loadJargon(rawJargon)
	if err != nil {
		return nil, err
	}
	return NewScorer(freq, jargon), nil
})

// DefaultScorer is the Scorer over the embedded data, loaded once per process.
func DefaultScorer() (*Scorer, error) { return defaultScorer() }

// loadFreq reads a gzip stream of "word<TAB>en<TAB>nl" lines.
func loadFreq(r io.Reader) (map[string][2]int, error) {
	fail := func(err error) (map[string][2]int, error) {
		return nil, &CannotCheckError{Reason: fmt.Sprintf("cannot read %s: %v", freqName, err)}
	}
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fail(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return fail(err)
	}
	if !utf8.Valid(raw) {
		return fail(fmt.Errorf("invalid UTF-8"))
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	table := map[string][2]int{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line == "" && text == "" {
			break
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return fail(fmt.Errorf("expected 3 fields, got %d in %q", len(fields), line))
		}
		en, errEn := strconv.Atoi(fields[1])
		nl, errNL := strconv.Atoi(fields[2])
		if errEn != nil || errNL != nil {
			return fail(fmt.Errorf("invalid number in %q", line))
		}
		table[fields[0]] = [2]int{en, nl}
	}
	return table, nil
}

// loadJargon reads one word per line; "#" starts a comment line.
func loadJargon(raw []byte) ([]string, error) {
	if !utf8.Valid(raw) {
		return nil, &CannotCheckError{Reason: fmt.Sprintf("cannot read %s: invalid UTF-8", jargonName)}
	}
	var words []string
	for _, line := range splitLines(string(raw)) {
		entry := strings.ToLower(trimSpace(line))
		if entry != "" && !strings.HasPrefix(entry, "#") {
			words = append(words, entry)
		}
	}
	return words, nil
}

// SplitName splits an identifier on "_", digits and case changes, in lower case. Only ASCII
// letters count. It is the hand-written form of [A-Z]+(?![a-z])|[A-Z]?[a-z]+.
func SplitName(name string) []string {
	parts := []string{}
	isUpper := func(c byte) bool { return c >= 'A' && c <= 'Z' }
	isLower := func(c byte) bool { return c >= 'a' && c <= 'z' }
	for i := 0; i < len(name); {
		switch c := name[i]; {
		case isUpper(c):
			j := i
			for j < len(name) && isUpper(name[j]) {
				j++
			}
			followedByLower := j < len(name) && isLower(name[j])
			switch {
			case !followedByLower:
				parts = append(parts, name[i:j])
				i = j
			case j-i > 1: // an acronym run, minus the capital that starts the next word
				parts = append(parts, name[i:j-1])
				i = j - 1
			default: // one capital plus its lower-case run
				k := j
				for k < len(name) && isLower(name[k]) {
					k++
				}
				parts = append(parts, name[i:k])
				i = k
			}
		case isLower(c):
			j := i
			for j < len(name) && isLower(name[j]) {
				j++
			}
			parts = append(parts, name[i:j])
			i = j
		default:
			i++
		}
	}
	for i, p := range parts {
		parts[i] = strings.ToLower(p)
	}
	return parts
}

const (
	pyNotSpace = `[^\p{Z}\t\n\v\f\r\x1c-\x1f\x85]`
)

var (
	wordRE     = regexp.MustCompile(`[\p{L}\p{Nl}\p{No}]+`)
	proseNoise = regexp.MustCompile("`[^`]*`|https?://" + pyNotSpace + "+")
)

// lowerPython lower-cases like Python's str.lower where it matters for Words: U+0130 becomes
// "i" plus a combining dot, which is no letter.
func lowerPython(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

// Words returns the words of text: runs of letters (accents included), in lower case.
func Words(text string) []string {
	found := wordRE.FindAllString(lowerPython(text), -1)
	if found == nil {
		return []string{}
	}
	return found
}

func isLetter(r rune) bool { return wordRE.MatchString(string(r)) }

// splitSentences splits text where a run of whitespace follows ".", "!" or "?", unless the
// three runes before that run are "." letter ".". With lineBreaks every "\n" splits as well.
// It is the hand-written form of (?<=[.!?])(?<!\.[^\W\d_]\.)\s+ (| \n).
func splitSentences(text string, lineBreaks bool) []string {
	runes := []rune(text)
	var parts []string
	start := 0
	for i := 0; i < len(runes); {
		prev := rune(0)
		if i > 0 {
			prev = runes[i-1]
		}
		abbreviation := i >= 3 && runes[i-3] == '.' && isLetter(runes[i-2]) && runes[i-1] == '.'
		sentenceEnd := isPySpace(runes[i]) && (prev == '.' || prev == '!' || prev == '?') && !abbreviation
		switch {
		case sentenceEnd:
			j := i
			for j < len(runes) && isPySpace(runes[j]) {
				j++
			}
			parts = append(parts, string(runes[start:i]))
			start, i = j, j
		case lineBreaks && runes[i] == '\n':
			parts = append(parts, string(runes[start:i]))
			start, i = i+1, i+1
		default:
			i++
		}
	}
	return append(parts, string(runes[start:]))
}

// scoreTenths is (nl - en) for a listed word and compoundTenths for an unlisted Dutch
// compound or participle.
func (s *Scorer) scoreTenths(w string) int {
	if e, ok := s.freq[w]; ok {
		return e[1] - e[0]
	}
	if s.DutchSplit(w) || s.DutchParticiple(w) {
		return compoundTenths
	}
	return 0
}

// WordScore is (nl - en) / 10; an unlisted word scores 3.0 if it is a Dutch compound or the
// Dutch participle of an English verb.
func (s *Scorer) WordScore(w string) float64 { return float64(s.scoreTenths(w)) / 10 }

func (s *Scorer) isDutchPart(part []rune) bool {
	if len(part) < partMin {
		return false
	}
	e, ok := s.freq[string(part)]
	return ok && e[1] >= partNLFloor && e[1] > e[0]
}

// DutchSplit reports whether w splits into two or more Dutch words, even if an English split
// exists. A word longer than 40 letters is never split.
func (s *Scorer) DutchSplit(w string) bool {
	runes := []rune(w)
	if len(runes) > splitMax {
		return false
	}
	memo := map[int]bool{}
	var splits func(from int) bool
	splits = func(from int) bool {
		if v, ok := memo[from]; ok {
			return v
		}
		rest := runes[from:]
		result := false
		for i := partMin; i <= len(rest)-partMin; i++ {
			if s.isDutchPart(rest[:i]) && (s.isDutchPart(rest[i:]) || splits(from+i)) {
				result = true
				break
			}
		}
		memo[from] = result
		return result
	}
	return splits(0)
}

// DutchParticiple reports whether w is "ge" plus a listed English past form in "-ed".
func (s *Scorer) DutchParticiple(w string) bool {
	rest, ok := strings.CutPrefix(w, "ge")
	if !ok {
		return false
	}
	e, listed := s.freq[rest]
	return listed && strings.HasSuffix(rest, "ed") && e[0] > e[1]
}

func (s *Scorer) judge(candidates []string, minimum int, sentences [][]string) Verdict {
	counted := func(group []string) []string {
		var kept []string
		for _, w := range group {
			if utf8.RuneCountInString(w) >= minimum && !s.jargon[w] {
				kept = append(kept, w)
			}
		}
		return kept
	}
	sum := func(group []string) int {
		total := 0
		for _, w := range counted(group) {
			total += s.scoreTenths(w)
		}
		return total
	}
	groups := append([][]string{candidates}, sentences...)
	for _, g := range groups {
		if sum(g) > thresholdTenths {
			return VerdictFinding
		}
	}
	for _, w := range counted(candidates) {
		if e, ok := s.freq[w]; ok && e[1]-e[0] >= doubtLean && e[1] >= doubtNL {
			return VerdictDoubt
		}
	}
	return VerdictClean
}

// JudgeName judges an identifier or a file name.
func (s *Scorer) JudgeName(name string) Verdict {
	return s.judge(SplitName(name), nameMin, nil)
}

// JudgeText judges prose; inline code and URLs do not count. The text as a whole and each of
// its sentences can make a finding. With lineBreaks every line is a boundary too.
func (s *Scorer) JudgeText(text string, lineBreaks bool) Verdict {
	text = proseNoise.ReplaceAllString(text, " ")
	parts := splitSentences(text, lineBreaks)
	var sentences [][]string
	if len(parts) > 1 {
		for _, p := range parts {
			sentences = append(sentences, Words(p))
		}
	}
	return s.judge(Words(text), textMin, sentences)
}
