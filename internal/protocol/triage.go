package protocol

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// TriageMarker is the first line of a triage comment (spec "Marker").
type TriageMarker struct {
	Gate     string // "0", "1", "2", "4a", "4b", "4c" or "invariant"
	Blockers []int  // gate 0: the blocked_by numbers
	Label    string // the stop label; "" is null. label3 of the older forms lands here
	Stop     bool   // the marker carries a stop: gate 0, invariant, or a label
}

var triageMarkerLine = regexp.MustCompile(`^<!-- (lighthouse|paia): (\{.*\}) -->$`)

// triageKeys are the JSON keys of one marker form.
type triageKeys struct{ kind, version, gate, blockers string }

var (
	triageKeysEN = triageKeys{"kind", "version", "gate", "blockers"}
	triageKeysNL = triageKeys{"soort", "versie", "poort", "blokkers"}
)

// ParseTriageMarker reads the marker on the first line of a comment body, in the lighthouse
// form and in both forms of paia. It reports false for any other first line, another kind,
// another version or a field of the wrong type.
func ParseTriageMarker(body string) (TriageMarker, bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	sub := triageMarkerLine.FindStringSubmatch(strings.TrimSpace(first))
	if sub == nil {
		return TriageMarker{}, false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(sub[2]), &raw) != nil {
		return TriageMarker{}, false
	}
	keys := triageKeysEN
	if sub[1] == "paia" {
		keys = triageKeysNL
	}
	var kind string
	var version int
	var m TriageMarker
	if json.Unmarshal(raw[keys.kind], &kind) != nil || kind != "triage" ||
		json.Unmarshal(raw[keys.version], &version) != nil || version != 1 ||
		json.Unmarshal(raw[keys.gate], &m.Gate) != nil || m.Gate == "" {
		return TriageMarker{}, false
	}
	// The older forms say label3 and the newer one label; either may be null.
	for _, key := range []string{"label", "label3"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		var label *string
		if json.Unmarshal(v, &label) != nil {
			return TriageMarker{}, false
		}
		m.Stop = true
		if label != nil {
			m.Label = *label
		}
	}
	if v, ok := raw[keys.blockers]; ok {
		if json.Unmarshal(v, &m.Blockers) != nil {
			return TriageMarker{}, false
		}
		if len(m.Blockers) == 0 {
			m.Blockers = nil
		}
		m.Stop = true
	}
	if m.Gate == "invariant" {
		m.Stop = true
	}
	return m, true
}

// RenderTriageMarker writes the compact marker line, the form artefacts.py writes. A marker
// with a stop carries blockers and label, except the invariant, which carries neither.
func RenderTriageMarker(m TriageMarker) string {
	type head struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
		Gate    string `json:"gate"`
	}
	type stop struct {
		head
		Blockers []int   `json:"blockers"`
		Label    *string `json:"label"`
	}
	h := head{"triage", 1, m.Gate}
	var v any = h
	if m.Stop && m.Gate != "invariant" {
		s := stop{head: h, Blockers: m.Blockers}
		if s.Blockers == nil {
			s.Blockers = []int{}
		}
		if m.Label != "" {
			s.Label = &m.Label
		}
		v = s
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // plain structs always encode
	return "<!-- lighthouse: " + strings.TrimSuffix(buf.String(), "\n") + " -->"
}
