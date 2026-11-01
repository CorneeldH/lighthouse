package triage

import (
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

var testLabels = repoconfig.Labels{
	BlockedIssue: "3-blocked-issue", BlockedExternal: "3-blocked-external",
	NeedsShaping: "3-needs-shaping", NeedsDecision: "3-needs-decision",
	NeedsHands: "3-needs-hands", NeedsTesting: "3-needs-testing",
}

var testCfg = repoconfig.Triage{MilestonesOnly: true, WaitForNight: true, MaxForwarded: 5, MaxJudgments: 15}

// facts is a candidate that passes every rule.
func facts() Facts {
	return Facts{
		Number: 7, State: "open", Author: "someone", AuthorTrusted: true,
		Milestone: &forge.Milestone{Number: 1, Title: "M1", State: "open"},
	}
}

func trusted(body string) Comment { return Comment{ID: 1, Body: body, Trusted: true} }

func TestCandidate(t *testing.T) {
	closed := &forge.Milestone{Number: 1, State: "closed"}
	tests := []struct {
		name string
		mod  func(*Facts)
		cfg  repoconfig.Triage
		want Skip
	}{
		{"open milestone", func(*Facts) {}, testCfg, SkipNone},
		{"no milestone", func(f *Facts) { f.Milestone = nil }, testCfg, SkipMilestone},
		{"closed milestone", func(f *Facts) { f.Milestone = closed }, testCfg, SkipMilestone},
		{"no milestone, milestones_only off", func(f *Facts) { f.Milestone = nil }, repoconfig.Triage{}, SkipNone},
		{"closed milestone, milestones_only off", func(f *Facts) { f.Milestone = closed }, repoconfig.Triage{}, SkipMilestone},
		{"closed issue", func(f *Facts) { f.State = "closed" }, testCfg, SkipMilestone},
		{"status empty", func(f *Facts) { f.Status = "" }, testCfg, SkipNone},
		{"status To do", func(f *Facts) { f.Status = "To do" }, testCfg, SkipNone},
		{"status In progress", func(f *Facts) { f.Status = "In progress" }, testCfg, SkipStatus},
		{"status Controller", func(f *Facts) { f.Status = "Controller" }, testCfg, SkipStatus},
		{"status On hold", func(f *Facts) { f.Status = "On hold" }, testCfg, SkipStatus},
		{"lighthouse marker 4b", func(f *Facts) {
			f.Comments = []Comment{trusted(`<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`)}
		}, testCfg, SkipMarker},
		{"paia marker", func(f *Facts) {
			f.Comments = []Comment{trusted(`<!-- paia: {"soort":"triage","versie":1,"poort":"4b"} -->`)}
		}, testCfg, SkipMarker},
		{"paia-form lighthouse marker with a label3 stop", func(f *Facts) {
			f.Comments = []Comment{trusted(`<!-- lighthouse: {"kind":"triage","version":1,"gate":"4c","label3":"3-needs-decision"} -->`)}
			f.Labels = []string{"3-needs-decision"}
		}, testCfg, SkipMarker},
		{"stop marker with nothing left to hold", func(f *Facts) {
			f.Comments = []Comment{trusted(`<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b","blockers":[],"label3":null} -->`)}
		}, testCfg, SkipNone},
		{"trusted handoff", func(f *Facts) { f.Comments = []Comment{trusted(handoff)} }, testCfg, SkipChain},
		{"foreign handoff", func(f *Facts) { f.Comments = []Comment{{ID: 1, Body: handoff}} }, testCfg, SkipNone},
		{"running work", func(f *Facts) { f.Work = []string{"PR #3"} }, testCfg, SkipWork},
		{"lapsed marker", func(f *Facts) {
			f.Comments = []Comment{trusted(`<!-- lighthouse: {"kind":"triage","version":1,"gate":"0","blockers":[114],"label":null} -->`)}
			f.Blockers = []forge.Blocker{{Issue: 114, State: "CLOSED"}}
		}, testCfg, SkipNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := facts()
			tt.mod(&f)
			if got := Candidate(f, tt.cfg, testLabels); got != tt.want {
				t.Errorf("Candidate = %q, want %q", got, tt.want)
			}
		})
	}
}

const handoff = "🤖 Handoff\nSkill: x\n\nbody"

func TestCandidateIgnoresUntrustedMarker(t *testing.T) {
	f := facts()
	f.Comments = []Comment{{ID: 1, Body: `<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`}}
	if got := Candidate(f, testCfg, testLabels); got != SkipNone {
		t.Errorf("Candidate = %q, want none", got)
	}
}

func TestLastMarkerNewestTrustedWins(t *testing.T) {
	cs := []Comment{
		trusted(`<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`),
		{ID: 2, Body: `<!-- lighthouse: {"kind":"triage","version":1,"gate":"2"} -->`},
		{ID: 3, Body: "plain", Trusted: true},
	}
	m, ok := LastMarker(cs)
	if !ok || m.Gate != "4b" {
		t.Errorf("LastMarker = %+v, %v; want gate 4b", m, ok)
	}
	cs = append(cs, trusted(`<!-- lighthouse: {"kind":"triage","version":1,"gate":"2"} -->`))
	if m, _ := LastMarker(cs); m.Gate != "2" {
		t.Errorf("newest gate = %q, want 2", m.Gate)
	}
}

func TestMarkerHolds(t *testing.T) {
	g0 := protocolMarker("0", []int{114}, "")
	tests := []struct {
		name string
		m    markerCase
		mod  func(*Facts)
		want bool
	}{
		{"gate 0 blocker open", g0, func(f *Facts) { f.Blockers = []forge.Blocker{{Issue: 114, State: "OPEN"}} }, true},
		{"gate 0 blocker closed, no label", g0, func(f *Facts) { f.Blockers = []forge.Blocker{{Issue: 114, State: "CLOSED"}} }, false},
		{"gate 0 closed but label stays", protocolMarker("0", []int{114}, "3-blocked-external"),
			func(f *Facts) {
				f.Blockers = []forge.Blocker{{Issue: 114, State: "CLOSED"}}
				f.Labels = []string{"3-blocked-external"}
			}, true},
		{"invariant fixed", protocolMarker("invariant", nil, ""), func(f *Facts) { f.Labels = []string{"2-area:x"} }, false},
		{"invariant still broken", protocolMarker("invariant", nil, ""), func(f *Facts) { f.Labels = nil }, true},
		{"4c label stays", protocolMarker("4c", nil, "3-needs-decision"), func(f *Facts) { f.Labels = []string{"3-needs-decision"} }, true},
		{"4c label removed", protocolMarker("4c", nil, "3-needs-decision"), func(f *Facts) { f.Labels = nil }, false},
		{"needs-hands label lapsed", protocolMarker("4a", nil, "3-needs-hands"), func(f *Facts) { f.Labels = []string{"3-needs-hands"} }, false},
		{"needs-testing label lapsed", protocolMarker("4a", nil, "3-needs-testing"), func(f *Facts) { f.Labels = []string{"3-needs-testing"} }, false},
		{"no stop", markerCase{Gate: "4b"}, func(f *Facts) {}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := facts()
			tt.mod(&f)
			if got := MarkerHolds(tt.m.marker(), f, areaLabels()); got != tt.want {
				t.Errorf("MarkerHolds = %v, want %v", got, tt.want)
			}
		})
	}
}

func areaLabels() repoconfig.Labels {
	l := testLabels
	l.AreaPrefix = "2-area:"
	return l
}

func TestChanged(t *testing.T) {
	base := func() Facts {
		f := facts()
		f.Status = "To do"
		f.Labels = []string{"a", "b"}
		f.Comments = []Comment{{ID: 1}}
		f.UpdatedAt = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
		f.Work = []string{"PR #1"}
		return f
	}
	tests := []struct {
		name string
		mod  func(*Facts)
		want bool
	}{
		{"same", func(*Facts) {}, false},
		{"label order", func(f *Facts) { f.Labels = []string{"b", "a"} }, false},
		{"status", func(f *Facts) { f.Status = "Controller" }, true},
		{"label", func(f *Facts) { f.Labels = []string{"a", "c"} }, true},
		{"new comment", func(f *Facts) { f.Comments = append(f.Comments, Comment{ID: 2}) }, true},
		{"updated", func(f *Facts) { f.UpdatedAt = f.UpdatedAt.Add(time.Second) }, true},
		{"work", func(f *Facts) { f.Work = []string{"PR #2"} }, true},
		{"work added", func(f *Facts) { f.Work = append(f.Work, "run 3") }, true},
		{"state", func(f *Facts) { f.State = "closed" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			after := base()
			tt.mod(&after)
			if got := Changed(base(), after); got != tt.want {
				t.Errorf("Changed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangedDoesNotReorderInput(t *testing.T) {
	a, b := facts(), facts()
	a.Labels = []string{"b", "a"}
	Changed(a, b)
	if a.Labels[0] != "b" {
		t.Errorf("Changed sorted the caller's labels: %v", a.Labels)
	}
}

func TestSort(t *testing.T) {
	fs := []Facts{{Number: 3, Priority: "P2"}, {Number: 1}, {Number: 9, Priority: "P0"}, {Number: 4, Priority: "P0"}}
	Sort(fs)
	var got []int
	for _, f := range fs {
		got = append(got, f.Number)
	}
	want := []int{4, 9, 3, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
