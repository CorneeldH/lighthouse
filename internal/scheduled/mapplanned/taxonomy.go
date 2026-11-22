// Package mapplanned holds the parts of the map-planned job.
package mapplanned

import (
	"bytes"
	"fmt"
	"strings"
)

// Planned says on which side a label creates entries on the map.
type Planned string

const (
	// PlannedAlways means every issue with the label is mapped.
	PlannedAlways Planned = "always"
	// PlannedMilestone means only issues with a milestone are mapped.
	PlannedMilestone Planned = "milestone"
)

// Row is one label with the document that maps it and when it is planned.
type Row struct {
	Label, Document string
	Planned         Planned
}

// Rows reads every row with a Document and a Planned value from the Markdown tables of md
// whose header has the columns Label, Document and Planned. Backticks around a cell go.
func Rows(md []byte) ([]Row, error) {
	var rows []Row
	label, doc, planned := -1, -1, -1
	inTable := false
	for _, line := range strings.Split(string(bytes.TrimSpace(md)), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			inTable = false
			continue
		}
		cells := splitRow(line)
		if !inTable {
			inTable = true
			label, doc, planned = column(cells, "Label"), column(cells, "Document"), column(cells, "Planned")
			continue
		}
		if label < 0 || doc < 0 || planned < 0 || isSeparator(cells) {
			continue
		}
		if len(cells) <= max(label, doc, planned) {
			continue
		}
		p, d := cells[planned], cells[doc]
		if p == "" || d == "" {
			continue
		}
		switch Planned(p) {
		case PlannedAlways, PlannedMilestone:
		default:
			return nil, fmt.Errorf("label %q: unknown Planned value %q (want %q or %q)", cells[label], p, PlannedAlways, PlannedMilestone)
		}
		rows = append(rows, Row{Label: cells[label], Document: d, Planned: Planned(p)})
	}
	return rows, nil
}

// splitRow splits a table line into its trimmed cells, on unescaped pipes only: `\|` is a pipe
// inside a cell and stays in it as `|`. The outer pipes of the line make no cell.
func splitRow(line string) []string {
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cell.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, cell.String())
			cell.Reset()
		default:
			cell.WriteByte(line[i])
		}
	}
	cells = append(cells, cell.String())
	// The line starts with a pipe, so the first cell is the empty text before it; a closing pipe
	// leaves an empty last one.
	cells = cells[1:]
	if n := len(cells); n > 0 && cells[n-1] == "" {
		cells = cells[:n-1]
	}
	for i, c := range cells {
		cells[i] = strings.Trim(strings.TrimSpace(c), "`")
	}
	return cells
}

func column(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}
