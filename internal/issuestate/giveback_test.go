package issuestate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
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
			f := &fakeForge{labels: []string{"needs_hands"}, item: tt.item, onBoard: tt.onBoard}
			got, err := Return(context.Background(), f, 7, "needs_mind", keyLabels, tt.board)
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Labeled = true
			tt.want.Label = LabelEdit{Winner: "needs_mind", Add: []string{"needs_mind"}, Remove: []string{"needs_hands"}}
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
	f := &fakeForge{labels: []string{"needs_hands"}, editErr: errors.New("boom"), onBoard: true,
		item: forge.BoardItem{ItemID: "I1", Status: "Controller"}}
	got, err := Return(context.Background(), f, 7, "needs_mind", keyLabels, true)
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
	f := &fakeForge{labels: []string{"needs_hands"}, onBoard: true, setErr: errors.New("board down"),
		item: forge.BoardItem{ItemID: "I1", Status: "Controller"}}
	got, err := Return(context.Background(), f, 7, "needs_mind", keyLabels, true)
	if err == nil {
		t.Fatal("Return gave no error")
	}
	if !got.Labeled {
		t.Error("Labeled is false after a successful label edit")
	}
}

// closedEnv is an Env over a closed issue 7 in a repo without a board; only Issue may be read.
func closedEnv(t *testing.T, f *fakeForge) Env {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte("[forge]\nrepo = \"Owner/repo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Env{
		Config: func() (*config.Config, error) { return &config.Config{Path: path, Repo: "Owner/repo"}, nil },
		Forge:  func(context.Context) (forge.Forge, error) { return f, nil },
	}
}

// Red when GiveBack or Label stops checking the issue state before Return/Apply: the edit then lands.
func TestGiveBackRefusesClosedIssue(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_hands"}, closed: true}
	var out bytes.Buffer
	code := GiveBack(context.Background(), closedEnv(t, f), "7", "needs_mind", &out)
	if code != 1 || !strings.Contains(out.String(), "closed") {
		t.Errorf("GiveBack = %d, %q; want 1 and a line that says closed", code, out.String())
	}
	if len(f.edits) != 0 || len(f.statuses) != 0 {
		t.Errorf("edits %v, statuses %v; want none", f.edits, f.statuses)
	}
}

func TestLabelRefusesClosedIssue(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_hands"}, closed: true}
	var out bytes.Buffer
	code := Label(context.Background(), closedEnv(t, f), "7", "needs_mind", &out)
	if code != 1 || !strings.Contains(out.String(), "closed") {
		t.Errorf("Label = %d, %q; want 1 and a line that says closed", code, out.String())
	}
	if len(f.edits) != 0 {
		t.Errorf("edits %v; want none", f.edits)
	}
}

// Red when open stops mapping a *forge.TokenError to exit 2.
func TestOpenMapsATokenErrorToExit2(t *testing.T) {
	env := closedEnv(t, &fakeForge{})
	env.Forge = func(context.Context) (forge.Forge, error) {
		return nil, &forge.TokenError{Kind: "github", Vars: []string{"GH_TOKEN"}}
	}
	var out bytes.Buffer
	if code := Show(context.Background(), env, "7", &out); code != 2 || !strings.Contains(out.String(), "token") {
		t.Errorf("Show = %d, %q; want 2 and a token line", code, out.String())
	}
}
