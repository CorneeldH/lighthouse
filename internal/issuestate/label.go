package issuestate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Keys are the blocking label keys in rank order: the lowest rank wins. This is the rank of the
// developer label taxonomy, not the order of repoconfig.Labels.Blocking.
var Keys = []string{"needs_shaping", "needs_decision", "blocked_issue", "blocked_external", "needs_hands", "needs_testing"}

// Name is the label name of key in l; ok is false for an unknown key or an empty name.
func Name(key string, l repoconfig.Labels) (name string, ok bool) {
	switch key {
	case "needs_shaping":
		name = l.NeedsShaping
	case "needs_decision":
		name = l.NeedsDecision
	case "blocked_issue":
		name = l.BlockedIssue
	case "blocked_external":
		name = l.BlockedExternal
	case "needs_hands":
		name = l.NeedsHands
	case "needs_testing":
		name = l.NeedsTesting
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

// LabelEdit is the edit of the blocking label of an issue, in label names.
type LabelEdit struct {
	Winner      string
	Add, Remove []string
}

// SetLabel applies the rank rule: the winner is the lowest rank among the blocking labels in
// current plus the one of wantKey; Add is the winner when current lacks it; Remove is every
// other blocking label in current, in rank order. An unknown key or a key without a name is an
// error.
func SetLabel(current []string, wantKey string, l repoconfig.Labels) (LabelEdit, error) {
	want, ok := Name(wantKey, l)
	if !ok {
		return LabelEdit{}, keyError(wantKey)
	}
	var present []string // blocking label names, in rank order
	for _, k := range Keys {
		n, ok := Name(k, l)
		if ok && (n == want || slices.Contains(current, n)) && !slices.Contains(present, n) {
			present = append(present, n)
		}
	}
	e := LabelEdit{Winner: present[0]}
	if !slices.Contains(current, e.Winner) {
		e.Add = []string{e.Winner}
	}
	for _, n := range present[1:] {
		if slices.Contains(current, n) {
			e.Remove = append(e.Remove, n)
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
