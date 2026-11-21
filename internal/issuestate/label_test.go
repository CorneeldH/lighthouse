package issuestate

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

func TestSetLabel(t *testing.T) {
	tests := []struct {
		name    string
		current []string
		want    string
		edit    LabelEdit
	}{
		{"empty gets the label", nil, "needs_hands", LabelEdit{Winner: "needs_hands", Add: []string{"needs_hands"}}},
		{"lower rank replaces higher", []string{"needs_testing"}, "needs_shaping",
			LabelEdit{Winner: "needs_shaping", Add: []string{"needs_shaping"}, Remove: []string{"needs_testing"}}},
		{"higher rank loses to lower", []string{"needs_shaping"}, "needs_hands", LabelEdit{Winner: "needs_shaping"}},
		{"same label is no edit", []string{"needs_hands"}, "needs_hands", LabelEdit{Winner: "needs_hands"}},
		{"removes every other blocking label in rank order, keeps the rest",
			[]string{"needs_testing", "2-area:x", "blocked_external"}, "blocked_issue",
			LabelEdit{Winner: "blocked_issue", Add: []string{"blocked_issue"}, Remove: []string{"blocked_external", "needs_testing"}}},
		{"two present, want the winner, removes the loser", []string{"needs_shaping", "needs_testing"}, "needs_shaping",
			LabelEdit{Winner: "needs_shaping", Remove: []string{"needs_testing"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SetLabel(tt.current, tt.want, keyLabels)
			if err != nil {
				t.Fatalf("SetLabel: %v", err)
			}
			if !reflect.DeepEqual(got, tt.edit) {
				t.Errorf("SetLabel = %#v, want %#v", got, tt.edit)
			}
		})
	}
}

// Two keys may share one label name: the shared label is one label, never removed as its own loser.
func TestSetLabelWithTwoKeysOnOneName(t *testing.T) {
	l := keyLabels
	l.NeedsHands, l.NeedsTesting = "help", "help"
	got, err := SetLabel([]string{"help"}, "needs_testing", l)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LabelEdit{Winner: "help"}); !reflect.DeepEqual(got, want) {
		t.Errorf("SetLabel = %#v, want %#v", got, want)
	}
}

func TestSetLabelRejectsAnUnknownKey(t *testing.T) {
	if _, err := SetLabel(nil, "needs-hands", keyLabels); err == nil {
		t.Error("SetLabel(needs-hands) gave no error")
	}
}

func TestSetLabelRejectsAKeyWithoutName(t *testing.T) {
	l := keyLabels
	l.NeedsTesting = ""
	if _, err := SetLabel(nil, "needs_testing", l); err == nil {
		t.Error("SetLabel(needs_testing) with an empty name gave no error")
	}
}

func TestName(t *testing.T) {
	if n, ok := Name("blocked_external", keyLabels); n != "blocked_external" || !ok {
		t.Errorf("Name(blocked_external) = %q, %v", n, ok)
	}
	if _, ok := Name("blocked", keyLabels); ok {
		t.Error("Name(blocked) is ok")
	}
}

// fakeForge is the forge of Apply and Return: one issue with labels and one board item. Any
// method it does not override panics through the nil embed.
type fakeForge struct {
	forge.Forge
	labels  []string
	item    forge.BoardItem
	onBoard bool
	editErr error // returned by EditIssue after it applied AddLabels
	setErr  error // returned by SetStatus

	edits     []forge.IssueEdit
	itemCalls int
	statuses  []string
}

func (f *fakeForge) Issue(_ context.Context, nr int) (forge.Issue, error) {
	if nr != 7 {
		return forge.Issue{}, errors.New("unexpected issue")
	}
	return forge.Issue{Number: 7, State: "open", Labels: slices.Clone(f.labels)}, nil
}

func (f *fakeForge) EditIssue(_ context.Context, nr int, e forge.IssueEdit) (forge.Issue, error) {
	if nr != 7 || e.Title != nil || e.Body != nil || e.Milestone != nil {
		return forge.Issue{}, errors.New("unexpected edit")
	}
	f.edits = append(f.edits, e)
	f.labels = append(f.labels, e.AddLabels...)
	if f.editErr != nil {
		return forge.Issue{}, f.editErr
	}
	f.labels = slices.DeleteFunc(f.labels, func(s string) bool { return slices.Contains(e.RemoveLabels, s) })
	return forge.Issue{Number: 7, Labels: slices.Clone(f.labels)}, nil
}

func (f *fakeForge) IssueItem(_ context.Context, nr int) (forge.BoardItem, bool, error) {
	f.itemCalls++
	if nr != 7 {
		return forge.BoardItem{}, false, errors.New("unexpected issue")
	}
	return f.item, f.onBoard, nil
}

func (f *fakeForge) SetStatus(_ context.Context, it forge.BoardItem, status string) error {
	if it.ItemID != f.item.ItemID {
		return errors.New("unexpected item")
	}
	f.statuses = append(f.statuses, status)
	return f.setErr
}

func TestApplyWithoutChangeSendsNoEdit(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_shaping"}}
	got, err := Apply(context.Background(), f, 7, "needs_hands", keyLabels)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LabelEdit{Winner: "needs_shaping"}); !reflect.DeepEqual(got, want) {
		t.Errorf("Apply = %#v, want %#v", got, want)
	}
	if len(f.edits) != 0 {
		t.Errorf("EditIssue calls = %d, want 0", len(f.edits))
	}
}

func TestApplySendsOneEditWithAddAndRemove(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_testing", "2-area:x"}}
	if _, err := Apply(context.Background(), f, 7, "needs_shaping", keyLabels); err != nil {
		t.Fatal(err)
	}
	want := []forge.IssueEdit{{AddLabels: []string{"needs_shaping"}, RemoveLabels: []string{"needs_testing"}}}
	if !reflect.DeepEqual(f.edits, want) {
		t.Errorf("edits = %#v, want %#v", f.edits, want)
	}
}

func TestApplyAnUnknownKeyCallsNoForge(t *testing.T) {
	// A nil forge panics at any call: the key must stop Apply before the forge.
	if _, err := Apply(context.Background(), nil, 7, "needs-hands", keyLabels); err == nil {
		t.Error("Apply(needs-hands) gave no error")
	}
}

// A GitHub edit adds before it removes: a failed DELETE leaves two blocking labels. A second
// Apply with the same key finishes the edit, since SetLabel works from the current labels.
func TestApplyAfterAHalfWriteFinishesTheEdit(t *testing.T) {
	f := &fakeForge{labels: []string{"needs_testing"}, editErr: errors.New("DELETE failed")}
	got, err := Apply(context.Background(), f, 7, "needs_shaping", keyLabels)
	if err == nil {
		t.Fatal("Apply gave no error")
	}
	want := LabelEdit{Winner: "needs_shaping", Add: []string{"needs_shaping"}, Remove: []string{"needs_testing"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Apply = %#v, want %#v", got, want)
	}

	f.editErr, f.edits = nil, nil
	if _, err := Apply(context.Background(), f, 7, "needs_shaping", keyLabels); err != nil {
		t.Fatal(err)
	}
	wantEdits := []forge.IssueEdit{{RemoveLabels: []string{"needs_testing"}}}
	if !reflect.DeepEqual(f.edits, wantEdits) {
		t.Errorf("second edits = %#v, want %#v", f.edits, wantEdits)
	}
}

func TestBlockingKeyRankAndEmptyNames(t *testing.T) {
	l := repoconfig.Labels{NeedsShaping: "shape", NeedsDecision: "decide", BlockedIssue: "blocked"}
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{"none", nil, ""},
		{"unrelated", []string{"x"}, ""},
		{"one", []string{"blocked"}, "blocked_issue"},
		{"rank wins over order", []string{"blocked", "decide", "shape"}, "needs_shaping"},
		{"empty never matches", []string{""}, ""},
	}
	for _, tt := range tests {
		if got := BlockingKey(tt.names, l); got != tt.want {
			t.Errorf("%s: BlockingKey(%q) = %q, want %q", tt.name, tt.names, got, tt.want)
		}
	}
}
