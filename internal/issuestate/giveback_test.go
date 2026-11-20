package issuestate

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

func TestReturn(t *testing.T) {
	tests := []struct {
		name     string
		item     forge.BoardItem
		onBoard  bool
		board    bool
		want     Returned
		statuses []string
		items    int
	}{
		{"from Controller to To do", forge.BoardItem{ItemID: "I1", Status: "Controller", StatusName: "Controller"}, true, true,
			Returned{From: "Controller", OnBoard: true}, []string{"To do"}, 1},
		{"already To do", forge.BoardItem{ItemID: "I1", Status: "To do", StatusName: "To do"}, true, true,
			Returned{From: "To do", OnBoard: true}, nil, 1},
		{"no board configured", forge.BoardItem{ItemID: "I1", Status: "Controller"}, true, false,
			Returned{}, nil, 0},
		{"not on the board", forge.BoardItem{}, false, true,
			Returned{}, nil, 1},
		{"unknown status uses the board name", forge.BoardItem{ItemID: "I1", Status: forge.UnknownStatusPrefix + "x", StatusName: "On hold"}, true, true,
			Returned{From: "On hold", OnBoard: true}, []string{"To do"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeForge{labels: []string{"needs_testing"}, item: tt.item, onBoard: tt.onBoard}
			got, err := Return(context.Background(), f, 7, "needs_hands", keyLabels, tt.board)
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Labeled = true
			tt.want.Label = LabelEdit{Winner: "needs_hands", Add: []string{"needs_hands"}, Remove: []string{"needs_testing"}}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Return = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(f.statuses, tt.statuses) {
				t.Errorf("SetStatus calls = %v, want %v", f.statuses, tt.statuses)
			}
			if f.itemCalls != tt.items {
				t.Errorf("IssueItem calls = %d, want %d", f.itemCalls, tt.items)
			}
		})
	}
}

func TestReturnStopsBeforeTheBoardOnAnEditError(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_testing"}, editErr: errors.New("boom"), onBoard: true,
		item: forge.BoardItem{ItemID: "I1", Status: "Controller"}}
	got, err := Return(context.Background(), f, 7, "needs_hands", keyLabels, true)
	if err == nil {
		t.Fatal("Return gave no error")
	}
	if got.Labeled {
		t.Error("Labeled is true after a failed label edit")
	}
	if f.itemCalls != 0 || len(f.statuses) != 0 {
		t.Errorf("IssueItem calls %d, SetStatus calls %v; want none", f.itemCalls, f.statuses)
	}
}

func TestReturnReportsTheLabelBeforeABoardError(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_testing"}, onBoard: true, setErr: errors.New("board down"),
		item: forge.BoardItem{ItemID: "I1", Status: "Controller"}}
	got, err := Return(context.Background(), f, 7, "needs_hands", keyLabels, true)
	if err == nil {
		t.Fatal("Return gave no error")
	}
	if !got.Labeled {
		t.Error("Labeled is false after a successful label edit")
	}
}
