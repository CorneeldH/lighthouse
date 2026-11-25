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

var dashLabels = repoconfig.Labels{
	NeedsMind: "needs-mind", BlockedIssue: "blocked-issue", BlockedExternal: "blocked-external", NeedsHands: "needs-hands",
}

func TestSetLabel(t *testing.T) {
	tests := []struct {
		name    string
		current []string
		want    string
		edit    LabelEdit
	}{
		{"empty gets the label", nil, "needs_hands", LabelEdit{Add: []string{"needs_hands"}}},
		{"needs_mind keeps needs_hands", []string{"needs_hands"}, "needs_mind", LabelEdit{Add: []string{"needs_mind"}}},
		{"needs_hands keeps needs_mind", []string{"needs_mind"}, "needs_hands", LabelEdit{Add: []string{"needs_hands"}}},
		{"same label is no edit", []string{"needs_hands"}, "needs_hands", LabelEdit{}},
		{"blocked_issue removes only blocked_external, keeps the rest",
			[]string{"needs_hands", "2-area:x", "blocked_external"}, "blocked_issue",
			LabelEdit{Add: []string{"blocked_issue"}, Remove: []string{"blocked_external"}}},
		{"blocked_external loses to blocked_issue", []string{"blocked_issue"}, "blocked_external", LabelEdit{}},
		{"blocked_external is added without blocked_issue", []string{"needs_mind"}, "blocked_external",
			LabelEdit{Add: []string{"blocked_external"}}},
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
	l.NeedsMind, l.NeedsHands = "help", "help"
	got, err := SetLabel([]string{"help"}, "needs_hands", l)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LabelEdit{}); !reflect.DeepEqual(got, want) {
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
	l.NeedsHands = ""
	if _, err := SetLabel(nil, "needs_hands", l); err == nil {
		t.Error("SetLabel(needs_hands) with an empty name gave no error")
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
	closed  bool  // Issue reports state closed

	edits     []forge.IssueEdit
	itemCalls int
	statuses  []string
}

func (f *fakeForge) Issue(_ context.Context, nr int) (forge.Issue, error) {
	if nr != 7 {
		return forge.Issue{}, errors.New("unexpected issue")
	}
	state := "open"
	if f.closed {
		state = "closed"
	}
	return forge.Issue{Number: 7, State: state, Labels: slices.Clone(f.labels)}, nil
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
	f := &fakeForge{labels: []string{"needs_hands"}}
	got, err := Apply(context.Background(), f, 7, "needs_hands", keyLabels)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LabelEdit{}); !reflect.DeepEqual(got, want) {
		t.Errorf("Apply = %#v, want %#v", got, want)
	}
	if len(f.edits) != 0 {
		t.Errorf("EditIssue calls = %d, want 0", len(f.edits))
	}
}

func TestApplySendsOneEditWithAddAndRemove(t *testing.T) {
	f := &fakeForge{labels: []string{"blocked_external", "2-area:x"}}
	if _, err := Apply(context.Background(), f, 7, "blocked_issue", keyLabels); err != nil {
		t.Fatal(err)
	}
	want := []forge.IssueEdit{{AddLabels: []string{"blocked_issue"}, RemoveLabels: []string{"blocked_external"}}}
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
	f := &fakeForge{labels: []string{"blocked_external"}, editErr: errors.New("DELETE failed")}
	got, err := Apply(context.Background(), f, 7, "blocked_issue", keyLabels)
	if err == nil {
		t.Fatal("Apply gave no error")
	}
	want := LabelEdit{Add: []string{"blocked_issue"}, Remove: []string{"blocked_external"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Apply = %#v, want %#v", got, want)
	}

	f.editErr, f.edits = nil, nil
	if _, err := Apply(context.Background(), f, 7, "blocked_issue", keyLabels); err != nil {
		t.Fatal(err)
	}
	wantEdits := []forge.IssueEdit{{RemoveLabels: []string{"blocked_external"}}}
	if !reflect.DeepEqual(f.edits, wantEdits) {
		t.Errorf("second edits = %#v, want %#v", f.edits, wantEdits)
	}
}

func TestSetLabelNeedsMindKeepsBlock(t *testing.T) {
	got, err := SetLabel([]string{"blocked-issue"}, "needs_mind", dashLabels)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LabelEdit{Add: []string{"needs-mind"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("SetLabel = %#v, want %#v", got, want)
	}
}

func TestSetLabelBlockedIssueKeepsHumanLabels(t *testing.T) {
	got, err := SetLabel([]string{"needs-mind", "needs-hands", "blocked-external"}, "blocked_issue", dashLabels)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LabelEdit{Add: []string{"blocked-issue"}, Remove: []string{"blocked-external"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("SetLabel = %#v, want %#v", got, want)
	}
}

func TestSetLabelBlockedExternalLosesToIssue(t *testing.T) {
	got, err := SetLabel([]string{"blocked-issue"}, "blocked_external", dashLabels)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Add) != 0 || len(got.Remove) != 0 {
		t.Errorf("SetLabel = %#v, want an empty edit", got)
	}
}

// Review focus 4: an issue with all four labels names all four keys, in Keys order.
func TestMarksListsAll(t *testing.T) {
	names := []string{"needs-hands", "x", "blocked-external", "blocked-issue", "needs-mind"}
	want := []string{"needs_mind", "blocked_issue", "blocked_external", "needs_hands"}
	if got := Marks(names, dashLabels); !reflect.DeepEqual(got, want) {
		t.Errorf("Marks = %v, want %v", got, want)
	}
	if got := Marks([]string{"x"}, dashLabels); len(got) != 0 {
		t.Errorf("Marks(unrelated) = %v, want none", got)
	}
}
