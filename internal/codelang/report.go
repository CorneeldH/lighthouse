package codelang

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// maxLines is the number of findings the report lists; the rest is counted.
	maxLines = 20
	// textLimit cuts the text of a block in the report, in runes.
	textLimit = 60
)

// reportLine is "<path>:<line>: <kind>: <text>", the text of a block cut to textLimit runes.
func reportLine(path string, f Finding) string {
	text := f.Text
	if f.Kind != "name" && utf8.RuneCountInString(text) > textLimit {
		text = string([]rune(text)[:textLimit])
	}
	return fmt.Sprintf("%s:%d: %s: %s", path, f.Line, f.Kind, text)
}

// notice says that layer 2 gave no verdict, so these doubts stay unchecked.
func notice(path string, doubts []Finding, reasons, prefix string) string {
	lines := []string{fmt.Sprintf("code-language-check: %slayer 2 did not answer (%s); %d doubtful units not checked:",
		prefix, reasons, len(doubts))}
	for _, d := range doubts {
		lines = append(lines, reportLine(path, d))
	}
	return strings.Join(lines, "\n")
}

// Resolve returns every finding (layer 1, plus each doubt layer 2 calls NOT-EN) and the
// notice, or "". Without doubts or without a chain it is layer 1 alone, silently. Under
// bash-write-check layer 2 does not run and the doubts go into a NOTICE. Layer 2 only hears
// the doubts, never a layer 1 finding.
func Resolve(ctx context.Context, in Inspection, path string, underGates bool, stateDir string) ([]Finding, string) {
	found := append([]Finding{}, in.Findings...)
	if len(in.Doubts) == 0 || in.Chain == nil {
		return found, ""
	}
	if underGates {
		return found, notice(path, in.Doubts, "layer 2 skipped under bash-write-check", "NOTICE — ")
	}
	asked := make([][2]string, len(in.Doubts))
	for i, d := range in.Doubts {
		asked[i] = [2]string{d.Kind, d.Body}
	}
	reply := Ask(ctx, in.Chain, asked, stateDir, PerModel, Total)
	if reply.NotEnglish == nil {
		return found, notice(path, in.Doubts, strings.Join(reply.Failures, "; "), "")
	}
	for i, d := range in.Doubts {
		if reply.NotEnglish[i] {
			d.Layer = "layer 2 (" + reply.Model + ")"
			d.Doubt = false
			found = append(found, d)
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Line < found[j].Line })
	return found, ""
}

// Report is the block a finding prints: the findings, at most maxLines of them, the rule, and
// why the marker cannot clear a module or directory name.
func Report(path string, found []Finding) string {
	lines := []string{fmt.Sprintf("code-language-check: %s is not English", path)}
	for _, f := range found[:min(len(found), maxLines)] {
		lines = append(lines, fmt.Sprintf("%s [%s]", reportLine(path, f), f.Layer))
	}
	if len(found) > maxLines {
		lines = append(lines, fmt.Sprintf("… and %d more", len(found)-maxLines))
	}
	lines = append(lines, "The whole file must be English. A line that is deliberately not English "+
		`gets "`+marker+`".`)
	_, stem := nameParts(filepath.Base(path))
	for _, f := range found {
		if f.Line == 1 && f.Kind == "name" && f.Text == stem {
			lines = append(lines, fmt.Sprintf("The module name '%s' is not English either; the marker cannot clear it: "+
				"rename the file or add it to `code_language_exempt`.", stem))
			break
		}
	}
	for _, f := range found {
		if f.Kind == "directory" {
			lines = append(lines, fmt.Sprintf("The directory name '%s' is not English either; the marker cannot "+
				"clear it: rename the file or its directory, or add the file to `code_language_exempt`.", f.Text))
		}
	}
	return strings.Join(lines, "\n")
}
