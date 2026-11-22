package mapplanned

import (
	"reflect"
	"strings"
	"testing"
)

// Classification: pure function. Fails if Rows stops reading columns by header name, keeps
// backticks around cells, or drops the row of the free-label table.
func TestRowsReadsTheFreeLabelTable(t *testing.T) {
	md := "| Label | Kleur | When | Document | Planned |\n" +
		"|---|---|---|---|---|\n" +
		"| `loop` | `F4D74B` | Voegt iets toe | `docs/site/data/loop.json` | `milestone` |\n"
	got, err := Rows([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{{"loop", "docs/site/data/loop.json", PlannedMilestone}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Classification: pure function. Fails if Rows accepts a table without both the Document and
// Planned columns.
func TestRowsSkipsTablesWithoutTheColumns(t *testing.T) {
	md := "| Area | Kleur | Vuistregel |\n|---|---|---|\n| `cli` | `AAAAAA` | x |\n\n" +
		"| Label | Document |\n|---|---|\n| `loop` | `a.json` |\n"
	got, err := Rows([]byte(md))
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

// Classification: pure function. Fails if a row with an empty Planned cell counts.
func TestRowsSkipsAnEmptyPlannedCell(t *testing.T) {
	md := "| Label | Document | Planned |\n|---|---|---|\n| `loop` | `a.json` |  |\n| `kaart` | `b.yaml` | `always` |\n"
	got, err := Rows([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{{"kaart", "b.yaml", PlannedAlways}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Classification: pure function. Fails if an unknown Planned value is accepted or the error
// leaves out the value or the label.
func TestRowsRefusesAnUnknownPlannedValue(t *testing.T) {
	md := "| Label | Document | Planned |\n|---|---|---|\n| `loop` | `a.json` | `soms` |\n"
	_, err := Rows([]byte(md))
	if err == nil || !strings.Contains(err.Error(), "soms") || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("got %v, want an error naming soms and loop", err)
	}
}
