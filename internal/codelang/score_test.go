package codelang

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fixtureDir = "../../tests/pack/fixtures/code_language"

func readFixture(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestScoreFixture(t *testing.T) {
	var fx struct {
		DutchParticiple map[string]bool     `json:"dutch_participle"`
		DutchSplit      map[string]bool     `json:"dutch_split"`
		Names           map[string]string   `json:"names"`
		SplitName       map[string][]string `json:"split_name"`
		Texts           []struct {
			LineBreaks bool   `json:"line_breaks"`
			Text       string `json:"text"`
			Want       string `json:"want"`
		} `json:"texts"`
	}
	readFixture(t, "score.json", &fx)
	if len(fx.Names) == 0 || len(fx.Texts) == 0 || len(fx.SplitName) == 0 {
		t.Fatal("fixture is empty")
	}
	s, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	for w, want := range fx.DutchParticiple {
		if got := s.DutchParticiple(w); got != want {
			t.Errorf("DutchParticiple(%q) = %v, want %v", w, got, want)
		}
	}
	for w, want := range fx.DutchSplit {
		if got := s.DutchSplit(w); got != want {
			t.Errorf("DutchSplit(%q) = %v, want %v", w, got, want)
		}
	}
	for name, want := range fx.Names {
		if got := s.JudgeName(name); string(got) != want {
			t.Errorf("JudgeName(%q) = %q, want %q", name, got, want)
		}
	}
	for name, want := range fx.SplitName {
		if got := SplitName(name); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitName(%q) = %q, want %q", name, got, want)
		}
	}
	for _, c := range fx.Texts {
		if got := s.JudgeText(c.Text, c.LineBreaks); string(got) != c.Want {
			t.Errorf("JudgeText(%q, %v) = %q, want %q", c.Text, c.LineBreaks, got, c.Want)
		}
	}
}

func TestScoreTableFixture(t *testing.T) {
	var groups []struct {
		Name   string            `json:"name"`
		Table  map[string][2]int `json:"table"`
		Jargon []string          `json:"jargon"`
		Cases  []struct {
			Fn   string          `json:"fn"`
			Arg  string          `json:"arg"`
			Want json.RawMessage `json:"want"`
		} `json:"cases"`
	}
	readFixture(t, "score_table.json", &groups)
	if len(groups) == 0 {
		t.Fatal("fixture is empty")
	}
	for _, g := range groups {
		s := NewScorer(g.Table, g.Jargon)
		for _, c := range g.Cases {
			var got, want any
			if err := json.Unmarshal(c.Want, &want); err != nil {
				t.Fatal(err)
			}
			switch c.Fn {
			case "judge_name":
				got = string(s.JudgeName(c.Arg))
			case "judge_text":
				got = string(s.JudgeText(c.Arg, false))
			case "word_score":
				got = s.WordScore(c.Arg)
			case "dutch_split":
				got = s.DutchSplit(c.Arg)
			default:
				t.Fatalf("%s: unknown fn %q", g.Name, c.Fn)
			}
			if got != want {
				t.Errorf("%s: %s(%.30q) = %v, want %v", g.Name, c.Fn, c.Arg, got, want)
			}
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{}},
		{"trailing newline", "a\n", []string{"a"}},
		{"no separator", "a", []string{"a"}},
		{"blank line kept", "a\n\nb", []string{"a", "", "b"}},
		{"cr", "a\rb", []string{"a", "b"}},
		{"crlf is one", "a\r\nb", []string{"a", "b"}},
		{"lfcr is two", "a\n\rb", []string{"a", "", "b"}},
		{"vertical tab", "a\vb", []string{"a", "b"}},
		{"form feed", "a\fb", []string{"a", "b"}},
		{"fs", "a\x1cb", []string{"a", "b"}},
		{"gs", "a\x1db", []string{"a", "b"}},
		{"rs", "a\x1eb", []string{"a", "b"}},
		{"nel", "a\u0085b", []string{"a", "b"}},
		{"line separator", "a b", []string{"a", "b"}},
		{"paragraph separator", "a b", []string{"a", "b"}},
		{"us is no separator", "a\x1fb", []string{"a\x1fb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitLines(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitLines(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func gzipped(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLoadFreq(t *testing.T) {
	got, err := loadFreq(bytes.NewReader(gzipped(t, "bestand\t12\t55\nfile\t60\t10\n")))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"bestand": {12, 55}, "file": {60, 10}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoadCorruptData(t *testing.T) {
	good := gzipped(t, "bestand\t12\t55\n")
	tests := []struct {
		name string
		data []byte
	}{
		{"not gzip", []byte("this is not gzip")},
		{"truncated gzip", good[:len(good)-6]},
		{"two fields", gzipped(t, "bestand\t12\n")},
		{"four fields", gzipped(t, "bestand\t12\t55\t1\n")},
		{"not a number", gzipped(t, "bestand\tx\t55\n")},
		{"invalid utf-8", gzipped(t, "best\xffand\t1\t2\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFreq(bytes.NewReader(tt.data))
			var cc *CannotCheckError
			if !errors.As(err, &cc) {
				t.Fatalf("err = %v, want *CannotCheckError", err)
			}
			if want := "cannot read code_language_freq.tsv.gz: "; len(cc.Reason) < len(want) || cc.Reason[:len(want)] != want {
				t.Errorf("Reason = %q, want prefix %q", cc.Reason, want)
			}
		})
	}
}

func TestSentenceBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		lineBreaks bool
		want       []string
	}{
		{"period and space", "One. Two", false, []string{"One.", "Two"}},
		{"whitespace run is one split", "One.  \t Two", false, []string{"One.", "Two"}},
		{"question and bang", "A? B! C", false, []string{"A?", "B!", "C"}},
		{"abbreviation e.g.", "see e.g. this", false, []string{"see e.g. this"}},
		{"abbreviation needs letter", "see 1.2. this", false, []string{"see 1.2.", "this"}},
		{"no space after period", "config.py is", false, []string{"config.py is"}},
		{"newline alone is no split", "a\nb", false, []string{"a\nb"}},
		{"newline splits with line breaks", "a\nb", true, []string{"a", "b"}},
		{"unicode space after period", "One. Two", false, []string{"One.", "Two"}},
		{"space before newline stays", "a \nb", true, []string{"a ", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitSentences(tt.text, tt.lineBreaks); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitSentences(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"lower-cased", "Het Bestand", []string{"het", "bestand"}},
		{"accents kept", "café — naïve", []string{"café", "naïve"}},
		{"digits and underscore split", "a1b_c", []string{"a", "b", "c"}},
		{"dotted capital I splits like Python", "İstanbul", []string{"i", "stanbul"}},
		{"no letters", "123 !?", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Words(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Words(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultScorerLoadsEmbeddedData(t *testing.T) {
	s, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := DefaultScorer()
	if s != again {
		t.Error("DefaultScorer must load once and return the same Scorer")
	}
	if got := s.JudgeText("Het bestand wordt niet gelezen.", false); got != VerdictFinding {
		t.Errorf("Dutch sentence = %q, want finding", got)
	}
	// "gids" is on the jargon list: judged as English.
	if got := s.JudgeName("gids"); got != VerdictClean {
		t.Errorf("jargon name = %q, want clean", got)
	}
}

// Boundaries of the constants, on a table of its own: the shared fixtures sit away from them.
func TestScoreBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		table map[string][2]int
		fn    func(*Scorer) any
		want  any
	}{
		{"compound part at the floor (nl 25)", map[string][2]int{"werk": {0, 25}, "boom": {0, 25}},
			func(s *Scorer) any { return s.DutchSplit("werkboom") }, true},
		{"compound part below the floor (nl 24)", map[string][2]int{"werk": {0, 24}, "boom": {0, 25}},
			func(s *Scorer) any { return s.DutchSplit("werkboom") }, false},
		{"compound part must lean Dutch (nl == en)", map[string][2]int{"werk": {25, 25}, "boom": {0, 25}},
			func(s *Scorer) any { return s.DutchSplit("werkboom") }, false},
		{"doubt at lean 20 and nl 30", map[string][2]int{"legacy": {40, 10}, "lijst": {10, 30}},
			func(s *Scorer) any { return s.JudgeName("legacy_lijst") }, VerdictDoubt},
		{"no doubt at lean 19", map[string][2]int{"legacy": {40, 10}, "lijst": {21, 40}},
			func(s *Scorer) any { return s.JudgeName("legacy_lijst") }, VerdictClean},
		{"no doubt at nl 29", map[string][2]int{"legacy": {40, 10}, "lijst": {0, 29}},
			func(s *Scorer) any { return s.JudgeName("legacy_lijst") }, VerdictClean},
		{"float sum just above the threshold (-1.7 + 2.7 is 1.0000000000000002)",
			map[string][2]int{"config": {20, 3}, "geel": {0, 27}},
			func(s *Scorer) any { return s.JudgeText("config geel", false) }, VerdictFinding},
		{"float sum exactly at the threshold (0.4 + 0.8 - 0.2)",
			map[string][2]int{"aa": {0, 4}, "bb": {0, 8}, "cc": {2, 0}},
			func(s *Scorer) any { return s.JudgeText("aa bb cc", false) }, VerdictClean},
		{"participle of a word that is not English-leaning", map[string][2]int{"parsed": {20, 20}},
			func(s *Scorer) any { return s.DutchParticiple("geparsed") }, false},
		{"participle of an English-leaning word", map[string][2]int{"parsed": {21, 20}},
			func(s *Scorer) any { return s.DutchParticiple("geparsed") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(NewScorer(tt.table, nil)); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitNameAcronymBoundary(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"ABc", []string{"a", "bc"}},
		{"HTTPServer", []string{"http", "server"}},
		{"aB", []string{"a", "b"}},
		{"x2Y", []string{"x", "y"}},
		{"héllo", []string{"h", "llo"}},
	}
	for _, tt := range tests {
		if got := SplitName(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
