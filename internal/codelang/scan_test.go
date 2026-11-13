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
		if c.Language == "python" {
			continue // the Python scanner arrives in a later task
		}
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
