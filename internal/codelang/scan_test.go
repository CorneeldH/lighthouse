package codelang

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// wantFinding is one expected row, [line, kind, text, doubt], of scan.json.
type wantFinding struct {
	Line  int
	Kind  string
	Text  string
	Doubt bool
}

func (w *wantFinding) UnmarshalJSON(raw []byte) error {
	var row []any
	if err := json.Unmarshal(raw, &row); err != nil {
		return err
	}
	if len(row) != 4 {
		return errors.New("a want row has four fields")
	}
	line, _ := row[0].(float64)
	kind, _ := row[1].(string)
	text, _ := row[2].(string)
	doubt, _ := row[3].(bool)
	*w = wantFinding{int(line), kind, text, doubt}
	return nil
}

func TestScanFixture(t *testing.T) {
	var cases []struct {
		Language    string        `json:"lang"`
		Name        string        `json:"name"`
		Source      string        `json:"source"`
		Want        []wantFinding `json:"want"`
		CannotCheck bool          `json:"cannot_check"`
	}
	readFixture(t, "scan.json", &cases)
	scorer, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Language+"/"+c.Name, func(t *testing.T) {
			found, err := Scan(scorer, c.Language, c.Source)
			if c.CannotCheck {
				var cc *CannotCheckError
				if !errors.As(err, &cc) {
					t.Fatalf("want a CannotCheckError, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := []wantFinding{}
			for _, f := range found {
				got = append(got, wantFinding{f.Line, f.Kind, f.Text, f.Doubt})
			}
			want := c.Want
			if want == nil {
				want = []wantFinding{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestScanUnknownLanguage(t *testing.T) {
	scorer, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(scorer, "cobol", "x"); err == nil {
		t.Fatal("an unknown language must be an error")
	}
}

// A name on a marked line is not even judged: it produces no unit, so layer 1 is not asked.
func TestMarkedNameProducesNoUnit(t *testing.T) {
	src := "lees_bestand=1 # lighthouse: allow-language\nresultaat=2\n"
	units := scanShell(src)
	var names []string
	for _, u := range units {
		if u.Name {
			names = append(names, u.Text)
		}
	}
	if !reflect.DeepEqual(names, []string{"resultaat"}) {
		t.Errorf("names = %v, want [resultaat]", names)
	}
}

// A comment block keeps the line, text and body Python's Finding carried, marked lines out.
func TestCommentUnitShape(t *testing.T) {
	units := scanGo("package p\n\n// first\n// dit is niet goed // lighthouse: allow-language\nvar x int\n")
	var got []Unit
	for _, u := range units {
		if u.Kind == "comment" {
			got = append(got, u)
		}
	}
	want := []Unit{{Line: 3, Kind: "comment", Text: "first", Body: "first", LineBreaks: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMarkdownUnitShape(t *testing.T) {
	units := scanMarkdown("# Titel [link](x.md)\n\nDit is een alinea\nover twee regels.\n")
	want := []Unit{
		{Line: 1, Kind: "prose", Text: "Titel link", Body: "Titel link"},
		{Line: 3, Kind: "prose", Text: "Dit is een alinea over twee regels.", Body: "Dit is een alinea over twee regels."},
	}
	if !reflect.DeepEqual(units, want) {
		t.Errorf("got %+v, want %+v", units, want)
	}
}

// The hand-written lookarounds and offsets, as a table with literal expectations.
func TestScanLookaroundEdges(t *testing.T) {
	tests := []struct {
		name string
		scan func(string) []Unit
		src  string
		want []string // names, in order
	}{
		{"qualified assignment is no new name", scanGo, "package p\n\nfunc f() {\n\ta.b := 1\n}\n", []string{"p", "f"}},
		{"multibyte word rune before :=", scanGo, "package p\n\nfunc f() {\n\téx := 1\n}\n", []string{"p", "f"}},
		{"ASCII start after a space is a name", scanGo, "package p\n\nfunc f() {\n\té x := 1\n}\n", []string{"p", "f", "x"}},
		{"multibyte word rune before func(", scanGo, "package p\n\nvar v = éfunc(a b)\n", []string{"p", "v"}},
		{"multibyte word rune before struct{", scanGo, "package p\n\nvar v = éstruct{\n\ta b\n}\n", []string{"p", "v"}},
		{"xfunc( is no func literal", scanGo, "package p\n\nvar v = xfunc(a b)\n", []string{"p", "v"}},
		{"xstruct{ is no struct", scanGo, "package p\n\nvar v = xstruct{\n\ta b\n}\n", []string{"p", "v"}},
		{"struct{ has fields", scanGo, "package p\n\nvar v = struct{\n\ta int\n}{}\n", []string{"p", "v", "a"}},
		{"localx is no declaration", scanShell, "localx=1\n", []string{"localx"}},
		{"keyword inside a longer word", scanShell, "localx abc=1\nlocalé=2\n", []string{"localé"}},
		{"local declares", scanShell, "local abc def=1\n", []string{"abc", "def"}},
		{"unterminated block comment", scanGo, "package p\n\nvar a int\n/* open", []string{"p", "a"}},
		{"CRLF input", scanShell, "abc=1\r\ndef=2\r\n", []string{"abc", "def"}},
		{"CRLF input in Go", scanGo, "package p\r\n\r\nvar a int\r\n", []string{"p", "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for _, u := range tt.scan(tt.src) {
				if u.Name {
					got = append(got, u.Text)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("names = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindingBodyFallsBackToText(t *testing.T) {
	scorer, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	found := judge(scorer, []Unit{
		{Line: 1, Kind: "name", Text: "lees_bestand", Name: true},
		{Line: 2, Kind: "comment", Text: "dit is niet goed", Body: "dit is niet goed", LineBreaks: true},
	})
	if len(found) != 2 {
		t.Fatalf("found %d, want 2", len(found))
	}
	if found[0].Body != "lees_bestand" || found[1].Body != "dit is niet goed" {
		t.Errorf("bodies = %q, %q", found[0].Body, found[1].Body)
	}
}
