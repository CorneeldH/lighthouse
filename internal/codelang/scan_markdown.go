package codelang

import (
	"regexp"
	"strings"
)

var (
	fenceRE          = regexp.MustCompile("^[" + pySpace + "]*(```|~~~)")
	headingRE        = regexp.MustCompile(`^#{1,6}[ \t]`)
	listItemRE       = regexp.MustCompile(`^[` + pySpace + `]*(?:[-*+]|\p{Nd}+[.)])[ \t]+(.*)`)
	tableSeparatorRE = regexp.MustCompile(`^[` + pySpace + `|:\-]+$`)
	frontKeyRE       = regexp.MustCompile(`^([\p{L}\p{N}_-]+):[ \t]*(.*)$`)
	htmlCommentRE    = regexp.MustCompile(`(?s)<!--.*?-->`)
	urlInAnglesRE    = regexp.MustCompile(`<https?://[^>]*>`)
	imageRE          = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	linkRE           = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	htmlTagRE        = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	folded           = map[string]bool{"": true, ">": true, "|": true, ">-": true, "|-": true}
)

// proseUnit makes a unit of numbered lines, or none when nothing is left; marked lines do not
// count.
func proseUnit(lines []numbered) (Unit, bool) {
	var kept []numbered
	var texts []string
	for _, l := range lines {
		if !strings.Contains(l.text, marker) {
			kept = append(kept, l)
			texts = append(texts, trimSpace(l.text))
		}
	}
	text := strings.Join(texts, " ")
	text = htmlCommentRE.ReplaceAllString(text, " ")
	text = urlInAnglesRE.ReplaceAllString(text, " ")
	text = imageRE.ReplaceAllString(text, "${1}")
	text = linkRE.ReplaceAllString(text, "${1}")
	text = trimSpace(htmlTagRE.ReplaceAllString(text, " "))
	if len(kept) == 0 || text == "" {
		return Unit{}, false
	}
	return Unit{Line: kept[0].line, Kind: "prose", Text: text, Body: text}, true
}

// frontmatterUnits returns the units of the frontmatter and the index of the first body line.
func frontmatterUnits(lines []string) ([][]numbered, int) {
	if len(lines) == 0 || trimSpace(lines[0]) != "---" {
		return nil, 0
	}
	var units [][]numbered
	for index := 1; index < len(lines); index++ {
		line, number := lines[index], index+1
		if trimSpace(line) == "---" {
			return units, index + 1
		}
		if trimSpace(line) == "" {
			continue
		}
		key := frontKeyRE.FindStringSubmatch(line)
		trimmedLeft := strings.TrimLeftFunc(line, isPySpace)
		switch {
		case (line[0] == ' ' || line[0] == '\t') && len(units) > 0:
			units[len(units)-1] = append(units[len(units)-1], numbered{number, line})
		case key != nil:
			raw := trimSpace(key[2])
			if folded[raw] {
				units = append(units, []numbered{{number, ""}})
			} else {
				units = append(units, []numbered{{number, strings.Trim(raw, "\"'")}})
			}
		case strings.HasPrefix(trimmedLeft, "- "):
			units = append(units, []numbered{{number, strings.Trim(trimSpace(trimmedLeft[2:]), "\"'")}})
		case len(units) > 0:
			units[len(units)-1] = append(units[len(units)-1], numbered{number, line})
		}
	}
	return units, len(lines) // never closed: all of it was frontmatter
}

// scanMarkdown returns the units of the prose of Markdown source. It is a line scanner, not
// a parser. Code (fenced or indented blocks, inline code), link targets, URLs and HTML are not
// language. Frontmatter values, headings, list items, table cells and paragraphs are the units.
func scanMarkdown(source string) []Unit {
	lines := splitLines(source)
	units, start := frontmatterUnits(lines)
	var paragraph []numbered
	fence := ""
	inList, inCode, previousBlank := false, false, true
	flush := func() {
		if len(paragraph) > 0 {
			units = append(units, paragraph)
		}
		paragraph = nil
	}
	for index := start; index < len(lines); index++ {
		line, number := lines[index], index+1
		blank := trimSpace(line) == ""
		if fence != "" {
			if strings.HasPrefix(trimSpace(line), fence) {
				fence = ""
			}
			continue
		}
		if inCode {
			if blank || line[0] == ' ' || line[0] == '\t' {
				continue
			}
			inCode = false
		}
		if opening := fenceRE.FindStringSubmatch(line); opening != nil {
			flush()
			fence = opening[1]
			continue
		}
		if blank {
			flush()
			previousBlank = true
			continue
		}
		if previousBlank && !inList && (strings.HasPrefix(line, "    ") || line[0] == '\t') {
			inCode = true
			continue
		}
		previousBlank = false
		item := listItemRE.FindStringSubmatch(line)
		switch {
		case headingRE.MatchString(line):
			flush()
			units = append(units, []numbered{{number, strings.TrimLeft(line, "#")}})
			inList = false
		case item != nil:
			flush()
			paragraph = []numbered{{number, item[1]}}
			inList = true
		case strings.HasPrefix(strings.TrimLeftFunc(line, isPySpace), "|"):
			flush()
			if !tableSeparatorRE.MatchString(line) {
				for _, cell := range strings.Split(strings.Trim(trimSpace(line), "|"), "|") {
					if trimSpace(cell) != "" {
						units = append(units, []numbered{{number, cell}})
					}
				}
			}
		default:
			if (line[0] != ' ' && line[0] != '\t') || !inList {
				inList = false
			}
			paragraph = append(paragraph, numbered{number, line})
		}
	}
	flush()
	var found []Unit
	for _, u := range units {
		if unit, ok := proseUnit(u); ok {
			found = append(found, unit)
		}
	}
	return found
}
