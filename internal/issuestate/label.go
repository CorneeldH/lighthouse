package issuestate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Keys are the label keys of the state commands, in the order Marks lists them. Only
// blocked_issue outranks another key (blocked_external); the others stand apart.
var Keys = []string{"needs_mind", "blocked_issue", "blocked_external", "needs_hands"}

// Name is the label name of key in l; ok is false for an unknown key or an empty name.
func Name(key string, l repoconfig.Labels) (name string, ok bool) {
	switch key {
	case "needs_mind":
		name = l.NeedsMind
	case "blocked_issue":
		name = l.BlockedIssue
	case "blocked_external":
		name = l.BlockedExternal
	case "needs_hands":
		name = l.NeedsHands
	}
	return name, name != ""
}

// BlockingKey is the key of the blocking label with the lowest rank among names, "" when none
// is there. A label whose configured name is empty never matches.
func BlockingKey(names []string, l repoconfig.Labels) string {
	for _, k := range Keys {
		if n, ok := Name(k, l); ok && slices.Contains(names, n) {
			return k
		}
	}
	return ""
}

// keyError is the error for a key that Name does not know or that has no name.
func keyError(key string) error {
	return fmt.Errorf("unknown or empty blocking label key %q (one of %s)", key, strings.Join(Keys, ", "))
}

// Marks is the keys of the blocking and human labels on names, in Keys order. Two keys that share
// one label name both appear. A label whose configured name is empty never matches.
func Marks(names []string, l repoconfig.Labels) []string {
	var keys []string
	for _, k := range Keys {
		if n, ok := Name(k, l); ok && slices.Contains(names, n) {
			keys = append(keys, k)
		}
	}
	return keys
}

// LabelEdit is the edit of the blocking labels of an issue, in label names.
type LabelEdit struct {
	Add, Remove []string
}

// SetLabel applies the rank rule per axis. needs_mind and needs_hands only add their label and
// remove nothing. blocked_issue adds its label and removes only blocked_external; blocked_external
// adds nothing when blocked_issue is already there. Add is empty when current already has the
// label. An unknown key or a key without a name is an error.
func SetLabel(current []string, wantKey string, l repoconfig.Labels) (LabelEdit, error) {
	want, ok := Name(wantKey, l)
	if !ok {
		return LabelEdit{}, keyError(wantKey)
	}
	has := func(key string) bool {
		n, ok := Name(key, l)
		return ok && slices.Contains(current, n)
	}
	var e LabelEdit
	if wantKey == "blocked_external" && has("blocked_issue") {
		return e, nil
	}
	if !slices.Contains(current, want) {
		e.Add = []string{want}
	}
	if wantKey == "blocked_issue" {
		if n, ok := Name("blocked_external", l); ok && n != want && slices.Contains(current, n) {
			e.Remove = []string{n}
		}
	}
	return e, nil
}

// Apply reads issue nr, applies SetLabel and sends one EditIssue with Add and Remove, or none
// when both are empty. On an EditIssue error it returns the intended LabelEdit with the error;
// a second Apply with the same key finishes a half-written edit.
func Apply(ctx context.Context, f forge.Forge, nr int, wantKey string, l repoconfig.Labels) (LabelEdit, error) {
	if _, ok := Name(wantKey, l); !ok {
		return LabelEdit{}, keyError(wantKey)
	}
	is, err := f.Issue(ctx, nr)
	if err != nil {
		return LabelEdit{}, err
	}
	e, err := SetLabel(is.Labels, wantKey, l)
	if err != nil {
		return LabelEdit{}, err
	}
	if len(e.Add) == 0 && len(e.Remove) == 0 {
		return e, nil
	}
	if _, err := f.EditIssue(ctx, nr, forge.IssueEdit{AddLabels: e.Add, RemoveLabels: e.Remove}); err != nil {
		return e, err
	}
	return e, nil
}
