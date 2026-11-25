// Package labelmigrate migrates a repository from the old human labels (needs_shaping,
// needs_decision, needs_testing) to needs_mind and needs_hands. This file rewrites the
// taxonomy file and the [labels] table of lighthouse.toml; the old names appear here on purpose.
package labelmigrate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/labels"
)

// MindColor is the colour of the needs_mind label.
const MindColor = "FBCA04"

// OldNames are the forge names of the three retired labels.
type OldNames struct{ Shaping, Decision, Testing string }

// OldDefaults are the built-in names the retired labels had without a [labels] table.
var OldDefaults = OldNames{"needs-shaping", "needs-decision", "needs-testing"}

// LabelsKeys says which keys [labels] holds: any of the old ones, needs_mind, needs_hands. Like
// ReadOldNames, ReadLabelsKeys reads raw.
type LabelsKeys struct{ Old, Mind, Hands bool }

// ReadLabelsKeys reads which of the old and new human-label keys [labels] of tomlPath sets.
func ReadLabelsKeys(tomlPath string) (LabelsKeys, error) {
	var raw struct {
		Labels map[string]any `toml:"labels"`
	}
	if _, err := toml.DecodeFile(tomlPath, &raw); err != nil {
		return LabelsKeys{}, fmt.Errorf("labelmigrate: cannot read %s: %w", tomlPath, err)
	}
	_, s := raw.Labels["needs_shaping"]
	_, d := raw.Labels["needs_decision"]
	_, t := raw.Labels["needs_testing"]
	_, m := raw.Labels["needs_mind"]
	_, h := raw.Labels["needs_hands"]
	return LabelsKeys{Old: s || d || t, Mind: m, Hands: h}, nil
}

// ReadOldNames reads the old keys of [labels] raw, without the refusal config.Load applies;
// a missing key gets its old default. A missing or unreadable file is an error.
func ReadOldNames(tomlPath string) (OldNames, error) {
	var raw struct {
		Labels struct {
			Shaping  string `toml:"needs_shaping"`
			Decision string `toml:"needs_decision"`
			Testing  string `toml:"needs_testing"`
		} `toml:"labels"`
	}
	if _, err := toml.DecodeFile(tomlPath, &raw); err != nil {
		return OldNames{}, fmt.Errorf("labelmigrate: cannot read %s: %w", tomlPath, err)
	}
	o := OldDefaults
	if v := raw.Labels.Shaping; v != "" {
		o.Shaping = v
	}
	if v := raw.Labels.Decision; v != "" {
		o.Decision = v
	}
	if v := raw.Labels.Testing; v != "" {
		o.Testing = v
	}
	return o, nil
}

// replaceCell replaces the first occurrence of from in cell, leaving the padding around it.
func replaceCell(cell, from, to string) string {
	return strings.Replace(cell, from, to, 1)
}

// RewriteTaxonomy merges, in every table with a label and a colour column, the rows of
// old.Shaping and old.Decision into one `mind` row with colour MindColor, and drops the row
// of old.Testing (renaming it to hands when the table has no hands row). Everything else stays
// byte for byte; without an old row the input comes back unchanged.
func RewriteTaxonomy(src []byte, old OldNames, mind, hands string) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	rows := labels.ParseRows(string(src))

	hasName := map[int]map[string]bool{} // table -> lower-cased names
	for _, r := range rows {
		if hasName[r.Table] == nil {
			hasName[r.Table] = map[string]bool{}
		}
		hasName[r.Table][strings.ToLower(r.Name)] = true
	}
	drop := map[int]bool{}
	mindDone := map[int]bool{}
	for _, r := range rows {
		isOld := strings.EqualFold(r.Name, old.Shaping) || strings.EqualFold(r.Name, old.Decision)
		switch {
		case isOld:
			if mindDone[r.Table] || hasName[r.Table][strings.ToLower(mind)] {
				drop[r.Line] = true
				continue
			}
			mindDone[r.Table] = true
			parts := strings.Split(lines[r.Line], "|")
			parts[r.NameCol+1] = replaceCell(parts[r.NameCol+1], "`"+r.Name+"`", "`"+mind+"`")
			c := parts[r.ColorCol+1]
			parts[r.ColorCol+1] = replaceCell(c, strings.TrimSpace(c), "`"+MindColor+"`")
			lines[r.Line] = strings.Join(parts, "|")
		case strings.EqualFold(r.Name, old.Testing):
			if hasName[r.Table][strings.ToLower(hands)] {
				drop[r.Line] = true
				continue
			}
			parts := strings.Split(lines[r.Line], "|")
			parts[r.NameCol+1] = replaceCell(parts[r.NameCol+1], "`"+r.Name+"`", "`"+hands+"`")
			lines[r.Line] = strings.Join(parts, "|")
		}
	}
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if !drop[i] {
			out = append(out, l)
		}
	}
	return []byte(strings.Join(out, "\n")), nil
}

var oldKeyRE = regexp.MustCompile(`^\s*(needs_shaping|needs_decision|needs_testing)\s*=`)
var mindKeyRE = regexp.MustCompile(`^\s*needs_mind\s*=`)
var handsKeyRE = regexp.MustCompile(`^\s*needs_hands\s*=`)

// tableHeader reports whether the trimmed line t is a table header ([x] or [[x]], with an
// optional trailing comment) and whether that header is [labels].
func tableHeader(t string) (isLabels, ok bool) {
	if !strings.HasPrefix(t, "[") {
		return false, false
	}
	if i := strings.Index(t, "]"); i > 0 {
		inner := strings.TrimSpace(t[1:i])
		return !strings.HasPrefix(t, "[[") && inner == "labels", true
	}
	return false, true
}

// RewriteLabelsTable replaces, in [labels], the lines of needs_shaping, needs_decision and
// needs_testing by one needs_mind = "<mind>" at the place of the first of them; other lines and
// comments stay, and other tables are not touched. When needs_mind is already there the old lines
// are only removed.
func RewriteLabelsTable(src []byte, mind string) ([]byte, error) {
	return RewriteLabelsTableHands(src, mind, "")
}

// RewriteLabelsTableHands is RewriteLabelsTable that, when hands is not empty and [labels] has no
// needs_hands, also writes needs_hands = "<hands>" at the first old line it removes.
func RewriteLabelsTableHands(src []byte, mind, hands string) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	inLabels := false
	hasMind, hasHands := false, false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if isLabelsHeader, ok := tableHeader(t); ok {
			inLabels = isLabelsHeader
			continue
		}
		if inLabels && mindKeyRE.MatchString(l) {
			hasMind = true
		}
		if inLabels && handsKeyRE.MatchString(l) {
			hasHands = true
		}
	}
	out := make([]string, 0, len(lines))
	inLabels = false
	placed := hasMind
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if isLabelsHeader, ok := tableHeader(t); ok {
			inLabels = isLabelsHeader
		} else if inLabels && oldKeyRE.MatchString(l) {
			if !placed {
				out = append(out, fmt.Sprintf("needs_mind = %q", mind))
				placed = true
			}
			if hands != "" && !hasHands {
				out = append(out, fmt.Sprintf("needs_hands = %q", hands))
				hasHands = true
			}
			continue
		}
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n")), nil
}
