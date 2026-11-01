package protocol

import (
	"reflect"
	"testing"
)

var triageRenders = []struct {
	name string
	m    TriageMarker
	want string
}{
	{"plain", TriageMarker{Gate: "4b"},
		`<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`},
	{"gate 0", TriageMarker{Gate: "0", Blockers: []int{114}, Stop: true},
		`<!-- lighthouse: {"kind":"triage","version":1,"gate":"0","blockers":[114],"label":null} -->`},
	{"label", TriageMarker{Gate: "4c", Label: "3-needs-decision", Stop: true},
		`<!-- lighthouse: {"kind":"triage","version":1,"gate":"4c","blockers":[],"label":"3-needs-decision"} -->`},
	{"invariant", TriageMarker{Gate: "invariant", Stop: true},
		`<!-- lighthouse: {"kind":"triage","version":1,"gate":"invariant"} -->`},
}

func TestRenderTriageMarker(t *testing.T) {
	for _, c := range triageRenders {
		t.Run(c.name, func(t *testing.T) {
			if got := RenderTriageMarker(c.m); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestParseTriageMarkerRoundTrip(t *testing.T) {
	for _, c := range triageRenders {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseTriageMarker(RenderTriageMarker(c.m) + "\n**Triage** · #1 Poort")
			if !ok || !reflect.DeepEqual(got, c.m) {
				t.Errorf("got %+v, %v; want %+v", got, ok, c.m)
			}
		})
	}
}

func TestParseTriageMarkerSpaced(t *testing.T) {
	cases := []struct {
		body string
		want TriageMarker
	}{
		{`<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "4b"} -->`,
			TriageMarker{Gate: "4b"}},
		{`<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "0", "blockers": [114], "label": null} -->`,
			TriageMarker{Gate: "0", Blockers: []int{114}, Stop: true}},
		{`<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "4c", "blockers": [], "label": "3-needs-decision"} -->`,
			TriageMarker{Gate: "4c", Label: "3-needs-decision", Stop: true}},
		{`<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "invariant"} -->`,
			TriageMarker{Gate: "invariant", Stop: true}},
	}
	for _, c := range cases {
		got, ok := ParseTriageMarker(c.body)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, %v; want %+v", c.body, got, ok, c.want)
		}
	}
}

func TestParseTriageMarkerPaia(t *testing.T) {
	cases := []struct {
		body string
		want TriageMarker
	}{
		{`<!-- paia: {"soort": "triage", "versie": 1, "poort": "0", "blokkers": [12], "label3": "3-blocked-issue"} -->`,
			TriageMarker{Gate: "0", Blockers: []int{12}, Label: "3-blocked-issue", Stop: true}},
		{`<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "4b", "blockers": [], "label3": "3-needs-hands"} -->`,
			TriageMarker{Gate: "4b", Label: "3-needs-hands", Stop: true}},
		{`<!-- paia: {"soort": "triage", "versie": 1, "poort": "4c", "label3": "3-needs-decision"} -->`,
			TriageMarker{Gate: "4c", Label: "3-needs-decision", Stop: true}},
	}
	for _, c := range cases {
		got, ok := ParseTriageMarker(c.body)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, %v; want %+v", c.body, got, ok, c.want)
		}
	}
}

func TestParseTriageMarkerRejects(t *testing.T) {
	cases := map[string]string{
		"artefact marker": `<!-- lighthouse: {"kind":"spec","version":1,"gate":"4b"} -->`,
		"version 2":       `<!-- lighthouse: {"kind":"triage","version":2,"gate":"4b"} -->`,
		"broken json":     `<!-- lighthouse: {"kind":"triage","version":1,"gate": -->`,
		"gate not string": `<!-- lighthouse: {"kind":"triage","version":1,"gate":4} -->`,
		"line two":        "intro\n" + `<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`,
		"blockers text":   `<!-- lighthouse: {"kind":"triage","version":1,"gate":"0","blockers":["x"]} -->`,
	}
	for name, body := range cases {
		if got, ok := ParseTriageMarker(body); ok {
			t.Errorf("%s: accepted as %+v", name, got)
		}
	}
}

func TestTriageCommentIsNotAnAnswer(t *testing.T) {
	body := RenderTriageMarker(TriageMarker{Gate: "4b"}) + "\n**Triage** · #1 Poort 4b: writing-plans"
	if _, ok := ParseHeader(body); ok {
		t.Error("ParseHeader accepted a triage comment")
	}
	if IsAnswer(body) {
		t.Error("IsAnswer accepted a triage comment")
	}
}
