package issuestate

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// keyLabels names every blocking label after its key, so a case can list keys as names.
var keyLabels = repoconfig.Labels{
	BlockedIssue: "blocked_issue", BlockedExternal: "blocked_external",
	NeedsMind: "needs_mind", NeedsHands: "needs_hands",
	LocationPrefix: "1-loc:", AreaPrefix: "2-area:",
}

type stateCase struct {
	Name     string   `json:"name"`
	Closed   bool     `json:"closed"`
	Labels   []string `json:"labels"`
	Status   string   `json:"status"`
	Run      string   `json:"run"`
	Blockers []struct {
		Issue int    `json:"issue"`
		State string `json:"state"`
	} `json:"blockers"`
	OpenQuestion bool `json:"open_question"`
	Want         struct {
		State string `json:"state"`
	} `json:"want"`
}

func TestOfCases(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/issue-state/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []stateCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases in the fixture")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			in := Input{Closed: c.Closed, Labels: c.Labels, Status: c.Status, Run: c.Run, OpenQuestion: c.OpenQuestion}
			for _, b := range c.Blockers {
				in.Blockers = append(in.Blockers, forge.Blocker{Issue: b.Issue, State: b.State})
			}
			want := Result{State: State(c.Want.State)}
			if got := Of(in, keyLabels); got != want {
				t.Errorf("Of = %+v, want %+v", got, want)
			}
		})
	}
}

func TestOfIgnoresKeysTheRepoLacks(t *testing.T) {
	// An empty label name is never present, even when the issue carries an empty label.
	got := Of(Input{Labels: []string{""}}, repoconfig.Labels{})
	if want := (Result{State: Ready}); got != want {
		t.Errorf("Of = %+v, want %+v", got, want)
	}
}

func TestGateZero(t *testing.T) {
	l := repoconfig.Labels{
		BlockedIssue: "3-blocked-issue", BlockedExternal: "3-blocked-external",
		NeedsMind: "3-needs-mind", NeedsHands: "3-needs-hands",
	}
	open5 := []forge.Blocker{{Issue: 5, State: "OPEN"}}
	tests := []struct {
		name     string
		labels   []string
		blockers []forge.Blocker
		want     Stop
		stop     bool
	}{
		{"external label", []string{"3-blocked-external"}, nil, Stop{Label: "3-blocked-external"}, true},
		{"open blocker no label", nil, open5, Stop{Blockers: []int{5}}, true},
		{"open blocker with needs-mind", []string{"3-needs-mind"}, open5, Stop{}, false},
		{"blocked-issue with needs-mind routes on", []string{"3-needs-mind", "3-blocked-issue"}, nil, Stop{}, false},
		{"blocked-external with needs-mind routes on", []string{"3-needs-mind", "3-blocked-external"}, open5, Stop{}, false},
		{"open blocker with blocked-issue", []string{"3-blocked-issue"}, open5, Stop{Blockers: []int{5}, Label: "3-blocked-issue"}, true},
		{"closed blockers only", nil, []forge.Blocker{{Issue: 5, State: "CLOSED"}}, Stop{}, false},
		{"nothing", nil, nil, Stop{}, false},
		{"blocked-issue before blocked-external", []string{"3-blocked-external", "3-blocked-issue"}, nil, Stop{Label: "3-blocked-issue"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, stop := GateZero(tt.labels, tt.blockers, l)
			if stop != tt.stop || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GateZero = %+v, %v; want %+v, %v", got, stop, tt.want, tt.stop)
			}
		})
	}
}

func TestOpenBlockers(t *testing.T) {
	tests := []struct {
		name string
		in   []forge.Blocker
		want []int
	}{
		{"mixed", []forge.Blocker{{Issue: 5, State: "OPEN"}, {Issue: 6, State: "CLOSED"}, {Issue: 7, State: "OPEN"}}, []int{5, 7}},
		{"none", nil, nil},
		{"closed only", []forge.Blocker{{Issue: 6, State: "CLOSED"}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OpenBlockers(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OpenBlockers = %#v, want %#v", got, tt.want)
			}
		})
	}
}
