package dispatch

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// Classification: logic with a fake forge. These tests fail if Prepare stops setting the prio
// weight of #445 on its items, builds the blocker graph from the held items only, lets a blocker
// of another repo count, or swallows a PrioInputs error.

// prioFake is a forge whose board and priority inputs are fixed.
type prioFake struct {
	forge.Forge
	board []forge.BoardItem
	in    forge.PrioInputs
	err   error
	calls int
}

func (f *prioFake) BoardItems(context.Context) ([]forge.BoardItem, error) { return f.board, nil }

func (f *prioFake) PrioInputs(context.Context) (forge.PrioInputs, error) {
	f.calls++
	return f.in, f.err
}

// prioBoard is o/r with milestones M10 (Priority: 1, factor 2) and M20 (Priority: 2, factor 1).
// #1 (M10) waits on #2 (M20) and on other/repo#5, #2 waits on #3, and #6 (M10, Done, so not an
// item) waits on #5. Weights: #1 2; #2 1+2 = 3; #3 0+1+2*2 = 5; #5 0+2 = 2. The cross-repo
// blocker would give #5 another 2.
func prioBoard() *prioFake {
	item := func(n int, status string, blockers ...forge.Blocker) forge.BoardItem {
		return forge.BoardItem{ItemID: "I", Repo: "o/r", Issue: n, Title: "Issue", IssueState: "OPEN",
			Status: status, BlockedBy: blockers}
	}
	return &prioFake{
		board: []forge.BoardItem{
			item(1, "Controller", forge.Blocker{Repo: "o/r", Issue: 2, State: "OPEN"},
				forge.Blocker{Repo: "other/repo", Issue: 5, State: "OPEN"}),
			item(2, "Controller", forge.Blocker{Repo: "o/r", Issue: 3, State: "OPEN"}),
			item(3, "Controller"),
			item(5, "Controller"),
			item(6, "Done", forge.Blocker{Repo: "o/r", Issue: 5, State: "OPEN"}),
		},
		in: forge.PrioInputs{
			Issues: map[int]int{1: 10, 2: 20, 3: 0, 5: 0, 6: 10},
			Milestones: []forge.Milestone{
				{Number: 10, Title: "M10", State: "open", Description: "Doel.\nPriority: 1"},
				{Number: 20, Title: "M20", State: "open", Description: "Priority: 2"},
			},
		},
	}
}

func TestPrepareSetsPrioWeight(t *testing.T) {
	f := prioBoard()
	pre, err := Prepare(context.Background(), f, testRepo, nil, nil, ToDoProbe{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := map[int]int{}
	for _, it := range pre.Items {
		got[it.Issue] = it.PrioWeight
	}
	if want := map[int]int{1: 2, 2: 3, 3: 5, 5: 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("PrioWeight per item = %v, want %v", got, want)
	}
	if f.calls != 1 {
		t.Errorf("PrioInputs calls = %d, want 1", f.calls)
	}
}

func TestPreparePrioInputsErrorFails(t *testing.T) {
	f := prioBoard()
	boom := errors.New("boom")
	f.err = boom
	if _, err := Prepare(context.Background(), f, testRepo, nil, nil, ToDoProbe{}); !errors.Is(err, boom) {
		t.Errorf("Prepare with a failing PrioInputs = %v; want the error, not weight 0", err)
	}
}

func TestWeights(t *testing.T) {
	in := forge.PrioInputs{
		Issues:     map[int]int{1: 10, 2: 20, 3: 0},
		Milestones: []forge.Milestone{{Number: 10, Description: "Priority: 1"}, {Number: 20, Description: "Priority: 2"}, {Number: 30}},
	}
	got := Weights(in, map[int][]int{1: {2}, 2: {9}})
	// #2: 1 + #1's 2; #9 is not an open issue and counts for nothing.
	if want := map[int]int{1: 2, 2: 3, 3: 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Weights = %v, want %v", got, want)
	}
}

// The weight is derived from what the fingerprint already holds plus the milestones; it must not
// make a quiet tick look changed.
func TestFingerprintIgnoresPrioWeight(t *testing.T) {
	a := Pre{Items: []Item{{Issue: 1, Status: "Controller"}}}
	b := Pre{Items: []Item{{Issue: 1, Status: "Controller", PrioWeight: 7}}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("PrioWeight changed the fingerprint")
	}
}
