package dispatch

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// fakeSetter keeps the arguments of every SetStatus as one line.
type fakeSetter struct{ calls []string }

func (s *fakeSetter) SetStatus(_ context.Context, projectID, itemID, fieldID, optionID string) error {
	s.calls = append(s.calls, strings.Join([]string{projectID, itemID, fieldID, optionID}, " "))
	return nil
}

var testBoard = repoconfig.Board{ProjectID: "PVT_x", StatusFieldID: "SF",
	StatusOptions: map[string]string{"Controller": "c1", "On hold": "h1"}}

func TestNewBoardMapsStatus(t *testing.T) {
	s := &fakeSetter{}
	if err := NewBoard(s, testBoard).SetStatus(context.Background(), "I7", "On hold"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if want := []string{"PVT_x I7 SF h1"}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("calls = %q, want %q", s.calls, want)
	}
}

func TestNewBoardUnknownStatus(t *testing.T) {
	s := &fakeSetter{}
	err := NewBoard(s, testBoard).SetStatus(context.Background(), "I7", "Gone")
	if err == nil || !strings.Contains(err.Error(), "Gone") {
		t.Errorf("SetStatus = %v, want an error naming Gone", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("calls = %q, want none", s.calls)
	}
}
