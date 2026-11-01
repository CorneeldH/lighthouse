package triage

import (
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

type markerCase struct {
	Gate     string
	Blockers []int
	Label    string
}

func protocolMarker(gate string, blockers []int, label string) markerCase {
	return markerCase{gate, blockers, label}
}

func (m markerCase) marker() protocol.TriageMarker {
	return protocol.TriageMarker{
		Gate: m.Gate, Blockers: m.Blockers, Label: m.Label,
		Stop: m.Gate == "0" || m.Gate == "invariant" || m.Label != "",
	}
}

func TestFixedGate0(t *testing.T) {
	open5 := []forge.Blocker{{Issue: 5, State: "OPEN"}}
	tests := []struct {
		name     string
		labels   []string
		blockers []forge.Blocker
		want     Gate0
		stop     bool
	}{
		{"external label", []string{"3-blocked-external"}, nil, Gate0{Label: "3-blocked-external"}, true},
		{"open blocker no label", nil, open5, Gate0{Blockers: []int{5}}, true},
		{"open blocker with needs-decision", []string{"3-needs-decision"}, open5, Gate0{}, false},
		{"open blocker with needs-shaping", []string{"3-needs-shaping"}, open5, Gate0{}, false},
		{"open blocker with blocked-issue", []string{"3-blocked-issue"}, open5, Gate0{Blockers: []int{5}, Label: "3-blocked-issue"}, true},
		{"closed blockers only", nil, []forge.Blocker{{Issue: 5, State: "CLOSED"}}, Gate0{}, false},
		{"nothing", nil, nil, Gate0{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := facts()
			f.Labels, f.Blockers = tt.labels, tt.blockers
			got, stop := FixedGate0(f, testLabels)
			if stop != tt.stop || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FixedGate0 = %+v, %v; want %+v, %v", got, stop, tt.want, tt.stop)
			}
		})
	}
}

func TestInvariant(t *testing.T) {
	area := repoconfig.Labels{AreaPrefix: "2-area:"}
	loc := repoconfig.Labels{LocationPrefix: "1-loc:"}
	tests := []struct {
		name   string
		labels []string
		l      repoconfig.Labels
		want   []Deviation
	}{
		{"area missing", nil, area, []Deviation{{Kind: "missing", Prefix: "2-area:"}}},
		{"area twice", []string{"2-area:a", "2-area:b"}, area, []Deviation{{Kind: "many", Prefix: "2-area:", Found: []string{"2-area:a", "2-area:b"}}}},
		{"area once", []string{"2-area:a"}, area, nil},
		{"location missing", []string{"other"}, loc, []Deviation{{Kind: "missing", Prefix: "1-loc:"}}},
		{"no prefixes", nil, repoconfig.Labels{}, nil},
		{"two blocking", []string{"3-needs-hands", "3-needs-testing"}, testLabels,
			[]Deviation{{Kind: "blocking", Found: []string{"3-needs-hands", "3-needs-testing"}}}},
		{"one blocking", []string{"3-needs-hands"}, testLabels, nil},
		{"empty label names are no blocking labels", []string{"x"}, repoconfig.Labels{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Invariant(tt.labels, tt.l); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Invariant = %+v, want %+v", got, tt.want)
			}
		})
	}
}
