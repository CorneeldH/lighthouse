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
	NeedsShaping: "needs_shaping", NeedsDecision: "needs_decision",
	NeedsHands: "needs_hands", NeedsTesting: "needs_testing",
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
		State      string `json:"state"`
		NeedsHuman bool   `json:"needs_human"`
	} `json:"want"`
}

func TestOfCases(t *testing.T) {
	raw, err := os.ReadFile("../../tests/fixtures/issue-state/cases.json")
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
			want := Result{State: State(c.Want.State), NeedsHuman: c.Want.NeedsHuman}
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
